#!/usr/bin/env bash
# End-to-end run on a machine with systemd and ssh, as root. The site is a
# stub that speaks just enough HTTP for the smoke list, so a failing scenario
# is a failing deploy and not a failing website.
#
# The deploy model it rehearses is the release model: one directory per
# commit under releases/, a current symlink the deploy flips, one script that
# does the whole release and undoes it if anything after the flip fails.
# Everything on the server runs as the deploy user over ssh, through
# systemctl --user, with no sudo anywhere. Root is used only to lay down the
# sandbox, the way setup-server does it once.

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

sha1=1111111111111111111111111111111111111111
sha2=2222222222222222222222222222222222222222
sha3=3333333333333333333333333333333333333333
sha4=4444444444444444444444444444444444444444
sha5=5555555555555555555555555555555555555555

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

# The static set the pipeline uploads before the deploy. The deploy swaps it
# in after the checks and swaps the previous one back on a rollback.
put_static() {
  rm -rf "$DEPLOY_PATH/public.next"
  install -d -o deploy -g deploy -m 755 "$DEPLOY_PATH/public.next"
  printf '<html>static %s</html>\n' "$1" >"$DEPLOY_PATH/public.next/index.html"
  chown -R deploy:deploy "$DEPLOY_PATH/public.next"
}

# The deploy of one release, the way the pipeline calls it.
deploy_release() {
  put_static "$1"
  remote deploy "SHA=$1" "URLS=$URLS" "DEPLOY_PATH=$DEPLOY_PATH"
}

rollback_release() { remote rollback "URLS=$URLS" "DEPLOY_PATH=$DEPLOY_PATH"; }

# A release on the server the way the pipeline leaves it: one directory per
# commit, built as root and owned by the deploy user.
put_release() {
  local sha="$1" binary="$2" dir
  # The releases directory belongs to the deploy user: the deploy renames
  # entries in it, exactly as CI leaves it.
  install -d -o deploy -g deploy -m 755 "$DEPLOY_PATH/releases"
  dir="$DEPLOY_PATH/releases/$sha"
  install -d -o deploy -g deploy -m 755 "$dir"
  cat "$binary" >"$dir/site-server"
  chmod 755 "$dir/site-server"
  chown deploy:deploy "$dir/site-server"
}
put_fatal_release() {
  local dir="$DEPLOY_PATH/releases/$1"
  install -d -o deploy -g deploy -m 755 "$DEPLOY_PATH/releases" "$dir"
  cat >"$dir/site-server" <<'BINARY'
#!/usr/bin/env bash
# Как настоящий бинарник: проверка и снимок проходят, падает только старт.
case "$1" in
  check) exit 0 ;;
  backup) cp "$DATA_DIR/site.db" "$2" ;;
  *) echo "стоп: база знает миграции дальше" >&2; exit 14 ;;
esac
BINARY
  chmod 755 "$dir/site-server"
  chown deploy:deploy "$dir/site-server"
}
put_broken_release() {
  local dir="$DEPLOY_PATH/releases/$1"
  install -d -o deploy -g deploy -m 755 "$DEPLOY_PATH/releases" "$dir"
  cat >"$dir/site-server" <<'BINARY'
#!/usr/bin/env bash
echo 'стоп: миграция 0003 не применилась' >&2
exit 1
BINARY
  chmod 755 "$dir/site-server"
  chown deploy:deploy "$dir/site-server"
}

release() { printf '%s\n' "$DEPLOY_PATH/releases/$1"; }
unit_env() { sed -n "s/^$1=//p" "$DEPLOY_PATH/deploy.env"; }
unit_envfile() { sed -n 's/^EnvironmentFile=//p' "$UNIT"; }
current_release() { readlink "$DEPLOY_PATH/current" 2>/dev/null; }

