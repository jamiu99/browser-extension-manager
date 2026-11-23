#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
target=$1
archive=$2
tags=${3:-}
version=${GITHUB_REF_NAME:-v0.1.1}
commit=$(git rev-parse HEAD)
built_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
wails_version=$(go list -m -f '{{.Version}}' github.com/wailsapp/wails/v2)
go run "github.com/wailsapp/wails/v2/cmd/wails@$wails_version" build -platform "$target" -s -skipbindings -m -trimpath -tags "$tags" \
  -ldflags "-X main.version=$version -X main.commit=$commit -X main.builtAt=$built_at"

mkdir -p dist
bundle=$(mktemp -d)
trap 'rm -rf "$bundle"' EXIT
cp docs/update-feed.md "$bundle"
printf '{"version":"%s","commit":"%s","builtAt":"%s"}\n' "$version" "$commit" "$built_at" > "$bundle/release.json"
{
  cat "$(go env GOROOT)/LICENSE"
  go mod download
  go list -m -f '{{.Path}}|{{.Version}}|{{.Dir}}' all | while IFS='|' read -r name dependency directory; do
    if [[ -n "$dependency" && -n "$directory" ]]; then
      find "$directory" -maxdepth 1 -type f \( -iname 'license*' -o -iname 'copying*' -o -iname 'notice*' \) -print0 |
        while IFS= read -r -d '' notice; do
          printf '\n\n--- %s %s — %s ---\n\n' "$name" "$dependency" "$(basename "$notice")"
          cat "$notice"
        done
    fi
  done
} > "$bundle/THIRD-PARTY-NOTICES.txt"
file="$PWD/dist/extension-manager-$version-$archive"
case "$target" in
  windows/*)
    cp build/bin/extension-manager.exe "$bundle"
    BUNDLE_WIN=$(cygpath -aw "$bundle") ARCHIVE_WIN=$(cygpath -aw "$file.zip") \
      powershell.exe -NoProfile -NonInteractive -Command '$ErrorActionPreference = "Stop"; Compress-Archive -Path (Join-Path $env:BUNDLE_WIN "*") -DestinationPath $env:ARCHIVE_WIN -Force'
    ;;
  darwin/*)
    cp -R "build/bin/Browser Extension Manager.app" "$bundle"
    ditto -c -k --sequesterRsrc --keepParent "$bundle/Browser Extension Manager.app" "$file.zip"
    (cd "$bundle" && zip -q "$file.zip" release.json update-feed.md THIRD-PARTY-NOTICES.txt)
    ;;
  linux/*)
    cp build/bin/extension-manager "$bundle"
    tar -czf "$file.tar.gz" -C "$bundle" extension-manager release.json update-feed.md THIRD-PARTY-NOTICES.txt
    ;;
esac
