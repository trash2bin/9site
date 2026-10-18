#!/usr/bin/env bats
# The remote scripts, called exactly the way the pipeline calls them:
# deploy/remote/run.sh concatenates lib.sh and the script and pipes it into
# fakessh, which is bash reading the stream from stdin.
#
# This is the net for nested quoting, which bash -n cannot see.

setup() {
  root="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
  export SSH_OPTS="$root/deploy/tests/fakessh"
  export TARGET=deploy@fake

  data="$BATS_TEST_TMPDIR/data"
  www="$BATS_TEST_TMPDIR/www"
  mkdir -p "$data" "$www" "$BATS_TEST_TMPDIR/bin"
  mkdir -p "$data/backups"
  # The pipeline uploads the binary here first; the swap script only moves it.
  printf '#!/usr/bin/env bash\n' >/tmp/site-server.new
  chmod +x /tmp/site-server.new
}

remote() {
  PATH="$BATS_TEST_TMPDIR/bin:$PATH" bash "$root/deploy/remote/run.sh" "$@"
}

stub() {
  cat >"$BATS_TEST_TMPDIR/bin/$1"
  chmod +x "$BATS_TEST_TMPDIR/bin/$1"
}

@test 'a path with a space survives the stream unchanged' {
  spaced="$BATS_TEST_TMPDIR/www with space"
  mkdir -p "$spaced"
  run remote swap-binary "DEPLOY_PATH=$spaced"
  [ "$status" -eq 0 ]
  [ -x "$spaced/site-server" ]
  [ -f "$spaced/site-server.new" ] && false || true
}

@test 'swap-binary keeps the previous binary for the rollback' {
  echo first >"$www/site-server"
  run remote swap-binary DEPLOY_PATH="$www"
  [ "$status" -eq 0 ]
  [ -x "$www/site-server" ]
  [ -f "$www/site-server.prev" ]
}

@test 'swap-binary does not create prev on the very first deploy' {
  run remote swap-binary DEPLOY_PATH="$www"
  [ "$status" -eq 0 ]
  [ ! -e "$www/site-server.prev" ]
  [ -x "$www/site-server" ]
}

@test 'backup-db says first deploy when there is no database' {
  run remote backup-db STAMP=20260101T000000Z \
    DATA_DIR="$data" DEPLOY_PATH="$www"
  [ "$status" -eq 0 ]
  [[ "$output" == *'no site.db yet: first deploy, snapshot skipped'* ]]
}

@test 'backup-db runs the new binary twice and keeps ten copies' {
  echo db >"$data/site.db"
  cat >"$www/site-server" <<'STUB'
#!/usr/bin/env bash
echo "site-server $*" >>"$ARGS_LOG"
exit 0
STUB
  chmod +x "$www/site-server"
  i=0
  while [ "$i" -lt 12 ]; do
    echo old >"$data/backups/site-2025$i.db"
    i=$((i + 1))
  done
  export ARGS_LOG="$BATS_TEST_TMPDIR/args.log"
  : >"$ARGS_LOG"

  run remote backup-db STAMP=20260101T000000Z \
    DATA_DIR="$data" DEPLOY_PATH="$www"
  [ "$status" -eq 0 ]
  [[ "$output" == *"Backup: $data/backups/site-20260101T000000Z.db"* ]]
  [[ "$output" == *'Pre-deploy copy kept for a paired rollback'* ]]
  [ "$(grep -c 'site-server backup' "$ARGS_LOG")" -eq 2 ]
  [ "$(ls -1 "$data/backups" | grep -c '^site-.*\.db$')" -eq 10 ]
}

@test 'preflight warns and prints the journal when the service is down' {
  stub systemctl <<'STUB'
#!/usr/bin/env bash
case "$*" in
  *is-active*) echo inactive ;;
  *status*) echo 'Active: inactive' ;;
esac
STUB
  stub journalctl <<'STUB'
#!/usr/bin/env bash
echo 'Main process exited, code=exited, status=14'
STUB
  run remote preflight DATA_DIR="$data" DEPLOY_PATH="$www"
  [ "$status" -eq 0 ]
  [[ "$output" == *'service: inactive'* ]]
  [[ "$output" == *'was already down before the deploy'* ]]
  [[ "$output" == *'status=14'* ]]
}

@test 'install-unit refuses to leave the database behind in the read-only tree' {
  export HOME="$BATS_TEST_TMPDIR"
  mkdir -p "$HOME/.config/systemd/user"
  printf 'unit\n' >/tmp/9site.service
  export DATA="$data"
  stub systemctl <<'STUB'
#!/usr/bin/env bash
case "$*" in
  *show*Environment*) echo "DATA_DIR=$DATA" ;;
  *) : ;;
esac
STUB
  mkdir -p "$www/var"
  echo stale >"$www/var/site.db"

  run remote install-unit DEPLOY_PATH="$www" DATA_DIR="$data"
  [ "$status" -ne 0 ]
  [[ "$output" == *'database left in the old place'* ]]
  [[ "$output" == *"$www/var/site.db"* ]]
}

@test 'dry-run passes the values of the unit to the new binary' {
  stub systemctl <<'STUB'
#!/usr/bin/env bash
case "$*" in
  *show*Environment*)
    echo 'DATA_DIR=/var/lib/9site BASE_URL=https://9swag.myftp.biz' ;;
  *) exec "$@" ;;
