# Learning Go With This Project — Interview Prep Guide

This document maps **basic Go topics** and **interview-style concepts** to where they appear in the URL shortener. Use it to study one topic at a time and see how it’s used in real code.

---

## 1. Project layout and `main`

| Topic | What to know | Where you see it |
|--------|---------------|-------------------|
| **Executable vs library** | Only a `package main` with a `func main()` produces an executable. Other packages are libraries. | `cmd/api/main.go`: `package main`, `func main()` |
| **Module path** | The `module` in `go.mod` is the import path prefix for all packages in the project. | `go.mod`: `module urlshortener` → imports like `urlshortener/internal/config` |
| **`internal`** | Code under `internal/` can only be imported by this module, not by other projects. | All packages under `internal/` |

---

## 2. Packages and imports

| Topic | What to know | Where you see it |
|--------|---------------|-------------------|
| **Imports** | You list every package you use; unused imports cause a compile error. | Any file’s `import (...)` block |
| **Exported vs unexported** | Names starting with a **capital letter** are exported (public); others are private to the package. | `Config`, `Load` vs `getEnvString`, `getEnvInt` in `config/config.go` |
| **Import path** | Standard library: `"context"`, `"net/http"`. Our code: `"urlshortener/internal/shortener"`. Third‑party: `"github.com/..."`. | `main.go`, `config.go`, etc. |

---

## 3. Basic types and literals

| Topic | What to know | Where you see it |
|--------|---------------|-------------------|
| **Strings** | Immutable; can index: `s[0]`. Use `strings` for splitting, trimming. | `base62/alphabet`, `httpserver` headers |
| **Numbers** | `int`, `int64`, `uint64` (unsigned). Integer division truncates. | `base62`: `uint64`; `config`: `getEnvInt` |
| **time.Duration** | Represents a length of time (nanoseconds internally). Use `time.Minute`, `time.Hour`, or `time.ParseDuration("10m")`. | `config` (CacheTTL, DefaultURLExpiry), `main.go` timeouts |
| **time.Time** | Represents a point in time. `time.Now()`, `.Add(duration)`, `.After(t)`, `.Format("2006-01-02")`. | `shortener` (expiresAt), `storage` (occurred_at, day_date) |

---

## 4. Structs

| Topic | What to know | Where you see it |
|--------|---------------|-------------------|
| **Defining a struct** | Group of named fields. No inheritance; use composition. | `Config`, `Service`, `Server`, `RedisCache`, `PostgresRepository`, `Stats` |
| **Struct literal** | `Config{ Field: value }`. `&Config{...}` gives a pointer. | `config.Load()`, `main.go` `http.Server`, `shortener.NewService` |
| **Methods** | `func (receiver Type) MethodName(args) returnType`. Receiver can be value or pointer (`*Type`). | All `(server *Server)`, `(service *Service)`, etc. |
| **Pointer receiver** | Use `*Type` when the method needs to modify the struct or the struct is large. | All service/handler/repository methods use pointer receivers |

---

## 5. Interfaces

| Topic | What to know | Where you see it |
|--------|---------------|-------------------|
| **Interface = set of methods** | A type “implements” an interface by having the same method set; no `implements` keyword. | `Repository`, `Cache` in shortener; `Logger` in httpserver |
| **Why interfaces** | Decouple “what we need” from “who does it”. Easy to swap implementations and to test with mocks. | Shortener depends on `Repository` and `Cache`, not Postgres/Redis directly |
| **Empty interface** | `interface{}` or `any`: “any type”. Used for generic containers and JSON. | `Logger.Printf(..., args ...interface{})`, `writeJSON(..., responsePayload any)` |

---

## 6. Error handling

| Topic | What to know | Where you see it |
|--------|---------------|-------------------|
| **Errors are values** | Functions return `(result, error)`. Caller checks `if err != nil`. | Every `err := ...; if err != nil { ... }` |
| **errors.New** | Creates a simple error. Good for sentinel errors like `ErrNotFound`, `ErrExpired`. | `shortener/errors.go` |
| **Comparing errors** | `err == shortener.ErrExpired`. For wrapped errors use `errors.Is(err, ErrExpired)`. | `httpserver` in `handleRedirect` |
| **defer** | Defers a call until the surrounding function returns. Used for cleanup (Close, cancel). | `main.go` (stopSignalHandler, pool.Close, redisClient.Close), `storage` (clickRows.Close) |

---

## 7. Slices and maps

| Topic | What to know | Where you see it |
|--------|---------------|-------------------|
| **Slice** | `[]T`: dynamic view over an array. `make([]byte, 0, 11)`, `append(slice, x)`. | `base62`: `encodedDigits` |
| **Map** | `map[K]V`. Must initialize with `make(map[string]int64)` or literal. | `Stats.ByDay map[string]int64` in shortener and storage |
| **Range** | `for i, v := range slice` or `for k, v := range m`. | Not heavily used here; storage uses `for clickRows.Next()` for DB rows |

---

## 8. Concurrency: goroutines and channels

