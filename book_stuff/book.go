// Package bookstuff
package bookstuff

import (
	"fmt"
	"log"
)

func greet(ch chan string) {
	msg := <-ch
	ch <- fmt.Sprintf("Thanks for %s", msg)
	ch <- "Hello David"
}

func Run() {
	ch := make(chan string)

	go greet(ch)

	ch <- "hello"
	log.Println(<-ch)
	log.Println(<-ch)

}
