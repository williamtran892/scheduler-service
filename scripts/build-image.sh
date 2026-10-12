#!/usr/bin/env bash
# Builds a static Linux binary for the Docker engine's architecture into bin/linux/scheduler,
# then packages it into the scheduler:local image (Dockerfile copies the binary; it does not compile).
set -euo pipefail
cd "$(dirname "$0")/.."

ARCH="$(docker version --format '{{.Server.Arch}}')"
echo "==> 1/4 Rebuilding the application (linux/${ARCH})"
CGO_ENABLED=0 GOOS=linux GOARCH="${ARCH}" go build -trimpath -o bin/linux/scheduler ./cmd/scheduler
echo "==> 2/4 Building the image scheduler:local"
docker compose build app
