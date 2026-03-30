.PHONY: dev-build dev dev-run dev-stop dev-logs docker-build docker-run compose-up compose-down compose-logs migrate-up migrate-down db-logs clean

DOCKER_IMAGE ?= foreignreader-be-api:local

dev-build:
	mkdir -p ./bin
	go build -o ./bin/app .

dev:
	go run .

dev-run: dev-build
	@mkdir -p ./bin
	@sh -c 'PORT="$${PORT:-8080}"; \
		./bin/app > ./bin/app.log 2>&1 & \
		echo $$! > ./bin/app.pid; \
		echo "started: pid=$$(cat ./bin/app.pid)"; \
		echo "api: http://localhost:$${PORT}"; \
		echo "health: http://localhost:$${PORT}/health"; \
		echo "logs: tail -f ./bin/app.log"; \
		echo "stop: make dev-stop"'

dev-stop:
	@sh -c 'if [ -f ./bin/app.pid ]; then \
		kill -TERM "$$(cat ./bin/app.pid)" 2>/dev/null || true; \
		rm -f ./bin/app.pid; \
		echo "stopped"; \
	else \
		echo "no pid file (./bin/app.pid)"; \
	fi'

dev-logs:
	@tail -f ./bin/app.log

docker-build:
	docker build -t $(DOCKER_IMAGE) .

docker-run: docker-build
	@sh -c 'PORT="$${PORT:-8080}"; docker run --rm -e PORT="$$PORT" -p "$$PORT:$$PORT" $(DOCKER_IMAGE)'

compose-up:
	docker compose up -d --build
	@sh -c 'PORT="$${PORT:-8080}"; \
		echo "started (compose)"; \
		echo "migrations: applied automatically by migrate service before api"; \
		echo "public: http://localhost:80"; \
		echo "health: http://localhost:80/health"; \
		echo "logs: make compose-logs"; \
		echo "stop: make compose-down"'

compose-down:
	docker compose down

compose-logs:
	docker compose logs -f --tail=200

# Requires golang-migrate CLI: https://github.com/golang-migrate/migrate
#   brew install golang-migrate
#   go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
# Loads DATABASE_URL from `.env` when present (same pattern as the app).
migrate-up:
	@set -a && [ -f .env ] && . ./.env && set +a; \
		test -n "$${DATABASE_URL}" || (echo "migrate-up: DATABASE_URL is not set (add it to .env)" && exit 1); \
		migrate -path migrations -database "$${DATABASE_URL}" up

migrate-down:
	@set -a && [ -f .env ] && . ./.env && set +a; \
		test -n "$${DATABASE_URL}" || (echo "migrate-down: DATABASE_URL is not set (add it to .env)" && exit 1); \
		migrate -path migrations -database "$${DATABASE_URL}" down 1

db-logs:
	docker compose logs -f --tail=200 postgres

clean:
	rm -rf ./bin

