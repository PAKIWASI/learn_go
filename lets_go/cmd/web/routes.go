package main

import "net/http"


func (app *application) routes() *http.ServeMux {
	// ServeMux is an HTTP request multiplexer.
	// It matches the URL of each incoming request against a list of registered
	// patterns and calls the handler for the pattern that most closely matches the URL.

	// Use the http.NewServeMux() function to initialize a new servemux, then
	// register the home function as the handler for the "/" URL pattern.
	mux := http.NewServeMux()

	// Create a file server which serves files out of the "./ui/static" directory.
	// Note that the path given to the http.Dir function is relative to the project directory root.
	fileServer := http.FileServer(http.Dir("./ui/static/"))

	// Use the mux.Handle() function to register the file server as the handler for
	// all URL paths that start with "/static/". For matching paths, we strip the
	// "/static" prefix before the request reaches the file server.
	mux.Handle("GET /static/", http.StripPrefix("/static", fileServer))

	// this is "catch all". if we req a route that starts with a '/' and we have not registered, then we are served this one
	// the optional {$} at the end restricts this catch all nature and only matches / requests. all other non registered req get a 404
	mux.HandleFunc("GET /{$}", app.home)
	// if a route ending with a '/' then "catch all" with routes that start with this substring are served this one
	// wild cards with {}
	mux.HandleFunc("GET /snippet/view/{snippetID}/", app.snippetView)
	// this does not end with a '/' so if we do create/wtf then we are served the '/' one
	mux.HandleFunc("GET /snippet/create", app.snippetCreateGet)

	mux.HandleFunc("POST /snippet/create", app.snippetCreatePost)

	/* NOTES
	MOST SPECIFIC PATTERN WINS if two route patterns conflict, also applies to routes that conflict with the http method
	a pattern that does not specify a method can get matched with ANY method
	*/

	return mux
}
