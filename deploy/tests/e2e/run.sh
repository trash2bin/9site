#!/usr/bin/env bash
# End-to-end run on a machine with systemd and ssh, as root. The site is a
# stub that speaks just enough HTTP for the smoke list, so a failing scenario
# is a failing deploy and not a failing website.
#
# The deploy model it rehearses is the user unit: everything on the server
# runs as the deploy user over ssh, through systemctl --user, with no sudo
# anywhere. Root is used only to lay down the sandbox, the way setup-server
# does it once.

set -uo pipefail

REPO="$(cd "$(dirname "$0")/../../.." && pwd)"
DEPLOY_PATH=/var/www/9site
DATA_DIR=/var/www/9site/var
USER_UNIT_DIR=/home/deploy/.config/systemd/user
UNIT="$USER_UNIT_DIR/9site.service"
KEY=/root/.ssh/e2e_key
STUB="$REPO/deploy/tests/e2e/stub-server.sh"
SSH="ssh -i $KEY -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null"
DEPLOY_UID=""

# Everything on the server goes through the deploy user, exactly like the
# pipeline: a runtime with no sudo anywhere. XDG_RUNTIME_DIR is set because
# non-login ssh does not set it and systemctl --user needs it.
u() { $SSH deploy@localhost "XDG_RUNTIME_DIR=/run/user/$DEPLOY_UID $*"; }

PASS=0
FAIL=0

say() { printf '\n== %s\n' "$1"; }
ok() { printf '  ok   %s\n' "$1"; PASS=$((PASS + 1)); }
no() { printf '  FAIL %s\n' "$1"; FAIL=$((FAIL + 1)); }
check() { if [ "$1" = 0 ]; then ok "$2"; else no "$2"; fi; }

# One list for the deploy check and the rollback check.
URLS="$(grep -v '^#' "$REPO/deploy/smoke-urls.txt")"

# Runs a script the way the pipeline does.
remote() {
  local name="$1"; shift
  (cd "$REPO" && SSH_OPTS="$SSH" TARGET=deploy@localhost \
    bash deploy/remote/run.sh "$name" "$@")
}

unit_env() { u "systemctl --user show 9site -p Environment --value" | tr ' ' '\n' | sed -n "s/^$1=//p"; }

# The match is on a variable, not a pipe: grep -q plus pipefail reports
# failure on SIGPIPE even when the pattern was found.
journal_has() {
  local pattern="$1" lines
  for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
    lines="$(u "journalctl --user -u 9site -n 300 --no-pager" 2>/dev/null)" || true
    case "$lines" in
      *"$pattern"*) return 0 ;;
    esac
    sleep 1
  done
  return 1
}

wait_http() {
  local code
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    code="$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/ || true)"
    [ "$code" = 200 ] && return 0
    sleep 1
  done
  return 1
}

# install from a pipe is flaky, so the binary is written as a file.
put_binary() { cat "$1" >/tmp/site-server.new && chown deploy:deploy /tmp/site-server.new; }
put_fatal_binary() {
  cat >/tmp/site-server.new <<'BINARY'
#!/usr/bin/env bash
[ "${1:-}" = check ] && exit 0
echo "стоп: база знает миграции дальше" >&2
exit 14
BINARY
  chown deploy:deploy /tmp/site-server.new
}
put_broken_binary() {
  cat >/tmp/site-server.new <<'BINARY'
#!/usr/bin/env bash
echo "стоп: миграция 0003 не применилась" >&2
exit 1
BINARY
  chown deploy:deploy /tmp/site-server.new
}

