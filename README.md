# Advanced Template

Project template for Firefly Software **Advanced tier** client projects. Superset of the standard tier — adds a persistence layer, migrations, session-backed auth, and a working login flow.

## Stack

- **Go 1.25** stdlib `net/http` router
- **templ** for server-side rendered components
- **Tailwind CSS** via standalone CLI
- **htmx** + **Alpine.js** (vendored)
- **SQLite** via `modernc.org/sqlite` (pure Go, no CGo) — [PostgreSQL upgrade path](POSTGRES.md)
- **sqlc** for type-safe query generation
- **goose** for migrations
- **bcrypt** password hashing, cookie-based sessions
- **Postmark** for transactional email
- **Cloudflare Turnstile** for contact form spam protection
- **Mage** build system

## Getting Started

### Prerequisites

Open in the devcontainer — all tools are installed automatically. Otherwise install manually:

- Go 1.25+
- [templ](https://templ.guide)
- [goose](https://github.com/pressly/goose)
- [sqlc](https://sqlc.dev)
- [Mage](https://magefile.org)

### Setup

```bash
cp .env.example .env        # edit with your values
mage installtailwind        # download Tailwind standalone CLI
mage seed admin@example.com yourpassword  # creates the DB, runs migrations, seeds an admin
```

Migrations are embedded in the binary and run automatically at server startup (and before seeding), so `mage migrateup` is only needed for manual migration work.

### Development

```bash
mage dev    # full build (CSS + templ + sqlc + Go) and run the server
```

### Production Build

```bash
mage build        # compiles Tailwind, generates templ + sqlc, builds Go binary
./bin/server
```

## Project Structure

```
advanced-template/
├── cmd/
│   ├── server/           # main application entry point
│   └── seed/             # CLI tool for creating admin users
├── internal/
│   ├── config/           # env-based config loader
│   ├── handler/          # HTTP handlers (home, contact, auth, admin)
│   ├── mail/             # Postmark email client
│   ├── middleware/       # logging, CORS, rate limit, CSRF, recovery
│   ├── session/          # session middleware + context helpers
│   ├── store/            # business logic over the sqlc query layer
│   ├── view/             # templ components and layouts
│   └── db/               # sqlc generated code (gitignored, built by `mage generate`)
├── migrations/           # goose SQL migrations (embedded into the binary)
├── queries/              # sqlc SQL query definitions
├── web/static/           # CSS, JS, images
├── tailwind/             # Tailwind config + standalone CLI
├── sqlc.yaml
├── magefile.go
├── Dockerfile
├── docker-compose.yml      # local: builds from source
├── docker-compose.prod.yml # production: GHCR image, synced to the VPS on deploy
└── POSTGRES.md
```

## Mage Targets

| Target | Description |
|---|---|
| `mage build` | Full production build (CSS + templ + sqlc + Go binary) |
| `mage buildcss` | Compile Tailwind CSS |
| `mage dev` | Full build, then run the server |
| `mage generate` | Run `templ generate` and `sqlc generate` |
| `mage migrateup` | Run all pending migrations |
| `mage migratedown` | Roll back the last migration |
| `mage migratestatus` | Show current migration state |
| `mage createmigration <name>` | Scaffold a new migration file |
| `mage seed <email> <password>` | Create an admin user |

## Auth

### Routes

| Route | Purpose |
|---|---|
| `GET /login` | Render login form |
| `POST /login` | Validate credentials, create session, redirect (rate limited: 5 quick attempts, then 1/sec per IP) |
| `POST /logout` | Destroy session, clear cookie, redirect |
| `GET /admin` | Example protected page (`session.RequireAuth`) — extend admin features from here |

The login form uses htmx (`hx-post`, `hx-swap="outerHTML"`) for inline error feedback without a full page reload. The nav shows Admin/Logout links when a user is signed in.

### Creating Users

There is no registration flow — admin users are created via the seed CLI:

```bash
mage seed admin@example.com yourpassword
```

Or directly:

```bash
go run ./cmd/seed admin@example.com yourpassword
```

### Protecting Routes

Wrap any handler with `session.RequireAuth` to redirect unauthenticated users to `/login`:

```go
mux.Handle("GET /admin", session.RequireAuth(handler.AdminDashboard()))
```

For a group of routes, wrap the sub-mux:

```go
admin := http.NewServeMux()
admin.Handle("GET /admin/dashboard", handler.Dashboard())
admin.Handle("GET /admin/settings", handler.Settings())

mux.Handle("/admin/", session.RequireAuth(admin))
```

### Accessing the Current User

The session middleware runs on every request and attaches the user to the context when a valid session cookie is present. Access it from any handler:

```go
func Dashboard() http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        user := session.FromContext(r.Context())
        if user != nil {
            // user.ID, user.Email are available
        }
    }
}
```

`session.FromContext` returns `nil` for unauthenticated requests — safe to call on public pages (e.g. to show/hide a nav login link).

### Sessions

- Stored in SQLite (`sessions` table), persist across server restarts
- Cookie: `session_token`, HttpOnly, Secure, SameSite=Lax, 7-day expiry
- Expired sessions are swept at startup and hourly
- Passwords hashed with bcrypt via `golang.org/x/crypto`

## Database Queries

SQL lives in `queries/*.sql`; [sqlc](https://sqlc.dev) generates type-safe Go into `internal/db/` (gitignored — regenerate with `mage generate`). `internal/store` wraps the generated queries and adds business logic like password hashing. To add a query: write it in `queries/`, run `mage generate`, then expose it through a `Store` method.

## Middleware

All middleware lives in `internal/middleware/` and follows the `func(http.Handler) http.Handler` pattern. They compose by wrapping — outermost runs first. Logging, rate limiting, CSRF, panic recovery, and sessions are wired in `main.go` by default; CORS is available but opt-in (same-origin sites don't need it).

### Logging

Wraps every request with a structured JSON log line (method, path, status, duration, request ID).

```go
srv := middleware.Logging(logger)(mux)
```

### CORS

Handles cross-origin requests. Preflight (OPTIONS) gets `Allow-Methods`/`Allow-Headers`/`Max-Age`; actual requests get `Allow-Origin` only. Disallowed origins receive no CORS headers (browser-enforced).

```go
srv = middleware.CORS(middleware.CORSConfig{
    AllowedOrigins:   []string{"https://example.com"},
    AllowedMethods:   []string{"GET", "POST"},
    AllowedHeaders:   []string{"Content-Type", "Authorization"},
    AllowCredentials: true,
    MaxAge:           3600,
})(srv)
```

Use `"*"` in `AllowedOrigins` to allow any origin.

### Rate Limiting

Per-client token bucket throttling. Each unique client gets its own bucket with the configured rate (requests/sec) and burst (max instant). Returns `429 Too Many Requests` with `Retry-After` when exceeded.

```go
srv = middleware.RateLimit(middleware.RateLimitConfig{
    Rate:           10,    // 10 requests/sec steady state
    Burst:          20,    // allow bursts up to 20
    TrustedProxies: 1,     // 1 = behind Caddy (reads X-Forwarded-For)
    CleanupInterval: 5 * time.Minute,
})(srv)
```

**`TrustedProxies`**: Set to the number of reverse proxies in front of the app. `0` uses `RemoteAddr` directly (no proxy). `1` trusts the rightmost `X-Forwarded-For` entry (Caddy on Hetzner). `2` for CDN + Caddy.

**Custom key function**: Rate limit by something other than IP (e.g. API key):

```go
middleware.RateLimitConfig{
    Rate:  5,
    Burst: 5,
    KeyFunc: func(r *http.Request) string {
        return r.Header.Get("X-API-Key")
    },
}
```

### CSRF Protection

Signed double-submit cookie pattern. A token is issued on every GET and validated on POST/PUT/DELETE. Uses HMAC-SHA256 to prevent cookie tampering.

```go
srv = middleware.CSRF(middleware.CSRFConfig{
    Secret: []byte(cfg.SessionSecret), // must be >= 32 bytes
})(srv)
```

**In templ forms** — include the shared component (it reads the token from the request context, which templ passes through as `ctx`):

```go
<form method="post" action="/example">
	@CSRFField()
	...
</form>
```

`view.CSRFField` is already included in the login, logout, and contact forms. For non-templ code, `middleware.Token(r)` returns the raw token, or use the `TemplateField` helper in Go `html/template`:

```go
template.HTML(middleware.TemplateField(r))
```

**With htmx** — send the token in a header. Configure globally:

```html
<body hx-headers='{"X-CSRF-Token": "{{ token }}"}'>
```

Or per-element with `hx-headers`.

**Config options:**
- `FieldName` — form field name (default: `"csrf_token"`)
- `HeaderName` — header name (default: `"X-CSRF-Token"`)
- `InsecureDev` — set `true` for local HTTP without TLS
- `ErrorHandler` — custom 403 response

### Panic Recovery

Catches panics in handlers, logs the stack trace, and returns a 500 instead of crashing the server. Safe with WebSocket upgrades and partial responses.

```go
srv = middleware.Recovery(middleware.RecoveryConfig{})(srv)
```

With a custom error page:

```go
srv = middleware.Recovery(middleware.RecoveryConfig{
    ErrorHandler: func(w http.ResponseWriter, r *http.Request, val any) {
        w.WriteHeader(http.StatusInternalServerError)
        view.ErrorPage().Render(r.Context(), w)
    },
})(srv)
```

Edge cases handled automatically:
- **`http.ErrAbortHandler`** — re-panics (intentional connection abort, not a bug)
- **WebSocket upgrades** — logs but writes no HTTP response (would corrupt the connection)
- **Headers already sent** — logs but skips the error response (can't change status mid-stream)

### Middleware Order

In `main.go`, middleware wraps inside-out. The default stack:

```go
srv := session.Middleware(st)(mux)      // innermost: attach user to context
srv = middleware.CSRF(csrfCfg)(srv)     // validate mutations before they reach handlers
srv = middleware.RateLimit(rlCfg)(srv)  // global limit (30/sec, burst 60 per IP)
srv = middleware.Recovery(recCfg)(srv)  // catch panics before they reach the logger
srv = middleware.Logging(logger)(srv)   // outermost: log everything including 500s
```

Add `middleware.CORS` between RateLimit and Recovery if the project serves cross-origin clients. `POST /login` additionally has its own strict limiter wrapped directly around the handler.

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `ENV` | `development` | `production` enables proxy-aware rate limiting and requires a real `SESSION_SECRET` (set automatically in the Docker image) |
| `PORT` | `8080` | Server listen port |
| `DB_PATH` | `./data/app.db` | SQLite database file path (`/data/app.db` in Docker, on a volume) |
| `SESSION_SECRET` | — | HMAC signing secret (CSRF), ≥ 32 bytes — `openssl rand -hex 32`. Required in production; dev falls back to an ephemeral secret with a warning |
| `POSTMARK_SERVER_TOKEN` | — | Postmark API token |
| `POSTMARK_FROM` | — | Sender email address |
| `POSTMARK_TO` | — | Recipient email address |
| `GTAG_ID` | — | Google Analytics measurement ID |
| `PIXEL_ID` | — | Facebook Pixel ID |
| `TURNSTILE_SITE_KEY` | — | Cloudflare Turnstile site key |
| `TURNSTILE_SECRET_KEY` | — | Cloudflare Turnstile secret key |

## Deployment

Built for deployment on Hetzner behind Caddy via Docker Compose. Pushing to `main` builds the image, pushes it to GHCR, syncs `docker-compose.prod.yml` to the VPS, and restarts the service pinned to the new image's SHA tag.

**One-time VPS setup:** create `/opt/<project>/.env` with production values (`SESSION_SECRET`, Postmark keys, etc.) and point Caddy at `127.0.0.1:8080`. The workflow needs `VPS_HOST`, `VPS_USER`, and `VPS_SSH_KEY` repository secrets.

The SQLite database lives on the `app-data` named volume (`/data/app.db`), so it survives image upgrades. Migrations are embedded in the binary and apply automatically on startup.
