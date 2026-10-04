#!/usr/bin/env sh
set -eu

echo "[offline] running tests with network package resolution disabled..."
GOPROXY=off GOSUMDB=off GOFLAGS="-mod=mod" go test ./...

echo "[offline] checking module graph..."
modules="$(GOPROXY=off GOSUMDB=off go list -m all)"
count="$(printf '%s\n' "$modules" | wc -l | tr -d ' ')"
if [ "$count" != "1" ]; then
  echo "ERROR: external Go modules detected:"
  printf '%s\n' "$modules"
  exit 1
fi

echo "[offline] checking embedded web assets..."
if grep -R -nE '(src|href)=["'"'"']https?://' internal/webui/static 2>/dev/null; then
  echo "ERROR: remote web resource detected"
  exit 1
fi

echo "[offline] checking forbidden production download commands..."
if grep -R -nE '(curl|wget|docker[[:space:]]+pull|go[[:space:]]+get|npm[[:space:]]+install|pip[[:space:]]+install)[[:space:]]+https?://'   cmd internal deploy runtime 2>/dev/null; then
  echo "ERROR: runtime download command detected"
  exit 1
fi

echo "[offline] OK: core has no online build/runtime dependency."
