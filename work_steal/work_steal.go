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
	// so there's no separate done-channel or monitor goroutine watching it.
	pending atomic.Int64

	// eg owns the fixed set of worker goroutines. The first error wins and cancels,
	// and eg.Wait() tells us when every worker has actually returned
	// which is the only safe moment to close results, since a worker can't be mid-send after that.
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
	poolSize, initialWorkerCap int,
	execute Task[T, R],
) *WorkerPool[T, R] {
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
		results: make(chan R),
	}
}

// Submit adds initial work to the pool. Call before Run
// Submit does not itself start any workers.
func (p *WorkerPool[T, R]) Submit(item T) {
	p.pending.Add(1)
	p.workers[0].deque.PushBottom(item)
}

// Run starts all workers and returns the results channel immediately, so
// the caller decides what to do with results. The channel closes once
// every worker has exited, either because there's no work left anywhere
// or because a task returned an error.
//
// Call Wait afterward (or concurrently, while draining results in another
// goroutine) to get the terminal error, if any.
func (p *WorkerPool[T, R]) Run() <-chan R {
	p.eg, p.ctx = errgroup.WithContext(p.ctx) // TODO: no sync.Once?

	for i := range p.workers {
		p.eg.Go(func() error {
			return p.runWorker(i)
		}) // if any error occurs in any worker, eg context is cancelled
	}

	// TODO: don't call wait here?
	go func() {
		p.eg.Wait() // discard here, Wait() below captures the real return
		close(p.results)
	}()

	return p.results
}

// Wait blocks until every worker has exited and returns the first error
// encountered (nil on normal completion). Safe to call while another
// goroutine drains the results channel returned by Run. That's the
// expected usage, since results only closes once workers have exited too.
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

// Runworksteal is a minimal usage example: a divide-and-conquer countdown.
// Each item spawns item-1 as a child and reports item as a result, bottoming
// out at 0. Real work would replace the body of execute.
func Runworksteal() {
	pool := NewWorkerPool[int, int](context.Background(), 10, 16,
		func(ctx context.Context, item int, spawn func(int)) (result *int, err error) {
			if item <= 0 {
				return nil, nil
			}
			spawn(item - 1)
			return &item, nil
		})

	pool.Submit(100)
	results := pool.Run()

	var total int
	for r := range results {
		total += r
		fmt.Println(r)
	}
	if err := pool.Wait(); err != nil {
		fmt.Errorf(err.Error())
	}
	fmt.Println("Total: ", total)
}


