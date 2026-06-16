#!/usr/bin/env bash
set -euo pipefail

packages="$(go list ./...)"
test -n "$packages"
go test ./...
go build ./...
