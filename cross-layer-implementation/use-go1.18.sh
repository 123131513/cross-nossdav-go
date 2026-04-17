#!/usr/bin/env bash
set -e

export GOROOT=/home/hellodaniel0/sdk/go1.18/usr/lib/go-1.18
export PATH="$GOROOT/bin:$PATH"
export GOCACHE="${HOME}/.cache/go-build-1.18"

exec "$@"
