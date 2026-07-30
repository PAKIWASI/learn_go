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
// Due to the syncronization, both workers take a turn do their computation in turns and also print in turn
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
	fmt.Println("done")
}

//	a \
//		c	-> main print
//
// b /
func fanIn(a, b <-chan string) <-chan string {
	c := make(chan string)
	go func() {
		for {
			c <- <-a // if you receive a value in a, send it in c
		}
	}()
	go func() {
		for {
			c <- <-b // if you receive a value in b, also send it to c
		}
	}()
	return c // if value arrives at a OR b, c will get it.
	// so now we channel both a and b through c so c is only waiting if both a and b are not ready
	// a and b never have to wait
}

// pattern 3:
// pattern 2 is cool but what if you want a garentee that both workers execute one after the other ie take turns

// sending a channle on a channle, making a goroutine wait it's turn
// receive all messages, then enable them again by sending on a private channel
// first we define a message type that contains a channel for the reply

// each worker owns a private waitForIt channel and hands a reference to it to the receiver bundled with its message.
// The receiver collects one message from each worker, prints both, and only then sends true back down each private channel
// that's the "send a channel on a channel" trick letting the receiver control exactly when each goroutine is allowed to proceed again.

type Message struct {
	str  string    // the message we want to print
	wait chan bool // worker blocks on wait until user says "go"
}

func boring(msg string, c chan Message) {
	waitForIt := make(chan bool)
	for i := 0; ; i++ {
		// send a message to c with a wait channel attached
		c <- Message{fmt.Sprintf("%s %d", msg, i), waitForIt}
		<-waitForIt // block here until the receiver says "go", meaning until waitforit receives a value
	}
}
func runmessage() {
	c := make(chan Message) // fan-in channel
	// these 2 workers will send messages to c
	go boring("Joe", c)
	go boring("Ann", c)

	for range 5 {
		// this lines blocks until one worker sends a value
		// whichever worker sent message first to c has his message arrived here and is blocked, waiting for a value for it's wait field
		msg1 := <-c
		fmt.Println(msg1.str)
		// the other one now sends the value to c
		msg2 := <-c
		fmt.Println(msg2.str)
		// we first send true to the first worker - meaning the worker that came first, so it waits first too
		// we don't care if msg1 was sent by the first worker or the second, we just block it as it arrived first
		msg1.wait <- true
		msg2.wait <- true
	}
	// Every round pairs exactly one Joe message with one Ann message at the same iteration count. That's the guarantee
	// not that Joe always goes first, but that both workers advance in lockstep, one increment each, every round
	fmt.Println("done")
}

func Runrobpike() {
	// channelBlocking()
	// channelGenerator()
	// fanRun()
	runmessage()

}
