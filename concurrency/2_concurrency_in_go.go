// Package concurrency
package concurrency

import (
	"fmt"
	"log"
	"runtime"
	"sync"
)

// following the book : Concurrency in Go (til chap 3)


// we have successfully synchronized access to the memory
func manualSync() {
	var memoryAccess sync.Mutex
	var value int
	go func() {
		memoryAccess.Lock()
		defer memoryAccess.Unlock()
		value++
	}()
	memoryAccess.Lock()
	defer memoryAccess.Unlock()
	if value == 0 {
		fmt.Printf("the value is %v.\n", value)
	}
}

// wait group

func waitGroup() {
	var wg sync.WaitGroup
	sayHello := func() {
		defer wg.Done() // (semophore --)
		fmt.Println("hello")
	}
	wg.Add(1)
	go sayHello()
	wg.Wait() // wait for 1 call to wg.Done() (this is like the semophore ++)
}

func waitGroup2() {
	var wg sync.WaitGroup
	salutation := "hello"
	// wg.Add(1)
	// go func() {
	// 	defer wg.Done()
	// 	salutation = "welcome"
	// }()

	// the Go func increments wait group, runs the func provided then decrements it again
	wg.Go(func() {
		salutation = "welcome"
	})
	wg.Wait()
	fmt.Println(salutation)
}

// goroutine memory usage

func mem() {
	memConsumed := func() uint64 {
		runtime.GC()
		var s runtime.MemStats
		runtime.ReadMemStats(&s)
		return s.Sys
	}
	var c <-chan any
	var wg sync.WaitGroup
	noop := func() { wg.Done(); <-c }
	const numGoroutines = 1e4
	wg.Add(numGoroutines)
	before := memConsumed()
	for i := numGoroutines; i > 0; i-- {
		go noop()
	}
	wg.Wait()
	after := memConsumed()
	fmt.Printf("%.3fkb", float64(after-before)/numGoroutines/1000)
}

//  sync.Cond

// Button type that contains a condition `Clicked`
type Button struct {
	Clicked *sync.Cond
}

func runcond() {
	// create a button
	button := Button{Clicked: sync.NewCond(&sync.Mutex{})}

	// a convenience function that will allow us to register functions to
	// handle signals from a condition. Each handler is run on its own goroutine, and
	// subscribe will not exit until that goroutine is confirmed to be running.
	subscribe := func(c *sync.Cond, fn func()) {
		var goroutineRunning sync.WaitGroup
		goroutineRunning.Add(1) // increment counter
		go func() {
			goroutineRunning.Done() // decrement : goroutine confirmed to be running
			c.L.Lock()              // need to lock because c.Wait() calls unlock() on enter
			defer c.L.Unlock()      // need to unlock at end as c.Wait() calls lock() on exit
			c.Wait()                // Here we wait to be notified that the condition has occurred. This is a blocking call and the goroutine will be suspended
			fn()                    // hanler for the condition
		}()
		goroutineRunning.Wait() // subscribe doesnot return until we are confirm that goroutine is running
	}

	// set a handler for when the mouse button is raised. It in turn calls Broadcast on the Clicked Cond to let all handlers
	// know that the mouse button has been clicked
	var clickRegistered sync.WaitGroup
	clickRegistered.Add(3)

	subscribe(button.Clicked, func() {
		log.Println("Maximizing window.")
		clickRegistered.Done() // decrement
	})
	subscribe(button.Clicked, func() {
		log.Println("Displaying annoying dialog box!")
		clickRegistered.Done() // decrement
	})
	subscribe(button.Clicked, func() {
		log.Println("Mouse clicked.")
		clickRegistered.Done() // decrement
	})

	log.Println("broadcasting...")
	button.Clicked.Broadcast() // we notify any goroutine blocked on c.Wait() so it continues and executes fn()
	clickRegistered.Wait()
}

// sync.Once

func runonce() {
	var count int
	increment := func() {
		count++
	}
	var once sync.Once
	var increments sync.WaitGroup
	increments.Add(100)
	for range 100 {
		go func() {
			defer increments.Done()
			once.Do(increment) // increment called only once
		}()
	}
	increments.Wait()
	fmt.Printf("Count is %d\n", count)
}

// sync.Pool
// the pool pattern is a way to create and make available a fixed number, or pool, of things for use.
// It’s commonly used to constrain the creation of things that are expensive (e.g., database connections) so that only a fixed number
// of them are ever created, but an indeterminate number of operations can still request access to these things

func runpool() {
	myPool := &sync.Pool{
		New: func() any {
			fmt.Println("Creating new instance.")
			return struct{}{}
		},
	}
	// These calls will invoke the New function defined on
	// the pool since instances haven’t yet been instantiated
	myPool.Get()
	instance := myPool.Get()
	// Here we put an instance previously retrieved back in the pool. This increases the
	// available number of instances to one
	myPool.Put(instance)
	// When this call is executed, we will reuse the instance previously allocated and put
	// it back in the pool. The New function will not be invoked.
	myPool.Get()
}

