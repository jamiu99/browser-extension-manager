#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
case "$(uname -s)" in
  Darwin) exec "build/bin/Browser Extension Manager.app/Contents/MacOS/extension-manager" ;;
  MINGW*|MSYS*|CYGWIN*) exec build/bin/extension-manager.exe ;;
  *) exec build/bin/extension-manager ;;
esac
