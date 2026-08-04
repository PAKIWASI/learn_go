// Package concurrency
package concurrency

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strings"
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

/* The or-channel

you may find yourself wanting to combine one or more done channels into a
single done channel that closes if any of its component channels close. It is perfectly
acceptable, albeit verbose, to write a select statement that performs this coupling;
however, sometimes you can’t know the number of done channels you’re working
with at runtime. In this case, or if you just prefer a one-liner, you can combine these
channels together using the or-channel pattern.
This pattern creates a composite done channel through recursion and goroutines.

*/

func runorchannel() {

	// variable `or` of type func()
	var or func(channels ...<-chan any) <-chan any
	// `or` takes in a variadic slice of channels and returns a single channel
	or = func(channels ...<-chan any) <-chan any {
		// base cases for recursive function
		switch len(channels) {
		case 0:
			return nil
		case 1:
			return channels[0]
		}
		orDone := make(chan any)
		// we create a goroutine so that we can wait for messages on our channels without blocking
		go func() {
			defer close(orDone)
			switch len(channels) {
			// every recursive call to or will at least have two channels. As an optimization to keep the number of goroutines constrained,
			// we place a special case here for calls to or with only two channels
			case 2:
				select {
				case <-channels[0]:
				case <-channels[1]:
				}
			// recursively create an or-channel from all the channels in our slice after the third index, and then select from this
			// This recurrence relation will destructure the rest of the slice into or-channels to form a tree from which the first signal will return
			// We also pass in the orDone channel so that when goroutines up the tree exit, goroutines down the tree also exit
			default:
				select {
				case <-channels[0]:
				case <-channels[1]:
				case <-channels[2]:
				case <-or(append(channels[3:], orDone)...):
				}
			}
		}()
		return orDone
	}

	// This is a fairly concise function that enables you to combine any number of channels
	// together into a single channel that WILL CLOSE AS SOON AS ANY OF ITS COMPONENT CHANNELS
	// ARE CLOSED, OR WRITTEN TO. Let’s take a look at how we can use this function. Here’s a brief example that takes channels
	// that close after a set duration, and uses the `or` function to combine these into a single channel that closes

	// creates a channel that closes after the time specified
	sig := func(after time.Duration) <-chan any {
		c := make(chan any)
		go func() {
			defer close(c)
			time.Sleep(after)
		}()
		return c
	}
	start := time.Now()
	// wait on the retured channel
	<-or(
		// pass 5 channels as variadic args
		sig(2*time.Hour),
		sig(5*time.Minute),
		sig(1*time.Second),
		sig(1*time.Hour),
		sig(1*time.Minute),
	) // this blocks until any one of the channels closes
	fmt.Printf("done after %v", time.Since(start))

	// despite placing several channels in our call to or that take various times to
	// close, our channel that closes after one second causes the entire channel created by
	// the call to `or` to close. This is because despite its place in the tree the `or` function
	// builds it will always close first and thus the channels that depend on its closure will close as well

	// We achieve this terseness at the cost of additional goroutines—f(x)=⌊x/2⌋ where x is
	// the number of goroutines—but remember that one of Go’s strengths is the ability to quickly create, schedule, and run goroutines

	// This pattern is useful to employ at the intersection of modules in your system
	// At these intersections, you tend to have multiple conditions for canceling trees of goroutines
	// through your call stack. Using the or function, you can simply combine these together and pass it down the stack
}

/* Error Handling (for concurrent programs)

The most fundamental question when thinking about error handling is, “Who should
be responsible for handling the error?” At some point, the program needs to stop ferrying
the error up the stack and actually do something with it. What is responsible for this?

With concurrent processes, this question becomes a little more complex
Because a concurrent process is operating independently of its parent or siblings,
it can be difficult for it to reason about what the right thing to do with the error is

*/

func runerrproblem() {

	checkStatus := func(
		done <-chan any,
		urls ...string,
	) <-chan *http.Response {
		responses := make(chan *http.Response)
		go func() {
			defer close(responses)
			for _, url := range urls {
				resp, err := http.Get(url)
				// Here we see the goroutine doing its best to signal that there’s an error. What else can it do?
				// It can’t pass it back! How many errors is too many? Does it continue making requests?
				if err != nil {
					fmt.Println(err)
					continue
				}
				select {
				case <-done:
					return
				case responses <- resp:
				}
			}
		}()
		return responses
	}

	done := make(chan any)
	defer close(done)
	urls := []string{"https://www.google.com", "https://badhost"}
	for response := range checkStatus(done, urls...) {
		fmt.Printf("Response: %v\n", response.Status)
	}

	// the goroutine has been given no choice in the matter. It can’t simply
	// swallow the error, and so it does the only sensible thing: it prints the error and hopes
	// something is paying attention. Don’t put your goroutines in this awkward position
}

