#!/usr/bin/env bash
# One-time cutover from the retired system unit to the user unit. Root is
# needed for exactly this: a unit under /etc/systemd/system can only be
# disabled and removed by root, and the deploy user must not be able to. Run
# it once on the server, then every deploy is root-free and sudo-free.

set -euo pipefail

SERVICE_NAME=9site
DEPLOY_USER=deploy
DEPLOY_PATH=/var/www/9site
DATA_DIR=/var/lib/9site

if [ "$(id -u)" -ne 0 ]; then
  echo "run as root: sudo bash $0" >&2
  exit 1
fi

# Two units on one port is a conflict, so the system unit goes. The deploy
# pipeline (deploy/remote/prepare.sh) sets up everything else as the user.
if [ -e "/etc/systemd/system/$SERVICE_NAME.service" ]; then
  systemctl disable --now "$SERVICE_NAME" 2>/dev/null || true
  rm -f "/etc/systemd/system/$SERVICE_NAME.service"
  systemctl daemon-reload
  echo "system unit $SERVICE_NAME disabled and removed"
fi

# The narrow sudoers existed only for that system unit; the user unit needs
# nothing of it, so the file leaves too.
rm -f "/etc/sudoers.d/$SERVICE_NAME-deploy"

# The old system unit kept the database under its working directory; the user
# unit wants it in DATA_DIR. Move it, or the first start resets the site. This
# is the one data step that needs root: the deploy user cannot write /var/lib.
OLD_DATA="$DEPLOY_PATH/var"
install -d -o "$DEPLOY_USER" -g "$DEPLOY_USER" -m 750 "$DATA_DIR"
if [ -d "$OLD_DATA" ] && [ ! -f "$DATA_DIR/site.db" ]; then
  cp -a "$OLD_DATA/." "$DATA_DIR/"
  chown -R "$DEPLOY_USER:$DEPLOY_USER" "$DATA_DIR"
  echo "database moved from $OLD_DATA to $DATA_DIR"
  echo "remove $OLD_DATA once the deploy is green"
fi

# Start the user manager now, no reboot required.
loginctl enable-linger "$DEPLOY_USER"
systemctl start "user@$(id -u "$DEPLOY_USER").service"

echo "migration done: user unit, no sudo, the next deploy is root-free"
