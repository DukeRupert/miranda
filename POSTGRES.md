# PostgreSQL Upgrade Path

This template defaults to **SQLite** via `modernc.org/sqlite` (pure Go, no CGo). If your project outgrows SQLite or requires features like full-text search, JSON operators, or concurrent writes from multiple processes, follow the steps below to switch to PostgreSQL.

## Steps

### 1. Swap the driver

Replace the SQLite driver with `pgx`:

```bash
go get github.com/jackc/pgx/v5/stdlib
```

In `cmd/server/main.go`, change the import and `sql.Open` call:

```diff
- _ "modernc.org/sqlite"
+ _ "github.com/jackc/pgx/v5/stdlib"
```

```diff
- db, err := sql.Open("sqlite", cfg.DBPath)
+ db, err := sql.Open("pgx", cfg.DatabaseURL)
```

Remove the SQLite PRAGMAs (`journal_mode=WAL`, `foreign_keys=ON`) — PostgreSQL does not need them.

### 2. Update config

In `internal/config/config.go`, replace `DBPath` with `DatabaseURL`:

```go
DatabaseURL string
```

```go
DatabaseURL: os.Getenv("DATABASE_URL"),
```

In `.env`:

```
DATABASE_URL=postgres://user:password@localhost:5432/myapp?sslmode=disable
```

### 3. Update sqlc.yaml

```diff
- engine: "sqlite"
+ engine: "postgresql"
```

### 4. Update goose dialect

In `cmd/server/main.go`:

```diff
- goose.SetDialect("sqlite3")
+ goose.SetDialect("postgres")
```

Update the Mage targets in `magefile.go`:

```diff
- return sh.Run("goose", "-dir", "migrations", "sqlite3", dbPath(), "up")
+ return sh.Run("goose", "-dir", "migrations", "postgres", os.Getenv("DATABASE_URL"), "up")
```

### 5. Update migrations (if needed)

The included migrations use standard SQL that works in both SQLite and PostgreSQL. If you've added SQLite-specific syntax (e.g., `AUTOINCREMENT`), adjust to PostgreSQL equivalents (e.g., `SERIAL` or `GENERATED ALWAYS AS IDENTITY`).

### 6. Regenerate

```bash
sqlc generate
```

### 7. Update the seed command

Apply the same driver and connection changes to `cmd/seed/main.go`.

## Docker Compose

Add a PostgreSQL service to `docker-compose.yml`:

```yaml
services:
  db:
    image: postgres:17-alpine
    environment:
      POSTGRES_USER: app
      POSTGRES_PASSWORD: secret
      POSTGRES_DB: myapp
    volumes:
      - pgdata:/var/lib/postgresql/data
    ports:
      - "5432:5432"

volumes:
  pgdata:
```

Then set `DATABASE_URL=postgres://app:secret@db:5432/myapp?sslmode=disable` in your app's environment.