// separate your concerns: in general, your concurrent processes should
// send their errors to another part of your program that has complete information
// about the state of your program, and can make a more informed decision about what to do

func runerrsol() {

	// type that encompasses both the *http.Response and the error possible from an iteration of the loop within our goroutine
	// TODO: union, or std::expected, std::optional ??
	type Result struct {
		Error    error
		Response *http.Response
	}

	//  returns a channel that can be read from to retrieve results of an iteration of our loop
	checkStatus := func(done <-chan any, urls ...string) <-chan Result {
		results := make(chan Result)
		go func() {
			defer close(results)
			for _, url := range urls {
				var result Result
				resp, err := http.Get(url)
				result = Result{Error: err, Response: resp}
				select {
				case <-done:
					return
				case results <- result: // write results to our channel
				}
			}
		}()
		return results
	}

	done := make(chan any)
	defer close(done)
	urls := []string{"https://www.google.com", "https://badhost"}
	for result := range checkStatus(done, urls...) {
		if result.Error != nil {
			fmt.Printf("error: %v", result.Error)
			continue
		}
		fmt.Printf("Response: %v\n", result.Response.Status)
	}

	// we are able to deal with errors coming out of the goroutine started by checkStatus intelligently
	// and with the full context of the larger program

	// we’ve coupled the potential result with the potential error. This represents the complete set of possible outcomes created from the goroutine
	// checkStatus, and allows our main goroutine to make decisions about what to do when errors occur.
	// In broader terms, we’ve successfully separated the concerns of error handling from our producer goroutine
	// This is desirable because the goroutine that spawned the producer goroutine—in this case our main goroutine—has more
	// context about the running program, and can make more intelligent decisions about what to do with errors
}

/* Pipeline

A pipeline is just another tool you can use to form an abstraction in your system. In
particular, it is a very powerful tool to use when your program needs to process
streams, or batches of data. The word pipeline is believed to have first been used in
1856, and likely referred to a line of pipes that transported liquid from one place to
another. We borrow this term in computer science because we’re also transporting
something from one place to another: data. A pipeline is nothing more than a series
of things that take data in, perform an operation on it, and pass the data back out. We
call each of these operations a stage of the pipeline.

By using a pipeline, you separate the concerns of each stage, which provides numerous benefits.
You can modify stages independent of one another, you can mix and match how stages are combined independent of modifying the stages
you can process each stage concurrent to upstream or downstream stages, and you can fan-out, or rate-limit portions of your pipeline

*/

func runpipeline() {
	// takes a slice of integers in with a multiplier, loops through them multiplying as it goes, and returns a new transformed slice out.
	multiply := func(values []int, multiplier int) []int {
		multipliedValues := make([]int, len(values))
		for i, v := range values {
			multipliedValues[i] = v * multiplier
		}
		return multipliedValues
	}
	// creates a new slice and adds a value to each element
	add := func(values []int, additive int) []int {
		addedValues := make([]int, len(values))
		for i, v := range values {
			addedValues[i] = v + additive
		}
		return addedValues
	}

	// combine the two

	ints := []int{1, 2, 3, 4}
	// we combine add and multiply within the range clause
	for _, v := range add(multiply(ints, 2), 1) {
		fmt.Println(v)
	}

	// we constructed them to have the properties of a pipeline stage, we’re able to combine them to form a pipeline

	/*
		the properties of a pipeline stage:
		• A stage consumes and returns the same type.
		• A stage must be reified by the language so that it may be passed around. Functions in Go are reified and fit this purpose nicely

		reification means that the language exposes a concept to the developers so
		that they can work with it directly. Functions in Go are said to be reified because you can define variables that
		have a type of a function signature. This also means you can pass functions around your program

		functional programming people thinking in terms of higher order functions and monads. pipeline stages are
		very closely related to functional programming and can be considered a SUBSET OF MONADS
	*/

	// each stage is taking a slice of data and returning a slice of data. These stages are performing what we call BATCH PROCESSING
	// This just means that they operate on chunks of data all at once instead of one discrete value at a time. There is another
	// type of pipeline stage that performs STREAM PROCESSING. This means that the stage receives and emits one element at a time

	// notice that for the original data to remain unaltered, each stage has to make a new slice of
	// equal length to store the results of its calculations. That means that the memory footprint of our
	// program at any one time is double the size of the slice we send into the start of our pipeline.
	// let's convert it to stream processing:

	fmt.Println()
	xply := func(value, multiplier int) int {
		return value * multiplier
	}
	ad := func(value, additive int) int {
		return value + additive
	}
	i := []int{1, 2, 3, 4}
	for _, v := range i {
		fmt.Println(xply(ad(xply(v, 2), 1), 2))
	}

	// Each stage is receiving and emitting a discrete value, and the memory footprint of
	// our program is back down to only the size of the pipeline’s input. But we had to pull
	// the pipeline down into the body of the for loop and let the range do the heavy lifting
	// of feeding our pipeline. Not only does this limit the reuse of how we feed the pipeline,
	// but as we’ll see later in this section, it also limits our ability to scale. We have other
	// problems too. Effectively, we’re instantiating our pipeline for every iteration of the
	// loop. Though it’s cheap to make function calls, we’re making three function calls for
	// each iteration of the loop. And what about concurrency? I stated earlier that one of
	// the benefits of utilizing pipelines was the ability to process individual stages concur‐
	// rently, and I mentioned something about fan-out. Where does all that come in?
}

