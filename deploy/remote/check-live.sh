#!/usr/bin/env bash
# Variables come from parse_args in lib.sh.
# shellcheck disable=SC2154
parse_args "$@"
load_env "$deploy_path" || exit 1

if ! wait_active; then
  echo 'service is not active after the restart, status and log:'
  journal_report '9site is not active after the restart' 160
  exit 1
fi
echo 'service is active'

smoke
check_counter
