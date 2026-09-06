#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd -- "$SCRIPT_DIR"

for dependency in go docker; do
    if ! command -v "$dependency" >/dev/null 2>&1; then
        printf 'ERROR: command %s not found. Install it and add to PATH.\n' "$dependency" >&2
        exit 1
    fi
done

if ! docker compose version >/dev/null 2>&1; then
    printf 'ERROR: Docker Compose is not available. Install the Compose plugin for Docker.\n' >&2
    exit 1
fi

printf 'Starting PostgreSQL and waiting for readiness...\n'
if ! docker compose -f compose.db.yaml up -d --wait --wait-timeout 60; then
    printf 'ERROR: PostgreSQL is not running or not ready. Check the Docker output above.\n' >&2
    exit 1
fi

printf 'Starting Lyrarma Cloud. Press Ctrl+C to stop.\n'
exec go run ./src
