#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
files=$(gofmt -l ./*.go internal/manager/*.go)
if [[ -n "$files" ]]; then
  printf 'Run gofmt on:\n%s\n' "$files" >&2
  exit 1
fi
node --check frontend/app.js
go test ./internal/...
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -tags production -ldflags '-H windowsgui' -o "$tmp/extension-manager.exe" .
