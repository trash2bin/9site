#!/usr/bin/env bash
# Variables come from parse_args in lib.sh.
# shellcheck disable=SC2154
# One rollback script for the automatic case and for the manual workflow.

parse_args "$@"
load_env "$deploy_path" || exit 1

if [ ! -f "$deploy_path/site-server.prev" ]; then
  echo 'no site-server.prev (first deploy), nothing to roll back to' >&2
  exit 0
fi

if systemctl --user stop 9site; then
  echo 'service stopped for the restore'
else
  echo 'warning: stop returned non-zero, the service may be down already' >&2
fi

mv -f "$deploy_path/site-server" "$deploy_path/site-server.failed"
mv -f "$deploy_path/site-server.prev" "$deploy_path/site-server"
echo 'rolled back, rejected binary kept as site-server.failed'

restore_db "$data_dir"

systemctl --user reset-failed 9site || true
# From the repository variable, splitting is intended.
# shellcheck disable=SC2086
$restart_cmd
echo 'service restarted with the rolled back binary and database'
