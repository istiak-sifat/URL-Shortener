// =============================================================================
// go.mod — Go Module File (Dependency Management)
// =============================================================================
//
// WHAT IS THIS FILE?
//   This file tells Go what your project is called and which external
//   libraries (dependencies) it uses. When you run "go build" or "go run",
//   Go reads this file and downloads the right versions of those libraries.
//
// GO CONCEPT: Modules
//   A "module" is a collection of Go packages that are versioned together.
//   The first line below (module urlshortener) is the module path. Other
//   packages in this project import each other using this path, e.g.:
//   import "urlshortener/internal/config"
//
// INTERVIEW TIP: You may be asked "How does Go manage dependencies?"
//   Answer: Go uses modules (go.mod) and a go.sum file that stores exact
//   checksums of dependencies. Dependencies are downloaded to a global
//   cache and linked into your project.
//
// =============================================================================

module urlshortener

// go 1.22 — Minimum Go version required to build this project.
go 1.22

// require — Lists the external packages this project needs.
// Format: "module path" "version"
require (
	// Chi: HTTP router. Lets us map URLs like GET /api/v1/shorten to handler functions.
	github.com/go-chi/chi/v5 v5.1.0

	// pgx: PostgreSQL driver. Used to talk to the Postgres database (store URLs, clicks).
	github.com/jackc/pgx/v5 v5.5.5

	// go-redis: Redis client. Used to cache short-code -> URL lookups for speed.
	github.com/redis/go-redis/v9 v9.5.1

	// errgroup: Run multiple goroutines and wait for all of them; cancel if any returns error.
	golang.org/x/sync v0.9.0
)

// "indirect" dependencies are pulled in automatically by the ones above.
// You don't import them directly; they are used internally by chi, pgx, redis, etc.
require (
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20221227161230-091c0ba34f0a // indirect
	github.com/jackc/puddle/v2 v2.2.1 // indirect
	golang.org/x/crypto v0.17.0 // indirect
	golang.org/x/text v0.14.0 // indirect
)
