package worksteal

// deque.go
// A lock-free, growable, array-based work-stealing deque, following the Chase-Lev algorithm as refined by
// Lê, Pop, Cohen & Nardelli ("Correct and Efficient Work-Stealing for Weak Memory Models", PPoPP 2013).
//
// Contract
//   - Exactly ONE goroutine — the "owner" — may call PushBottom / PopBottom.
//   - Any number of other goroutines — "thieves" — may call Steal concurrently,
//     from any number of goroutines, at any time.

import (
	"fmt"
	"sync"
	"sync/atomic"
)

const minCap = 10

// circularArray is an immutable-after-publish ring buffer snapshot.
// Once a *circularArray is stored into LFdeque.array, its contents at
// any index a thief might read are never mutated again. A retired array is never modified again.
// Only the owner writes into it, and only at the current "bottom" slot, before
// bottom is published. This is what makes it safe for a thief to keep
// reading from an old array even after the owner has grown/shrunk and
// swapped in a new one: the thief is holding a Go reference to the old
// array, so the GC keeps it alive, and nobody is mutating it further.
type circularArray[T any] struct {
	buf []T
}

func newCircularArray[T any](capacity int) *circularArray[T] {
	if capacity < minCap {
		capacity = minCap
	}
	return &circularArray[T]{buf: make([]T, capacity)}
}

func (a *circularArray[T]) cap() int64 { return int64(len(a.buf)) }

func (a *circularArray[T]) get(i int64) T { return a.buf[i%a.cap()] }

func (a *circularArray[T]) put(i int64, v T) { a.buf[i%a.cap()] = v }

// resizeCopy builds a new array of newCap, containing the same logical
// values as a for indices [from, to). Owner-only: called before the
// new array is published via LFdeque.array.Store.
func (a *circularArray[T]) resizeCopy(newCap int, from, to int64) *circularArray[T] {
	na := newCircularArray[T](newCap)
	for i := from; i < to; i++ {
		na.put(i, a.get(i))
	}
	return na
}

// LFdeque is a lock-free double-ended queue.
//
//   - top and bottom are ever-increasing int64 counters
//     indices into the backing array are always `counter % cap`.
//   - The invarient is `top <= bottom `and the size is just `bottom - top`
//   - Because they only ever increase, there is no ABA problem on the CAS
//     below: a value top once held can never recur later.
//   - The owner works the bottom end (LIFO: PushBottom/PopBottom).
//   - Thieves work the top end (FIFO: Steal), racing each other, and resolved by CAS
//   - Thieves only race for the owner's PopBottom for the very last element,
//   - This race is resolved by a CAS by both the thief and the owner
type LFdeque[T any] struct {
	top    atomic.Int64
	bottom atomic.Int64
	array  atomic.Pointer[circularArray[T]]
}

func NewLFdeque[T any](capacity int) *LFdeque[T] {
	d := &LFdeque[T]{}
	d.array.Store(newCircularArray[T](capacity))
	return d
}

// PushBottom adds v to the bottom (owner-only).
func (d *LFdeque[T]) PushBottom(v T) {
	b := d.bottom.Load()
	t := d.top.Load()
	a := d.array.Load()

	if b-t >= a.cap() {
		// Full: grow before writing. Only the owner ever installs a new
		// array, so this Store does not race
		a = a.resizeCopy(int(a.cap())*2, t, b)
		d.array.Store(a)
	}

	// Write the value into the array BEFORE publishing the new bottom.
	// Go's atomic operations are sequentially consistent, so this Store cannot be
	// observed as reordered before the a.put above by any goroutine that later Loads bottom.
	a.put(b, v)
	d.bottom.Store(b + 1)
}

// PopBottom removes and returns the value at the bottom (owner-only).
// ok is false if the deque was empty, or if a concurrent thief won the
// race for the last remaining element.
func (d *LFdeque[T]) PopBottom() (v T, ok bool) {
	b := d.bottom.Load()
	a := d.array.Load()

	b--
	// Tentatively claim one fewer element. This immediately makes the
	// deque "look" one shorter to any thief that loads bottom after this
	// point. It's how the owner avoids a thief racing it for an element the owner
	// has already decided to take, UNLESS it's the very last element, handled below.
	d.bottom.Store(b)

	t := d.top.Load()
	size := b - t

	if size < 0 {
		// Deque was already empty before we decremented; undo.
		d.bottom.Store(t)
		var zero T
		return zero, false
	}

	v = a.get(b)

	if size > 0 {
		// Still at least one element left even AFTER our decrement, so
		// no thief could possibly be racing us for THIS slot (a thief
		// only ever targets index top, and top < our slot here).
		return v, true
	}

	// size == 0: b == t. There's now exactly one remaining logical element, and it sits at index t
	// which is precisely the index a concurrent thief's Steal() is also trying to claim
	// the arbitration has to happen on the same variable thieves arbitrate on: top.
	// Steal() claims its slot via d.top.CompareAndSwap(t, t+1). So PopBottom, 
	// to compete fairly for that same slot, has to attempt the exact same CAS.

	// top records which elements the owner itself has already consumed from that end.
	// Every code path that takes the last element has to advance it
	if !d.top.CompareAndSwap(t, t+1) {
		v = *new(T)
		ok = false
	} else {
		ok = true
	}
	// we already did d.bottom.Store(b) where b = t (the tentative decrement). So at this point, bottom == t.
	// But top is now t+1, whether because the owner's own CAS just succeeded, or because a thief's CAS beat it there.
	// Either way, the invariant top == bottom should hold at t+1.
	d.bottom.Store(t + 1)
	return v, ok
}

