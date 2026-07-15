# Firefly Software — Advanced Tier Template Plan

## Overview

The advanced tier template is a superset of the standard tier. It targets client projects with dynamic needs: CMS-style content management, invoicing, authenticated admin areas, advanced forms, and similar data-driven features.

The standard tier template is built first and used as the base. The advanced tier adds a persistence layer, migrations, session-backed auth, and a working login flow on top of it.

---

## What Carries Over Unchanged from Standard

- Go 1.25 stdlib `net/http` router
- `templ` for server-side rendering (components are typed Go functions, compile-time errors, AI-agent friendly)
- Tailwind CSS via standalone CLI, compiled by Mage
- htmx and Alpine.js vendored into the repo
- Contact form with server-side validation and htmx inline feedback
- Postmark integration
- Env-based config loader
- Mage build system
- Devcontainer (with additions noted below)
- Dockerfile + docker-compose for Hetzner fleet deployment behind Caddy

---

## What Gets Added

### Database

- **Default:** SQLite via `modernc.org/sqlite` (pure Go, no CGo)
- **Upgrade path:** PostgreSQL — documented in `POSTGRES.md`, requires swapping driver and sqlc dialect only
- `sqlc` for type-safe query generation
- `goose` for migrations

### Migrations

Directory: `migrations/`

Included out of the box:

```
001_create_users.sql
002_create_sessions.sql
```

**Users table:** `id`, `email`, `password_hash`, `created_at`, `updated_at`

**Sessions table:** `id` (token), `user_id`, `expires_at`, `created_at`

### Auth

- SQLite-backed session storage (persistent across restarts)
- Session token stored in a secure HTTP-only cookie
- Middleware loads session from cookie and attaches user to request context
- Auth middleware wraps protected routes — projects extend by wrapping additional handlers

### Login Flow (included in template)

| Route | Handler |
|---|---|
| `GET /login` | Render login form |
| `POST /login` | Validate credentials, create session, set cookie, redirect |
| `POST /logout` | Destroy session, clear cookie, redirect |

Password hashing via `bcrypt` (`golang.org/x/crypto`).

---

## Directory Structure

```
advanced-template/
├── cmd/
│   └── server/
│       └── main.go
├── internal/
│   ├── config/
│   │   └── config.go          # env loader (extended for DB path)
│   ├── handler/
│   │   ├── home.go
│   │   ├── contact.go
│   │   └── auth.go            # login/logout handlers
│   ├── mail/
│   │   └── postmark.go
│   ├── db/                    # sqlc generated code
│   ├── store/                 # query wrappers / business logic
│   └── session/
│       └── session.go         # session middleware + context helpers
├── migrations/
│   ├── 001_create_users.sql
│   └── 002_create_sessions.sql
├── web/
│   ├── template/
│   │   ├── base.templ
│   │   ├── home.templ
│   │   ├── contact.templ
│   │   ├── login.templ
│   │   └── partials/
│   │       ├── nav.templ
│   │       ├── footer.templ
│   │       └── contact_form.templ
│   └── static/
│       ├── css/
│       │   └── site.css       # compiled Tailwind output (gitignored)
│       └── js/
│           ├── htmx.min.js
│           └── alpine.min.js
├── tailwind/
│   ├── input.css
│   ├── tailwind.config.js
│   └── tailwindcss            # standalone CLI binary (gitignored)
├── sqlc.yaml
├── magefile.go
├── .devcontainer/
│   ├── devcontainer.json
│   └── install.sh
├── .env.example
├── .gitignore
├── go.mod
├── Dockerfile
├── docker-compose.yml
└── POSTGRES.md
```

---

## Config Additions

Additional env vars beyond standard tier:

```
DB_PATH=./data/app.db          # SQLite database path
SESSION_SECRET=changeme        # used to sign session tokens
```

---

## Mage Targets (additions to standard)

| Target | Description |
|---|---|
| `mage generate` | Run `templ generate` then `sqlc generate` |
| `mage migrateup` | Run all pending goose migrations |
| `mage migratedown` | Roll back the last migration |
| `mage migratestatus` | Show current migration state |
| `mage createmigration` | Scaffold a new goose migration file |
| `mage generate` | Run `templ generate` and `sqlc generate` |

---

## Devcontainer Additions

Additional tools installed in `install.sh` beyond standard tier:

- `templ` — component code generator (`go install github.com/a-h/templ/cmd/templ@latest`)
- `goose` — migration runner
- `sqlc` — query code generator

---

## PostgreSQL Upgrade Path

Documented in `POSTGRES.md`. Steps:

1. Swap `modernc.org/sqlite` driver for `lib/pq` or `pgx`
2. Change `sqlc.yaml` engine from `sqlite` to `postgresql`
3. Update goose driver in `main.go`
4. Set `DATABASE_URL` in `.env`
5. Re-run `sqlc generate`

No migration files need to change — goose SQL syntax is compatible between SQLite and Postgres for standard DDL.

---

## Build Order

1. Fork standard tier template into `advanced-template` repo
2. Add devcontainer tool additions (`templ`, `goose`, `sqlc`)
3. Add config additions (`DB_PATH`, `SESSION_SECRET`)
4. Add goose + sqlc setup (`sqlc.yaml`, `migrations/`)
5. Add `internal/db/` and `internal/store/` with sqlc generated queries
6. Add `internal/session/` middleware
7. Add auth handlers (`login`, `logout`)
8. Add login template22
9. Add Mage migration targets
10. Wire everything into `main.go`
11. Write `POSTGRES.md`

---

## What This Template Is Not

- Not a multi-tenant SaaS scaffold (see Hiri for that)
- Not a CMS with a content editing UI (that is a project-level concern)
- Not opinionated about authorization beyond basic session auth — role-based access control is left to the project