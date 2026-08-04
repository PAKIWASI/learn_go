// Package concurrency
package concurrency

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"
)

// following the book : Concurrency in Go (chap 4)


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
It makes it impossible to do the wrong thing. This is BETTER.

*/

func runlexconf() {

	// we instantiate the channel within the lexical scope of the chanOwner().
	// This limits the scope of the write aspect of the results channel to the closure defined below it.
	// It confines the write aspect of this channel to prevent other goroutines from writing to it
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

	// we receive a read-only copy of an int channel. By declaring that the only usage we require
	// is read access, we confine usage of the channel within the con sume function to only reads
	consumer := func(results <-chan int) {
		for result := range results {
			fmt.Printf("Received: %d\n", result)
		}
		fmt.Println("Done receiving!")
	}

	// we receive the read aspect of the channel and we’re able to pass it into the consumer, which can do
	// nothing but read from it. This confines the main goroutine to a read-only view of the channel
	results := chanOwner()
	consumer(results)
}

// an example of confinement that uses a data structure which is not concurrent-safe, an instance of bytes.Buffer

// because printData doesn’t close around the data slice, it cannot access it, and needs to take in a slice of byte to operate on.
// We pass in different subsets of the slice, thus constraining the goroutines we start to only the part of the slice we’re passing in.
// Because of the lexical scope, we’ve made it impossible to do the wrong thing,
// and so we don’t need to synchronize memory access or share data through communication.

func runconfunsafe() {
	printData := func(wg *sync.WaitGroup, data []byte) {
		defer wg.Done()
		var buff bytes.Buffer
		for _, b := range data {
			fmt.Fprintf(&buff, "%c", b)
		}
		fmt.Println(buff.String())
	}

	var wg sync.WaitGroup
	wg.Add(2)
	data := []byte("golang")
	go printData(&wg, data[:3]) // pass a slice containing first three bytes
	go printData(&wg, data[3:]) // pass a slice containing last three bytes
	wg.Wait()
}

/* For-Select Loop

why do you need it?

1. Sending iteration variables out on a channel
	you’ll want to convert something that can be iterated over into values on a channel.

2. Looping infinitely waiting to be stopped
	It’s very common to create goroutines that loop infinitely until they’re stopped
*/

func runforselect() {

	done := make(chan any)
	stringStream := make(chan any)

	// 1
	for _, s := range []string{"a", "b", "c"} {
		select {
		case <-done:
			return
		case stringStream <- s:
		}
	}

	// 2
	// The first variation keeps the select statement as short as possible:
	for {
		select {
		case <-done:
			return
		default:
		}
		// Do non-preemptable work
	}

	// If the done channel isn’t closed, we’ll exit the select statement and continue on
	// to the rest of our for loop’s body. The second variation embeds the work in a default clause of the select statement:
	for {
		select {
		case <-done:
			return
		default:
			// Do non-preemptable work
		}
	}
}

/* Preventing Goroutine Leaks

goroutines are not garbage collected by the runtime, so regardless of how small their memory footprint is
we don’t want to leave them lying about our process.

The goroutine has a few paths to termination:
• When it has completed its work.
• When it cannot continue its work due to an unrecoverable error.
• When it’s told to stop working.

first two are easy, third is the hard one

if you’ve begun a goroutine, it’s most likely cooperating with several other goroutines in some sort of organized fashion.
We could even represent this interconnectedness as a graph: whether or not a child goroutine should continue executing
might be predicated on knowledge of the state of many other goroutines. The parent goroutine (often the main goroutine)
with this full contextual knowledge should be able to tell its child goroutines to terminate.
*/

func rungoroutineLEAK() {
	doWork := func(strings <-chan string) <-chan any {
		completed := make(chan any)
		go func() {
			defer fmt.Println("doWork exited.")
			defer close(completed)
			for s := range strings {
				// Do something interesting
				fmt.Println(s)
			}
		}()
		return completed
	}
	// the main goroutine passes a nil channel into doWork. Therefore, the
	// strings channel will never actually gets any strings written onto it, and the goroutine
	// containing doWork will remain in memory for the lifetime of this process (we would
	// even deadlock if we joined the goroutine within doWork and the main goroutine).
	doWork(nil)
	// Perhaps more work is done here
	fmt.Println("Done.")
}

// The way to successfully mitigate this is to establish a signal between the parent goroutine and its children that
// allows the parent to signal cancellation to its children. By convention, this signal is usually a read-only channel named done.
// The parent goroutine passes this channel to the child goroutine and then closes the channel when it wants to cancel the child goroutine.

func rungoroutineFIXED() {
	doWork := func(
		done <-chan any, // by convention, channel is first param
		strings <-chan string,
	) <-chan any {
		terminated := make(chan any)
		go func() {
			defer fmt.Println("doWork exited.")
			defer close(terminated)
			for {
				select {
				case s := <-strings:
					// Do something interesting
					fmt.Println(s)
				case <-done: // if done is signaled
					return
				}
			}
		}()
		return terminated
	}

	done := make(chan any)
	terminated := doWork(done, nil)
	go func() {
		// Cancel the operation after 1 second.
		time.Sleep(1 * time.Second)
		fmt.Println("Canceling doWork goroutine...")
		close(done)
	}()
	// This is where we join the goroutine spawned from doWork with the main goroutine
	<-terminated // wait's for a value to be placed into terminated
	fmt.Println("Done.")

	// even though we passed nil, our goroutine exits
	// we do join the two goroutines, and yet do not receive a deadlock. This is because before we join the two
	// goroutines, we create a third goroutine to cancel the goroutine within doWork after a second.
}

// what if we’re dealing with the reverse situation: a goroutine blocked on attempting to write a value to a channel
func runreadblocked() {
	newRandStream := func() <-chan int {
		randStream := make(chan int)
		go func() {
			defer fmt.Println("newRandStream closure exited.") // these never runs because we never exit the loop
			defer close(randStream)                            // goroutine blocks on write until the end of the program
			for {
				randStream <- rand.Int() // this blocks until someone wants to read
			}
		}()
		return randStream
	}

	randStream := newRandStream()
	fmt.Println("3 random ints:")
	for i := range 3 { // we read 3 times, get 3 values, then goroutine blocks until end of program
		fmt.Printf("%d: %d\n", i, <-randStream)
	}
}

// We have no way of telling the producer it can stop. The solution is to provide the producer goroutine with a channel informing it to exit

func runreadblockedFIXED() {

	newRandStream := func(done <-chan any) <-chan int {
		randStream := make(chan int)
		go func() {
			defer fmt.Println("newRandStream closure exited.")
			defer close(randStream)
			for {
				select {
				case randStream <- rand.Int():
				case <-done:
					return
				}
			}
		}()
		return randStream
	}

	done := make(chan any)
	randStream := newRandStream(done)
	fmt.Println("3 random ints:")
	for i := range 3 {
		fmt.Printf("%d: %d\n", i, <-randStream)
	}
	close(done)
	// Simulate ongoing work
	time.Sleep(1 * time.Second)
}

/*


*/


func Run() {
	// runlexconf()
	// runconfunsafe()
	// rungoroutineLEAK()
	// rungoroutineFIXED()
	// runreadblocked()
	runreadblockedFIXED()
}
