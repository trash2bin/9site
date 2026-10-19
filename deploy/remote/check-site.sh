#!/usr/bin/env bash
# Variables come from parse_args in lib.sh.
# shellcheck disable=SC2154
parse_args "$@"

if ! wait_active; then
  echo 'rollback did not help, the site is still down, status and log:'
  journal_report '9site is not active after the rollback' 160
  exit 1
fi

code="$(curl -s -o /dev/null -w '%{http_code}' "$probe_url" || true)"
printf '  service: %s, GET / -> %s\n' active "${code:-no response}"
if [ "$code" != 200 ]; then
  echo 'rollback did not help, the site is still down, status and log:'
  journal_report 'the site does not answer after the rollback' 160
  exit 1
fi
echo 'Site responds again after rollback'
