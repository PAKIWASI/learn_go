// Package worksteal implements concurrent work stealing for worker pools.
//
// A worker pool consists of multiple worker goroutines. Each worker owns a
// work-stealing deque. Workers execute their own work from the bottom of the
// deque and steal work from the top of other workers' deques when they run out of local work.
package worksteal

import "math/rand/v2"

// TODO: later
//   - Steal work in batches instead of one job at a time.
//   - Add a per-worker next-job slot for newly spawned work.
//   - Use randomized victim selection. (add NOW)
//   - Batch submissions into worker deques.
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
	deque *LFdeque[T]
}

func newWorker[T any](id int, capacity int) *Worker[T] {
	return &Worker[T]{deque: NewLFdeque[T](capacity)}
}

// WorkerPool manages a collection of workers and schedules work between them.
//
// The pool does not care what T represents. It only moves T between worker
// deques. execute defines how a worker executes a T.
type WorkerPool[T any] struct {
	// the index of the worker is it's implicit id
	workers []*Worker[T]
	execute func(T)
}

// steal attempts to steal work for the given worker from a randomly chosen
// victim among the other workers in the pool. It tries up to len(workers)-1
// distinct victims before giving up.
func (p *WorkerPool[T]) StealHalf(thiefIdx int) (ok bool) {
	n := len(p.workers)
	if n > 1 {
		// Random start index, then scan forward so we don't retry the same
		// victim twice and don't bias toward low-index workers.
		start := rand.IntN(n)

		for i := 0; i < n; i++ {
			idx := (start + i) % n
			if idx == thiefIdx {
				continue
			}
			v, ok := p.workers[i].deque.StealHalf()
			if ok {
				p.workers[thiefIdx].deque.PushSliceBottom(v)
				return true
			}
		}
	}
	return false
}



func Runworksteal() {

}