setup_sandbox() {
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y -qq openssh-server python3 curl rsync procps >/dev/null

  # The deploy user. No sudoers file and no group membership: the service
  # and its data belong to this user, and the unit lives in the user manager.
  id deploy >/dev/null 2>&1 || useradd --system --create-home --shell /bin/bash deploy
  install -d -o deploy -g deploy -m 755 "$DEPLOY_PATH" "$DEPLOY_PATH/public" "$DATA_DIR"
  install -d -o deploy -g deploy -m 755 "$USER_UNIT_DIR"
  install -d -m 700 -o root -g root /root/.ssh
  [ -f "$KEY" ] || ssh-keygen -q -t ed25519 -N '' -f "$KEY"
  install -d -m 700 -o deploy -g deploy /home/deploy/.ssh
  install -m 600 "$KEY.pub" /home/deploy/.ssh/authorized_keys
  chown deploy:deploy /home/deploy/.ssh/authorized_keys

  ssh-keygen -A
  mkdir -p /run/sshd
  /usr/sbin/sshd

  # The user manager has to be alive for systemctl --user to have anything to
  # talk to; enable-linger keeps it running and starts it now without a login
  # session. This is the last root the model needs, it runs once, here.
  DEPLOY_UID="$(id -u deploy)"
  loginctl enable-linger deploy
  systemctl start "user@$DEPLOY_UID.service"
  # The stub listens on port 80 for the smoke list; an unprivileged process
  # cannot bind low ports unless the floor is lowered for the sandbox.
  sysctl -qw net.ipv4.ip_unprivileged_port_start=0
}

new_database() {
  printf 'snapshot:%s\n' "$1" >"$DATA_DIR/site.db"
  chown deploy:deploy "$DATA_DIR/site.db"
  printf '1\n' >"$DATA_DIR/visits.dat"
  chown deploy:deploy "$DATA_DIR/visits.dat"
}

# A fresh machine on every run.
reset_state() {
  u 'systemctl --user stop 9site' 2>/dev/null
  rm -rf "$DEPLOY_PATH" "$DATA_DIR" "$USER_UNIT_DIR"
  rm -f /tmp/site-server.new /tmp/9site.service
}

say 'sandbox'
reset_state
setup_sandbox
new_database clean
install -d -o deploy -g deploy -m 755 "$DEPLOY_PATH" "$DATA_DIR"

# --- scenario 1: the first deploy ---------------------------------------
say 'scenario 1: first deploy'
out="$(remote preflight "DATA_DIR=$DATA_DIR" "DEPLOY_PATH=$DEPLOY_PATH" 2>&1)"
[ "$?" = 0 ]; check "$?" 'preflight passes on a server with no binary yet'
[ ! -f "$UNIT" ]; check "$?" 'no unit before the first deploy'
rsync -az -e "$SSH" "$REPO/deploy/9site.service" deploy@localhost:/tmp/9site.service
chown deploy:deploy /tmp/9site.service
out="$(remote install-unit "DEPLOY_PATH=$DEPLOY_PATH" "DATA_DIR=$DATA_DIR" 2>&1)"
[ "$?" = 0 ]; check "$?" 'install-unit writes the unit as the deploy user'
[ -f "$UNIT" ]; check "$?" 'the unit landed in the user directory'
[ "$(unit_env DATA_DIR)" = "$DATA_DIR" ]; check "$?" 'the unit on the server knows DATA_DIR'
[ ! -f "$DEPLOY_PATH/site-server" ]; check "$?" 'nothing runs before the binary arrives'

put_binary "$STUB"
remote swap-binary "DEPLOY_PATH=$DEPLOY_PATH" >/dev/null
check "$?" 'swap-binary swaps atomically'
[ ! -f "$DEPLOY_PATH/site-server.prev" ]; check "$?" 'a first deploy has nothing to roll back to'
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
remote backup-db "STAMP=$stamp" "DATA_DIR=$DATA_DIR" "DEPLOY_PATH=$DEPLOY_PATH" >/dev/null
check "$?" 'backup-db snapshots the database'
[ -f "$DATA_DIR/site.db.pre-deploy" ]; check "$?" 'the paired snapshot exists'
remote restart "RESTART_CMD=systemctl --user restart 9site" >/dev/null
check "$?" 'restart succeeds'
wait_http; check "$?" 'the site answers after the restart'
remote check-live "URLS=$URLS" BASE_URL=http://127.0.0.1:8080/ >/dev/null
check "$?" 'check-live verifies the whole site'

