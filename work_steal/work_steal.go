// Package worksteal implements concurrent work stealing for worker pools.
//
// Work-stealing pools exist for a specific shape of problem: recursive divide-and-conquer.
// The classic example is parallel quicksort or a parallel tree walk.
// You don't know the full list of work upfront. Each piece of work, when you look at it, discovers more work.
//
// A worker pool consists of multiple worker goroutines. Each worker owns a
// lock-free deque. Workers execute their own work from the bottom of the
// deque and steal work from the top of other workers' deques when they run out of local work.
package worksteal

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync/atomic"

	"golang.org/x/sync/errgroup"
)

// TODO: later
//   - Add a per-worker next-job slot for newly spawned work.
//   - Add per-worker scheduling state for tracking idle/running workers.

// Worker owns a local work-stealing deque.
//
// The worker's normal path is to pop work from the bottom of its deque and
// push newly spawned work onto the bottom. This keeps the common path local
// to the worker.
//
// Worker does not know about other workers. The WorkerPool coordinates
// stealing between workers.
type Worker[T any] struct {
	// this has to be ptr as you can't copy the internal atomic ints
	deque *LFdeque[T]
}

func newWorker[T any](capacity int) Worker[T] {
	return Worker[T]{deque: NewLFdeque[T](capacity)}
}

// Task is the unit of work a WorkerPool executes.
//
// ctx should be checked by long-running tasks that want to be interruptible.
// spawn schedules a child item of work onto the calling worker's local deque
// it must only be called synchronously, from within this Task invocation.
// A non-nil error causes the pool to record it (first error wins) and begin shutting down all workers.
// T is the input type and R is the result type
type Task[T, R any] func(ctx context.Context, item T, spawn func(T)) (result *R, err error)

// WorkerPool manages a collection of workers and schedules work between them.
//
// The pool does not care what T represents. It only moves T between worker
// deques. execute defines how a worker executes a T.
//
// R is the result type expected from each execute call of the worker
type WorkerPool[T, R any] struct {
	workers []Worker[T]
	execute Task[T, R]

	ctx    context.Context
	cancel context.CancelFunc

	// pending counts items that have been submitted or spawned but not yet
	// finished executing. It's what tells the pool "there is no more work
	// anywhere". The task that decrements it to zero calls cancel itself,
	// so there's no separate done-channel or monitor goroutine watching it
	pending atomic.Int64

	// eg owns the fixed set of worker goroutines. The first error wins and cancels,
	// and eg.Wait() tells us when every worker has actually returned
	// which is the only safe moment to close results, since a worker can't be mid-send after that
	eg *errgroup.Group

	results chan R
}

// NewWorkerPool creates a pool of poolSize workers, each with its own deque
// of initial capacity initialWorkerCap.
//
// execute defines the work each worker performs for a given item. See Task
// for the contract around ctx, spawn, and error handling.
//
// The pool does not start running until Submit is called and workers begin
// pulling from their deques; there is no separate "Start" step, workers
// run as soon as they're constructed, watching ctx and their deques.
func NewWorkerPool[T, R any](
	ctx context.Context,
	poolSize, initialWorkerCap, resultBuffSize int,
	execute Task[T, R],
) *WorkerPool[T, R] {
	// derive a cancellable context from the user's
	ctx, cancel := context.WithCancel(ctx)

	workers := make([]Worker[T], poolSize)
	for i := range poolSize {
		workers[i] = newWorker[T](initialWorkerCap)
	}

	return &WorkerPool[T, R]{
		workers: workers,
		execute: execute,
		ctx:     ctx,
		cancel:  cancel,
		results: make(chan R, resultBuffSize),
	}
}

// Submit adds initial work to the pool. Call before Run
// Submit does not itself start any workers.
func (p *WorkerPool[T, R]) Submit(item T) {
	p.pending.Add(1)
	p.workers[0].deque.PushBottom(item)
}

// Run: Result channel generator. Starts all workers and returns the results
// channel. The channel closes once every worker has exited,
// either because there's no work left anywhere or because a task returned an error.
//
// Call Wait afterward (or concurrently, while draining results in another
// goroutine) to get the terminal error, if any.
func (p *WorkerPool[T, R]) Run() <-chan R {
	p.eg, p.ctx = errgroup.WithContext(p.ctx)

	// if any worker returns a non-nil error, errgroup cancels this new p.ctx
	// automatically and remembers that error as the one Wait() will report.
	for i := range p.workers {
		// lauch all workers as errgroup goroutines
		p.eg.Go(func() error {
			return p.runWorker(i)
		})
	}

	go func() {
		p.eg.Wait()      // blocks until all worker goroutines return
		close(p.results) // only then can we close the results channel
	}()

	return p.results
}

