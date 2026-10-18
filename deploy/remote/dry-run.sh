#!/usr/bin/env bash
# Variables come from parse_args in lib.sh.
# shellcheck disable=SC2154
parse_args "$@"

cd "$deploy_path" || exit 1
envs="$(systemctl --user show 9site -p Environment --value | tr ' ' '\n' | grep '=' | tr '\n' ' ')"
# The unit value list is meant to be split into arguments.
# shellcheck disable=SC2086
env $envs ./site-server check
