#!/usr/bin/env bash
# Variables come from parse_args in lib.sh.
# shellcheck disable=SC2154
parse_args "$@"

systemctl --user reset-failed 9site || true
# From the repository variable, splitting is intended.
# shellcheck disable=SC2086
$restart_cmd
