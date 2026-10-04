#!/usr/bin/env sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "ERRO: execute como root: sh offline/install.sh"
  exit 1
fi

REPO_ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
INSTALL_DIR="${NVR_INSTALL_DIR:-/opt/nvr}"
DATA_DIR="${NVR_DATA_DIR:-/var/lib/nvr}"

case "$(uname -m)" in
  x86_64|amd64) PLATFORM="linux-amd64" ;;
  aarch64|arm64) PLATFORM="linux-arm64" ;;
  *)
    echo "ERRO: arquitetura não suportada: $(uname -m)"
    exit 1
    ;;
esac

SOURCE="$REPO_ROOT/offline/bin/$PLATFORM/nvr"
CHECKSUM="$SOURCE.sha256"
PLATE_SOURCE="$REPO_ROOT/offline/plugins/plate-ocr/$PLATFORM/plate-ocr"
PLATE_CHECKSUM="$PLATE_SOURCE.sha256"

if [ ! -f "$SOURCE" ]; then
  echo "ERRO: binário offline não encontrado: $SOURCE"
  echo "O checkout precisa conter offline/bin/$PLATFORM/nvr."
  exit 1
fi

if [ -f "$CHECKSUM" ] && command -v sha256sum >/dev/null 2>&1; then
  (cd "$(dirname "$SOURCE")" && sha256sum -c "$(basename "$CHECKSUM")")
fi

if [ ! -f "$PLATE_SOURCE" ]; then
  echo "ERRO: Plate OCR offline não encontrado: $PLATE_SOURCE"
  echo "O checkout precisa conter offline/plugins/plate-ocr/$PLATFORM/plate-ocr."
  exit 1
fi
if [ -f "$PLATE_CHECKSUM" ] && command -v sha256sum >/dev/null 2>&1; then
  (cd "$(dirname "$PLATE_SOURCE")" && sha256sum -c "$(basename "$PLATE_CHECKSUM")")
fi

if ! id nvr >/dev/null 2>&1; then
  if command -v useradd >/dev/null 2>&1; then
    useradd --system --home "$DATA_DIR" --shell /usr/sbin/nologin nvr
  else
    echo "ERRO: useradd não disponível e usuário nvr não existe."
    exit 1
  fi
fi

install -d -m 0755 "$INSTALL_DIR" "$INSTALL_DIR/runtime" "$INSTALL_DIR/plugins" "$INSTALL_DIR/plugins/plate-ocr"
install -d -o nvr -g nvr -m 0750 "$DATA_DIR" "$DATA_DIR/recordings"
install -d -o nvr -g nvr -m 0750 /var/lib/plate-ocr /var/lib/plate-ocr/spool
install -d -m 0755 /etc/nvr /etc/nvr/plugins
install -m 0755 "$SOURCE" "$INSTALL_DIR/nvr"
install -m 0755 "$PLATE_SOURCE" "$INSTALL_DIR/plugins/plate-ocr/plate-ocr"
install -m 0644 "$REPO_ROOT/deploy/nvr.service" /etc/systemd/system/nvr.service
install -m 0644 "$REPO_ROOT/deploy/nvr-plate-ocr.service" /etc/systemd/system/nvr-plate-ocr.service

if [ ! -f /etc/nvr/plugins/plate-ocr.json ]; then
  install -m 0640 "$REPO_ROOT/deploy/plate-ocr.json" /etc/nvr/plugins/plate-ocr.json
fi
chown root:nvr /etc/nvr/plugins/plate-ocr.json

systemctl daemon-reload
systemctl enable nvr.service nvr-plate-ocr.service
systemctl restart nvr.service
systemctl restart nvr-plate-ocr.service || true

echo "NVR + Plate OCR instalados sem acesso à Internet."
echo "NVR: systemctl status nvr --no-pager"
echo "OCR: systemctl status nvr-plate-ocr --no-pager"
echo "Token admin: $DATA_DIR/admin.token"
echo "Token plugin: $DATA_DIR/plugin.token"
