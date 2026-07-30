// Package httpserver
package httpserver

import (
	"log"
	"net/http"
)

type myHandler struct {
}

func (m myHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Println("A request was received")
	log.Println(r)
	_, err := w.Write([]byte("HI"))
	if err != nil {
		log.Printf("an error occurred: %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
	}

	log.Println("A response was sent")
	log.Println(w)
}

func Run() {

	http.ListenAndServe(":6969", myHandler{})
}
