// =============================================================================
// internal/shortener/errors.go — Domain Errors (So Callers Can React)
// =============================================================================
//
// WHAT DOES THIS FILE DO?
//   It defines two specific error values: ErrNotFound (short code doesn't exist) and
//   ErrExpired (short code exists but the link has expired). The HTTP layer uses these
//   to return the right status code: 404 for not found, 410 Gone for expired.
//
// GO CONCEPT: Errors are values
//   In Go, there is no "throw exception". Functions return an error value (type error).
//   error is an interface with one method: Error() string. So any type that has that
//   method can be used as an error. errors.New("message") creates a simple error value.
//
// GO CONCEPT: Exported variables
//   ErrNotFound and ErrExpired start with a capital letter, so they are exported. Other
//   packages can do: if err == shortener.ErrExpired { ... }. We use var ( ... ) to
//   declare multiple variables in one block.
//
// INTERVIEW TIP: errors.Is and errors.As
//   For more complex code, we use errors.Is(err, ErrExpired) to check if err wraps this
//   error. errors.As extracts a specific error type. This project keeps it simple with
//   direct comparison (err == shortener.ErrExpired).
//
// =============================================================================

package shortener

import "errors"

var (
	// ErrNotFound means the short code was not found in the database (or cache).
	// The HTTP handler will return 404 Not Found when it sees this.
	ErrNotFound = errors.New("short code not found")

	// ErrExpired means the short code exists but its expiry time has passed, so we
	// no longer redirect to the URL. The HTTP handler will return 410 Gone.
	ErrExpired = errors.New("short code expired")
)
