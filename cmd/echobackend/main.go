// Command echobackend is a minimal stand-in backend for manually testing
// the load balancer. It does nothing but report its own listen address in
// every response body, so a request routed through the load balancer's
// round-robin (or, later, any other selection policy) can be traced back
// to the specific backend that handled it just by reading the response --
// no log-watching across terminals required.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
)

// newHandler builds the handler this backend serves every request with.
// addr is baked in at construction rather than read from the request,
// since what we want to prove (in the round-robin/hey simulations) is
// "which backend process answered this," and a backend can't discover
// its own bind address from an incoming request.
func newHandler(logger *log.Logger, addr string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger.Printf("%s %s", r.Method, r.URL.Path)
		fmt.Fprintf(w, "echobackend %s handled %s %s\n", addr, r.Method, r.URL.Path)
	}
}

func main() {
	listenAddr := flag.String("listen", ":9001", "address for this backend to listen on")
	flag.Parse()

	logger := log.New(os.Stdout, "", log.LstdFlags)
	logger.Printf("echobackend listening on %s", *listenAddr)

	if err := http.ListenAndServe(*listenAddr, newHandler(logger, *listenAddr)); err != nil {
		logger.Fatalf("server error: %v", err)
	}
}