# The match is on a variable, not a pipe: grep -q plus pipefail reports
# failure on SIGPIPE even when the pattern was found.
journal_has() {
  local pattern="$1" lines
  for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
    # Both views of the same unit: the user view is empty when the user
    # manager writes into the system journal, which is what a lingering user
    # does on the deploy machine.
    lines="$(u "journalctl --user -u 9site -n 300 --no-pager 2>/dev/null; journalctl _SYSTEMD_USER_UNIT=9site.service -n 300 --no-pager 2>/dev/null" 2>/dev/null)" || true
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

setup_sandbox() {
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y -qq openssh-server python3 curl rsync procps >/dev/null

  # The deploy user. No sudoers file and no group membership: the service
  # and its data belong to this user, and the unit lives in the user manager.
  id deploy >/dev/null 2>&1 || useradd --system --create-home --shell /bin/bash deploy
  # journal-report reads journalctl --user as deploy; without the
  # systemd-journal group the user cannot see the journal and the crash
  # reason never reaches the rehearsal.
  usermod -aG systemd-journal deploy 2>/dev/null || true
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
  rm -f /tmp/9site.service /tmp/deploy.env
}

say 'sandbox'
reset_state
setup_sandbox
new_database clean
install -d -o deploy -g deploy -m 755 "$DEPLOY_PATH" "$DATA_DIR"

# --- scenario 1: the first deploy ---------------------------------------
say 'scenario 1: the first deploy'
out="$(remote preflight "DEPLOY_PATH=$DEPLOY_PATH" 2>&1)"
[ "$?" = 0 ]; check "$?" 'preflight passes on a server with no binary yet'
[ ! -f "$UNIT" ]; check "$?" 'no unit before the first deploy'
rsync -az -e "$SSH" "$REPO/deploy/9site.service" \
  "$REPO/deploy/deploy.env" deploy@localhost:/tmp/
chown deploy:deploy /tmp/9site.service /tmp/deploy.env
out="$(remote install-unit "DEPLOY_PATH=$DEPLOY_PATH" 2>&1)"
[ "$?" = 0 ]; check "$?" 'install-unit writes the unit as the deploy user'
[ -f "$UNIT" ]; check "$?" 'the unit landed in the user directory'
[ "$(unit_envfile)" = "$DEPLOY_PATH/deploy.env" ]
check "$?" 'the unit takes its environment from one file'
[ "$(unit_env DATA_DIR)" = "$DATA_DIR" ]; check "$?" 'the file names the data directory'
[ ! -e "$DEPLOY_PATH/current" ]; check "$?" 'nothing serves before the first release'

put_release "$sha1" "$STUB"
out="$(deploy_release "$sha1" 2>&1)"
check "$?" 'the deploy of the first release succeeds'
printf '%s\n' "$out" | grep -q 'flipped.*releases/'"$sha1"
check "$?" 'the current symlink points at the release'
[ -f "$DATA_DIR/site.db.pre-$sha1" ]; check "$?" 'the pre-deploy copy is named by the release'
[ "$(cat "$DEPLOY_PATH/public/index.html")" = "<html>static $sha1</html>" ]
check "$?" 'the static set of the release replaced the one on the server'
wait_http; check "$?" 'the site answers after the restart'
out="$(curl -s http://127.0.0.1:8080/healthz || true)"
printf '%s\n' "$out" | grep -q '"schema"'
check "$?" 'the health endpoint answers, like the real binary does'

# --- scenario 2: a second release over a working service ----------------
say 'scenario 2: a second release over a working service'
before="$(cat "$DATA_DIR/site.db")"
out="$(remote preflight "DEPLOY_PATH=$DEPLOY_PATH" 2>&1)"
[ "$?" = 0 ]; check "$?" 'preflight passes over a working service'
printf '%s\n' "$out" | grep -q 'counter before the deploy'
check "$?" 'preflight records the number the site serves'

put_release "$sha2" "$STUB"
out="$(deploy_release "$sha2" 2>&1)"
check "$?" 'the deploy of the second release succeeds'
[ "$(current_release)" = "$(release "$sha2")" ]
check "$?" 'current moved to the second release'
[ "$(cat "$DATA_DIR/release-before")" = "$(release "$sha1")" ]
check "$?" 'the release before is recorded for a rollback'
[ -f "$DATA_DIR/backups/site-$sha2.db" ]; check "$?" 'the snapshot is named by the release'
[ "$(cat "$DATA_DIR/site.db.pre-$sha2")" = "$before" ]
check "$?" 'the pre-deploy copy holds the live database'
wait_http; check "$?" 'the site kept serving through the second release'
[ -d "$(release "$sha1")" ]; check "$?" 'the previous release is kept for the rollback'

# --- scenario 3: a release that cannot pass the startup checks ----------
say 'scenario 3: a release that fails the check'
put_broken_release "$sha3"
out="$(deploy_release "$sha3" 2>&1)"
[ "$?" != 0 ]; check "$?" 'the deploy refuses a release that fails the check'
printf '%s\n' "$out" | grep -q 'миграция 0003'
check "$?" 'the reason from the binary reaches the log'
[ "$(current_release)" = "$(release "$sha2")" ]
check "$?" 'current did not move: the flip never happened'
wait_http; check "$?" 'the site served the whole time'
[ -d "$(release "$sha3")" ]; check "$?" 'the rejected release is kept for a local rerun'

# --- scenario 4: a release that passes the check and dies on start ------
say 'scenario 4: a release that passes the check and fails on start'
printf 'snapshot:before\n' >"$DATA_DIR/site.db"
chown deploy:deploy "$DATA_DIR/site.db"
put_fatal_release "$sha4"
out="$(deploy_release "$sha4" 2>&1)"
[ "$?" != 0 ]; check "$?" 'the deploy fails when the release dies on start'
[ "$(current_release)" = "$(release "$sha2")" ]
check "$?" 'current is back to the release that served'
[ "$(cat "$DATA_DIR/site.db")" = 'snapshot:before' ]
check "$?" 'the database is back to the pre-deploy copy'
[ -d "$DEPLOY_PATH/releases/$sha4.rejected" ]
check "$?" 'the rejected release is kept out of the flip'
[ "$(cat "$DEPLOY_PATH/public/index.html")" = "<html>static $sha2</html>" ]
check "$?" 'the rollback put the static set of the serving release back'
wait_http; check "$?" 'the site answers again with the rolled back release'
journal_has 'знает миграции дальше'
check "$?" 'the log holds the reason of the crash'
out="$(remote journal-report 2>&1)"
check "$?" 'journal-report runs'
printf '%s\n' "$out" | grep -q 'знает миграции дальше'
check "$?" 'the report carries the reason'

# --- scenario 5: the manual rollback ------------------------------------
say 'scenario 5: the manual rollback'
put_release "$sha5" "$STUB"
out="$(deploy_release "$sha5" 2>&1)"
check "$?" 'the deploy of a good release succeeds'
printf 'new-schema!\n' >"$DATA_DIR/site.db"
chown deploy:deploy "$DATA_DIR/site.db"
out="$(rollback_release 2>&1)"
check "$?" 'the manual rollback runs'
[ "$(current_release)" = "$(release "$sha2")" ]
check "$?" 'current is back to the release before the last deploy'
printf '%s\n' "$out" | grep -q 'rolled back to'
check "$?" 'the rollback says which release it went back to'
printf '%s\n' "$out" | grep -q 'database rolled back'
check "$?" 'the rollback restores the database as well'
[ "$(cat "$DEPLOY_PATH/public/index.html")" = "<html>static $sha2</html>" ]
check "$?" 'the rollback restores the static set of that release as well'
wait_http; check "$?" 'the site is back after the manual rollback'
out="$(rollback_release 2>&1)"
[ "$?" = 0 ]; check "$?" 'the rollback of an already rolled back site is a no-op'
[ "$(current_release)" = "$(release "$sha2")" ]
check "$?" 'current did not move on the second rollback'

# --- scenario 6: a database the new release cannot open -----------------
say 'scenario 6: a database the release cannot open'
printf 'root-owned\n' >"$DATA_DIR/site.db"
chown root:root "$DATA_DIR/site.db"
put_release "$sha5" "$STUB"
out="$(deploy_release "$sha5" 2>&1)"
[ "$?" != 0 ]; check "$?" 'the deploy refuses a database the release cannot open'
printf '%s\n' "$out" | grep -q 'unable to open database file'
check "$?" 'the reason from the binary reaches the log'
printf '%s\n' "$out" | grep -q 'the deploy stops before the flip'
check "$?" 'the deploy stops before the flip, not after it'
[ "$(current_release)" = "$(release "$sha2")" ]
check "$?" 'current did not move'
wait_http; check "$?" 'the site served the whole time'
chown deploy:deploy "$DATA_DIR/site.db"

# --- scenario 7: the first deploy on a server that still runs the old binary -
say 'scenario 7: the first deploy can roll back to the binary it replaced'
rm -f "$DEPLOY_PATH/current" "$DATA_DIR/release-before"
# The server runs the binary the old unit started, straight under the deploy
# path: no current yet, and it is what a failed first deploy must return to.
cp "$STUB" "$DEPLOY_PATH/site-server"
chmod 755 "$DEPLOY_PATH/site-server"
chown deploy:deploy "$DEPLOY_PATH/site-server"
put_fatal_release "$sha5"
out="$(deploy_release "$sha5" 2>&1)"
[ "$?" != 0 ]; check "$?" 'the first deploy of a fatal release fails'
[ -d "$DEPLOY_PATH/releases/legacy" ]
check "$?" 'the running binary became the release to roll back to'
[ "$(current_release)" = "$(release legacy)" ]
check "$?" 'current is back to the legacy release'
wait_http; check "$?" 'the site answers with the binary it ran before'

# Ground truth for a failing journal check. Root always reads the journal, so
# compare what root and the deploy session each see for the same unit line:
# root sees it, deploy does not, is journal access; root sees nothing is routing.
say 'diagnostics'
printf '  groups of deploy: %s\n' "$(id -nG deploy | tr -d '\r')"
printf '  groups the deploy ssh session sees: %s\n' "$(u 'id -nG' 2>&1 | tr -d '\r')"
printf '  journald is: %s\n' "$(systemctl is-active systemd-journald 2>&1 | tr -d '\r')"
printf '  what root sees, _SYSTEMD_USER_UNIT=9site.service:\n'
journalctl _SYSTEMD_USER_UNIT=9site.service -n 8 --no-pager 2>&1 | sed 's/^/    /'
printf '  what the deploy session sees, the same unit line:\n'
runuser -u deploy -- env XDG_RUNTIME_DIR=/run/user/"$DEPLOY_UID" \
  journalctl _SYSTEMD_USER_UNIT=9site.service -n 8 --no-pager 2>&1 | sed 's/^/    /'

printf '\n== result: %s ok, %s failed\n' "$PASS" "$FAIL"
[ "$FAIL" = 0 ]
