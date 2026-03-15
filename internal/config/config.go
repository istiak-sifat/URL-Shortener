// =============================================================================
// internal/config/config.go — Configuration from Environment Variables
// =============================================================================
//
// WHAT DOES THIS FILE DO?
//   It reads configuration from environment variables (e.g. HTTP_ADDR, POSTGRES_DSN)
//   and puts them into a single Config struct. If a variable is not set or is
//   invalid, it uses a default value. This way we can run the same program in
//   development (localhost) and production (real hostnames) by changing env vars.
//
// GO CONCEPT: Packages and visibility
//   This package is named "config". Other files import it as "urlshortener/internal/config".
//   In Go, names that start with a CAPITAL letter are "exported" (public); others are
//   private to the package. So "Config" and "Load" are public; "getEnvString" is private.
//
// INTERVIEW TIP: Why environment variables for config?
//   - Same binary can run in dev/staging/prod without recompiling.
//   - No secrets in code; they are set in the deployment environment.
//   - Standard practice for cloud-native / 12-factor apps.
//
// =============================================================================

package config

import (
	"log"     // To log when a value is invalid and we fall back to default
	"os"      // os.Getenv(KEY) reads an environment variable
	"strconv" // strconv.Atoi converts string to int; time.ParseDuration parses "5m", "1h", etc.
	"time"    // time.Duration is a type for time spans (nanoseconds under the hood)
)

// Config holds all settings the application needs at runtime.
// GO CONCEPT: struct
//   A struct is a group of named fields (like a simple class without methods inheritance).
//   We use it so we can pass one "config" object around instead of many separate parameters.
type Config struct {
	// HTTPAddr: address the HTTP server will listen on.
	// Examples: ":8080" (all interfaces, port 8080), "127.0.0.1:3000" (localhost only).
	// The colon means "listen on this port"; the empty part before : means "all IPs".
	HTTPAddr string

	// PostgresDSN: "Data Source Name" — the connection string for PostgreSQL.
	// Format: postgres://USER:PASSWORD@HOST:PORT/DATABASE?options
	// Example: postgres://postgres:postgres@localhost:5432/urlshortener?sslmode=disable
	// sslmode=disable is often used locally; in production you'd use sslmode=require or verify-full.
	PostgresDSN string

	// RedisAddr: host and port of the Redis server. Usually "localhost:6379".
	RedisAddr string

	// RedisPassword: password for Redis. Empty string if Redis has no password (common in dev).
	RedisPassword string

	// RedisDB: Redis database number (0–15 by default). Redis can have multiple logical
	// "databases" in one server; we use 0 unless you need to separate data.
	RedisDB int

	// CacheTTL: how long a cached URL stays in Redis before expiring.
	// time.Duration: type that represents a length of time. Examples: 10*time.Minute, 24*time.Hour.
	// After TTL, Redis deletes the key so we fetch fresh data from Postgres next time.
	CacheTTL time.Duration

	// DefaultURLExpiry: when a user creates a short link without specifying expiry, we use this.
	// Example: 7*24*time.Hour = 7 days. So short links default to expiring in one week.
	DefaultURLExpiry time.Duration
}

// Load reads environment variables and builds a Config. It's the only function other
// packages need; they call config.Load() and get a Config struct with everything set.
func Load() Config {
	applicationConfig := Config{
		HTTPAddr:         getEnvString("HTTP_ADDR", ":8080"),
		PostgresDSN:      getEnvString("POSTGRES_DSN", "postgres://postgres:postgres@localhost:5432/urlshortener?sslmode=disable"),
		RedisAddr:        getEnvString("REDIS_ADDR", "localhost:6379"),
		RedisPassword:    getEnvString("REDIS_PASSWORD", ""),
		RedisDB:          getEnvInt("REDIS_DB", 0),
		CacheTTL:         getEnvDuration("CACHE_TTL", 10*time.Minute),
		DefaultURLExpiry: getEnvDuration("DEFAULT_URL_EXPIRY", 7*24*time.Hour),
	}

	return applicationConfig
}

// getEnvString returns the value of the environment variable named envKey.
// If it's not set (or empty), it returns defaultValue. Environment variables
// are always strings in the OS, so we don't need to convert.
func getEnvString(envKey, defaultValue string) string {
	environmentValue := os.Getenv(envKey)
	if environmentValue == "" {
		return defaultValue
	}
	return environmentValue
}

// getEnvInt reads an env var and converts it to an integer (e.g. "3" -> 3).
// strconv.Atoi("123") returns (123, nil). If the string is not a valid number,
// we log a message and return the default. This way a typo in REDIS_DB won't crash the app.
func getEnvInt(envKey string, defaultValue int) int {
	environmentValue := os.Getenv(envKey)
	if environmentValue == "" {
		return defaultValue
	}
	parsedInteger, err := strconv.Atoi(environmentValue)
	if err != nil {
		log.Printf("invalid int value for %s: %v, using default %d", envKey, err, defaultValue)
		return defaultValue
	}
	return parsedInteger
}

// getEnvDuration reads an env var and parses it as a duration (e.g. "10m", "1h30m").
// time.ParseDuration understands: "ns", "us", "ms", "s", "m", "h". So "CACHE_TTL=15m"
// in your environment would set cache TTL to 15 minutes. Very useful for config.
func getEnvDuration(envKey string, defaultValue time.Duration) time.Duration {
	environmentValue := os.Getenv(envKey)
	if environmentValue == "" {
		return defaultValue
	}
	parsedDuration, err := time.ParseDuration(environmentValue)
	if err != nil {
		log.Printf("invalid duration value for %s: %v, using default %s", envKey, err, defaultValue)
		return defaultValue
	}
	return parsedDuration
}
