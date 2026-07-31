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
		value++
		memoryAccess.Unlock()
	}()
	memoryAccess.Lock()
	if value == 0 {
		fmt.Printf("the value is %v.\n", value)
	}
}

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
	wg.Go(func() {
		salutation = "welcome"
	})

	wg.Wait()
	fmt.Println(salutation)
}

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

func Run() {
	// manualSync()
	// waitGroup()
	// waitGroup2()
	mem()
}
