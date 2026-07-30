// Package concurrency
package concurrency

import (
	"fmt"
)

/* Rob Pike Google I/0 2012

1. Goroutines are light weight threads that are managed by the go runtime
2. The runtime multiplexes all goroutines onto os level threads. Everyone get's a turn on the cpu
3. When you launch a goroute with `go func()...`. It goes off into the background and the program continues from the next line
4. So we have to mainly deal with syncronization issues and race conditions when working with goroutines
5. Channels are used to send data to and from a goroutine. It's a type so `chan int` means "channel of int type". you use `make(chan ..)`
6. You send a value to a channel using <- like: `ch <- 1`. This sends the value `1` into the channel `ch`
7. You receive a value from a channel with `x := <- ch`. This takes the arrived value at `ch` and assignes it to x
8. Both sending to and receiving from a channel is a BLOCKING operation. When you send a value to a channel, you block until someone picks up the value
and if you are receiving from a channel, you block until someone sends a value
9. A sender and receiver must both be ready to play their part in the communication, otherwise we wait until they are
So channels do both: COMMUNICATION and SYNCRONIZATION in a single operation
10. The syncronization part is true for unbuffered channels. Buffering removes syncronization. (will discuss later)


*/

// eg 1
func channelBlocking() {
	ch := make(chan string) // unbuffered channel
	go borin("hello", ch)   // lauch a function with the challen
	for range 5 {
		fmt.Println(<-ch) // each iteration blocks for a value to arrive into ch
	}
	// runs 5 times, blocks 5 times, get's a value, prints, then loop exits

	fmt.Println("im done")
}
func borin(msg string, c chan string) {
	for i := 0; ; i++ { // runs forever
		c <- fmt.Sprintf("i got : %s %d", msg, i) // sends this string to a channel
		fmt.Println("sent")
		// blocks until someone receives it
	}
}

// pattern 1
// gen() starts computation concurrently and returns a channel
// so we can communicate with the goroutine (the worker)
// Due to the syncronization, both workers take a turn do their computation in turns
func channelGenerator() {
	c := gen("hello")
	d := gen("wtf")
	for range 5 {
		fmt.Println("you said: ", <-c)
		fmt.Println("you said: ", <-d)
	}

	fmt.Println("im done")
}
func gen(msg string) <-chan string { // returns a receive-only channel of type string
	c := make(chan string)
	go func() {
		for i := 0; ; i++ {
			c <- fmt.Sprintf("%s %d", msg, i)
		}
	}()
	return c
}

// pattern 2 : multiplexing
// what if one you launch 2 worker goroutines but what if one is ready to send a value but the other is blocked, so it has to wait too
// not desired if one worker might send more data more frequently than the other
// we fix this my making a fan-in function (a multiplexer)
// instead of blocking, now we let any worker (who is ready) send to a channel
func fanRun() {
	c := fanIn(gen("hello"), gen("wtf"))
	for range 10 {
		fmt.Println(<-c)
	}
}
func fanIn(a, b <-chan string) <-chan string {
	c := make(chan string)
	go func() {
		for {
			c <- <-a	// if you receive a value in a, send it in c
		}
	}()
	go func() {
		for {
			c <- <-b	// if you receive a value in b, also send it to c
		}
	}()
	return c	// if value arrives at a OR b, c will get it.
	// so now we channel both a and b through c so c is only waiting if both a and b are not ready
	// a and b never have to wait
}

func Runrobpike() {
	// channelBlocking()
	channelGenerator()
}
