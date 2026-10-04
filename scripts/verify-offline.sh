#!/usr/bin/env sh
set -eu

echo "[offline] verifying vendored Go dependencies..."
test -f vendor/modules.txt
grep -q '^# github.com/pion/webrtc/v4 v4.2.22$' vendor/modules.txt

echo "[offline] running tests with all module downloads disabled..."
GOPROXY=off GOSUMDB=off GOFLAGS="-mod=vendor" go test ./...

echo "[offline] checking vendored module manifest..."
if grep -q '^# github.com/pion/webrtc/v4 v' vendor/modules.txt && ! grep -q '^# github.com/pion/webrtc/v4 v4.2.22$' vendor/modules.txt; then
  echo "ERROR: unexpected Pion WebRTC version in vendor/modules.txt"
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

echo "[offline] OK: runtime and build use only vendored/embedded dependencies."
