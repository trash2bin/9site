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

  # The files the pipeline ships before install-unit, and the environment
  # file the unit and the scripts share.
  ship="$BATS_TEST_TMPDIR/ship"
  mkdir -p "$ship"
  printf 'unit\n' >"$ship/9site.service"
  printf 'DATA_DIR=%s\nSTATIC_ROOT=%s/public\nBASE_URL=https://9swag.myftp.biz\n' \
    "$data" "$www" >"$ship/deploy.env"
  cp "$ship/deploy.env" "$www/deploy.env"

  # The site that answers exactly what deploy/smoke-urls.txt expects; a url
  # outside the list answers 500, which is how smoke failures look.
  export SMOKE_MAP="$root/deploy/smoke-urls.txt"
  urls="$(grep -v '^#' "$root/deploy/smoke-urls.txt")"
}

remote() {
  PATH="$BATS_TEST_TMPDIR/bin:$PATH" bash "$root/deploy/remote/run.sh" "$@"
}

stub() {
  cat >"$BATS_TEST_TMPDIR/bin/$1"
  chmod +x "$BATS_TEST_TMPDIR/bin/$1"
}

# A release directory the way CI leaves it: releases/<sha>/site-server.
release() {
  local dir="$www/releases/$1"
  mkdir -p "$dir"
  cat >"$dir/site-server"
  chmod +x "$dir/site-server"
}

# What the pipeline does before install-unit: the unit and the environment
# land in a private directory under the deploy path.
ship_unit() {
  mkdir -p "$www/.incoming"
  cp "$ship/9site.service" "$ship/deploy.env" "$www/.incoming/"
}

# The static set CI uploads before the deploy: the deploy swaps it in.
static_set() {
  rm -rf "$www/public.next"
  mkdir -p "$www/public.next"
  echo '<html>static</html>' >"$www/public.next/index.html"
}

# The unit reports started, so a restart is instant and the service is active.
stub_active_systemd() {
  stub systemctl <<'STUB'
#!/usr/bin/env bash
case "$*" in *is-active*) echo active ;; esac
exit 0
STUB
}

# The smoke list answered correctly, and the counter page for the probe.
stub_site_curl() {
  stub curl <<'STUB'
#!/usr/bin/env bash
url=""
for arg in "$@"; do case "$arg" in http*) url="$arg" ;; esac; done
case "$url" in *counter*) echo 'x class="counter" y <span>7</span> z' ;; esac
want="$(awk -v u="$url" '$1 == u {print $2}' "$SMOKE_MAP" 2>/dev/null | tail -n 1)"
echo "${want:-500}"
STUB
}

@test 'deploy refuses a release that was never uploaded' {
  run remote deploy SHA=nosuch "URLS=http://x/ 200" DEPLOY_PATH="$www"
  [ "$status" -ne 0 ]
  [[ "$output" == *"no $www/releases/nosuch/site-server"* ]]
}

@test 'deploy refuses a release without its static set' {
  release good <<'BINARY'
#!/usr/bin/env bash
exit 0
BINARY
  run remote deploy SHA=good "URLS=http://x/ 200" DEPLOY_PATH="$www"
  [ "$status" -ne 0 ]
  [[ "$output" == *"no $www/public.next"* ]]
  [ ! -e "$www/current" ]
}

@test 'deploy refuses a release without a sha' {
  run remote deploy "URLS=http://x/ 200" DEPLOY_PATH="$www"
  [ "$status" -ne 0 ]
  [[ "$output" == *'SHA is required'* ]]
}

@test 'deploy runs the check in the environment of the unit, then flips' {
  stub_active_systemd
  stub_site_curl
  static_set
  echo db >"$data/site.db"
  release good <<'BINARY'
#!/usr/bin/env bash
case "$1" in
  backup) cp "$DATA_DIR/site.db" "$2" ;;
  check) echo "check saw $DATA_DIR and $BASE_URL and $STATIC_ROOT" ;;
esac
exit 0
BINARY

  run remote deploy SHA=good "URLS=$urls" "PROBE_URL=http://stub/counter" \
    DEPLOY_PATH="$www"
  [ "$status" -eq 0 ]
  [[ "$output" == *"check saw $data and https://9swag.myftp.biz and $www/public.next"* ]]
  [[ "$output" == *'flipped'* ]]
  [ "$(readlink "$www/current")" = "$www/releases/good" ]
  [ -f "$www/public/index.html" ]
  [ ! -e "$www/public.next" ]
  [ -f "$data/site.db.pre-good" ]
  [[ "$output" == *'Counter is growing'* ]]
}

