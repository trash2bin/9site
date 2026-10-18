#!/usr/bin/env bash
# Helpers for the scripts the deploy runs on the server.

# Non-login ssh sets no XDG_RUNTIME_DIR; the user bus lives at this fixed
# path, and linger keeps the manager and the directory up.
XDG_RUNTIME_DIR="/run/user/$(id -u)"
export XDG_RUNTIME_DIR

# Some values are used only by the scripts concatenated after this file.
# shellcheck disable=SC2034
parse_args() {
  data_dir="" deploy_path="" base_url="http://127.0.0.1:8080"
  restart_cmd="systemctl --user restart 9site" urls="" stamp=""
  for arg in "$@"; do
    case "$arg" in
      DATA_DIR=*) data_dir="${arg#*=}" ;;
      DEPLOY_PATH=*) deploy_path="${arg#*=}" ;;
      BASE_URL=*) base_url="${arg#*=}" ;;
      RESTART_CMD=*) restart_cmd="${arg#*=}" ;;
      URLS=*) urls="${arg#*=}" ;;
      STAMP=*) stamp="${arg#*=}" ;;
      *) echo "unknown argument: $arg" >&2; return 1 ;;
    esac
  done
}

# A restart loop spends seconds in "activating".
wait_active() {
  local state
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    state="$(systemctl --user is-active 9site || true)"
    if [ "$state" = active ]; then
      return 0
    fi
    sleep 2
  done
  return 1
}

# The user view holds only the per-uid journal files; a lingering user
# manager writes its units into the system journal, where the same line is
# reachable through the unit match. Reading both keeps the report working
# in either layout.
journal_lines() {
  local out
  out="$(journalctl --user -u 9site -n "$1" --no-pager 2>/dev/null || true)"
  case "$out" in
    '' | *'-- No entries --'*)
      out="$(journalctl _SYSTEMD_USER_UNIT=9site.service -n "$1" --no-pager 2>/dev/null || true)"
      ;;
  esac
  printf '%s\n' "$out"
}

journal_report() {
  local title="$1" lines="$2"
  echo "### $title"
  echo
  echo '```'
  systemctl --user status 9site --no-pager -l -n 20 || true
  echo '--- journalctl ---'
  journal_lines "$lines"
  echo '```'
}

# One list for the deploy check and the rollback check.
smoke() {
  local url want code rc=0
  while read -r url want; do
    case "$url" in ''|\#*) continue ;; esac
    # A Type=simple unit says active before the listener is up, and a slow
    # start on a big database is not a failed deploy: wait for the port.
    code="$(curl -s --max-time 5 --retry 3 --retry-delay 1 \
      --retry-connrefused --retry-all-errors \
      -o /dev/null -w '%{http_code}' "$url" || true)"
    printf '  %-36s %s\n' "$url" "${code:-no response}"
    if [ "$code" != "$want" ]; then
      echo "Expected $want, got ${code:-no response}"
      rc=1
    fi
  done <<< "$urls"
  return "$rc"
}

counter_value() {
  curl -s "$base_url" \
    | tr -d '\n' \
    | awk -F'class="counter"' '{print $2}' \
    | head -c 200 \
    | grep -o '<span>[0-9]</span>' | grep -o '[0-9]' | tr -d '\n'
}

# The number the site served before the deploy comes from preflight, and the
# site is measured against it: two reads after the restart cannot tell a
# healthy site from a recreated database, because a reset counter grows from
# zero exactly the same way.
check_counter() {
  local recorded before after second
  recorded=""
  if [ -n "$data_dir" ] && [ -f "$data_dir/.counter-before" ]; then
    recorded="$(cat "$data_dir/.counter-before")"
  fi

  after="$(counter_value)"
  if [ -z "$after" ]; then
    echo 'Counter not found in the response'
    return 1
  fi

  if [ -n "$recorded" ]; then
    before=$((10#$recorded))
    if [ "$((10#$after))" -lt "$before" ]; then
      echo "Counter went down: $before -> $((10#$after))"
      return 1
    fi
    echo 'Counter holds the value from before the deploy'
    return 0
  fi

  # Nothing recorded, the first deploy: two reads of a live site.
  second="$(counter_value)"
  before=$((10#$after))
  if [ "$((10#$second))" -lt "$before" ]; then
    echo "Counter went down: $before -> $((10#$second))"
    return 1
  fi
  echo 'Counter is growing'
}

# No data_dir check here would silently create an empty database and reset
# the counter to zero.
guard_db_location() {
  if [ -z "$data_dir" ] || [ -f "$data_dir/site.db" ]; then
    return 0
  fi
  if [ ! -f "$deploy_path/var/site.db" ]; then
    return 0
  fi
  echo "DATA_DIR=$data_dir is empty, but the site keeps its data elsewhere" >&2
  echo "live database: $deploy_path/var/site.db" >&2
  echo "point the unit at it, then deploy: DATA_DIR=$deploy_path/var" >&2
  echo "  or move it: cp $deploy_path/var/site.db $data_dir/site.db" >&2
  exit 1
}

restore_db() {
  if [ ! -f "$data_dir/site.db.pre-deploy" ]; then
    echo 'no site.db.pre-deploy: nothing to restore' >&2
    return 0
  fi
  cp -f "$data_dir/site.db" "$data_dir/site.db.new-schema"
  cp -f "$data_dir/site.db.pre-deploy" "$data_dir/site.db"
  rm -f "$data_dir/site.db-wal" "$data_dir/site.db-shm"
  echo 'database rolled back to the pre-deploy snapshot'
}

