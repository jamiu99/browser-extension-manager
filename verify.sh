#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
files=$(gofmt -l ./*.go internal cmd)
if [[ -n "$files" ]]; then
  printf 'Run gofmt on:\n%s\n' "$files" >&2
  exit 1
fi
for script in frontend/*.js; do
  node --check "$script"
done
go test ./internal/... ./cmd/...
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -tags production -ldflags '-H windowsgui' -o "$tmp/extension-manager.exe" .
for target in windows/amd64 darwin/arm64 darwin/amd64 linux/amd64; do
  GOOS="${target%/*}" GOARCH="${target#*/}" CGO_ENABLED=0 go build -trimpath -o "$tmp/config-cli" ./cmd/extension-manager-cli
  GOOS="${target%/*}" GOARCH="${target#*/}" CGO_ENABLED=0 go build ./internal/selfupdate
done
