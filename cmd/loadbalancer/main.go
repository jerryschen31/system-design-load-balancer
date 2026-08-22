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

func main() {
	listenAddr := flag.String("listen", ":8080", "address for the load balancer to listen on")
	backendAddr := flag.String("backend", "http://localhost:9000", "backend server URL to forward requests to")
	flag.Parse()

	target, err := parseBackendURL(*backendAddr)
	if err != nil {
		log.Fatalf("invalid backend URL %q: %v", *backendAddr, err)
	}

	logger := log.New(os.Stdout, "", log.LstdFlags)

	handler := middleware.Logging(logger)(proxy.New(target, logger))

	server := &http.Server{
		Addr:    *listenAddr,
		Handler: handler,
	}

	go func() {
		logger.Printf("load balancer listening on %s, forwarding to %s", *listenAddr, target.String())
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	logger.Println("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Printf("graceful shutdown failed: %v", err)
	}
}
