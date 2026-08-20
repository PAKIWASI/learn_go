// Package worksteal
package worksteal

import "fmt"

/*

<- 1 2 3 4 5 6 7    , size=7, cap=7
   |           |
   h           t

pushTail: size < cap -> (t + 1) % cap
popTail: size > 0    -> (t - 1) % cap

	_ 2 3 4 5 6 7    , size=6, cap=7
	  |         |
	  h         t


	8 2 3 4 5 6 7,    size=7, cap=7
	| |
	t h

pushHead: size < cap -> (h - 1) % cap
popHead: size > 0    -> (h + 1) % cap

	1 2 3 4 5 6 _    , size=6, cap=7
	|         |
	h         t

	1 2 3 4 5 6 7 ,    size=7, cap=7
	          | |
			  t h

*/

// LFdeque is a lock-free, double-ended queue
// push, pop from both head and tail is amortised O(1)
// made lock-free by CAS operation on writes (push ops)
type LFdeque[T any] struct {
	arr []T
	head int
	tail int
	size int
}

func NewLFdeque[T any](cap int) *LFdeque[T] {
	return &LFdeque[T]{
		arr: make([]T, cap),
		head: -1,
		tail: -1,
		size: 0,
	}
}

func (q *LFdeque[T]) pushTail(v T) {
	c := len(q.arr)

	if q.size == c {
		q.arr = append(q.arr, v)
	}

	q.tail = (q.tail + 1) % c
	q.arr[q.tail] = v

	q.size++
}






func Rundeque() {

	q := NewLFdeque[int](10)

	fmt.Println(len(q.arr))
	fmt.Println(cap(q.arr))
	fmt.Println(q.arr)

	q.pushTail(1)
	q.pushTail(1)
	q.pushTail(1)
	q.pushTail(1)
	q.pushTail(1)
	q.pushTail(1)
	q.pushTail(1)
	q.pushTail(1)
	q.pushTail(1)
	q.pushTail(1)
	q.pushTail(1)
	q.pushTail(1)
	q.pushTail(1)
	q.pushTail(1)
	q.pushTail(1)
	q.pushTail(1)
	q.pushTail(1)
	q.pushTail(1)
	q.pushTail(1)
	q.pushTail(1)
	q.pushTail(1)

	fmt.Println(len(q.arr))
	fmt.Println(cap(q.arr))
	fmt.Println(q.arr)
}