# --- scenario 4: a second deploy over a working service ------------------
say 'scenario 4: second deploy over a working service'
before="$(cat "$DATA_DIR/site.db")"
put_binary "$STUB"
remote swap-binary "DEPLOY_PATH=$DEPLOY_PATH" >/dev/null
check "$?" 'a second deploy swaps too'
[ -f "$DEPLOY_PATH/site-server.prev" ]; check "$?" 'the previous binary is kept for the rollback'
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
remote backup-db "STAMP=$stamp" "DATA_DIR=$DATA_DIR" "DEPLOY_PATH=$DEPLOY_PATH" >/dev/null
check "$?" 'backup-db runs again'
[ "$(cat "$DATA_DIR/site.db.pre-deploy")" = "$before" ]
check "$?" 'the snapshot holds the live database'
remote dry-run "DEPLOY_PATH=$DEPLOY_PATH" >/dev/null
check "$?" 'dry-run passes the startup checks'

# --- scenario 3: the database stays where it is --------------------------
say 'scenario 3: install keeps the database in place'
before="$(cat "$DATA_DIR/site.db")"
remote install-unit "DEPLOY_PATH=$DEPLOY_PATH" "DATA_DIR=$DATA_DIR" >/dev/null
check "$?" 'install-unit succeeds with the data already present'
[ "$(unit_env DATA_DIR)" = "$DATA_DIR" ]; check "$?" 'the unit points at the live data dir'
[ "$(cat "$DATA_DIR/site.db")" = "$before" ]; check "$?" 'the database was neither moved nor reset'
[ "$(stat -c '%U' "$DATA_DIR/site.db")" = deploy ]
check "$?" 'the database still belongs to the deploy user'
remote restart "RESTART_CMD=systemctl --user restart 9site" >/dev/null
wait_http; check "$?" 'the site kept serving, the data never moved'

# --- scenario 5: a binary that cannot even run the checks ---------------
say 'scenario 5: binary that fails the dry-run'
wait_http; check "$?" 'the good binary serves the site before the deploy'
put_broken_binary
remote swap-binary "DEPLOY_PATH=$DEPLOY_PATH" >/dev/null
check "$?" 'the broken binary reaches the server'
out="$(remote dry-run "DEPLOY_PATH=$DEPLOY_PATH" 2>&1)"
[ "$?" != 0 ]; check "$?" 'the dry-run takes the deploy down'
printf '%s\n' "$out" | grep -q 'миграция 0003'
check "$?" 'the reason from the binary reaches the log'
wait_http; check "$?" 'the old process served the site the whole time'
remote rollback "DATA_DIR=$DATA_DIR" "DEPLOY_PATH=$DEPLOY_PATH" \
  "RESTART_CMD=systemctl --user restart 9site" >/dev/null
check "$?" 'the automatic rollback undoes the swap'
wait_http; check "$?" 'the site is green again after the automatic rollback'

# --- scenario 6: a binary that passes the checks but dies on start ------
say 'scenario 6: binary that passes the check and fails on start'
put_binary "$STUB"
remote swap-binary "DEPLOY_PATH=$DEPLOY_PATH" >/dev/null
printf 'snapshot:before\n' >"$DATA_DIR/site.db"
chown deploy:deploy "$DATA_DIR/site.db"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
remote backup-db "STAMP=$stamp" "DATA_DIR=$DATA_DIR" "DEPLOY_PATH=$DEPLOY_PATH" >/dev/null
check "$?" 'the snapshot is taken before the restart'
printf 'new-schema!\n' >"$DATA_DIR/site.db"
put_fatal_binary
remote swap-binary "DEPLOY_PATH=$DEPLOY_PATH" >/dev/null
check "$?" 'the binary that will fail on start is swapped in'
remote dry-run "DEPLOY_PATH=$DEPLOY_PATH" >/dev/null
check "$?" 'its check passes, the failure is only visible on start'
remote restart "RESTART_CMD=systemctl --user restart 9site" >/dev/null 2>&1
sleep 6
remote check-live "URLS=$URLS" BASE_URL=http://127.0.0.1:8080/ >/dev/null 2>&1
[ "$?" != 0 ]; check "$?" 'check-live refuses a service that crashed on start'
journal_has 'знает миграции дальше'
check "$?" 'the log holds the reason of the crash'
out="$(remote rollback "DATA_DIR=$DATA_DIR" "DEPLOY_PATH=$DEPLOY_PATH" \
  "RESTART_CMD=systemctl --user restart 9site" 2>&1)"
