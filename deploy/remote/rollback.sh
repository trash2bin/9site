#!/usr/bin/env bash
# Откат вручную: релиз до последнего деплоя, база, на которой он работал,
# рестарт. Один скрипт для workflow и для человека.
#
#   rollback remote URLS=...

# Variables come from parse_args in lib.sh.
# shellcheck disable=SC2154
# On the server only the checks below decide, and a command that breaks must
# not be skipped over: a flip that never happened used to look like a deploy.
set -euo pipefail

parse_args "$@"

load_env "$deploy_path" || exit 1

current="$deploy_path/current"
running="$(readlink "$current" 2>/dev/null || true)"
if [ -z "$running" ]; then
  echo 'no current release: nothing to roll back' >&2
  exit 0
fi

# The deploy writes the release the site ran before it flipped, so a manual
# rollback returns exactly to it.
before_file="$data_dir/release-before"
before="$(cat "$before_file" 2>/dev/null || true)"
if [ -z "$before" ]; then
  echo "no release recorded before $running: nothing to roll back to" >&2
  exit 0
fi
if [ ! -x "$before/site-server" ]; then
  echo "no $before/site-server: the release is gone, roll back by hand" >&2
  exit 1
fi
if [ "$before" = "$running" ]; then
  echo "the site already runs $before: nothing to roll back"
  exit 0
fi

sha="$(basename "$running")"
systemctl --user stop "$UNIT" || true
mv -f "$running" "$running.rejected"
ln -sfn "$before" "$current"
echo "rolled back to $before, the rejected release is kept as $running.rejected"
static_swap_back
restore_db "$data_dir/site.db.pre-$sha"

systemctl --user reset-failed "$UNIT" 2>/dev/null || true
systemctl --user restart "$UNIT" || true

if check_live; then
  echo 'the site answers again with the rolled back release'
  exit 0
fi
echo 'the rollback did not bring the site back, this needs a human'
exit 1
