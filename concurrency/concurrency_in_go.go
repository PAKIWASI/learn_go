// Package concurrency
package concurrency

import (
	"fmt"
	"runtime"
	"sync"
)

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
	button := Button{Clicked: sync.NewCond(&sync.Mutex{})}

	// a convenience function that will allow us to register functions to
	// handle signals from a condition. Each handler is run on its own goroutine, and
	// subscribe will not exit until that goroutine is confirmed to be running.
	subscribe := func(c *sync.Cond, fn func()) {
		var goroutineRunning sync.WaitGroup
		goroutineRunning.Add(1)	// increment counter
		go func() {
			goroutineRunning.Done()	// decrement : goroutine confirmed to be running
			c.L.Lock()	// need to lock because c.Wait() calls unlock() on enter 
			defer c.L.Unlock()	// need to unlock at end as c.Wait() calls lock() on exit
			c.Wait()	// Here we wait to be notified that the condition has occurred. This is a blocking call and the goroutine will be suspended
			fn()		// hanler for the condition
		}()
		goroutineRunning.Wait()	// subscribe doesnot return until we are confirm that goroutine is running
	}

	var clickRegistered sync.WaitGroup
	clickRegistered.Add(3)
	subscribe(button.Clicked, func() {
		fmt.Println("Maximizing window.")
		clickRegistered.Done()
	})
	subscribe(button.Clicked, func() {
		fmt.Println("Displaying annoying dialog box!")
		clickRegistered.Done()
	})
	subscribe(button.Clicked, func() {
		fmt.Println("Mouse clicked.")
		clickRegistered.Done()
	})
	button.Clicked.Broadcast()
	clickRegistered.Wait()
}



func Run() {
	// manualSync()
	// waitGroup()
	// waitGroup2()
	// mem()
	runcond()
}
