// Package concurrency
package concurrency

import "fmt"

/* Confinement

When working with concurrent code, there are a few different options for safe operation. We’ve gone over two of them:
• Synchronization primitives for sharing memory (e.g., sync.Mutex)
• Synchronization via communicating (e.g., channels)

However, there are a couple of other options that are implicitly safe within multiple concurrent processes:
• Immutable data
• Data protected by confinement

Confinement is the simple yet powerful idea of ensuring information is only ever available from one concurrent process. When this is achieved,
a concurrent program is implicitly safe and no synchronization is needed. There are two kinds of confinement possible: ad hoc and lexical

Ad hoc confinement is when you achieve confinement through a convention whether it be set by the languages community,
the group you work within, or the codebase you work within

Lexical confinement involves using lexical scope to expose only the correct data and concurrency primitives for multiple concurrent processes to use.
It makes it impossible to do the wrong thing. This is better

*/

func runlexconf() {
	chanOwner := func() <-chan int {
		results := make(chan int, 5)
		go func() {
			defer close(results)
			for i := range 6 {
				results <- i
			}
		}()
		return results
	}
	consumer := func(results <-chan int) {
		for result := range results {
			fmt.Printf("Received: %d\n", result)
		}
		fmt.Println("Done receiving!")
	}
	results := chanOwner()
	consumer(results)
}

func Run() {
	runlexconf()
}


