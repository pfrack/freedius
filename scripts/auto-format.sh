#!/usr/bin/env bash
# Auto-format a Go file with gofmt + goimports.
# Usage: scripts/auto-format.sh <file.go>
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

gofmt -w "$FILE"
goimports -w -local github.com/pfrack/freedius "$FILE"
