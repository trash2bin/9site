#!/usr/bin/env bash
# Variables come from parse_args in lib.sh.
# shellcheck disable=SC2154
parse_args "$@"

journal_report 'deploy failed' 200
