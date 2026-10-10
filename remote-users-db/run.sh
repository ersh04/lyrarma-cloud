#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd -- "$SCRIPT_DIR"
umask 077

for dependency in docker openssl; do
    if ! command -v "$dependency" >/dev/null 2>&1; then
        printf 'ERROR: command %s not found. Install it and add it to PATH.\n' "$dependency" >&2
        exit 1
    fi
done

if ! docker compose version >/dev/null 2>&1; then
    printf 'ERROR: Docker Compose is not available. Install the Compose plugin for Docker.\n' >&2
    exit 1
fi

if [ ! -f .env ]; then
    database_password="$(openssl rand -hex 32)"
    {
        printf 'USERS_DB_PASSWORD=%s\n' "$database_password"
        printf 'USERS_DB_BIND_ADDRESS=0.0.0.0\n'
        printf 'USERS_DB_PORT=5432\n'
    } > .env
    printf 'Created .env with a generated database password.\n'
fi

printf 'Starting users PostgreSQL and waiting for readiness...\n'
if ! docker compose -f compose.yaml up -d --wait --wait-timeout 60; then
    printf 'ERROR: Users PostgreSQL is not running or not ready. Check the Docker output above.\n' >&2
    exit 1
fi

printf 'Users PostgreSQL started. Connection settings are stored in .env.\n'
