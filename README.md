# URL Shortener (High-Scale, Go)

Production-ready URL shortener showcasing advanced Go features:

- Base62 encoding for compact short codes
- Expiry handling
- Click tracking & analytics
- Redis caching
- Graceful shutdown & context-aware handlers
- Interface-based architecture for storage & cache
- Channel-based ID generation (pluggable for distributed IDs)

## Running locally

1. Ensure Postgres and Redis are running:

- **Postgres** DSN default:
  `postgres://postgres:postgres@localhost:5432/urlshortener?sslmode=disable`
- **Redis** default: `localhost:6379`

2. Create tables:

```sql
CREATE TABLE IF NOT EXISTS urls (
    code         TEXT PRIMARY KEY,
    original_url TEXT NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS clicks (
    id          BIGSERIAL PRIMARY KEY,
    code        TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    ip          TEXT,
    user_agent  TEXT
);
```

3. Run the API:

```bash
go run ./cmd/api
```

4. Shorten a URL:

```bash
curl -X POST http://localhost:8080/api/v1/shorten \
  -H "Content-Type: application/json" \
  -d '{"url":"https://golang.org","expiry":"72h"}'
```

5. Redirect:

Open `http://localhost:8080/{code}` in a browser.

6. Stats:

```bash
curl http://localhost:8080/api/v1/stats/{code}
```

