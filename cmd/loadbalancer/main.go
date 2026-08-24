// Command loadbalancer runs the reverse proxy.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jerryschen31/system-design-load-balancer/internal/balancer"
	"github.com/jerryschen31/system-design-load-balancer/internal/healthcheck"
	"github.com/jerryschen31/system-design-load-balancer/internal/middleware"
	"github.com/jerryschen31/system-design-load-balancer/internal/proxy"
)

func parseBackendURL(raw string) (*url.URL, error) {
	target, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse backend URL: %w", err)
	}
	if target.Host == "" {
		return nil, fmt.Errorf("backend URL must include a host (for example http://hostname:port)")
	}
	switch strings.ToLower(target.Scheme) {
	case "http", "https":
		return target, nil
	default:
		return nil, fmt.Errorf("backend URL scheme must be http or https")
	}
}

// parseBackendURLs validates every raw backend URL and preserves the order
// they were given in -- that order is what the round-robin cycle follows.
func parseBackendURLs(raw []string) ([]*url.URL, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("at least one -backend is required")
	}
	targets := make([]*url.URL, 0, len(raw))
	for _, r := range raw {
		target, err := parseBackendURL(r)
		if err != nil {
			return nil, fmt.Errorf("invalid backend URL %q: %w", r, err)
		}
		targets = append(targets, target)
	}
	return targets, nil
}

// backendFlag collects every occurrence of a repeated command-line flag.
// flag.String and friends only keep the last value if a flag is given more
// than once; implementing flag.Value's two methods (String, Set) instead
// lets us register our own flag type with flag.Var, and the flag package
// calls Set once per occurrence on the command line -- so "-backend a
// -backend b" calls Set("a") then Set("b"), and we just append each time.
type backendFlag []string

func (b *backendFlag) String() string {
	return strings.Join(*b, ",")
}

func (b *backendFlag) Set(value string) error {
	*b = append(*b, value)
	return nil
}