/* Pipelines best practices

Channels are uniquely suited to constructing pipelines in Go because they fulfill all of
our basic requirements. They can receive and emit values, they can safely be used
concurrently, they can be ranged over, and they are reified by the language.
*/

func runpipelinechannels() {

	//  They all look like they start one goroutine inside their bodies, and use the pattern we established in “Preventing Goroutine Leaks”

	// converts a discrete set of values into a stream of data on a channel
	generator := func(done <-chan any, integers ...int) <-chan int {
		intStream := make(chan int, len(integers)) // buffered channel with length of integers
		go func() {
			defer close(intStream)
			for _, i := range integers {
				select {
				case <-done:
					return
				case intStream <- i:
				}
			}
		}()
		return intStream
	}

	multiply := func(
		done <-chan any,
		intStream <-chan int,
		multiplier int,
	) <-chan int {
		multipliedStream := make(chan int)
		go func() {
			defer close(multipliedStream)
			for i := range intStream {
				select {
				case <-done:
					return
				case multipliedStream <- i * multiplier:
				}
			}
		}()
		return multipliedStream
	}

	add := func(
		done <-chan any,
		intStream <-chan int,
		additive int,
	) <-chan int {
		addedStream := make(chan int)
		go func() {
			defer close(addedStream)
			for i := range intStream {
				select {
				case <-done:
					return
				case addedStream <- i + additive:
				}
			}
		}()
		return addedStream
	}

	done := make(chan any)
	defer close(done) // ensure our program exits cleanly with the `done` pattern
	intStream := generator(done, 1, 2, 3, 4)
	// pipeline is a channel
	// each stage we can safely execute concurrently because our inputs and outputs are safe in concurrent contexts
	// each stage of the pipeline is executing concurrently. This means that any stage only need wait for its inputs
	// and to be able to send its outputs
	pipeline := multiply(done, add(done, multiply(done, intStream, 2), 1), 2)
	// range over a channel
	for v := range pipeline {
		fmt.Println(v)
	}

	/*
		The stages are interconnected in two ways: by the common done channel, and by the
		channels that are passed into subsequent stages of the pipeline. In other words, the
		channel created by the multiply function is passed into the add function, and so forth

		closing the done channel cascades through the pipeline This is made possible by two things in each stage of the pipeline:
		• Ranging over the incoming channel. When the incoming channel is closed, the range will exit.
		• The send sharing a select statement with the done channel.

		There is a recurrence relation at play here. At the beginning of the pipeline, we’ve
		established that we must convert discrete values into a channel. There are two points in this process that must be preemptable:
		• Creation of the discrete value that is not nearly instantaneous.
		• Sending of the discrete value on its channel.

		The first is up to you. In our example, in the generator function, the discrete values
		are generated by ranging over the variadic slice, which is instantaneous enough that it
		doesn’t need to be preemptable. The second is handled via our select statement and
		done channel, which ensures that generator is preemptable even if it is blocked
		attempting to write to intStream.

		On the other end of the pipeline, the final stage is ensured preemptability by induc‐ tion.
		It is preemptable because the channel we’re ranging over will be closed when
		preempted, and therefore our range will break when this occurs. The final stage is
		preemptable because the stream we rely on is preemptable.

		In between the beginning of the pipeline and the end of the pipeline, the code is
		always ranging over a channel and sending on another channel within a select statement containing a done channel.

		If a stage is blocked on retrieving a value from the incoming channel, it will become
		unblocked when that channel is closed. We know by induction that the channel will
		be closed because it is either a stage written like the stage we are within, or the begin‐
		ning of the pipeline that we have established is preemptable. If a stage is blocked on
		sending a value, it is preemptable thanks to the select statement.
		Thus, our entire pipeline is always preemptable by closing the done channel.
	*/
}