check "$?" 'the rollback runs'
printf '%s\n' "$out" | grep -q 'rolled back, rejected binary'
check "$?" 'the rejected binary is kept as site-server.failed'
[ "$(cat "$DATA_DIR/site.db")" = 'snapshot:before' ]
check "$?" 'the database is back to the pre-deploy snapshot'
[ ! -e "$DATA_DIR/site.db-wal" ] && [ ! -e "$DATA_DIR/site.db-shm" ]
check "$?" 'a stale wal would have reopened the future schema'
wait_http; check "$?" 'the site answers again with the rolled back binary'
out="$(remote check-site "BASE_URL=http://127.0.0.1:8080/" 2>&1)"
check "$?" 'check-site is green after the rollback'

# --- scenario 7: a manual rollback after a good deploy ------------------
say 'scenario 7: manual rollback after a good deploy'
put_binary "$STUB"
remote swap-binary "DEPLOY_PATH=$DEPLOY_PATH" >/dev/null
printf 'snapshot:manual\n' >"$DATA_DIR/site.db"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
remote backup-db "STAMP=$stamp" "DATA_DIR=$DATA_DIR" "DEPLOY_PATH=$DEPLOY_PATH" >/dev/null
remote restart "RESTART_CMD=systemctl --user restart 9site" >/dev/null
wait_http; check "$?" 'the good deploy serves the site'
printf 'new-schema!\n' >"$DATA_DIR/site.db"
put_fatal_binary
remote swap-binary "DEPLOY_PATH=$DEPLOY_PATH" >/dev/null
remote restart "RESTART_CMD=systemctl --user restart 9site" >/dev/null 2>&1
sleep 6
remote check-live "URLS=$URLS" BASE_URL=http://127.0.0.1:8080/ >/dev/null 2>&1
[ "$?" != 0 ]; check "$?" 'check-live refuses the crashed service before the manual rollback'
out="$(remote rollback "DATA_DIR=$DATA_DIR" "DEPLOY_PATH=$DEPLOY_PATH" \
  "RESTART_CMD=systemctl --user restart 9site" 2>&1)"
check "$?" 'the manual rollback runs the same script'
printf '%s\n' "$out" | grep -q 'service restarted with the rolled back binary'
check "$?" 'the manual path restarts too, not only restores files'
[ "$(cat "$DATA_DIR/site.db")" = 'snapshot:manual' ]
check "$?" 'the manual rollback restored the database as well'
wait_http; check "$?" 'the site is back after the manual rollback'

# --- scenario 8: a database that cannot be opened ------------------------
say 'scenario 8: database that cannot be opened'
remote restart "RESTART_CMD=systemctl --user restart 9site" >/dev/null 2>&1
sleep 2
printf 'root-owned\n' >"$DATA_DIR/site.db"
chown root:root "$DATA_DIR/site.db"
remote restart "RESTART_CMD=systemctl --user restart 9site" >/dev/null 2>&1
sleep 5
journal_has 'unable to open database file'
check "$?" 'the log holds the reason: the database cannot be opened'
out="$(remote journal-report "DATA_DIR=$DATA_DIR" 2>&1)"
check "$?" 'the journal of a failed start reaches the report'
printf '%s\n' "$out" | grep -q 'unable to open database file'
check "$?" 'the report carries the reason to the job summary'
chown deploy:deploy "$DATA_DIR/site.db"

printf '\n== result: %s ok, %s failed\n' "$PASS" "$FAIL"
[ "$FAIL" = 0 ]