| Topic | What to know | Where you see it |
|--------|---------------|-------------------|
| **Goroutine** | `go f()` runs `f` in a new goroutine. Lightweight; many can run at once. | `main.go`: `shutdownGroup.Go(...)`; shortener: `go service.runIDGenerator()`, `go func(){ ... }()` in TrackClick |
| **Channel** | `chan T`: typed conduit. Send: `ch <- value`. Receive: `value := <-ch`. Can be buffered: `make(chan uint64, 1024)`. | shortener: `idSequenceChannel chan uint64` |
| **select** | Choose one of several channel operations (or default). Used for non-blocking send in runIDGenerator. | shortener: `select { case ch <- id: ... default: ... }` |
| **errgroup** | Run multiple goroutines; wait for all; cancel rest if one returns error. | `main.go`: start server + graceful shutdown in parallel |

---

## 9. Context

| Topic | What to know | Where you see it |
|--------|---------------|-------------------|
| **context.Context** | Carries cancellation, timeout, and request-scoped values. Pass as first argument. | Every service/repository/cache method: `ctx context.Context` |
| **context.Background()** | Root context; never cancelled. Use when you’re not in a request (e.g. TrackClick goroutine). | shortener `TrackClick` |
| **signal.NotifyContext** | Context that is cancelled when the process receives SIGINT/SIGTERM. | `main.go` for graceful shutdown |
| **context.WithTimeout** | New context that cancels after a duration. Used for shutdown deadline. | `main.go` in shutdown goroutine |

---

## 10. HTTP (net/http and chi)

| Topic | What to know | Where you see it |
|--------|---------------|-------------------|
| **http.Handler** | Interface: `ServeHTTP(w http.ResponseWriter, r *http.Request)`. Chi’s router implements it. | `Server.Router()` returns `http.Handler` |
| **ResponseWriter** | You write status, headers, body. No “return” of response. | All handlers: `w.Header().Set`, `w.WriteHeader`, `json.NewEncoder(w).Encode(...)` |
| **Request** | Method, URL, Header, Body. `r.Context()` is the request’s context. | Every handler receives `request *http.Request` |
| **Router** | Maps method + path to handler. Path params: `chi.URLParam(r, "code")`. | `setupRoutes`: Get, Post, Use (middleware) |
| **Middleware** | Wraps every request: logging, panic recovery, timeout, request ID. | chi middleware in `setupRoutes` |

---

## 11. JSON

| Topic | What to know | Where you see it |
|--------|---------------|-------------------|
| **Struct tags** | `` `json:"field_name"` `` tells the encoder/decoder the JSON key. | `ShortenRequest`, `ShortenResponse`, `Stats` |
| **Encode** | `json.NewEncoder(w).Encode(v)` writes struct (or map) as JSON to `w`. | `writeJSON`, `handleHealth`, `handleShorten`, etc. |
| **Decode** | `json.NewDecoder(r.Body).Decode(&struct)` reads JSON from body into struct. | `handleShorten`: decode into `ShortenRequest` |

---

## 12. Database (pgx)

| Topic | What to know | Where you see it |
|--------|---------------|-------------------|
| **Connection pool** | One pool per process; many goroutines share it. Created once, passed around. | `main.go` creates pool; `PostgresRepository` holds it |
| **Parameterized queries** | Use `$1`, `$2`, … and pass values separately. Prevents SQL injection. | Every query in `storage/postgres.go` |
| **Exec** | Run a query that doesn’t return rows (INSERT, UPDATE, DELETE). | `StoreURL`, `IncrementClick` |
| **QueryRow** | Expect exactly one row. Use `.Scan(&var1, &var2)` to fill variables. | `FindURL` |
| **Query + Next + Scan** | Multiple rows: `Query` → loop `Next()` → `Scan`. Always `defer rows.Close()`. | `GetStats` |

---

## 13. Configuration and 12-factor style

| Topic | What to know | Where you see it |
|--------|---------------|-------------------|
| **Environment variables** | `os.Getenv("KEY")`. Use for config and secrets; same binary, different env. | `config/config.go` |
| **Config struct** | One struct holding all settings; load once at startup. | `Config` and `Load()` |
| **Defaults** | If env is missing or invalid, use a sensible default so the app still runs (e.g. local dev). | All `getEnvString`, `getEnvInt`, `getEnvDuration` |

---

## 14. Dependency injection and “composition root”

| Topic | What to know | Where you see it |
|--------|---------------|-------------------|
| **Composition root** | One place (usually `main`) where you create and connect all dependencies. | `main.go`: create pool, Redis, repository, cache, service, server |
| **Inject interfaces** | Shortener gets `Repository` and `Cache`, not concrete Postgres/Redis. Easier to test and swap. | `shortener.NewService(urlRepository, urlCache, ...)` |
| **Functional options** | Optional config via functions: `WithDefaultExpiry(d)`, `WithLogger(l)`. Keeps constructor clean. | shortener: `Option func(*Service)` |

---

## How to use this for interview prep

1. **Pick a topic** from the table (e.g. “Interfaces” or “Context”).
2. **Read the “What to know”** and open the **“Where you see it”** files.
3. **Trace one flow** end-to-end, e.g. “Create short link”:  
   `POST /api/v1/shorten` → `handleShorten` → `Shorten` → `StoreURL` (and cache if you add it).
4. **Explain out loud** what each type and function does; that’s what you’ll do in an interview.

If you want more detail on a specific file, use the file-level and inline comments in the code; they’re written so you can prepare for a Go role from this single project.
