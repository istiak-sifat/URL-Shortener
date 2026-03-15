// =============================================================================
// internal/shortener/service.go — Core Business Logic (Shorten, Resolve, Track)
// =============================================================================
//
// WHAT DOES THIS PACKAGE DO?
//   It contains the "brain" of the URL shortener: rules for creating short links,
//   looking them up, checking expiry, and recording clicks. It does NOT know about
//   HTTP or SQL directly. Instead it talks to two interfaces: Repository (database)
//   and Cache (Redis). That way we can test the logic with fake storage or swap
//   Postgres for another database without changing this file.
//
// GO CONCEPTS YOU'LL SEE:
//   - Interfaces: Repository and Cache are interfaces (contracts). Any type that has
//     the right methods "implements" them automatically — no "implements" keyword.
//   - Struct and methods: Service is a struct; Shorten, Resolve, TrackClick are methods on it.
//   - Pointers: We use *Service so methods can modify the service if needed.
//   - Channels: idSequenceChannel is a chan uint64 — a way to pass numbers between goroutines.
//   - Goroutines: We start a background loop (runIDGenerator) with "go".
//   - Functional options: WithDefaultExpiry(...) and WithLogger(...) let callers configure the service.
//
// =============================================================================

package shortener

import (
	"context" // Every method takes ctx — used for cancellation/timeouts
	"log"
	"time"

	"urlshortener/internal/base62"
)

// -----------------------------------------------------------------------------
// Repository interface — "What we need from the database"
// -----------------------------------------------------------------------------
// An interface in Go is a set of method signatures. Any type that has these methods
// (with the same parameters and return types) automatically satisfies the interface.
// Our Postgres code in internal/storage implements this; we could also make a
// mock implementation for tests.
type Repository interface {
	StoreURL(ctx context.Context, shortCode, originalURL string, expiresAt time.Time) error
	FindURL(ctx context.Context, shortCode string) (string, time.Time, error)
	IncrementClick(ctx context.Context, shortCode string, occurredAt time.Time, clientIP, userAgent string) error
	GetStats(ctx context.Context, shortCode string, since time.Time) (Stats, error)
}

// -----------------------------------------------------------------------------
// Cache interface — "What we need from the cache (e.g. Redis)"
// -----------------------------------------------------------------------------
// We only cache the mapping shortCode -> originalURL. Get returns (url, found, error).
// Set stores a key with a TTL. Invalidate removes a key (e.g. when a link expires).
type Cache interface {
	Get(ctx context.Context, shortCode string) (string, bool, error)
	Set(ctx context.Context, shortCode, originalURL string) error
	Invalidate(ctx context.Context, shortCode string) error
}

// Stats is the result of GetStats: total clicks and a map of date string -> count per day.
// The `json:"..."` tags tell the JSON encoder what key names to use when serializing.
// GO CONCEPT: struct tags — backtick strings like `json:"total_clicks"` are used by
// encoding/json and other libraries to map struct fields to JSON keys.
type Stats struct {
	TotalClicks int64            `json:"total_clicks"`
	ByDay       map[string]int64 `json:"by_day"`
}

// Service holds the dependencies (repository, cache, logger) and state (default expiry,
// channel for IDs). It's the main type callers use to shorten URLs, resolve them, and get stats.
type Service struct {
	urlRepository     Repository   // Where we save/load URLs and clicks (e.g. Postgres)
	urlCache          Cache        // Optional cache for fast lookups (e.g. Redis)
	logger            *log.Logger  // For logging errors (e.g. when click tracking fails)
	defaultExpiry     time.Duration // If user doesn't set expiry, links expire after this
	idSequenceChannel chan uint64  // Buffered channel of unique numbers for generating short codes
}

// Option is a "functional option" type: a function that takes *Service and configures it.
// So WithDefaultExpiry(24*time.Hour) returns a function that sets service.defaultExpiry = 24h.
// This pattern lets us add optional settings without a long constructor parameter list.
type Option func(*Service)

func WithDefaultExpiry(defaultExpiryDuration time.Duration) Option {
	return func(service *Service) { service.defaultExpiry = defaultExpiryDuration }
}

func WithLogger(loggerInstance *log.Logger) Option {
	return func(service *Service) { service.logger = loggerInstance }
}

// NewService builds a Service. It needs a Repository and a Cache (injected from main).
// options is a variadic parameter: callers can pass zero or more Option functions, e.g.:
//   NewService(repo, cache, WithDefaultExpiry(72*time.Hour), WithLogger(logger))
// GO CONCEPT: variadic — options ...Option means "any number of Option arguments".
func NewService(urlRepository Repository, urlCache Cache, options ...Option) *Service {
	service := &Service{
		urlRepository:     urlRepository,
		urlCache:          urlCache,
		defaultExpiry:     7 * 24 * time.Hour, // Default: 7 days
		idSequenceChannel: make(chan uint64, 1024), // Buffered channel: can hold 1024 IDs
	}
	for _, applyOption := range options {
		applyOption(service)
	}
	if service.logger == nil {
		service.logger = log.New(log.Writer(), "[shortener] ", log.LstdFlags|log.Lmicroseconds)
	}

	// Start the ID generator in the background. It will run until the process exits.
	// GO CONCEPT: goroutine — "go function()" runs function in a new lightweight thread.
	go service.runIDGenerator()

	return service
}

