#!/usr/bin/env bash
# Variables come from parse_args in lib.sh.
# shellcheck disable=SC2154
# The unit travels with the deploy: a server on an old unit file starts a
# binary that looks for the database in the wrong place.

parse_args "$@"

install -d -m 755 "$HOME/.config/systemd/user"
install -m 0644 /tmp/9site.service "$HOME/.config/systemd/user/9site.service"
systemctl --user daemon-reload

data_dir="$(systemctl --user show 9site -p Environment --value \
  | tr ' ' '\n' | sed -n 's/^DATA_DIR=//p')"
printf '  unit installed, DATA_DIR=%s\n' "${data_dir:-unset}"

guard_db_location