@test 'deploy of the same commit twice takes a fresh snapshot' {
  stub_active_systemd
  stub_site_curl
  echo db >"$data/site.db"
  # backup behaves like the real one: VACUUM INTO refuses an existing file.
  release good <<'BINARY'
#!/usr/bin/env bash
case "$1" in
  backup)
    if [ -e "$2" ]; then echo 'output file already exists' >&2; exit 1; fi
    cp "$DATA_DIR/site.db" "$2"
    ;;
esac
exit 0
BINARY

  static_set
  run remote deploy SHA=good "URLS=$urls" "PROBE_URL=http://stub/counter" \
    DEPLOY_PATH="$www"
  [ "$status" -eq 0 ]

  # The retry of a failed attempt is the normal path: the snapshot of the
  # first attempt is still there and must not be an error.
  static_set
  run remote deploy SHA=good "URLS=$urls" "PROBE_URL=http://stub/counter" \
    DEPLOY_PATH="$www"
  [ "$status" -eq 0 ]
  [[ "$output" == *"Snapshot: $data/backups/site-good.db"* ]]
  [ -f "$data/site.db.pre-good" ]
}

@test 'deploy keeps ten snapshots and ten pre-deploy copies' {
  stub_active_systemd
  stub_site_curl
  echo db >"$data/site.db"
  mkdir -p "$data/backups"
  # Fifteen deploys of history, one file of each kind per deploy.
  for n in $(seq 1 15); do
    touch -t "20200101$(printf '%02d' "$n")00" \
      "$data/backups/site-old$n.db" "$data/site.db.pre-old$n"
  done
  release good <<'BINARY'
#!/usr/bin/env bash
case "$1" in backup) cp "$DATA_DIR/site.db" "$2" ;; esac
exit 0
BINARY

  static_set
  run remote deploy SHA=good "URLS=$urls" "PROBE_URL=http://stub/counter" \
    DEPLOY_PATH="$www"
  [ "$status" -eq 0 ]
  [ "$(ls -1 "$data"/backups/site-*.db | wc -l)" -eq 10 ]
  [ "$(ls -1 "$data"/site.db.pre-* | wc -l)" -eq 10 ]
  [ -f "$data/site.db.pre-good" ]
}

@test 'deploy stops before the flip when the check fails' {
  static_set
  echo db >"$data/site.db"
  release dead <<'BINARY'
#!/usr/bin/env bash
if [ "$1" = check ]; then
  echo 'стоп: миграция 0003 не применилась' >&2
  exit 1
fi
exit 0
BINARY

  run remote deploy SHA=dead "URLS=http://x/ 200" DEPLOY_PATH="$www"
  [ "$status" -ne 0 ]
  [[ "$output" == *'миграция 0003'* ]]
  [[ "$output" == *'the deploy stops before the flip'* ]]
  [ ! -e "$www/current" ]
  [ ! -e "$data/site.db.pre-dead" ]
  [ ! -e "$www/public" ]
  [ -d "$www/public.next" ]
}

@test 'deploy rolls back a release that dies on start' {
  # The site serves the previous release; the new one passes its check, is
  # flipped in, and never comes up.
  release good <<'BINARY'
#!/usr/bin/env bash
case "$1" in backup) cp "$DATA_DIR/site.db" "$2" ;; esac
exit 0
BINARY
  ln -sfn "$www/releases/good" "$www/current"
  mkdir -p "$www/public"
  echo '<html>serving</html>' >"$www/public/index.html"
  printf 'snapshot:live\n' >"$data/site.db"
  release fatal <<'BINARY'
#!/usr/bin/env bash
case "$1" in
  backup) cp "$DATA_DIR/site.db" "$2" ;;
  check) exit 0 ;;
esac
exit 0
BINARY

  # Type=notify: the restart job only finishes when the service reports ready,
  # and a restart loop never says active.
  stub systemctl <<'STUB'
#!/usr/bin/env bash
case "$*" in
  *is-active*) echo activating ;;
  *restart*) exit 0 ;;
esac
exit 0
STUB
  stub sleep <<'STUB'
#!/usr/bin/env bash
exit 0
STUB

  static_set
  run remote deploy SHA=fatal "URLS=http://x/ 200" DEPLOY_PATH="$www"
  [ "$status" -ne 0 ]
  [ "$(readlink "$www/current")" = "$www/releases/good" ]
  [ "$(cat "$www/public/index.html")" = '<html>serving</html>' ]
  [ -d "$www/public.rejected" ]
  [ "$(cat "$data/site.db")" = "$(cat "$data/site.db.pre-fatal")" ]
  [ -d "$www/releases/fatal.rejected" ]
  [ "$(cat "$data/release-before")" = "$www/releases/good" ]
}