// package main: in Go, every file belongs to a package (a namespace/compilation unit). A package literally named main, containing a function literally named main(), is what tells the Go compiler "this produces an executable binary," not a library. Everything under internal/ is a library package (balancer, healthcheck, etc.) — importable within this module, but internal/ is a special directory name the compiler enforces: nothing outside this module can import them at all.
func main() {
	listenAddr := flag.String("listen", ":8080", "address for the load balancer to listen on")

	// An interface in Go is a type defined purely as a set of method signatures — no fields, no implementation. The standard library's flag package defines an interface called flag.Value requiring exactly two methods: String() string and Set(string) error. Unlike languages where a type must explicitly declare implements SomeInterface, in Go, any type that happens to have methods matching an interface's signatures automatically satisfies that interface — there's no keyword linking them. Here, backendFlag (just a named slice-of-strings type) gets those two methods defined on it, so it silently becomes usable anywhere a flag.Value is expected — specifically via flag.Var(&backends, "backend", ...) down in main().
	var backends backendFlag
	flag.Var(&backends, "backend", "backend server URL to forward requests to (repeatable, e.g. -backend http://localhost:9001 -backend http://localhost:9002)")
	healthCheckInterval := flag.Duration("health-check-interval", 5*time.Second, "how often to actively poll each backend's health check path")
	healthCheckTimeout := flag.Duration("health-check-timeout", 2*time.Second, "how long a single health check probe may take before it counts as a failure")
	healthCheckPath := flag.String("health-check-path", "/health", "path to request on each backend for health checks")
	flag.Parse()

	targets, err := parseBackendURLs(backends)
	if err != nil {
		log.Fatalf("%v", err)
	}

	logger := log.New(os.Stdout, "", log.LstdFlags)

	rr := balancer.NewRoundRobin(targets)
	handler := middleware.Logging(logger)(proxy.New(rr, logger))

	// This is the core composition. proxy.New(rr, logger) returns an *httputil.ReverseProxy — a standard-library type that satisfies http.Handler (an interface requiring one method, ServeHTTP(ResponseWriter, *Request) — anything with that method can handle HTTP requests). middleware.Logging(logger) returns a function of type func(http.Handler) http.Handler — a function that takes a handler and returns a new handler wrapping it. So middleware.Logging(logger)(proxy.New(rr, logger)) is two calls chained: first build the logging wrapper (configured with logger), then immediately call it on the proxy handler. The result, handler, is: request comes in → logging middleware runs first → delegates to the reverse proxy. This is Go's idiomatic middleware pattern — no framework, just functions wrapping functions, all typed through the one http.Handler interface.
	server := &http.Server{
		Addr:    *listenAddr,
		Handler: handler,
	}

	// healthCtx controls the health checker's polling goroutines specifically. It's cancelled on the same shutdown signal as the HTTP server below, so the checker stops polling backends rather than continuing to run after the load balancer itself has stopped accepting connections.
	healthCtx, cancelHealth := context.WithCancel(context.Background())

	// defer cancelHealth(): defer schedules a function call to run when the enclosing function (main, here) returns — regardless of how it returns (normal fall-through, or via a later return). It's a safety net: if main exits some other way than reaching line 126 normally, cancelHealth still fires and no goroutine leaks polling forever. Note it's also called explicitly at line 120 during normal shutdown — the defer is the backstop, not the primary mechanism.
	defer cancelHealth()

	// rr.SetHealthy (a method value — passing rr.SetHealthy as an argument, without calling it, packages up "call this method on this particular rr" as a plain function value) is passed in as the onResult func(backend *url.URL, healthy bool) callback parameter — the health checker doesn't know or care about RoundRobin internals; it just calls this function whenever a probe succeeds or fails, and RoundRobin updates its own state. That's a deliberate decoupling: the checker's only contract with the balancer is "call me back with a URL and a bool."
	checker := healthcheck.NewChecker(targets, *healthCheckInterval, *healthCheckTimeout, *healthCheckPath, rr.SetHealthy, logger)
	checker.Start(healthCtx)

	// go func() { ... }(): launches a goroutine — a lightweight, independently-scheduled function execution managed by the Go runtime 
	// (not an OS thread directly, though the runtime multiplexes goroutines onto OS threads). go followed by a function call starts 
	//  that call running concurrently and returns immediately to the next line of main — it does not wait for the function to finish. 
	// This is necessary here because server.ListenAndServe() blocks — it runs forever, accepting connections, until the server is shut 
	// down or errors. If it ran directly in main() without go, execution would never reach the signal-handling code below.
	go func() {
		logger.Printf("load balancer listening on %s, forwarding to %s", *listenAddr, backends.String())
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatalf("server error: %v", err)
		}
	}()

	// chan os.Signal: a channel is Go's built-in typed pipe for passing values between goroutines, with the runtime handling the 
	// synchronization. make(chan os.Signal, 1) creates one with buffer capacity 1 — meaning one value can be sent into it without 
	// a receiver ready to take it immediately (an unbuffered channel, capacity 0, would block the sender until someone receives).
	// In plain language, I think this means stop is waiting for a SINGLE signal.
	// signal.Notify(stop, os.Interrupt, syscall.SIGTERM) tells the Go runtime "when the OS delivers SIGINT (Ctrl+C) or SIGTERM 
	// (the default signal kill sends) to this process, deliver it into the stop channel instead of the process's default action 
	// (which would just terminate immediately)."
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// <-stop: the receive operator. This line blocks — main's goroutine sits here doing nothing — until a value arrives on stop. 
	// This is the whole synchronization mechanism: the main goroutine is parked here while the go func(){ ... }() above independently 
	// serves requests, until an OS signal wakes it up.
	// Once unblocked (upon SIGINT Ctrl+C or SIGTERM kill signal): cancelHealth() below stops the health-check polling goroutines. 
	<-stop

	logger.Println("shutting down...")
	cancelHealth()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Printf("graceful shutdown failed: %v", err)
	}
}
