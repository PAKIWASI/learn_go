# worksteal

A lock-free work-stealing scheduler for recursive divide-and-conquer workloads in Go.

- `deque.go` — `LFdeque[T]`, a growable, array-based Chase-Lev work-stealing
  deque (Lê, Pop, Cohen & Nardelli, PPoPP 2013). One owner goroutine pushes
  and pops from the bottom; any number of thief goroutines steal from the top.
- `work_pool.go` — `WorkerPool[T, R]`, a pool of workers, each owning one
  `LFdeque[T]`. Workers execute local work LIFO and steal from other workers'
  deques FIFO (in half-batches) when they run dry.
- `primecount.go` — a divide-and-conquer prime-counting workload used to
  exercise the pool end-to-end, and as the correctness/benchmark harness for
  the rest of the package.

## Usage

```go
pool := worksteal.NewWorkerPool[T, R](ctx, poolSize, initialCap, resultBuf, task)
pool.Submit(initialItem)
for r := range pool.Run() {
    // consume results as they arrive
}
if err := pool.Wait(); err != nil {
    // first error from any worker
}
```

A `Task[T, R]` either returns a result (leaf) or calls `spawn` to schedule
more work of the same type onto the calling worker's own deque (internal
node). See `primecount.go` for a worked example.

## Known limitation: benign data race under `-race`

Running the test suite with `-race` will occasionally (not every run — it's
timing-dependent) report a data race between `LFdeque.PushBottom`
(`deque.go`, the `a.put` write) and `LFdeque.Steal` / `StealHalf`
(`deque.go`, the `a.get` read). This is **expected** and does not indicate
an incorrect result. See `TestCountPrimesParallel_MatchesSequential` and
`TestCountPrimesParallel_Repeated` in `primecount_test.go`, which are the
tests most likely to reproduce it (they drive real concurrent
push/steal traffic through `WorkerPool` on a struct-typed `T`).

### Why it happens

A thief calls `Steal()`, which reads the value at its snapshot of `top`
*before* attempting `CompareAndSwap(top, top+1)` — this is intentional,
matching the Chase-Lev paper: grab the value first, then verify you won the
claim, and discard the value if you lost.

If a thief stalls (goroutine preemption, cache miss, scheduling noise)
between reading `top` and actually reading the array slot, the owner can
keep running in the meantime: it pushes new items, `top` advances further
(via another thief's successful steal), and the owner's `PushBottom` sees
that `top` has moved past the stalled thief's slot and reuses that slot for
a brand-new, unrelated element. When the stalled thief finally reads the
array, it can land on a slot the owner is concurrently overwriting.

### Why it's safe anyway

The owner only ever reclaims a slot after observing that `top` has already
moved past it. That means the stalled thief's later
`CompareAndSwap(top, top+1)` is guaranteed to fail (`top` is no longer what
the thief read), so the racy read is always discarded via the `ok == false`
path and never returned to a caller. This is exactly the "read before CAS"
pattern the original paper relies on, proven safe under a memory model
where the array itself uses atomic (or fenced) element access.

### What isn't fully guaranteed

`circularArray.get` / `circularArray.put` use plain, non-atomic slice
element access rather than atomic loads/stores. Under Go's memory model,
an unsynchronized concurrent write and read of the same location is
technically undefined — for a multi-word `T` (e.g. `primeRange{Lo, Hi int}`)
a stalled thief could in principle observe a torn value (part old, part
new) before the CAS discards it. In practice this doesn't threaten
correctness here, because the value is always thrown away when this
happens, but it's the reason `-race` flags it and the reason this isn't
just silenced as a false positive.

### Fixing it properly (not done here, on purpose)

- **Box elements**: store `atomic.Pointer[T]` per slot instead of `T`
  directly, so `get`/`put` become real atomic operations. Removes the race
  entirely; costs one heap allocation per pushed element.
- **Epoch/hazard-pointer reclamation**: thieves announce which slot/array
  generation they're touching before reading it; the owner defers reusing a
  slot until no thief has it announced. This is what production
  work-stealing deques (e.g. Rust's `crossbeam-deque`, Java's
  `ForkJoinPool`) do. Removes the race without per-element boxing, at the
  cost of meaningfully more machinery.

Neither is implemented here: the allocation cost of boxing defeats a chunk
of the point of this package, and hazard pointers are a lot of complexity
for a learning/benchmarking deque whose results are already provably
correct. If you're adapting this code for production use, pick one of the
above.

### Practical guidance

- `go test ./...` (no `-race`) is unaffected and is the normal way to run
  the suite.
- `go test -race ./...` may intermittently report this specific race
  (`PushBottom` write vs `Steal`/`StealHalf` read in `deque.go`) on the
  `TestCountPrimesParallel_*` tests. That failure is expected and does not
  indicate a regression — it's about the memory model, not the result. Any
  *other* race, or any test asserting a wrong count/value, is a real bug.
