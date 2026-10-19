#!/usr/bin/env bash
# The startup checks of the new binary, in the environment of the unit: a
# binary that cannot run never reaches the restart.

# shellcheck disable=SC2154
parse_args "$@"
load_env "$deploy_path" || exit 1

cd "$deploy_path" || exit 1
./site-server check