// runIDGenerator runs in a goroutine and pushes unique numbers into idSequenceChannel.
// When Shorten needs an ID, it reads one from the channel (<-service.idSequenceChannel).
// We use a non-blocking send: if the channel is full, we sleep a tiny bit and try again,
// so we don't block the goroutine forever. For production at huge scale, you might replace
// this with a distributed ID generator (e.g. Snowflake, ULID) so multiple servers don't
// generate the same IDs.
func (service *Service) runIDGenerator() {
	nextNumericID := uint64(time.Now().UnixNano())
	for {
		// select: try to send nextNumericID into the channel. If the channel buffer is full,
		// the send would block, so we go to "default" and sleep instead.
		select {
		case service.idSequenceChannel <- nextNumericID:
			nextNumericID++
		default:
			time.Sleep(100 * time.Microsecond)
		}
	}
}

// ShortenRequest is what the API sends: the long URL and optional expiry (e.g. "72h").
// ShortenResponse is what we return: short code, full short URL, expiry time, original URL.
type ShortenRequest struct {
	URL    string        `json:"url"`
	Expiry time.Duration `json:"expiry"`
}

type ShortenResponse struct {
	Code      string    `json:"code"`
	ShortURL  string    `json:"short_url"`
	ExpiresAt time.Time `json:"expires_at"`
	Original  string    `json:"original"`
}

// Shorten creates a new short link.
// Steps: (1) Decide expiry (use request or default). (2) Get next ID from channel and encode to Base62.
// (3) Save to repository. (4) Build and return the response. If Save fails, we return the error.
func (service *Service) Shorten(ctx context.Context, baseURL string, request ShortenRequest) (ShortenResponse, error) {
	if request.Expiry <= 0 {
		request.Expiry = service.defaultExpiry
	}
	expiresAt := time.Now().Add(request.Expiry)

	// Block until we get an ID from the channel. This is the only place we "consume" IDs.
	nextNumericID := <-service.idSequenceChannel
	shortCode := base62.Encode(nextNumericID)

	if err := service.urlRepository.StoreURL(ctx, shortCode, request.URL, expiresAt); err != nil {
		return ShortenResponse{}, err
	}

	shortURL := baseURL + "/" + shortCode

	return ShortenResponse{
		Code:      shortCode,
		ShortURL:  shortURL,
		ExpiresAt: expiresAt,
		Original:  request.URL,
	}, nil
}

// Resolve finds the original URL for a short code. Used when someone clicks a short link.
// Flow: (1) Try cache first (fast). (2) If not in cache, load from repository. (3) If expired,
// invalidate cache and return ErrExpired. (4) Otherwise, store in cache and return the URL.
func (service *Service) Resolve(ctx context.Context, shortCode string) (string, error) {
	if service.urlCache != nil {
		cachedURL, foundInCache, cacheErr := service.urlCache.Get(ctx, shortCode)
		if cacheErr == nil && foundInCache {
			return cachedURL, nil
		}
	}

	originalURL, expiresAt, err := service.urlRepository.FindURL(ctx, shortCode)
	if err != nil {
		return "", err
	}
	if time.Now().After(expiresAt) {
		if service.urlCache != nil {
			_ = service.urlCache.Invalidate(ctx, shortCode)
		}
		return "", ErrExpired
	}

	if service.urlCache != nil {
		_ = service.urlCache.Set(ctx, shortCode, originalURL)
	}

	return originalURL, nil
}

// TrackClick records a click in the background. We don't wait for it — we fire a goroutine
// and return. That way the HTTP redirect is fast; if the DB is slow, we don't block the user.
// We use context.Background() because the request context might already be cancelled by the
// time the goroutine runs; the click still matters so we use a fresh context.
func (service *Service) TrackClick(ctx context.Context, shortCode, clientIP, userAgent string) {
	go func() {
		if err := service.urlRepository.IncrementClick(context.Background(), shortCode, time.Now(), clientIP, userAgent); err != nil {
			service.logger.Printf("failed to track click for %s: %v", shortCode, err)
		}
	}()
}

// Stats returns aggregated click counts for a short code since the given time (e.g. last 30 days).
// The repository does the actual aggregation (e.g. SQL GROUP BY date).
func (service *Service) Stats(ctx context.Context, shortCode string, since time.Time) (Stats, error) {
	return service.urlRepository.GetStats(ctx, shortCode, since)
}
