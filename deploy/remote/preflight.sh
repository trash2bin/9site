#!/usr/bin/env bash
# Variables come from parse_args in lib.sh.
# shellcheck disable=SC2154
parse_args "$@"

state="$(systemctl --user is-active 9site || true)"
code="$(curl -s -o /dev/null -w '%{http_code}' "$base_url" || true)"
printf '  service: %s, GET / -> %s\n' "$state" "${code:-no response}"

if [ "$state" != active ]; then
  echo 'warning: the service is down before the deploy, log above shows why'
  journal_report '9site was already down before the deploy' 60
fi
