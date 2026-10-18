#!/usr/bin/env bats
# Behaviour of deploy/lib.sh, which runs on the server. The server commands
# are stubs on PATH, so the tests answer for a restart loop, a journal with
# a reason in it, and a site that serves pages.

setup() {
  root="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
  # shellcheck source=../../lib.sh
  source "$root/deploy/lib.sh"

  bin="$BATS_TEST_TMPDIR/bin"
  mkdir -p "$bin"
  export PATH="$bin:$PATH"

  args="$BATS_TEST_TMPDIR/args.log"
  : >"$args"

  # The site that answers exactly what deploy/smoke-urls.txt expects; a url
  # outside the list answers 500, which is how smoke failures look.
  export SMOKE_MAP="$root/deploy/smoke-urls.txt"

  # Default stubs, silent and successful: a test that needs a specific answer
  # writes its own and shadows these.
  for cmd in systemctl journalctl; do
    cat >"$bin/$cmd" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
    chmod +x "$bin/$cmd"
  done

  cat >"$bin/curl" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "curl $*" >>"$ARGS_LOG"
url=""
for arg in "$@"; do
  case "$arg" in http*) url="$arg" ;; esac
done
if [[ "$url" == *counter* ]]; then
  echo 'x class="counter" y <span>7</span> z'
  exit 0
fi
want="$(awk -v u="$url" '$1 == u { print $2 }' "$SMOKE_MAP" 2>/dev/null | tail -n 1)"
echo "${want:-500}"
STUB
  chmod +x "$bin/curl"

  export ARGS_LOG="$args"
}

@test 'wait_active returns as soon as the service says active' {
  cat >"$bin/systemctl" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "systemctl $*" >>"$ARGS_LOG"
case "$*" in *is-active*) echo active ;; esac
exit 0
STUB
  chmod +x "$bin/systemctl"
  run wait_active
  [ "$status" -eq 0 ]
  grep -q 'systemctl --user is-active 9site' "$args"
}

@test 'wait_active gives a restart loop ten tries and then fails' {
  cat >"$bin/systemctl" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "systemctl $*" >>"$ARGS_LOG"
case "$*" in *is-active*) echo activating ;; esac
exit 0
STUB
  chmod +x "$bin/systemctl"
  sleep() { :; }
  export -f sleep
  run wait_active
  [ "$status" -ne 0 ]
  # Ten attempts: the loop asks is-active each time, the stub logs each call.
  [ "$(grep -c '^systemctl --user is-active 9site$' "$args")" -eq 10 ]
}

@test 'journal_report prints status, journal and the summary frame' {
  cat >"$bin/systemctl" <<'STUB'
#!/usr/bin/env bash
case "$*" in *status*) echo 'Active: failed' ;; esac
exit 0
STUB
  chmod +x "$bin/systemctl"
  cat >"$bin/journalctl" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "journalctl $*" >>"$ARGS_LOG"
case "$*" in *-u*) echo 'стоп: база: var/site.db: unable to open database file' ;; esac
exit 0
STUB
  chmod +x "$bin/journalctl"

  run journal_report 'site down after deploy' 160
  [ "$status" -eq 0 ]
  [[ "$output" == *'### site down after deploy'* ]]
  [[ "$output" == *'Active: failed'* ]]
  [[ "$output" == *'--- journalctl ---'* ]]
  [[ "$output" == *'стоп: база'* ]]
  [[ "$output" == *'```'* ]]
  grep -q -- '--no-pager' "$args"
}

@test 'smoke reads url and expected code from the shared list' {
  urls="$(grep -v '^#' "$root/deploy/smoke-urls.txt")"
  run smoke
  [ "$status" -eq 0 ]
  [ "$(grep -c '^curl' "$args")" -eq "$(printf '%s\n' "$urls" | wc -l)" ]
}

@test 'smoke fails on a url that answers the wrong code' {
  urls='http://x/ 200'
  cat >"$bin/curl" <<'STUB'
#!/usr/bin/env bash
echo 500
exit 0
STUB
  chmod +x "$bin/curl"
  run smoke
  [ "$status" -ne 0 ]
  [[ "$output" == *'Expected 200, got 500'* ]]
}

@test 'smoke ignores comments and blank lines' {
  urls='# a comment

http://x/ 200'
  SMOKE_MAP="$BATS_TEST_TMPDIR/map"
  echo 'http://x/ 200' >"$SMOKE_MAP"
  run smoke
  [ "$status" -eq 0 ]
  [ "$(grep -c '^curl' "$args")" -eq 1 ]
}

