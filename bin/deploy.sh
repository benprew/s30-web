#!/usr/bin/env bash
set -euo pipefail

SERVER="${DEPLOY_HOST:-throwingbones}"
SSH_USER="${DEPLOY_USER:-root}"
SSH="ssh ${SSH_USER}@${SERVER}"

SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$SCRIPT_DIR"

echo "==> Building Linux binary (static amd64)..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o s30-web .

echo "==> Deploying binary to ${SERVER}..."
HASH=$(md5 -q s30-web 2>/dev/null | head -c8 || md5sum s30-web | head -c8)
REMOTE_FILE="s30-web-${HASH}"

$SSH bash -s <<'REMOTE'
    mkdir -p /var/www/s30-web
    semanage fcontext -a -t bin_t '/var/www/s30-web/s30-web-[^/]*' 2>/dev/null || true
REMOTE

scp s30-web "${SSH_USER}@${SERVER}:/var/www/s30-web/${REMOTE_FILE}"
rm s30-web

$SSH bash -s <<REMOTE
    ln -sf "/var/www/s30-web/${REMOTE_FILE}" /var/www/s30-web/s30-web
    chmod 755 "/var/www/s30-web/${REMOTE_FILE}" /var/www/s30-web/s30-web
    chown -R throwingbones:throwingbones /var/www/s30-web
    restorecon -v "/var/www/s30-web/${REMOTE_FILE}" 2>/dev/null || true
    systemctl daemon-reload
    systemctl enable s30-web 2>/dev/null || true
    # Keep the last 3 deployed binaries
    ls -t /var/www/s30-web/s30-web-* 2>/dev/null | tail -n +4 | xargs -r rm -f
REMOTE

echo "==> Syncing static frontend assets..."
rsync -av public/ "${SSH_USER}@${SERVER}:/var/www/s30-web/public/"
$SSH bash -s <<'REMOTE'
    mkdir -p /var/www/html/s30
    ln -sfn /var/www/s30-web/public /var/www/html/s30/public
    chown -R throwingbones:throwingbones /var/www/html/s30 /var/www/s30-web/public
    chmod -R 755 /var/www/s30-web/public
REMOTE

echo "==> Restarting s30-web service..."
$SSH "systemctl restart s30-web"

echo "==> Verifying service status..."
sleep 2
$SSH "systemctl status s30-web --no-pager -l"
