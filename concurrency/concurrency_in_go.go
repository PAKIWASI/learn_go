// Package concurrency
package concurrency

import (
	"fmt"
	"sync"
)


// we have successfully synchronized access to the memory
func manual_sync() {
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


func Run() {
	manual_sync()
}