@test 'restore_db removes wal and shm next to the restored database' {
  data_dir="$BATS_TEST_TMPDIR/data"
  mkdir -p "$data_dir"
  echo old >"$data_dir/site.db"
  echo live >"$data_dir/site.db-wal"
  echo live >"$data_dir/site.db-shm"
  echo snapshot >"$data_dir/site.db.pre-deploy"

  run restore_db
  [ "$status" -eq 0 ]
  [ "$(cat "$data_dir/site.db")" = snapshot ]
  [ ! -e "$data_dir/site.db-wal" ]
  [ ! -e "$data_dir/site.db-shm" ]
  [ -f "$data_dir/site.db.new-schema" ]
  [[ "$output" == *'database rolled back'* ]]
}

@test 'restore_db says so when there is no snapshot' {
  data_dir="$BATS_TEST_TMPDIR/data"
  mkdir -p "$data_dir"
  run restore_db
  [ "$status" -eq 0 ]
  [[ "$output" == *'nothing to restore'* ]]
  [ ! -e "$data_dir/site.db.new-schema" ]
}

@test 'guard_db_location passes when the unit points at the live database' {
  data_dir="$BATS_TEST_TMPDIR/data"
  deploy_path="$BATS_TEST_TMPDIR/www"
  mkdir -p "$data_dir" "$deploy_path/var"
  echo live >"$data_dir/site.db"
  echo stale >"$deploy_path/var/site.db"
  run guard_db_location
  [ "$status" -eq 0 ]
}

@test 'guard_db_location stops when the database was left in the old place' {
  data_dir="$BATS_TEST_TMPDIR/data"
  deploy_path="$BATS_TEST_TMPDIR/www"
  mkdir -p "$data_dir" "$deploy_path/var"
  echo stale >"$deploy_path/var/site.db"

  run guard_db_location
  [ "$status" -ne 0 ]
  [[ "$output" == *"$deploy_path/var/site.db"* ]]
  [[ "$output" == *'cp '* ]]
}

@test 'check_counter fails when the number goes down' {
  base_url=http://stub/counter
  cat >"$bin/curl" <<'STUB'
#!/usr/bin/env bash
n=$(cat "$BATS_TEST_TMPDIR/n" 2>/dev/null || echo 3)
echo "x class=\"counter\" y <span>$n</span> z"
echo $((n - 1)) >"$BATS_TEST_TMPDIR/n"
STUB
  chmod +x "$bin/curl"
  run check_counter
  [ "$status" -ne 0 ]
  [[ "$output" == *'Counter went down: 3 -> 2'* ]]
}

@test 'check_counter passes when the number stays or grows' {
  base_url=http://stub/counter
  cat >"$bin/curl" <<'STUB'
#!/usr/bin/env bash
echo 'x class="counter" y <span>7</span> z'
STUB
  chmod +x "$bin/curl"
  run check_counter
  [ "$status" -eq 0 ]
  [[ "$output" == *'Counter is growing'* ]]
}

@test 'check_counter refuses a counter reset by a recreated database' {
  base_url=http://stub/counter
  data_dir="$BATS_TEST_TMPDIR/data"
  mkdir -p "$data_dir"
  printf '4242\n' >"$data_dir/.counter-before"
  cat >"$bin/curl" <<'STUB'
#!/usr/bin/env bash
echo 'x class="counter" y <span>0</span> z'
STUB
  chmod +x "$bin/curl"
  run check_counter
  [ "$status" -ne 0 ]
  [[ "$output" == *'Counter went down: 4242 -> 0'* ]]
}

@test 'check_counter measures the site against the recorded number' {
  base_url=http://stub/counter
  data_dir="$BATS_TEST_TMPDIR/data"
  mkdir -p "$data_dir"
  printf '7\n' >"$data_dir/.counter-before"
  cat >"$bin/curl" <<'STUB'
#!/usr/bin/env bash
echo 'x class="counter" y <span>9</span> z'
STUB
  chmod +x "$bin/curl"
  run check_counter
  [ "$status" -eq 0 ]
  [[ "$output" == *'Counter holds the value from before the deploy'* ]]
}

@test 'parse_args keeps a value with spaces and rejects an unknown key' {
  # Called directly, not through run: run isolates in a subshell, so the
  # values would not reach the caller, which is how the scripts use it.
  parse_args 'DATA_DIR=/var/lib/9site' 'DEPLOY_PATH=/var/www/9site' 'URLS=http://x/ 200
http://y/ 404'
  [ "$data_dir" = /var/lib/9site ]
  [ "$deploy_path" = /var/www/9site ]
  [ "$(printf '%s\n' "$urls" | wc -l)" -eq 2 ]

  run parse_args 'WHAT=ever'
  [ "$status" -ne 0 ]
}