// Wait blocks until every worker has exited and returns the first error
// encountered (nil on normal completion). Safe to call while another
// goroutine drains the results channel returned by Run,
// since results only closes once workers have exited too.
func (p *WorkerPool[T, R]) Wait() error {
	return p.eg.Wait() // errgroup.Wait is safe to call more than once
}

func (p *WorkerPool[T, R]) runWorker(idx int) error {
	w := p.workers[idx]
	for {
		select {
		case <-p.ctx.Done():
			return nil // shutdown, not this worker's own failure
		default:
		}

		item, ok := w.deque.PopBottom()
		if !ok {
			if p.StealHalf(idx) {
				continue
			}
			continue // TODO: spin; swap for a backoff/park strategy later
		}

		if err := p.runTask(idx, item); err != nil {
			return err
		}
	}
}

func (p *WorkerPool[T, R]) runTask(idx int, item T) error {
	w := p.workers[idx]

	spawn := func(child T) {
		p.pending.Add(1) // before push: must be visible before any thief can see the child
		w.deque.PushBottom(child)
	}

	result, err := p.execute(p.ctx, item, spawn)
	if err != nil {
		return err // errgroup records it and cancels egCtx for every worker
	}
	if result != nil {
		select {
		case p.results <- *result:
		case <-p.ctx.Done():
		}
	}

	if p.pending.Add(-1) == 0 {
		p.cancel() // last piece of work finished anywhere in the pool
	}
	return nil
}

// StealHalf attempts to steal work for the given worker from a randomly chosen
// victim among the other workers in the pool. It tries up to len(workers)-1
// distinct victims before giving up.
func (p *WorkerPool[T, R]) StealHalf(thiefIdx int) (ok bool) {
	n := len(p.workers)
	if n > 1 {
		// Random start index, then scan forward so we don't retry the same
		// victim twice and don't bias toward low-index workers.
		start := rand.IntN(n)

		for i := range n {
			idx := (start + i) % n
			if idx == thiefIdx {
				continue
			}
			v, okk := p.workers[idx].deque.StealHalf()
			if okk {
				p.workers[thiefIdx].deque.PushSliceBottom(v)
				return true
			}
		}
	}
	return false
}

// chunkSize controls how far we split before testing individual numbers.
// Splitting all the way down to size 1 would flood the deques with tiny
// tasks; testing a batch per leaf amortizes the scheduling overhead.
const chunkSize = 64

// numRange is a half-open-ish inclusive range of ints to test for primality.
type numRange struct{ lo, hi int } // inclusive on both ends

func isPrime(n int) bool {
	if n < 2 {
		return false
	}
	for d := 2; d*d <= n; d++ {
		if n%d == 0 {
			return false
		}
	}
	return true
}

// RunworkstealPrimes finds every prime in [2, limit] using the work-stealing
// pool. Each leaf task independently discovers a different piece of
// the answer (a batch of primes), and the only way to get the complete
// set back is to read every value sent on the results channel.
func Runworksteal() {
	pool := NewWorkerPool[numRange, []int](context.Background(), 16, 8, 0,
		func(ctx context.Context, r numRange, spawn func(numRange)) (*[]int, error) {
			if r.hi-r.lo+1 > chunkSize {
				mid := r.lo + (r.hi-r.lo)/2
				spawn(numRange{r.lo, mid})
				spawn(numRange{mid + 1, r.hi})
				return nil, nil // this task itself found nothing directly
			}

			var found []int
			for n := r.lo; n <= r.hi; n++ {
				if isPrime(n) {
					found = append(found, n)
				}
			}
			if found == nil {
				return nil, nil // avoid sending an empty slice as a "result"
			}
			return &found, nil
		})

	pool.Submit(numRange{2, 10000000})
	resCh := pool.Run()

	// Drain concurrently with Wait — results is unbuffered (resultBuffSize
	// 0), so if we waited first, a worker blocked on `p.results <- *result`
	// would deadlock against a Wait() that's blocked on that same worker exiting.
	go func() {
		for batch := range resCh {
			fmt.Println(batch)
		}
	}()

	err := pool.Wait()
	if err != nil {
		fmt.Println(err.Error())
	}
}

