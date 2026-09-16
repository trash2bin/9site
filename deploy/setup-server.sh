#!/usr/bin/env bash
#
# Разовая подготовка сервера для 9site.
#
# Запускать от root на чистой Ubuntu, из папки deploy/:
#
#   bash setup-server.sh
#
# Скрипт можно запускать повторно: он не ломает уже сделанное.
#
set -euo pipefail

DEPLOY_USER="deploy"
DEPLOY_PATH="/var/www/9site"
SERVICE_NAME="9site"

say() { printf '\n== %s\n' "$*"; }

if [ "$(id -u)" -ne 0 ]; then
  echo "Нужны права root. Запусти: sudo bash $0" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
for f in 9site.service nginx-9site.conf sudoers-9site-deploy; do
  if [ ! -f "$SCRIPT_DIR/$f" ]; then
    echo "Не найден $SCRIPT_DIR/$f. Запускай скрипт из папки deploy/." >&2
    exit 1
  fi
done

say "Пакеты"
DEBIAN_FRONTEND=noninteractive apt-get update -qq
DEBIAN_FRONTEND=noninteractive apt-get install -y -qq nginx rsync curl

say "Пользователь $DEPLOY_USER"
if id -u "$DEPLOY_USER" >/dev/null 2>&1; then
  echo "  уже существует"
else
  adduser --disabled-password --gecos "" "$DEPLOY_USER"
  echo "  создан, вход по паролю запрещён"
fi

say "Папка $DEPLOY_PATH"
install -d -o "$DEPLOY_USER" -g "$DEPLOY_USER" -m 755 "$DEPLOY_PATH"

say "SSH-ключ для GitHub Actions"
SSH_DIR="/home/$DEPLOY_USER/.ssh"
install -d -o "$DEPLOY_USER" -g "$DEPLOY_USER" -m 700 "$SSH_DIR"
if [ ! -f "$SSH_DIR/ci_ed25519" ]; then
  ssh-keygen -t ed25519 -C "github-actions@9site" -f "$SSH_DIR/ci_ed25519" -N "" -q
  chown "$DEPLOY_USER:$DEPLOY_USER" "$SSH_DIR/ci_ed25519" "$SSH_DIR/ci_ed25519.pub"
  echo "  ключевая пара создана"
else
  echo "  ключ уже был, оставляю прежний"
fi
touch "$SSH_DIR/authorized_keys"
chmod 600 "$SSH_DIR/authorized_keys"
chown "$DEPLOY_USER:$DEPLOY_USER" "$SSH_DIR/authorized_keys"
PUB="$(cat "$SSH_DIR/ci_ed25519.pub")"
if grep -qF "$PUB" "$SSH_DIR/authorized_keys"; then
  echo "  публичная часть уже в authorized_keys"
else
  printf '%s\n' "$PUB" >>"$SSH_DIR/authorized_keys"
  echo "  публичная часть добавлена в authorized_keys"
fi

say "systemd"
install -m 644 "$SCRIPT_DIR/9site.service" "/etc/systemd/system/$SERVICE_NAME.service"
systemctl daemon-reload
systemctl enable "$SERVICE_NAME" >/dev/null
echo "  юнит установлен, включён в автозапуск"
echo "  запускать пока нечего: бинарник придёт первым деплоем"

say "Разрешение на перезапуск"
install -m 0440 "$SCRIPT_DIR/sudoers-9site-deploy" "/etc/sudoers.d/$SERVICE_NAME-deploy"
visudo -c -f "/etc/sudoers.d/$SERVICE_NAME-deploy" >/dev/null
echo "  $DEPLOY_USER может перезапускать $SERVICE_NAME без пароля"

say "nginx"
install -m 644 "$SCRIPT_DIR/nginx-9site.conf" "/etc/nginx/sites-available/$SERVICE_NAME"
ln -sfn "/etc/nginx/sites-available/$SERVICE_NAME" "/etc/nginx/sites-enabled/$SERVICE_NAME"
# Дефолтный сайт nginx помечен default_server и перехватывает все запросы,
# поэтому его надо убрать, иначе наша конфигурация не получит трафик.
if [ -e /etc/nginx/sites-enabled/default ]; then
  rm -f /etc/nginx/sites-enabled/default
  echo "  дефолтный сайт отключён"
fi
nginx -t
systemctl reload nginx
echo "  конфигурация проверена, nginx перезагружен"

say "Firewall"
# Сначала открываем SSH и только потом включаем ufw, иначе можно
# потерять доступ к серверу.
SSH_PORT="$(sshd -T 2>/dev/null | awk '/^port /{print $2; exit}')"
SSH_PORT="${SSH_PORT:-22}"
ufw allow OpenSSH >/dev/null
if [ "$SSH_PORT" != "22" ]; then
  ufw allow "$SSH_PORT/tcp" >/dev/null
  echo "  SSH слушает нестандартный порт $SSH_PORT, открыл и его"
fi
ufw allow 'Nginx HTTP' >/dev/null
ufw --force enable >/dev/null
ufw status | sed 's/^/  /'

say "Что дальше"
cat <<INFO
Пропиши эти значения в GitHub, в Settings -> Secrets and variables -> Actions.

Переменные (вкладка Variables):
  DEPLOY_ENABLED      = true
  DEPLOY_RESTART_CMD  = sudo systemctl restart $SERVICE_NAME

Секреты (вкладка Secrets):
  SSH_HOST            = IP этого сервера
  SSH_PORT            = $SSH_PORT
  SSH_USER            = $DEPLOY_USER
  DEPLOY_PATH         = $DEPLOY_PATH
  SSH_PRIVATE_KEY     = ключ ниже, целиком

Приватный ключ (скопируй вместе со строками BEGIN и END):

$(cat "$SSH_DIR/ci_ed25519")

После того как деплой заработает, приватную половину с сервера лучше убрать:
  rm $SSH_DIR/ci_ed25519
Публичная часть в authorized_keys останется и продолжит работать.
INFO