/* Some handy generators

a generator for a pipeline is any function that converts a set of discrete values into a stream of values on a channel.

*/

func runrepeat() {

	// repeat the values you pass to it infinitely until you tell it to stop
	repeat := func(
		done <-chan any,
		values ...any,
	) <-chan any {
		valueStream := make(chan any)
		go func() {
			defer close(valueStream)
			for {
				for _, v := range values {
					select {
					case <-done:
						return
					case valueStream <- v:
					}
				}
			}
		}()
		return valueStream
	}

	// This pipeline stage will only take the first num items off of its incoming valueStream and then exit
	take := func(
		done <-chan any,
		valueStream <-chan any,
		num int,
	) <-chan any {
		takeStream := make(chan any)
		go func() {
			defer close(takeStream)
			for range num {
				select {
				case <-done:
					return
				case takeStream <- <-valueStream:
				}
			}
		}()
		return takeStream
	}

	// use them together
	done := make(chan any)
	defer close(done)
	for num := range take(done, repeat(done, 1), 10) {
		fmt.Printf("%v ", num)
	}
}

func runrepeatfunc() {
	// let’s create another repeating generator, but this time, let’s create one that repeatedly calls a function
	repeatFn := func(
		done <-chan any,
		fn func() any,
	) <-chan any {
		valueStream := make(chan any)
		go func() {
			defer close(valueStream)
			for {
				select {
				case <-done:
					return
				case valueStream <- fn():
				}
			}
		}()
		return valueStream
	}

	// This pipeline stage will only take the first num items off of its incoming valueStream and then exit
	take := func(
		done <-chan any,
		valueStream <-chan any,
		num int,
	) <-chan any {
		takeStream := make(chan any)
		go func() {
			defer close(takeStream)
			for range num {
				select {
				case <-done:
					return
				case takeStream <- <-valueStream:
				}
			}
		}()
		return takeStream
	}

	done := make(chan any)
	defer close(done)
	// this rand function is the repeater
	rand := func() any { return rand.Int() }
	for num := range take(done, repeatFn(done, rand), 10) {
		fmt.Println(num)
	}

}

func runtypeassert() {

	// repeat the values you pass to it infinitely until you tell it to stop
	repeat := func(
		done <-chan any,
		values ...any,
	) <-chan any {
		valueStream := make(chan any)
		go func() {
			defer close(valueStream)
			for {
				for _, v := range values {
					select {
					case <-done:
						return
					case valueStream <- v:
					}
				}
			}
		}()
		return valueStream
	}

	// This pipeline stage will only take the first num items off of its incoming valueStream and then exit
	take := func(
		done <-chan any,
		valueStream <-chan any,
		num int,
	) <-chan any {
		takeStream := make(chan any)
		go func() {
			defer close(takeStream)
			for range num {
				select {
				case <-done:
					return
				case takeStream <- <-valueStream:
				}
			}
		}()
		return takeStream
	}

	// it is kinda better to use `any` for types in channel pipelines (but it's kinda taboo)
	// here it's ok as we are concerned to get data into a stream, take some of it etc. types don't matter
	// for type assertions, add another stage in the pipeline

	toString := func(
		done <-chan any,
		valueStream <-chan any,
	) <-chan string {
		stringStream := make(chan string)
		go func() {
			defer close(stringStream)
			for v := range valueStream {
				select {
				case <-done:
					return
				case stringStream <- v.(string):
				}
			}
		}()
		return stringStream
	}

	done := make(chan any)
	defer close(done)
	var message strings.Builder
	for token := range toString(done, take(done, repeat(done, "I", "am."), 5)) {
		message.WriteString(token)
	}
	fmt.Printf("message: %s...", message.String())
}

