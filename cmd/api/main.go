// =============================================================================
// cmd/api/main.go — Program Entry Point
// =============================================================================
//
// WHAT DOES THIS FILE DO?
//   1. Loads configuration (from environment variables).
//   2. Connects to the database (Postgres) and Redis.
//   3. Builds the "shortener" service and the HTTP server.
//   4. Starts the HTTP server so it can accept requests.
//   5. When you press Ctrl+C (or the process gets a stop signal), it shuts
//      down the server gracefully (finishes current requests, then exits).
//
// GO CONCEPT: The main package
//   In Go, a program that you run (an executable) must have:
//   - A package named "main"
//   - A function named "main()" with no arguments and no return value.
//   When you run "go run ./cmd/api" or "go build", Go looks for this main
//   function and starts your program there.
//
// GO CONCEPT: Imports
//   The "import" block lists all the packages this file uses. There are two
//   groups: standard library ("context", "log", "net/http", etc.) and our
//   own packages ("urlshortener/internal/...") plus third-party ("golang.org/x/sync/errgroup").
//   In Go, you must use every imported package; unused imports cause a compile error.
//
// =============================================================================

package main

import (
	"context"  // Used to pass cancellation and timeouts (e.g. "stop everything")
	"log"      // Simple logging (writes to stdout/stderr)
	"net/http" // HTTP server and client types
	"os"       // Access to environment variables, exit codes, stdin/stdout
	"os/signal" // Listen for OS signals like Ctrl+C (SIGINT) or kill (SIGTERM)
	"syscall"  // Constants for signals (SIGINT, SIGTERM)
	"time"     // Durations and timeouts (e.g. 5 * time.Second)

	"urlshortener/internal/cache"    // Redis cache implementation
	"urlshortener/internal/config"   // Load settings from environment
	"urlshortener/internal/httpserver" // HTTP routes and handlers
	"urlshortener/internal/shortener" // Business logic: shorten, resolve, track clicks
	"urlshortener/internal/storage"   // Postgres: save URLs and clicks

	"golang.org/x/sync/errgroup" // Run several goroutines and wait for all; if one errors, cancel the rest
)

