#!/usr/bin/env sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

case "$(uname -m)" in
  x86_64|amd64) PLATFORM="linux-amd64" ;;
  aarch64|arm64) PLATFORM="linux-arm64" ;;
  *) echo "Arquitetura não suportada: $(uname -m)"; exit 1 ;;
esac

BIN="$ROOT/offline/bin/$PLATFORM/nvr"
[ -f "$BIN" ] || { echo "Binário ausente: $BIN"; exit 1; }

export NVR_DATA_DIR="${NVR_DATA_DIR:-$ROOT/data}"
export NVR_RUNTIME_DIR="${NVR_RUNTIME_DIR:-$ROOT/runtime}"
export NVR_STORAGE_DIR="${NVR_STORAGE_DIR:-$NVR_DATA_DIR/recordings}"
export NVR_LISTEN="${NVR_LISTEN:-0.0.0.0:8080}"

exec "$BIN"
