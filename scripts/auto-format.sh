#!/usr/bin/env bash
# Auto-format a Go file with gofmt + goimports.
# Usage: scripts/auto-format.sh <file.go>
#
# Dependencies: gofmt (Go toolchain), goimports (golang.org/x/tools/cmd/goimports).
# Install with: go install golang.org/x/tools/cmd/goimports@latest
set -e

FILE="$1"

if [ -z "$FILE" ]; then
  echo "Usage: $0 <file.go>" >&2
  exit 1
fi

case "$FILE" in
  vendor/*|magefiles/*) exit 0 ;;
esac

case "$FILE" in
  *.go) ;;
  *) exit 0 ;;
esac

if ! command -v goimports >/dev/null 2>&1; then
  echo "goimports not found in PATH; install with: go install golang.org/x/tools/cmd/goimports@latest" >&2
  exit 1
fi

gofmt -w "$FILE"
goimports -w -local github.com/pfrack/freedius "$FILE"
