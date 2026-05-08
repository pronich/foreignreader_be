# CLAUDE.md — foreignreader_be

Backend API for ForeignReader. Go service behind Nginx, backed by PostgreSQL, deployed on DigitalOcean.

## Git Rules

- Final branch: **`develop`**
- Push `develop` to remote, then open a PR manually via the git hosting interface.
- Never push directly to any branch other than `develop` or a feature branch.

## Build & Development

```bash
make dev              # go run . — hot-reload not included, restart manually
make dev-build        # compile to ./bin/app
make dev-run          # build + run in background, logs to ./bin/app.log
make dev-stop         # stop background process
make dev-logs         # tail ./bin/app.log
```

Local server runs on `http://localhost:8080` by default (`PORT` env var overrides).

### Docker Compose (full stack with Postgres + Nginx)

```bash
make compose-up       # builds image, runs postgres → migrate → api → nginx
make compose-down
make compose-logs
```

`APP_ENV` in `.env` controls which compose file is used: `production`/`prod` → `docker-compose.yml`; anything else → `docker-compose.dev.yml`. Dev compose exposes Postgres on `127.0.0.1:5433`.

### Database Migrations

Requires `golang-migrate` CLI (`brew install golang-migrate`). `DATABASE_URL` must be set in `.env`.

```bash
make migrate-up       # apply all pending migrations
make migrate-down     # roll back one migration
```

Migrations live in `migrations/`. In compose mode, the `migrate` service runs automatically before `api` starts.

## Architecture

Entry point: `main.go` → sets up config, DB pool, and HTTP server from `internal/server`.

### `internal/` Packages

| Package | Responsibility |
|---|---|
| `server` | HTTP router, middleware, handler wiring |
| `config` | Env-var loading and validation |
| `db` | Database connection pool |
| `auth` | JWT issuance/validation, Google + Apple Sign-In, session management |
| `translate` | Contextual translation via OpenAI; prompt loaded from `prompts/` |
| `onboardingsession` | Unauthenticated onboarding translation flow with rate limiting |
| `entitlement` | PRO subscription status resolution |
| `billing` | Stripe checkout, webhooks, subscription lifecycle |
| `appleiap` | Apple In-App Purchase receipt/transaction validation |
| `stripeapp` | Stripe-specific event handling |
| `readingposition` | Sync reading position across devices |
| `monthlycontexttranslation` | Free-tier usage tracking (100 translations/month default) |
| `ratelimit` | IP-based rate limiting middleware |
| `rateus` | Rate-us prompt tracking |
| `analytics` | Event ingestion (PostHog or compatible) |

### Key Environment Variables

See `docker-compose.dev.yml` for the full list. Required at minimum:
- `DATABASE_URL` / `DATABASE_URL_COMPOSE`
- `JWT_SECRET`
- `GOOGLE_SERVER_CLIENT_ID`
- `APPLE_AUDIENCE`
- `OPENAI_API_KEY`
- `STRIPE_SECRET_KEY`, `STRIPE_WEBHOOK_SECRET`, `STRIPE_PRICE_ID_PRO`

### Nginx

`nginx/` contains `default.conf` (production) and `default-dev.conf` (development). Nginx proxies port 80 → API port 8080 and handles TLS termination in production.

## Deployment

Runs on DigitalOcean. The `develop` branch is the staging point before production PRs. Production deploy is triggered by merging the PR on the remote hosting interface.