@test 'the manual rollback returns to the release before it' {
  stub_active_systemd
  stub_site_curl
  release good <<'BINARY'
#!/usr/bin/env bash
exit 0
BINARY
  ln -sfn "$www/releases/good" "$www/current"
  printf '%s\n' "$www/releases/good" >"$data/release-before"
  # The released database in the new schema, and the copy the deploy took
  # before it flipped: the rollback owes the second one.
  printf 'new-schema!\n' >"$data/site.db"
  printf 'live-db\n' >"$data/site.db.pre-bad"
  release bad <<'BINARY'
#!/usr/bin/env bash
case "$1" in backup) cp "$DATA_DIR/site.db" "$2" ;; esac
exit 0
BINARY
  ln -sfn "$www/releases/bad" "$www/current"

  run remote rollback "URLS=$urls" "PROBE_URL=http://stub/counter" \
    DEPLOY_PATH="$www"
  [ "$status" -eq 0 ]
  [ "$(readlink "$www/current")" = "$www/releases/good" ]
  [ -d "$www/releases/bad.rejected" ]
  [ "$(cat "$data/site.db")" = live-db ]
  [[ "$output" == *'database rolled back'* ]]
}

@test 'the rollback of a first release says so and leaves everything alone' {
  release first <<'BINARY'
#!/usr/bin/env bash
exit 0
BINARY
  ln -sfn "$www/releases/first" "$www/current"

  run remote rollback "URLS=http://x/ 200" DEPLOY_PATH="$www"
  [ "$status" -eq 0 ]
  [[ "$output" == *'no release recorded before'* ]]
  [ -d "$www/releases/first" ]
  [ ! -e "$www/releases/first.rejected" ]
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
  run remote preflight DEPLOY_PATH="$www"
  [ "$status" -eq 0 ]
  [[ "$output" == *'service: inactive'* ]]
  [[ "$output" == *'was already down before the deploy'* ]]
  [[ "$output" == *'status=14'* ]]
}

@test 'preflight records the counter the site serves before the deploy' {
  stub systemctl <<'STUB'
#!/usr/bin/env bash
case "$*" in *is-active*) echo active ;; esac
STUB
  stub curl <<'STUB'
#!/usr/bin/env bash
echo 'x class="counter" y <span>1</span><span>3</span> z'
STUB
  run remote preflight DEPLOY_PATH="$www"
  [ "$status" -eq 0 ]
  [ "$(cat "$data/.counter-before")" = 13 ]
  [[ "$output" == *'counter before the deploy: 13'* ]]
}

@test 'install-unit refuses to leave the database behind in the read-only tree' {
  export HOME="$BATS_TEST_TMPDIR"
  mkdir -p "$HOME/.config/systemd/user"
  mkdir -p "$www/var"
  echo stale >"$www/var/site.db"
  rm -f "$data/site.db"
  ship_unit

  run remote install-unit DEPLOY_PATH="$www"
  [ "$status" -ne 0 ]
  [[ "$output" == *'live database'* ]]
  [[ "$output" == *"$www/var/site.db"* ]]
}

@test 'install-unit installs the unit and seeds the environment file' {
  export HOME="$BATS_TEST_TMPDIR"
  mkdir -p "$HOME/.config/systemd/user"
  stub systemctl <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
  rm -f "$www/deploy.env"
  ship_unit

  run remote install-unit DEPLOY_PATH="$www"
  [ "$status" -eq 0 ]
  [ -f "$HOME/.config/systemd/user/9site.service" ]
  [ -f "$www/deploy.env" ]
  # The file carries the SMTP password once it is filled in.
  [ "$(ls -l "$www/deploy.env" | cut -c1-10)" = '-rw-------' ]
  [ ! -e "$www/.incoming" ]
  [[ "$output" == *"unit installed, DATA_DIR=$data"* ]]
}

@test 'install-unit keeps the environment file the server already has' {
  export HOME="$BATS_TEST_TMPDIR"
  mkdir -p "$HOME/.config/systemd/user"
  stub systemctl <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
  printf 'DATA_DIR=%s\nSMTP_PASS=hand-set\n' "$data" >"$www/deploy.env"
  ship_unit

  run remote install-unit DEPLOY_PATH="$www"
  [ "$status" -eq 0 ]
  # A deploy that overwrote it would take the password and every hand edit away.
  [ "$(cat "$www/deploy.env")" = "DATA_DIR=$data
SMTP_PASS=hand-set" ]
  [[ "$output" == *'differs from the repository, the server keeps its own'* ]]
  [[ "$output" == *'SMTP_PASS=hand-set'* ]]
}

@test 'install-unit refuses to run without the files the pipeline ships' {
  export HOME="$BATS_TEST_TMPDIR"
  mkdir -p "$HOME/.config/systemd/user"

  run remote install-unit DEPLOY_PATH="$www"
  [ "$status" -ne 0 ]
  [[ "$output" == *"no $www/.incoming/9site.service"* ]]
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
