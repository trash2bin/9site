#!/usr/bin/env bash
# Helpers for the scripts the deploy runs on the server.

# Non-login ssh sets no XDG_RUNTIME_DIR; the user bus lives at this fixed
# path, and linger keeps the manager and the directory up.
XDG_RUNTIME_DIR="/run/user/$(id -u)"
export XDG_RUNTIME_DIR

# One name for the unit, everywhere: the user manager knows the service only
# by it.
UNIT=9site

# Some values are used only by the scripts concatenated after this file.
# shellcheck disable=SC2034
parse_args() {
  deploy_path="" probe_url="http://127.0.0.1:8080"
  sha="" urls=""
  for arg in "$@"; do
    case "$arg" in
      DEPLOY_PATH=*) deploy_path="${arg#*=}" ;;
      PROBE_URL=*) probe_url="${arg#*=}" ;;
      SHA=*) sha="${arg#*=}" ;;
      URLS=*) urls="${arg#*=}" ;;
      *) echo "unknown argument: $arg" >&2; return 1 ;;
    esac
  done
}

# The unit takes its environment from one file under the deploy path; the
# scripts read the same file the unit reads, so the two cannot disagree.
load_env() {
  local file="$1/deploy.env"
  if [ ! -f "$file" ]; then
    echo "no $file: no environment for the unit, run the setup first" >&2
    return 1
  fi
  set -a
  # shellcheck source=/dev/null
  . "$file"
  set +a
  # The file spells it DATA_DIR, the scripts carry data_dir: one directory,
  # one name each. probe_url stays the loopback parse_args gave it; the
  # BASE_URL of the file is the public site, not what the checks talk to.
  data_dir="$DATA_DIR"
}

# A restart loop spends seconds in "activating".
wait_active() {
  local state
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    state="$(systemctl --user is-active "$UNIT" || true)"
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
  out="$(journalctl --user -u "$UNIT" -n "$1" --no-pager 2>/dev/null || true)"
  case "$out" in
    '' | *'-- No entries --'*)
      out="$(journalctl _SYSTEMD_USER_UNIT=$UNIT.service -n "$1" --no-pager 2>/dev/null || true)"
      ;;
  esac
  printf '%s\n' "$out"
}

journal_report() {
  local title="$1" lines="$2"
  echo "### $title"
  echo
  echo '```'
  systemctl --user status "$UNIT" --no-pager -l -n 20 || true
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
    # 000 is a connection that never happened. Waiting it out on every url
    # only stretches a dead deploy: one such url is the answer.
    if [ "$code" = 000 ]; then
      echo "no answer from $url, the service is not listening"
      return 1
    fi
    if [ "$code" != "$want" ]; then
      echo "Expected $want, got ${code:-no response}"
      rc=1
    fi
  done <<< "$urls"
  return "$rc"
}

counter_value() {
  curl -s "$probe_url" \
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

# The static set travels with the release: CI uploads it to public.next, the
# deploy swaps it in only after the new binary passed its checks, and a
# rollback swaps the previous set back. Both are renames on one filesystem,
# so the window where public/ is missing is one rename long.
static_swap_in() {
  rm -rf "$deploy_path/public.old"
  if [ -d "$deploy_path/public" ]; then
    mv "$deploy_path/public" "$deploy_path/public.old"
  fi
  mv "$deploy_path/public.next" "$deploy_path/public"
  echo "static swapped in from $deploy_path/public.next"
}

# The set the site served before the deploy, back in place; the rejected one
# stays next to it, the way a rejected release does.
static_swap_back() {
  if [ ! -d "$deploy_path/public.old" ]; then
    echo 'no static set from before the deploy, the files stay as they are'
    return 0
  fi
  rm -rf "$deploy_path/public.rejected"
  if [ -d "$deploy_path/public" ]; then
    mv "$deploy_path/public" "$deploy_path/public.rejected"
  fi
  mv "$deploy_path/public.old" "$deploy_path/public"
  echo "static rolled back to the set from before the deploy, the rejected one is $deploy_path/public.rejected"
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

# restore_db возвращает базу из снимка прошлого релиза. Снимок делал
# VACUUM INTO, то есть в схеме того релиза, wal и shm этой схемы сносим:
# иначе открылась бы будущая.
restore_db() {
  local snapshot="$1"
  if [ ! -f "$snapshot" ]; then
    echo "no $snapshot: nothing to restore" >&2
    return 0
  fi
  cp -f "$data_dir/site.db" "$data_dir/site.db.new-schema"
  cp -f "$snapshot" "$data_dir/site.db"
  rm -f "$data_dir/site.db-wal" "$data_dir/site.db-shm"
  echo "database rolled back to $snapshot"
}

# check_live - общая проверка после рестарта: сервис слушает, весь smoke
# список отвечает, счётчик держит значение до деплоя.
check_live() {
  if ! wait_active; then
    echo 'service is not active after the restart, status and log:'
    journal_report 'the service is not active after the restart' 120
    return 1
  fi
  smoke || return 1
  check_counter || return 1
}

