// =============================================================================
// internal/storage/postgres.go — Postgres Implementation of the Repository
// =============================================================================
//
// WHAT DOES THIS FILE DO?
//   It implements the shortener.Repository interface using PostgreSQL. So when the
//   shortener calls StoreURL, we run an INSERT (or upsert). When it calls FindURL,
//   we run a SELECT. Clicks are stored in a separate table with INSERT. GetStats
//   runs a query that groups clicks by day and returns counts.
//
// DATABASE HANDLING IN GO (concepts used here):
//   - Connection pool: We don't open a new connection per request. pgxpool.Pool keeps
//     a set of connections and hands one to each goroutine when needed. That's why
//     we create the pool once in main and pass it here.
//   - Parameterized queries: We never concatenate user input into SQL. We use $1, $2, ...
//     and pass values separately. That prevents SQL injection and lets the driver handle
//     escaping. Example: QueryRow(ctx, "SELECT ... WHERE code = $1", shortCode).
//   - context.Context: Every DB call takes ctx. If the HTTP request is cancelled (e.g. client
//     closed the tab), we can cancel the query too so we don't waste resources.
//
// TABLES (you create these with the SQL in README):
//   - urls: code (PK), original_url, expires_at
//   - clicks: id, code, occurred_at, ip, user_agent
//
// =============================================================================

package storage

import (
	"context"
	"time"

	"urlshortener/internal/shortener"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresRepository holds a connection pool. We don't store a single connection;
// we use the pool so many goroutines can run queries at the same time.
type PostgresRepository struct {
	connectionPool *pgxpool.Pool
}

// NewPostgresPool creates the pool. connectionString is the DSN from config (e.g.
// postgres://user:pass@localhost:5432/dbname?sslmode=disable). pgxpool.New parses
// it and opens a pool of connections.
func NewPostgresPool(ctx context.Context, connectionString string) (*pgxpool.Pool, error) {
	return pgxpool.New(ctx, connectionString)
}

// NewPostgresRepository takes an existing pool and returns a Repository that uses it.
// We create the pool once in main and pass it here so all handlers share the same pool.
func NewPostgresRepository(connectionPool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{connectionPool: connectionPool}
}

// StoreURL inserts a new row into urls, or updates it if the code already exists (upsert).
// SQL: INSERT ... ON CONFLICT (code) DO UPDATE ... means "insert, but if code is already
// there, update the other columns instead". So we can reuse the same short code if we want.
// We pass shortCode, originalURL, expiresAt as separate arguments — they fill $1, $2, $3.
// Exec runs the query and returns (result, error). We ignore the result and only care about err.
func (repository *PostgresRepository) StoreURL(ctx context.Context, shortCode, originalURL string, expiresAt time.Time) error {
	const insertURLQuery = `
INSERT INTO urls (code, original_url, expires_at)
VALUES ($1, $2, $3)
ON CONFLICT (code) DO UPDATE
SET original_url = EXCLUDED.original_url,
    expires_at   = EXCLUDED.expires_at;
`
	_, err := repository.connectionPool.Exec(ctx, insertURLQuery, shortCode, originalURL, expiresAt)
	return err
}

// FindURL fetches one row: the original URL and expiry time for the given short code.
// QueryRow returns at most one row. Scan copies the two columns into our Go variables.
// If there is no row (e.g. wrong code), QueryRow(...).Scan(...) returns an error (e.g. pgx.ErrNoRows).
func (repository *PostgresRepository) FindURL(ctx context.Context, shortCode string) (string, time.Time, error) {
	const findURLQuery = `
SELECT original_url, expires_at
FROM urls
WHERE code = $1;
`
	var originalURL string
	var expiresAt time.Time
	if err := repository.connectionPool.QueryRow(ctx, findURLQuery, shortCode).Scan(&originalURL, &expiresAt); err != nil {
		return "", time.Time{}, err
	}
	return originalURL, expiresAt, nil
}

// IncrementClick inserts one row into the clicks table. We don't "update a counter"; we append
// an event (one row per click). That way we have a full history and can aggregate later (e.g. by day).
// This is a common pattern for analytics: event sourcing / append-only log.
func (repository *PostgresRepository) IncrementClick(ctx context.Context, shortCode string, occurredAt time.Time, clientIP, userAgent string) error {
	const insertClickQuery = `
INSERT INTO clicks (code, occurred_at, ip, user_agent)
VALUES ($1, $2, $3, $4);
`
	_, err := repository.connectionPool.Exec(ctx, insertClickQuery, shortCode, occurredAt, clientIP, userAgent)
	return err
}

// GetStats runs a query that groups clicks by day and counts them. So we get one row per day
// with the date and the number of clicks that day. We loop over the rows with Next() and
// Scan each into dayDate and clickCount, then add them into the analyticsStats struct.
// defer clickRows.Close() ensures we close the result set when we're done (important to
// return the connection to the pool). rows.Err() at the end tells us if something went wrong
// during iteration.
func (repository *PostgresRepository) GetStats(ctx context.Context, shortCode string, since time.Time) (shortener.Stats, error) {
	const getStatsQuery = `
SELECT
    DATE(occurred_at) AS day_date,
    COUNT(*)         AS click_count
FROM clicks
WHERE code = $1
  AND occurred_at >= $2
GROUP BY DATE(occurred_at)
ORDER BY day_date;
`
	clickRows, err := repository.connectionPool.Query(ctx, getStatsQuery, shortCode, since)
	if err != nil {
		return shortener.Stats{}, err
	}
	defer clickRows.Close()

	analyticsStats := shortener.Stats{
		ByDay: make(map[string]int64), // map date string -> count
	}

	for clickRows.Next() {
		var dayDate time.Time
		var clickCount int64
		if err := clickRows.Scan(&dayDate, &clickCount); err != nil {
			return shortener.Stats{}, err
		}
		analyticsStats.TotalClicks += clickCount
		analyticsStats.ByDay[dayDate.Format("2006-01-02")] = clickCount // Go's reference date for formatting is 2006-01-02
	}

	return analyticsStats, clickRows.Err()
}
