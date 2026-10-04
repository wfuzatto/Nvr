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

if [ ! -f "$SOURCE" ]; then
  echo "ERRO: binário offline não encontrado: $SOURCE"
  echo "O checkout precisa conter offline/bin/$PLATFORM/nvr."
  exit 1
fi

if [ -f "$CHECKSUM" ] && command -v sha256sum >/dev/null 2>&1; then
  (cd "$(dirname "$SOURCE")" && sha256sum -c "$(basename "$CHECKSUM")")
fi

if ! id nvr >/dev/null 2>&1; then
  if command -v useradd >/dev/null 2>&1; then
    useradd --system --home "$DATA_DIR" --shell /usr/sbin/nologin nvr
  else
    echo "ERRO: useradd não disponível e usuário nvr não existe."
    exit 1
  fi
fi

install -d -m 0755 "$INSTALL_DIR" "$INSTALL_DIR/runtime"
install -d -o nvr -g nvr -m 0750 "$DATA_DIR" "$DATA_DIR/recordings"
install -m 0755 "$SOURCE" "$INSTALL_DIR/nvr"
install -m 0644 "$REPO_ROOT/deploy/nvr.service" /etc/systemd/system/nvr.service

systemctl daemon-reload
systemctl enable nvr.service
systemctl restart nvr.service

echo "NVR instalado sem acesso à Internet."
echo "Status: systemctl status nvr --no-pager"
echo "Token: $DATA_DIR/admin.token"
