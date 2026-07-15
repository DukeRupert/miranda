# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this repo is

A Go web application built on Firefly Software's **Advanced tier** template: stdlib `net/http` + templ + htmx + Alpine + Tailwind, backed by SQLite (sqlc queries, goose migrations) with cookie-based session auth. The Go module is `github.com/dukerupert/miranda`; the repo is `miranda`.

`spec.md` (untracked) specifies the domain feature this template is being extended into: an **FAA ATC facility scheduling engine**. It is merged to `main`: see `SCHEDULING.md` for current status, the load-bearing rotation-aware staffing model, and the backlog (including an open product decision on two engine-vs-spec numeric deviations). The pure engine lives in `internal/{domain,coverage,validate,materialize}` with `internal/fixtures` holding the shared HLN reference data; lines/controllers/leave are editable and persisted as named scenarios (`internal/store/schedule.go`, migration `003`), and the explorer UI is at `GET /explore` (`internal/handler/schedule.go`, `internal/view/schedule.templ`). When extending it, follow the non-negotiable §7 test suite in `spec.md`, and keep the engine packages pure (no I/O).

For deeper rationale beyond this file, see `README.md`, `ARCHITECTURE.md`, and `POSTGRES.md`.

## Commands

Mage drives all builds (`magefile.go`). Common targets:

```bash
mage dev            # full build (CSS + generate + go build) then run — primary dev loop
mage build          # production build → ./bin/server
mage generate       # templ generate + sqlc generate (see "generated code" below)
mage buildcss       # compile Tailwind only
mage installtailwind          # one-time: download the pinned Tailwind standalone CLI
mage seed <email> <password>  # create an admin user (also creates DB + runs migrations)
mage migrateup / migratedown / migratestatus
mage createmigration <name>   # scaffold a goose migration
```

Tests (standard Go; table-driven; concentrated in `internal/middleware` and `internal/session`):

```bash
go test ./...                                   # all packages
go test ./internal/middleware/                  # one package
go test ./internal/middleware/ -run TestCSRF    # one test
```

## Generated code — a fresh checkout does NOT compile

Two output trees are **gitignored** and must be produced by `mage generate` before `go build` succeeds:

- `internal/db/` — sqlc output compiled from `queries/*.sql` (schema read from `migrations/`).
- `internal/view/*_templ.go` — templ output compiled from `*.templ`.

The Docker build and devcontainer regenerate both. If the build fails on missing `db` or `view` symbols, run `mage generate`. After editing any `queries/*.sql` or `*.templ`, regenerate before building.

## Architecture & layering

Request path (middleware wraps inside-out in `cmd/server/main.go`; outermost runs first):

```
Logging → Recovery → RateLimit (30/s, burst 60/IP) → CSRF → session.Middleware → ServeMux → handler
```

Strict layering — respect these boundaries:

- **`internal/handler/`** — one file per feature. Parses input, calls `store`, renders templ components. **No SQL here.** Constructor-style: handlers are `func Foo() http.HandlerFunc`.
- **`internal/store/`** — the *only* package that touches the sqlc query layer. Wraps generated `db.Queries` and adds business logic (e.g. bcrypt hashing, so plaintext passwords never leave the handler→store boundary). **To add a query: write SQL in `queries/`, `mage generate`, then expose a `Store` method** — do not call `db` directly from handlers.
- **`internal/session/`** — session lifecycle + middleware. Depends on an interface (`session.Store`), not the concrete store. The middleware never blocks: it attaches the user to context when the cookie is valid and passes through otherwise. Enforce auth by wrapping a handler or sub-mux in `session.RequireAuth` (see `GET /admin`). Templ components read the user via `session.FromContext(ctx)` (returns `nil` when unauthenticated — safe on public pages).
- **`internal/middleware/`** — deliberately generic (`func(http.Handler) http.Handler`, config structs, no project imports) so it copies between projects unchanged. Each has tests. CORS exists but is opt-in.
- **`internal/view/`** — render-only templ. Its few globals (`GtagID`, `PixelID`, `TurnstileSiteKey`, `SiteName`) are set once at startup in `main.go`; templates never read env directly.

Data layer: migrations are embedded (`migrations/embed.go`) and run automatically at server startup and before seeding — a deploy is self-migrating, no separate migration step. SQLite runs in WAL mode with foreign keys on. See `POSTGRES.md` for the Postgres upgrade path.

## Conventions that bite

- **CSRF**: mutations (POST/PUT/DELETE) require a token. In templ forms include `@CSRFField()` (already in login/logout/contact). For htmx, send it via an `X-CSRF-Token` header.
- **htmx forms** post with `hx-post` + `hx-swap="outerHTML"`; handlers re-render just the form component with inline errors on failure. Handlers issue both `HX-Redirect` and `http.Redirect` so non-JS posts still work.
- **No registration flow** by design — admins are created only via `mage seed`. Login returns identical errors for unknown-email and wrong-password (no account enumeration) and has an extra strict per-IP limiter.
- **`SESSION_SECRET`** (≥32 bytes, HMAC/CSRF signing): production refuses to start without it; dev falls back to an ephemeral secret with a warning. Integrations (Postmark, Turnstile, analytics) degrade gracefully when their env vars are unset.
- **Tailwind version is pinned in two places** — `magefile.go` and `Dockerfile`. Keep them in sync when upgrading.

## Firefly stack skills

This repo vendors the Firefly agent skills under `.claude/skills/`. When writing or reviewing Go/templ/SQL here, consult `firefly-stack-agent` (stack conventions), `firefly-architect-agent` (boundary contracts before planning a feature), and `firefly-review-agent` (comment-vs-implementation review). The `impeccable`/`frontend-design` skills cover UI work.

## Deploy

Push to `main` → `.github/workflows/deploy.yml` builds a multi-stage Docker image, pushes to GHCR (tagged `latest` + commit SHA), syncs `docker-compose.prod.yml` to the Hetzner VPS, and restarts pinned to the SHA tag. Caddy on the host terminates TLS and proxies to `127.0.0.1:8080`. The SQLite file lives on the `app-data` volume at `/data/app.db`.
