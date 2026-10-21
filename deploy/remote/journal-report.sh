#!/usr/bin/env bash
# Variables come from parse_args in lib.sh.
# shellcheck disable=SC2154
# On the server only the checks below decide, and a command that breaks must
# not be skipped over: a flip that never happened used to look like a deploy.
set -euo pipefail

parse_args "$@"

journal_report 'deploy failed' 200