// Channels

func runchannel() {
	// read only
	var receiveChan <-chan any
	// write only
	var sendChan chan<- any
	// unidirectional
	dataStream := make(chan any)
	// conversion is valid (dataSteam converted to read/write only)
	receiveChan = dataStream
	sendChan = dataStream
	fmt.Println(receiveChan, sendChan)

	// idiomatic way of making channels
	stringStream := make(chan string)
	go func() {
		stringStream <- "Hello channels!" // blocks until someone wants to read value
	}()
	fmt.Println(<-stringStream) // blocks until someone want to write a value

	// Buffered channels have a capacity N (make(chan T, N)).
	// - A send blocks only when the buffer currently holds N items (full);
	//   it unblocks as soon as ONE item is removed, not when the buffer is fully drained.
	// - A receive blocks only when the buffer currently holds 0 items (empty);
	//   it unblocks as soon as ONE item is added, not when the buffer is full.
	// An unbuffered channel is just the special case N=0 (make(chan T, 0)) : every send must
	// rendezvous directly with a matching receive, since there's no room to buffer.

	// closing a channel is like a universal sentinel that says, “Hey, upstream isn’t going to be writing any more values, do what you will.”
	valueStream := make(chan any)
	close(valueStream)

	// you can read from a closed channel
	// intStream := make(chan int)
	// close(intStream)
	// integer, ok := <-intStream	// ok will be false
	// fmt.Printf("(%v): %v\n", ok, integer)

	// ranging over a channel: The range keyword—used in conjunction with the for statement—supports channels as
	// arguments, and will automatically break the loop when a channel is closed. This allows for concise iteration over the values on a channel
	intStream := make(chan int)
	go func() {
		defer close(intStream)
		for i := 1; i <= 5; i++ {
			intStream <- i
		}
	}()
	for integer := range intStream {
		fmt.Printf("%v ", integer)
	}

}

// channel ownership

// ownership as being a goroutine that instantiates, writes, and closes a channel.
// Much like memory in languages without garbage collection, it’s important to clarify which goroutine owns a channel
// in order to reason about our programs logically. Unidirectional channel declarations are the tool that will allow us
// to distinguish between goroutines that own channels and those that only utilize them:
// channel owners have a write-access view into the channel (chan or chan<-), and channel utilizers only have a read-only
// view into the channel (<-chan). Once we make this distinction between channel owners and nonchannel owners, the results
// from the preceding table follow naturally, and we can begin to assign responsibilities to goroutines that own channels and those that do not.
// The goroutine that owns a channel should:
// 1. Instantiate the channel.
// 2. Perform writes, or pass ownership to another goroutine.
// 3. Close the channel.
// 4. Ecapsulate the previous three things in this list and expose them via a reader channel.

// as a consumer of a channel, I only have to worry about two things:
// 1. Knowing when a channel is closed
// 2. Responsibly handling blocking for any reason

func runchanowner() {
	// create a goroutine that clearly owns a channel, and a consumer that clearly handles blocking and closing of a channel
	chanOwner := func() <-chan int {
		// buffered channel. Since we know we’ll produce six results, we create a buffered channel of five
		// so that the goroutine can complete as quickly as possible
		resultStream := make(chan int, 5)
		// performs writes on resultStream. we’ve inverted how we create goroutines. It is now encapsulated within the surrounding function
		go func() {
			// ensure resultStream is closed once we’re finished with it. As the channel owner, this is our responsibility
			defer close(resultStream)
			for i := range 15 {
				resultStream <- i // write 10 ints
			}
		}()
		// since the return value is declared as a read-only channel, resultStream will implicitly be converted to read-only for consumers
		return resultStream
	}
	// Since the return value is declared as a read-only channel, resultStream will implicitly be converted to read-only for consumers
	resultStream := chanOwner()
	// As a consumer, we are only concerned with blocking and closed channels.
	for result := range resultStream {
		fmt.Println("len: ", len(resultStream))
		fmt.Printf("Received: %d\n", result)
	}
	fmt.Println("Done receiving!")

	/*
		1. Producer starts immediately in its goroutine and races ahead: resultStream <- 0, 1, 2, 3, 4 fill the buffer to capacity 5.
			It then tries resultStream <- 5 — this blocks, since the buffer is full and there's no room.
		2. Consumer's range receives — say it gets 0. The moment that receive completes, the buffer has room again (4 items in it, 1 free slot).
			This simultaneously unblocks the producer, which is sitting there waiting exactly for that.
		3. Now it's a race: does the producer manage to push 5 into that freed slot before your Println("len: ", len(resultStream)) executes?
			If yes → you see len: 5 (it got refilled). If the print wins the race → you see len: 4.

		len(ch) on a buffered channel is a live number of items currently sitting in the buffer
	*/
}

func Runconcurrency() {
	// manualSync()
	// waitGroup()
	// waitGroup2()
	// mem()
	// runcond()
	// runonce()
	// runpool()
	// runchannel()
	runchanowner()
}
