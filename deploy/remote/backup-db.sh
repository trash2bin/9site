#!/usr/bin/env bash
# Variables come from parse_args in lib.sh.
# shellcheck disable=SC2154
# Before the restart, not after: migrations run when the new binary starts.

parse_args "$@"

mkdir -p "$data_dir/backups"
if [ ! -f "$data_dir/site.db" ]; then
  echo 'no site.db yet: first deploy, snapshot skipped'
  exit 0
fi

"$deploy_path/site-server" backup "$data_dir/backups/site-$stamp.db"
(
  cd "$data_dir/backups" || exit 1
  ls -1t site-*.db 2>/dev/null | tail -n +11 | xargs -r rm -f
)
echo "Backup: $data_dir/backups/site-$stamp.db"

rm -f "$data_dir/site.db.pre-deploy"
"$deploy_path/site-server" backup "$data_dir/site.db.pre-deploy"
echo 'Pre-deploy copy kept for a paired rollback'
