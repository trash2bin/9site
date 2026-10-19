#!/usr/bin/env bash
# Variables come from parse_args in lib.sh.
# shellcheck disable=SC2154
# The unit and its environment travel with the deploy: a server on an old
# unit starts a binary that looks for the database in the wrong place, and
# the scripts read the same environment file the unit reads.

parse_args "$@"

install -d -m 755 "$deploy_path"
install -m 0644 /tmp/deploy.env "$deploy_path/deploy.env"

load_env "$deploy_path" || exit 1

install -d -m 755 "$HOME/.config/systemd/user"
install -m 0644 /tmp/9site.service "$HOME/.config/systemd/user/9site.service"
systemctl --user daemon-reload

printf '  unit installed, DATA_DIR=%s\n' "${data_dir:-unset}"

guard_db_location
