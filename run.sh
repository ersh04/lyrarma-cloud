#!/bin/bash

cd lyrarma-cloud-go-api

docker compose -f compose.db.yaml up -d

# Restart cloud if it need
PID=$(sudo lsof -t -i :8080)
if [ -n "$PID" ]; then
        sudo kill "$PID"
fi
nohup go run ./src > log 2>&1 &

echo 'Server started'
