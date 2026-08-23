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

func main() {
	listenAddr := flag.String("listen", ":8080", "address for the load balancer to listen on")
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

	server := &http.Server{
		Addr:    *listenAddr,
		Handler: handler,
	}

	// healthCtx controls the health checker's polling goroutines
	// specifically. It's cancelled on the same shutdown signal as the
	// HTTP server below, so the checker stops polling backends rather
	// than continuing to run after the load balancer itself has stopped
	// accepting connections.
	healthCtx, cancelHealth := context.WithCancel(context.Background())
	defer cancelHealth()
	checker := healthcheck.NewChecker(targets, *healthCheckInterval, *healthCheckTimeout, *healthCheckPath, rr.SetHealthy, logger)
	checker.Start(healthCtx)

	go func() {
		logger.Printf("load balancer listening on %s, forwarding to %s", *listenAddr, backends.String())
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	logger.Println("shutting down...")
	cancelHealth()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Printf("graceful shutdown failed: %v", err)
	}
}
