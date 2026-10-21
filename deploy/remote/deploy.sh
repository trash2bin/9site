#!/usr/bin/env bash
# Один релиз целиком: снимок базы, проверка бинарника, флип симлинка current,
# рестарт, проверка сайта. Всё, что случилось после флипа и не удалось,
# откатывается: прошлый релиз, база, на которой он работал, рестарт.
#
#   deploy remote SHA=<sha> URLS=...

# Variables come from parse_args in lib.sh.
# shellcheck disable=SC2154
# On the server only the checks below decide, and a command that breaks must
# not be skipped over: a flip that never happened used to look like a deploy.
set -euo pipefail

parse_args "$@"

if [ -z "$sha" ]; then
  echo 'SHA is required: which release this deploy is' >&2
  exit 1
fi
load_env "$deploy_path" || exit 1

release="$deploy_path/releases/$sha"
current="$deploy_path/current"
binary="$release/site-server"

if [ ! -x "$binary" ]; then
  echo "no $binary: upload the release before the deploy" >&2
  exit 1
fi

# The static files the release is written against. They sit next to the live
# set until the new binary proves itself against them: uploading them over the
# serving tree first would leave new pages in place under a binary that never
# passed its checks, and a rollback would not give the old pages back.
if [ ! -d "$deploy_path/public.next" ]; then
  echo "no $deploy_path/public.next: upload the static files before the deploy" >&2
  exit 1
fi

# The release the site runs now, for the rollback below. The first deploy has
# no current yet, so the binary the server still runs becomes the release to
# go back to: the first deploy can be rolled back too.
before="$(readlink "$current" 2>/dev/null || true)"
if [ -z "$before" ] && [ -x "$deploy_path/site-server" ]; then
  before="$deploy_path/releases/legacy"
  mkdir -p "$before"
  cp -f "$deploy_path/site-server" "$before/site-server"
  chmod 755 "$before/site-server"
fi

# Snapshot before anything migrates the live database: VACUUM INTO copies the
# schema as it stands, so the rollback gets back the database the previous
# release ran on. The copies of an earlier attempt at the same commit go
# first: VACUUM INTO refuses an existing file, and re-running a failed deploy
# is the normal way to retry it.
if [ -f "$data_dir/site.db" ]; then
  mkdir -p "$data_dir/backups"
  rm -f "$data_dir/backups/site-$sha.db" "$data_dir/site.db.pre-$sha"
  if ! "$binary" backup "$data_dir/backups/site-$sha.db" \
    || ! "$binary" backup "$data_dir/site.db.pre-$sha"; then
    echo 'snapshot failed: the release cannot read the live database'
    exit 1
  fi
  echo "Snapshot: $data_dir/backups/site-$sha.db, pre-deploy copy kept for the rollback"
  # One file of each kind per deploy, no delete anywhere else: ten of the
  # newest is five deploys of history, enough for the rollback and small
  # enough for the disk. The pre-deploy copies live next to the database, not
  # in backups/, so they need their own list.
  ls -1t "$data_dir"/backups/site-*.db 2>/dev/null | tail -n +11 | xargs -r rm -f || true
  ls -1t "$data_dir"/site.db.pre-* 2>/dev/null | tail -n +11 | xargs -r rm -f || true
else
  echo 'no site.db yet: first deploy, snapshot skipped'
fi

# Startup checks in the environment of the unit, but against the static set
# this release travels with. A binary that cannot run never reaches the flip,
# and the site keeps serving what it serves.
if ! STATIC_ROOT="$deploy_path/public.next" "$binary" check; then
  echo 'check failed, the deploy stops before the flip'
  exit 1
fi
echo 'check passed'

static_swap_in

if [ -n "$before" ]; then
  printf '%s\n' "$before" >"$data_dir/release-before"
else
  rm -f "$data_dir/release-before"
fi
ln -sfn "$release" "$current"
echo "flipped $current to $release"

if ! systemctl --user restart "$UNIT"; then
  echo 'the restart did not bring the service up, status and log:'
  journal_report 'the restart failed' 100
fi

if check_live; then
  echo 'the site answers the smoke list and holds the counter'
  # Releases accumulate one per commit. Five is enough history for a rollback
  # and small enough not to fill the disk.
  ls -1dt "$deploy_path"/releases/*/ 2>/dev/null | tail -n +6 | xargs -r rm -rf || true
  exit 0
fi

echo 'deploy failed, undoing it'
journal_report 'the deploy failed' 200
if [ -z "$before" ] || [ "$before" = "$release" ]; then
  echo 'nothing to roll back to: this was the first release'
  exit 1
fi

ln -sfn "$before" "$current"
static_swap_back
if mv -f "$release" "$release.rejected"; then
  echo "rolled back to $before, the rejected release is kept as $release.rejected"
else
  # current points at $before already, so the site is safe either way: the
  # release keeps its name and is simply not quarantined.
  echo "rolled back to $before, the rejected release kept the name $release"
fi
restore_db "$data_dir/site.db.pre-$sha"

systemctl --user reset-failed "$UNIT" 2>/dev/null || true
systemctl --user restart "$UNIT" || true
if check_live; then
  echo 'the site answers again with the rolled back release'
else
  echo 'the rollback did not bring the site back, this needs a human'
fi
exit 1
