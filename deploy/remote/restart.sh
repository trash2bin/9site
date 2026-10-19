#!/usr/bin/env bash
# Variables come from parse_args in lib.sh.
# shellcheck disable=SC2154
parse_args "$@"

# A unit that was never loaded has no failed state to clear.
systemctl --user reset-failed "$UNIT" 2>/dev/null || true
# From the repository variable, splitting is intended.
# shellcheck disable=SC2086
$restart_cmd
