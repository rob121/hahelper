#!/usr/bin/env bash
# Deploy hahelper to a Linux host over SSH.
# Usage: ./scripts/deploy.sh user@host
set -euo pipefail

TARGET="${1:?usage: $0 user@host}"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

REMOTE_BIN="/usr/local/bin/hahelper"
REMOTE_ETC="/etc/hahelper"
REMOTE_LIB="/var/lib/hahelper"
SERVICE_NAME="hahelper.service"

echo "==> Target: $TARGET"

ARCH="$(ssh -o BatchMode=yes "$TARGET" 'uname -m')"
case "$ARCH" in
  x86_64|amd64) GOARCH=amd64 ;;
  aarch64|arm64) GOARCH=arm64 ;;
  armv7l|armv6l) GOARCH=arm GOARM=7 ;;
  *)
    echo "unsupported remote architecture: $ARCH" >&2
    exit 1
    ;;
esac

export GOOS=linux
export GOARCH
if [[ "$GOARCH" == "arm" ]]; then
  export GOARM="${GOARM:-7}"
fi
echo "==> Building linux/${GOARCH}..."
mkdir -p "$ROOT/.deploy"
go build -o "$ROOT/.deploy/hahelper" ./cmd/hahelper

chmod 755 "$ROOT/.deploy/hahelper"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
cp "$ROOT/.deploy/hahelper" "$TMP/hahelper"
cp "$ROOT/deploy/hahelper.service" "$TMP/"
cp "$ROOT/deploy/config.json.example" "$TMP/"
cp "$ROOT/deploy/env.example" "$TMP/"

echo "==> Uploading..."
scp -q "$TMP/hahelper" "$TARGET:/tmp/hahelper.new"
scp -q "$TMP/hahelper.service" "$TARGET:/tmp/hahelper.service"
scp -q "$TMP/config.json.example" "$TARGET:/tmp/hahelper.config.json.example"
scp -q "$TMP/env.example" "$TARGET:/tmp/hahelper.env.example"

echo "==> Installing on remote (sudo)..."
ssh -t "$TARGET" "sudo bash -s" <<'REMOTE'
set -euo pipefail
REMOTE_BIN="/usr/local/bin/hahelper"
REMOTE_ETC="/etc/hahelper"
REMOTE_LIB="/var/lib/hahelper"

if ! id -u hahelper >/dev/null 2>&1; then
  useradd --system --home "$REMOTE_LIB" --shell /usr/sbin/nologin hahelper
fi
mkdir -p "$REMOTE_ETC" "$REMOTE_LIB/db"
chown hahelper:hahelper "$REMOTE_LIB"

install -m 0755 /tmp/hahelper.new "$REMOTE_BIN"
install -m 0644 /tmp/hahelper.service /etc/systemd/system/hahelper.service

if [[ ! -f "$REMOTE_ETC/config.json" ]]; then
  install -m 0640 /tmp/hahelper.config.json.example "$REMOTE_ETC/config.json"
  echo "Created $REMOTE_ETC/config.json from example — edit before first use."
else
  echo "Keeping existing $REMOTE_ETC/config.json"
fi

if [[ ! -f "$REMOTE_ETC/env" ]]; then
  install -m 0600 /tmp/hahelper.env.example "$REMOTE_ETC/env"
fi

chown -R hahelper:hahelper "$REMOTE_LIB"
chown root:hahelper "$REMOTE_ETC/config.json"
chmod 0640 "$REMOTE_ETC/config.json"

systemctl daemon-reload
systemctl enable hahelper.service
systemctl restart hahelper.service
systemctl --no-pager status hahelper.service || true

rm -f /tmp/hahelper.new /tmp/hahelper.service /tmp/hahelper.config.json.example /tmp/hahelper.env.example
REMOTE

echo ""
echo "Done. Admin UI: http://<host>:8080"
echo "Config:       $REMOTE_ETC/config.json on $TARGET"
echo "Logs:         ssh $TARGET 'sudo journalctl -u hahelper -f'"