// main is where the program starts. It has no parameters and does not return anything.
// If something goes wrong, we log it and call os.Exit(1) to tell the OS the program failed.
func main() {
	// --- Step 1: Load configuration ---
	// config.Load() reads environment variables (or uses defaults) and returns a Config struct.
	// Example: HTTP_ADDR=:8080 means "listen on port 8080". See internal/config for all options.
	applicationConfig := config.Load()

	// Create a logger that prefixes every line with "[urlshortener] " and includes time + file/line.
	// os.Stdout = where to write. log.LstdFlags = date+time. log.Lshortfile = file name and line number.
	logger := log.New(os.Stdout, "[urlshortener] ", log.LstdFlags|log.Lmicroseconds|log.Lshortfile)

	// --- Step 2: Set up "graceful shutdown" ---
	// GO CONCEPT: context.Context
	//   Context is used to signal "cancel" or "timeout" across many functions. Here we create a
	//   context that gets cancelled when the user presses Ctrl+C (SIGINT) or the process
	//   receives SIGTERM (e.g. in Docker/Kubernetes). Any code that receives this context
	//   can check ctx.Done() and stop working.
	//
	// signal.NotifyContext returns (ctx, stopFunction). We call defer stopSignalHandler() so that
	// when main exits, we stop listening for signals (cleanup).
	shutdownContext, stopSignalHandler := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignalHandler()

	// --- Step 3: Connect to Postgres ---
	// A "connection pool" keeps a set of open connections to the database so we don't open
	// a new connection for every request. applicationConfig.PostgresDSN is the connection
	// string (host, port, user, password, database name).
	//
	// GO CONCEPT: Error handling
	//   In Go, functions often return (result, error). We check "if err != nil" and handle the
	//   error (here we log and exit). defer databaseConnectionPool.Close() means "when main
	//   exits, close the pool" so we don't leak connections.
	databaseConnectionPool, err := storage.NewPostgresPool(shutdownContext, applicationConfig.PostgresDSN)
	if err != nil {
		logger.Fatalf("failed to init postgres: %v", err)
	}
	defer databaseConnectionPool.Close()

	// --- Step 4: Connect to Redis ---
	// Redis is used as a cache: we store "shortCode -> originalURL" so we don't have to hit
	// the database on every redirect. applicationConfig.RedisAddr is usually "localhost:6379".
	redisClient := cache.NewRedisClient(applicationConfig.RedisAddr, applicationConfig.RedisPassword, applicationConfig.RedisDB)
	defer func() { _ = redisClient.Close() }()

	// --- Step 5: Build the "layers" of the app ---
	// urlRepository: knows how to save and load URLs and clicks from Postgres.
	// urlCache: knows how to get/set/invalidate shortCode->URL in Redis.
	// shortenerService: contains the business logic; it uses the repository and cache.
	//   We pass options: default expiry time and the logger. This is called "dependency injection":
	//   we give the service everything it needs from the outside.
	urlRepository := storage.NewPostgresRepository(databaseConnectionPool)
	urlCache := cache.NewRedisCache(redisClient, applicationConfig.CacheTTL)

	shortenerService := shortener.NewService(
		urlRepository,
		urlCache,
		shortener.WithDefaultExpiry(applicationConfig.DefaultURLExpiry),
		shortener.WithLogger(logger),
	)

	// --- Step 6: Build the HTTP server ---
	// httpserver.NewServer creates a router (chi) and registers all routes (e.g. POST /api/v1/shorten).
	// The server needs the config, logger, and shortener service so handlers can call them.
	httpHandler := httpserver.NewServer(applicationConfig, logger, shortenerService)

	// http.Server is from the standard library. We set:
	//   Addr:         where to listen (e.g. ":8080")
	//   Handler:      the chi router that dispatches requests to our handlers
	//   ReadTimeout:  max time to read the request body (protects against slow clients)
	//   WriteTimeout: max time to write the response
	//   IdleTimeout:  max time to keep an idle connection open
	// GO CONCEPT: Struct literals
	//   &http.Server{ ... } creates a pointer to a new Server struct with the fields set.
	httpServer := &http.Server{
		Addr:         applicationConfig.HTTPAddr,
		Handler:      httpHandler.Router(),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// --- Step 7: Run the server and graceful shutdown in parallel ---
	// GO CONCEPT: errgroup
	//   We want to do two things at once: (1) run the HTTP server, (2) wait for the shutdown
	//   signal and then stop the server. errgroup.WithContext gives us a group that runs
	//   multiple goroutines; when the context is cancelled (e.g. Ctrl+C), the group knows to
	//   stop. shutdownGroup.Go(...) starts a goroutine; shutdownGroup.Wait() waits for all
	//   of them to finish. If any goroutine returns an error, Wait() returns that error.
	shutdownGroup, shutdownContextWithCancel := errgroup.WithContext(shutdownContext)

	// Goroutine 1: Start the HTTP server. ListenAndServe blocks until the server is stopped.
	// So we run it inside shutdownGroup.Go so it runs in the background.
	shutdownGroup.Go(func() error {
		logger.Printf("HTTP server listening on %s", applicationConfig.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	})

	// Goroutine 2: Wait for the shutdown signal (context cancelled), then tell the HTTP server
	// to stop. httpServer.Shutdown(...) stops accepting new connections and waits (up to the
	// timeout) for existing requests to finish. That's "graceful shutdown".
	shutdownGroup.Go(func() error {
		<-shutdownContextWithCancel.Done() // Block until Ctrl+C or SIGTERM

		gracefulShutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelShutdown()

		logger.Println("shutting down HTTP server")
		return httpServer.Shutdown(gracefulShutdownContext)
	})

	// Wait for both goroutines. If either returns an error, we log it and exit with code 1.
	if err := shutdownGroup.Wait(); err != nil {
		logger.Printf("service exited with error: %v", err)
		os.Exit(1)
	}

	logger.Println("service stopped gracefully")
}