// Steal removes and returns the value at the top (thief-safe: any number
// of goroutines may call this concurrently, including concurrently with
// the owner's PushBottom/PopBottom).
func (d *LFdeque[T]) Steal() (v T, ok bool) {
	t := d.top.Load()
	b := d.bottom.Load()

	if b-t <= 0 {
		var zero T
		return zero, false
	}

	a := d.array.Load()
	// get the value BEFORE seeing if it's a valid steal.
	// so that if the steal is valid, you already have the value and
	// you don't potentially get another value written betweeen your CAS
	// and the a.get(). If steal is invalid, you just discard the value
	v = a.get(t)

	// if the top val incremented after we did the `t := d.top.Load()`, then
	// the value `v` at index `t` that we just got is already taken by another thief
	// or the owner's cas won it
	if !d.top.CompareAndSwap(t, t+1) {
		var zero T
		return zero, false
	}
	return v, true
}

// StealHalf steals half of the victim's deque. This is better for
// a concurrent worker pool: thieves won't just starve again after
// stealing one value. Concurrent-safe via CAS.
func (d *LFdeque[T]) StealHalf() (v []T, ok bool) {
	t := d.top.Load()
	b := d.bottom.Load()
	halfSize := (b - t) / 2

	if halfSize <= 0 {
		return nil, false
	}

	a := d.array.Load()
	v = make([]T, halfSize)
	for i := int64(0); i < halfSize; i++ {
		v[i] = a.get(t + i) // get() gets the correct element by wrapping around
	}

	if !d.top.CompareAndSwap(t, t+halfSize) {
		return nil, false
	}
	return v, true
}

// Len is an advisory size, safe to call from anywhere, but may be stale
// the instant it returns since top/bottom can move concurrently.
func (d *LFdeque[T]) Len() int64 {
	b := d.bottom.Load()
	t := d.top.Load()
	if b < t {
		return 0
	}
	return b - t
}

// Print is for debugging only. It is NOT safe to call concurrently with
// PushBottom/PopBottom/Steal from other goroutines — it takes an
// unsynchronized snapshot of top/bottom/array.
func (d *LFdeque[T]) Print() {
	t := d.top.Load()
	b := d.bottom.Load()
	a := d.array.Load()
	fmt.Print("[ ")
	for i := t; i < b; i++ {
		fmt.Printf("%v ", a.get(i))
	}
	fmt.Println("]")
}

// Rundeque shows the intended usage pattern: one owner goroutine
// pushing/popping its own end, several thief goroutines stealing
// concurrently from the other end.
func Rundeque() {
	type A struct {
		a int
		b int
	}
	var zero A

	d := NewLFdeque[A](8)

	// Owner seeds the deque.
	for range 5000 {
		d.PushBottom(zero)
	}

	var stolen int64
	var owned int64
	var wg sync.WaitGroup

	// Thieves.
	for range 4 {
		wg.Go(func() {
			for {
				t := d.top.Load()
				b := d.bottom.Load()
				_, ok := d.StealHalf()
				if !ok {
					if d.Len() <= 0 {
						return
					}
					continue // lost a race, try again
				}
				atomic.AddInt64(&stolen, b - t)
			}
		})
	}

	// Owner keeps popping its own end concurrently with the thieves.
	wg.Go(func() {
		c := 0
		for {
			_, ok := d.PopBottom()
			if !ok {
				if d.Len() <= 0 {
					return
				}
				continue
			}
			atomic.AddInt64(&owned, 1)
			c++
			if c%10 == 0 {
				d.PushBottom(zero)
			}
		}
	})

	wg.Wait()
	fmt.Printf("owner popped=%d, thieves stole=%d, total=%d\n",
		owned, stolen, owned+stolen)
}


