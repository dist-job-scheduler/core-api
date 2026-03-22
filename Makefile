.PHONY: build test test-int test-e2e test-all lint migrate migrate-reset install-hooks

# ── Build ─────────────────────────────────────────────
build:
	go build -v ./cmd/server/...
	go build -v ./cmd/scheduler/...

# ── Tests ─────────────────────────────────────────────
test:
	go test -race -count=1 -timeout=60s -short ./...

test-int:
	go test -race -count=1 -timeout=120s -tags=integration ./...

test-e2e:
	./scripts/e2e-local.sh

test-all: test test-int

# ── Lint ──────────────────────────────────────────────
lint:
	golangci-lint run ./...

# ── Pre-commit (lint + unit tests) ────────────────────
pre-commit: lint test

# ── Git hooks ─────────────────────────────────────────
install-hooks:
	cp scripts/pre-commit .git/hooks/pre-commit
	chmod +x .git/hooks/pre-commit
	@echo "Pre-commit hook installed."

# ── Database ──────────────────────────────────────────
migrate:
	goose -dir ./migrations postgres "$$DATABASE_URL" up

migrate-reset:
	goose -dir ./migrations postgres "$$DATABASE_URL" reset
	goose -dir ./migrations postgres "$$DATABASE_URL" up

migrate-status:
	goose -dir ./migrations postgres "$$DATABASE_URL" status

# ── Dev ───────────────────────────────────────────────
seed:
	go run ./cmd/seed

run-server:
	go run ./cmd/server

run-scheduler:
	go run ./cmd/scheduler
