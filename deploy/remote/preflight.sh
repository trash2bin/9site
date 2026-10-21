#!/usr/bin/env bash
# Variables come from parse_args in lib.sh.
# shellcheck disable=SC2154
# On the server only the checks below decide, and a command that breaks must
# not be skipped over: a flip that never happened used to look like a deploy.
set -euo pipefail

parse_args "$@"
# The environment file arrives with the unit, so on the first deploy there is
# none yet and the counter is simply not recorded.
if [ -f "$deploy_path/deploy.env" ]; then
  load_env "$deploy_path" || exit 1
fi

state="$(systemctl --user is-active "$UNIT" || true)"
code="$(curl -s -o /dev/null -w '%{http_code}' "$probe_url" || true)"
printf '  service: %s, GET / -> %s\n' "$state" "${code:-no response}"

# The number the site serves right now, for check-live to measure the site
# after the restart against it.
if [ "$state" = active ] && [ -n "$data_dir" ]; then
  n="$(counter_value || true)"
  if [ -n "$n" ]; then
    printf '%s\n' "$n" >"$data_dir/.counter-before"
    printf '  counter before the deploy: %s\n' "$n"
  fi
fi

if [ "$state" != active ]; then
  echo 'warning: the service is down before the deploy, log above shows why'
  journal_report '9site was already down before the deploy' 60
fi