esac
STUB
  cat >"$www/site-server" <<'STUB'
#!/usr/bin/env bash
echo "check saw $BASE_URL and $DATA_DIR"
exit 0
STUB
  chmod +x "$www/site-server"

  run remote dry-run DEPLOY_PATH="$www"
  [ "$status" -eq 0 ]
  [[ "$output" == *'check saw https://9swag.myftp.biz and /var/lib/9site'* ]]
}

@test 'dry-run takes the deploy down before it damages the live service' {
  cat >"$www/site-server" <<'STUB'
#!/usr/bin/env bash
echo 'стоп: миграция 0003 не применилась' >&2
exit 1
STUB
  chmod +x "$www/site-server"
  stub systemctl <<'STUB'
#!/usr/bin/env bash
case "$*" in *Environment*) ;; *) exec "$@" ;; esac
STUB

  run remote dry-run DEPLOY_PATH="$www"
  [ "$status" -ne 0 ]
}

@test 'rollback restores the binary and the database, wal and shm included' {
  echo current >"$www/site-server"
  echo previous >"$www/site-server.prev"
  echo live >"$data/site.db"
  echo live >"$data/site.db-wal"
  echo live >"$data/site.db-shm"
  echo snapshot >"$data/site.db.pre-deploy"
  stub systemctl <<'STUB'
#!/usr/bin/env bash
echo "systemctl $*"
exit 0
STUB

  run remote rollback DATA_DIR="$data" DEPLOY_PATH="$www"
  [ "$status" -eq 0 ]
  [ ! -e "$www/site-server.prev" ]
  [ "$(cat "$www/site-server")" = previous ]
  [ "$(cat "$www/site-server.failed")" = current ]
  [ "$(cat "$data/site.db")" = snapshot ]
  [ ! -e "$data/site.db-wal" ]
  [ ! -e "$data/site.db-shm" ]
  [[ "$output" == *'service restarted with the rolled back binary and database'* ]]
}

@test 'rollback says so on the first deploy and leaves everything alone' {
  echo first >"$www/site-server"
  run remote rollback DATA_DIR="$data" DEPLOY_PATH="$www"
  [ "$status" -eq 0 ]
  [ "$(cat "$www/site-server")" = first ]
  [ ! -e "$www/site-server.failed" ]
  [[ "$output" == *'no site-server.prev (first deploy)'* ]]
}

@test 'check-site reports the journal when the rollback did not help' {
  stub systemctl <<'STUB'
#!/usr/bin/env bash
case "$*" in
  *is-active*) echo failed ;;
  *status*) echo 'Active: failed' ;;
esac
STUB
  stub journalctl <<'STUB'
#!/usr/bin/env bash
echo 'стоп: база знает миграции дальше'
STUB

  run remote check-site DATA_DIR="$data" DEPLOY_PATH="$www"
  [ "$status" -ne 0 ]
  [[ "$output" == *'is not active after the rollback'* ]]
  [[ "$output" == *'стоп: база знает миграции дальше'* ]]
}

@test 'check-live runs every url from the one shared list' {
  urls="$(grep -v '^#' "$root/deploy/smoke-urls.txt")"
  stub systemctl <<'STUB'
#!/usr/bin/env bash
echo active
exit 0
STUB
  stub curl <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "curl $*" >>"$ARGS_LOG"
url=""
for arg in "$@"; do case "$arg" in http*) url="$arg" ;; esac; done
if [[ "$url" == *counter* ]]; then
  echo 'x class="counter" y <span>5</span> z'
  exit 0
fi
want="$(awk -v u="$url" '$1 == u {print $2}' "$SMOKE_MAP")"
echo "${want:-500}"
STUB
  export SMOKE_MAP="$root/deploy/smoke-urls.txt"
  export ARGS_LOG="$BATS_TEST_TMPDIR/args.log"
  : >"$ARGS_LOG"

  run remote check-live "URLS=$urls" BASE_URL=http://127.0.0.1:8080/?counter=1
  [ "$status" -eq 0 ]
  [[ "$output" == *'service is active'* ]]
  [[ "$output" == *'Counter is growing'* ]]
  # 28 urls plus the two counter probes.
  [ "$(grep -c '^curl' "$ARGS_LOG")" -ge 30 ]
}

@test 'run.sh refuses a script that does not exist' {
  SSH_OPTS=/bin/cat TARGET=x run bash -c \
    'source "$1/deploy/remote/run.sh" nope' _ "$root"
  [ "$status" -ne 0 ]
  [[ "$output" == *'no such remote script: nope'* ]]
}

@test 'prepare makes the unit directory and turns on linger, no sudo' {
  export HOME="$BATS_TEST_TMPDIR"
  stub loginctl <<'STUB'
#!/usr/bin/env bash
echo "loginctl $*"
STUB
  stub systemctl <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
  run remote prepare DEPLOY_PATH="$www"
  [ "$status" -eq 0 ]
  [ -d "$HOME/.config/systemd/user" ]
  [[ "$output" == *'enable-linger'* ]]
  [[ "$output" != *sudo* ]]
}
