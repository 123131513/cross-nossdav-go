#!/usr/bin/env bash
set -e

export GOROOT=/usr/local/go
export PATH="$GOROOT/bin:$PATH"
export GOCACHE="${HOME}/.cache/go-build-1.24"

exec "$@"
