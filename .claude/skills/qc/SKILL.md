---
name: qc
description: "Run pre-deploy quality control check for a standard-tier Firefly client site. Use when the site is nearing completion and needs a final review before deployment."
disable-model-invocation: true
allowed-tools: Read Grep Glob Bash
argument-hint: "[client name]"
---

# Firefly Software — Pre-Deploy Quality Control Skill
## Standard Tier

Run this skill when a standard-tier client site is nearing completion. Work through each section in order, recording results, then produce a structured tiered report at the end.

Do not attempt to fix issues automatically. Report them clearly and wait for instruction.

---

## Tiers

- ✅ **PASS** — requirement is met
- ⚠️ **WARN** — advisory, should be addressed but does not block deploy
- ❌ **FAIL** — must be resolved before deploy

If any FAILs exist at the end, state clearly: **"This site is not ready to deploy."**

---

## Section 1 — Project Structure

Verify the following files and directories exist:

| Path | Tier if missing |
|---|---|
| `cmd/server/main.go` | ❌ FAIL |
| `internal/config/config.go` | ❌ FAIL |
| `internal/handler/home.go` | ❌ FAIL |
| `internal/handler/contact.go` | ❌ FAIL |
| `internal/mail/postmark.go` | ❌ FAIL |
| `web/static/css/site.css` | ❌ FAIL |
| `web/static/js/htmx.min.js` | ❌ FAIL |
| `web/static/js/alpine.min.js` | ❌ FAIL |
| `internal/view/layout.templ` | ❌ FAIL |
| `internal/view/home.templ` | ❌ FAIL |
| `internal/view/contact.templ` | ❌ FAIL |
| `.env.example` | ❌ FAIL |
| `Dockerfile` | ❌ FAIL |
| `docker-compose.yml` | ❌ FAIL |
| `magefile.go` | ⚠️ WARN |
| `internal/view/nav.templ` | ⚠️ WARN |
| `internal/view/footer.templ` | ⚠️ WARN |
| `web/static/robots.txt` | ⚠️ WARN |
| `web/static/sitemap.xml` | ⚠️ WARN |
| `web/static/favicon.ico` | ⚠️ WARN |

---

## Section 2 — Build Verification

Run each command and verify it succeeds without errors:

```bash
mage build
```

Check:
- `mage buildcss` produces `web/static/css/site.css` — ❌ FAIL if missing
- `mage buildgo` produces `bin/server` — ❌ FAIL if missing
- `go vet ./...` reports no issues — ⚠️ WARN if issues found
- `golangci-lint run` reports no errors — ⚠️ WARN if issues found

---

## Section 3 — Functionality

### 3.1 Routing

Inspect `cmd/server/main.go` and verify the following routes are registered:

| Route | Tier if missing |
|---|---|
| `GET /` | ❌ FAIL |
| `GET /contact` | ❌ FAIL |
| `POST /contact` | ❌ FAIL |
| `GET /static/` | ❌ FAIL |
| `GET /robots.txt` or served via static | ⚠️ WARN |
| `GET /sitemap.xml` or served via static | ⚠️ WARN |

### 3.2 Contact Form

Inspect `internal/handler/contact.go`:

| Check | Tier if failing |
|---|---|
| POST handler calls `r.ParseForm()` | ❌ FAIL |
| Server-side validation present for name, email, and message fields | ❌ FAIL |
| Email field validated for format (contains `@` at minimum) | ⚠️ WARN |
| On validation failure, form re-renders with errors and preserved field values | ⚠️ WARN |
| On success, Postmark send is called — not a stub or TODO | ❌ FAIL |
| Request body size limited via `http.MaxBytesHandler` or equivalent | ⚠️ WARN |

### 3.3 Postmark Integration

Inspect `internal/mail/postmark.go`:

| Check | Tier if failing |
|---|---|
| Postmark API key read from environment, not hardcoded | ❌ FAIL |
| From and To addresses configurable via env or config | ⚠️ WARN |
| Send errors are logged, not silently swallowed | ⚠️ WARN |
| `.env.example` documents `POSTMARK_API_KEY`, `MAIL_FROM`, `MAIL_TO` | ⚠️ WARN |

### 3.4 Config

Inspect `internal/config/config.go`:

| Check | Tier if failing |
|---|---|
| Missing required env vars fail loudly, not silently | ❌ FAIL |
| No secrets or API keys set as hardcoded defaults | ❌ FAIL |

---

## Section 4 — SEO

Inspect all `.templ` files in `internal/view/`.

### 4.1 Per-Page Requirements

For each page (home, contact, and any additional pages):

| Check | Tier if failing |
|---|---|
| Unique `<title>` tag present, under 60 characters | ❌ FAIL |
| `<meta name="description">` with content, under 160 characters | ❌ FAIL |
| Single `<h1>` tag per page | ❌ FAIL |
| Logical heading hierarchy (H1 → H2 → H3, no skipped levels) | ⚠️ WARN |
| `<link rel="canonical">` tag present | ⚠️ WARN |

### 4.2 Global Requirements

Inspect `internal/view/layout.templ`:

| Check | Tier if failing |
|---|---|
| `<html lang="en">` (or appropriate language code) | ❌ FAIL |
| `<meta charset="UTF-8">` present | ❌ FAIL |
| `<meta name="viewport" content="width=device-width, initial-scale=1.0">` present | ❌ FAIL |
| Open Graph tags (`og:title`, `og:description`, `og:url`) present | ⚠️ WARN |

### 4.3 Robots and Sitemap

Inspect `web/static/robots.txt`:

| Check | Tier if failing |
|---|---|
| `robots.txt` exists and allows crawling | ❌ FAIL if blocks all crawlers |
| Does not contain `noindex` directives left from development | ❌ FAIL if present |
| References sitemap URL | ⚠️ WARN |

Inspect `web/static/sitemap.xml`:

| Check | Tier if failing |
|---|---|
| Sitemap lists all public pages | ⚠️ WARN |
| URLs use `https://` not `http://` | ⚠️ WARN |
| URLs use the production domain, not `localhost` | ❌ FAIL if localhost present |

---

## Section 5 — Accessibility (WCAG 2.2)

Inspect all `.templ` files in `internal/view/`.

### 5.1 Semantic Structure

| Check | Tier if failing |
|---|---|
| Page uses `<header>`, `<nav>`, `<main>`, `<footer>` semantic elements | ⚠️ WARN |
| Layout does not rely solely on `<div>` where semantic elements apply | ⚠️ WARN |

### 5.2 Images

| Check | Tier if failing |
|---|---|
| Every `<img>` has an `alt` attribute | ❌ FAIL |
| Decorative images use `alt=""` | ⚠️ WARN |

### 5.3 Forms

| Check | Tier if failing |
|---|---|
| Every form input has an associated `<label>` via `for`/`id` pairing | ❌ FAIL |
| Required fields marked with `required` attribute | ⚠️ WARN |
| Error messages associated with their field via `aria-describedby` or proximity | ⚠️ WARN |
| Submit button has descriptive text (not just "Submit") | ⚠️ WARN |

### 5.4 Navigation and Keyboard Access

| Check | Tier if failing |
|---|---|
| All interactive elements reachable via keyboard | ⚠️ WARN |
| Focus styles visible — not removed via `outline: none` without a replacement | ⚠️ WARN |
| Skip-to-content link present for keyboard users | ⚠️ WARN |

---

## Section 6 — Security

### 6.1 Security Headers

Inspect `cmd/server/main.go` or any middleware. These may also be set in Caddy config — verify at least one approach is in place per header.

| Header | Tier if missing |
|---|---|
| `X-Content-Type-Options: nosniff` | ⚠️ WARN |
| `X-Frame-Options: DENY` | ⚠️ WARN |
| `Referrer-Policy: strict-origin-when-cross-origin` | ⚠️ WARN |
| `Content-Security-Policy` (basic policy) | ⚠️ WARN |

Note: `Strict-Transport-Security` (HSTS) is handled by Caddy in the Firefly fleet — no need to set in Go.

### 6.2 CSRF

| Check | Tier if failing |
|---|---|
| Contact form POST uses CSRF token or htmx `HX-Request` header validation | ⚠️ WARN |

Note: For standard-tier sites with no auth and a simple contact form, validating the `HX-Request` header on POST is acceptable. Flag for review if the form handles sensitive data.

### 6.3 Input Safety

| Check | Tier if failing |
|---|---|
| No secrets or API keys hardcoded anywhere in the codebase | ❌ FAIL |
| No `TODO` comments remaining in handler files | ⚠️ WARN |
| No `localhost` or `127.0.0.1` references in templates or config | ❌ FAIL |
| Form inputs trimmed before validation | ⚠️ WARN |

---

## Section 7 — Deployment Readiness

### 7.1 Environment

Inspect `.env.example` and `.gitignore`:

| Check | Tier if failing |
|---|---|
| All required env vars documented in `.env.example` | ❌ FAIL |
| No actual secrets in `.env.example` — placeholder values only | ❌ FAIL |
| `.env` listed in `.gitignore` | ❌ FAIL |
| `web/static/css/site.css` listed in `.gitignore` | ⚠️ WARN |
| `bin/` listed in `.gitignore` | ⚠️ WARN |
| `tailwind/tailwindcss` listed in `.gitignore` | ⚠️ WARN |

### 7.2 Docker

Inspect `Dockerfile` and `docker-compose.yml`:

| Check | Tier if failing |
|---|---|
| `Dockerfile` uses a multi-stage build | ⚠️ WARN |
| Runtime stage uses a minimal base image | ⚠️ WARN |
| `docker-compose.yml` does not hardcode production secrets | ❌ FAIL |
| Port mapping matches `PORT` env var | ⚠️ WARN |

### 7.3 Client Content

| Check | Tier if failing |
|---|---|
| All placeholder copy replaced ("Lorem ipsum", "Your Company", "Coming Soon", etc.) | ❌ FAIL |
| Client's actual business name in `<title>` tags | ❌ FAIL |
| Client's actual contact information in footer or contact page | ⚠️ WARN |
| Favicon is client-specific, not the default template favicon | ⚠️ WARN |
| Social media links (if present) point to client's actual profiles | ⚠️ WARN |

---

## Report Format

Produce the final report in this exact format:

```
## Pre-Deploy QC Report
**Client:** [client name]
**Date:** [date]
**Prepared by:** Claude Code

---

### ❌ Failures (must fix before deploy)
- [Section X.X] Description of issue

### ⚠️ Warnings (advisory — should fix)
- [Section X.X] Description of issue

### ✅ Passed Sections
- Section 1 — Project Structure
- Section 2 — Build Verification
- [etc.]

---

**Deploy status:** READY / READY WITH WARNINGS / NOT READY
```

**Status definitions:**
- **NOT READY** — one or more ❌ FAILs exist
- **READY WITH WARNINGS** — no FAILs, one or more ⚠️ WARNs exist
- **READY** — no FAILs and no WARNs
