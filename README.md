# ForeignReader — Backend API

REST API backend for the ForeignReader iOS app. Handles authentication, contextual AI translation, reading session sync, and subscription management.

## Features

- JWT-based auth with Google and Apple Sign-In
- Contextual translation pipeline using OpenAI: returns translation, grammatical form, lemma, part of speech, and usage notes
- Reading position sync across sessions
- Stripe subscription billing and Apple In-App Purchase validation
- Monthly usage quota enforcement per subscription tier
- Rate limiting and entitlement checks
- Admin analytics endpoint

## Stack

- Go
- PostgreSQL (pgx/v5)
- OpenAI API
- Stripe
- Docker, Docker Compose
- Nginx (reverse proxy)

## Running locally

```bash
cp .env.example .env   # fill in required environment variables
docker compose -f docker-compose.dev.yml up
```

The API runs on `localhost:8080` by default.

Database migrations are in `migrations/` and run automatically on startup.

## Deployment

Deployed on DigitalOcean via Docker Compose behind Nginx. Production API is available at `api.foreignreader.io`.