/* Fan-In, Fan-Out

Fan-out is a term to describe the process of starting multiple goroutines to handle input from the pipeline
Fan-in is a term to describe the process of combining multiple results into one channel
(multiplexing or joining together multiple streams of data into a single stream)

used when some stages of a pipeline are computationally expensive. So you get multiple goroutines processing the same chunk of data

*/

/*
We’re generating a stream of random numbers, capped at 50,000,000, converting the
stream into an integer stream, and then passing that into our primeFinder stage. pri
meFinder naively begins to attempt to divide the number provided on the input
stream by every number below it. If it’s unsuccessful, it passes the value on to the next
stage. Certainly, this is a horrible way to try and find prime numbers, but it fulfills our
requirement of taking a long time

func runprimeslow() {
	rand := func() interface{} { return rand.IntN(50000000) }
	done := make(chan interface{})
	defer close(done)
	start := time.Now()
	randIntStream := toInt(done, repeatFn(done, rand))
	fmt.Println("Primes:")
	for prime := range take(done, primeFinder(done, randIntStream), 10) {
		fmt.Printf("\t%d\n", prime)
	}
	fmt.Printf("Search took: %v", time.Since(start))
}


we only have two stages: random number generation and prime sieving. In a larger program, your pipeline might be composed of
many more stages; how do we know which one to fan out? Remember our criteria
from earlier: order-independence and duration. Our random integer generator is certainly order-independent,
but it doesn’t take a particularly long time to run. The primeFinder stage is also order-independent—numbers are either prime or not—and
because of our naive algorithm, it certainly takes a long time to run. It looks like a good candidate for fanning out


func runprimesfanout() {
	primeStream := primeFinder(done, randIntStream)
	numFinders := runtime.NumCPU()
	finders := make([]<-chan int, numFinders)
	for i := range numFinders {
		finders[i] = primeFinder(done, randIntStream)
	}
}

Now for fan in:

*/

func runfanin() {
	fanIn := func(
		done <-chan any,
		channels ...<-chan any,
	) <-chan any {
		var wg sync.WaitGroup
		multiplexedStream := make(chan any)
		// when passed a channel, will read from the channel, and pass the value read onto the multiplexedStream channel
		multiplex := func(c <-chan any) {
			defer wg.Done()
			for i := range c {
				select {
				case <-done:
					return
				case multiplexedStream <- i:
				}
			}
		}
		// Select from all the channels
		wg.Add(len(channels))
		for _, c := range channels {
			go multiplex(c)
		}
		// Wait for all the reads to complete
		go func() {
			wg.Wait()
			close(multiplexedStream)
		}()
		return multiplexedStream
	}
	fanIn(make(chan any))

	/*
		fanning in involves creating the multiplexed channel consumers will
		read from, and then spinning up one goroutine for each incoming channel, and one
		goroutine to close the multiplexed channel when the incoming channels have all been
		closed. Since we’re going to be creating a goroutine that is waiting on N other gorou‐
		tines to complete, it makes sense to create a sync.WaitGroup to coordinate things.
		The multiplex function also notifies the WaitGroup that it’s done.
	*/

}

/* complete prime fan-in/fan-out

func runfancomp() {
	done := make(chan interface{})
	defer close(done)

	start := time.Now()
	rand := func() interface{} { return rand.Intn(50000000) }
	randIntStream := toInt(done, repeatFn(done, rand))
	numFinders := runtime.NumCPU()

	fmt.Printf("Spinning up %d prime finders.\n", numFinders)

	finders := make([]<-chan interface{}, numFinders)

	fmt.Println("Primes:")
	for i := 0; i < numFinders; i++ {
		finders[i] = primeFinder(done, randIntStream)
	}

	for prime := range take(done, fanIn(done, finders...), 10) {
		fmt.Printf("\t%d\n", prime)
	}

	fmt.Printf("Search took: %v", time.Since(start))
}
*/

/* Or done

you don’t know if the fact that your goroutine was canceled means the channel you’re
reading from will have been canceled. For this reason, as we laid out in “Preventing
Goroutine Leaks” on page 90, we need to wrap our read from the channel with a
select statement that also selects from a done channel

*/

func Run() {
	// runlexconf()
	// runconfunsafe()
	// rungoroutineLEAK()
	// rungoroutineFIXED()
	// runreadblocked()
	// runreadblockedFIXED()
	// runorchannel()
	// runerrproblem()
	// runpipeline()
	// runpipelinechannels()
	// runrepeat()
	// runrepeatfunc()
	runtypeassert()
}
