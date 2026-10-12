#!/usr/bin/env bash
# Runs the Gherkin acceptance suite against the service running in a container:
#   1. rebuild the application (a static Linux binary for the Docker engine's architecture)
#   2. build it into the scheduler:local image
#   3. docker compose up the app container and a fresh MySQL, waiting until both are healthy
#      (the app is healthy when GET /healthz answers 200, which needs MySQL)
#   4. run test/acceptance against it over HTTP. GODOG_TAGS and GODOG_FORMAT pass through;
#      extra arguments go to go test (e.g. -v)
#   5. always tear both containers down, discarding the database, pass or fail
# Every run therefore starts from schema.sql + data.sql and leaves nothing behind.
set -euo pipefail
cd "$(dirname "$0")/.."

APP_PORT="${APP_PORT:-8080}"
BASE_URL="http://127.0.0.1:${APP_PORT}"

show_app_logs() {
  echo "---- app container log (last 100 lines) ----" >&2
  docker compose logs --no-color --tail=100 app >&2 || true
}

teardown() {
  echo "==> Tearing down the app and MySQL"
  docker compose down -v --remove-orphans >/dev/null 2>&1 || true
}

./scripts/build-image.sh                     # steps 1 and 2

docker compose down -v --remove-orphans >/dev/null 2>&1 || true   # in case an earlier run was interrupted
trap teardown EXIT

echo "==> 3/4 Starting the app and MySQL, waiting until healthy"
if ! docker compose up -d --wait; then
  echo "The app did not become healthy." >&2
  show_app_logs
  exit 1
fi

echo "==> 4/4 Running the acceptance suite against ${BASE_URL}"
status=0
SCHEDULER_BASE_URL="${BASE_URL}" go test ./test/acceptance/ -run TestFeatures -count=1 -timeout 20m "$@" || status=$?
if [[ ${status} -ne 0 ]]; then
  echo "Acceptance suite failed." >&2
  show_app_logs
fi
exit ${status}
