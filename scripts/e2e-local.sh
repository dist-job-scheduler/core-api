#!/usr/bin/env bash
#
# Run E2E tests locally. Starts Postgres, server, and scheduler automatically.
# Usage: ./scripts/e2e-local.sh
#
set -euo pipefail

cleanup() {
  echo "==> Stopping background processes..."
  kill "$SERVER_PID" "$SCHEDULER_PID" 2>/dev/null || true
}
trap cleanup EXIT

echo "==> Ensuring Postgres is running..."
docker compose up -d postgres
sleep 2

echo "==> Running migrations..."
goose -dir ./migrations postgres "$DATABASE_URL" up

echo "==> Seeding test data..."
go run ./cmd/seed

echo "==> Building binaries..."
go build -o ./bin/server ./cmd/server
go build -o ./bin/scheduler ./cmd/scheduler

echo "==> Starting server..."
./bin/server &
SERVER_PID=$!

echo "==> Starting scheduler..."
./bin/scheduler &
SCHEDULER_PID=$!

echo "==> Waiting for server to be ready..."
for i in $(seq 1 20); do
  if curl -sf http://localhost:8080/jobs -H "Authorization: Bearer test" -o /dev/null 2>/dev/null; then
    break
  fi
  sleep 1
done

echo "==> Generating auth token..."
E2E_AUTH_TOKEN=$(go run ./scripts/gentoken.go)
export E2E_AUTH_TOKEN
export E2E_BASE_URL="http://localhost:8080"

echo "==> Running E2E tests..."
go test -race -count=1 -timeout=300s -tags=e2e ./tests/e2e/...

echo "==> E2E tests passed!"
