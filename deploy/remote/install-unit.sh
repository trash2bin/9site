#!/usr/bin/env bash
# Variables come from parse_args in lib.sh.
# shellcheck disable=SC2154
# The unit and its environment travel with the deploy: a server on an old
# unit starts a binary that looks for the database in the wrong place, and
# the scripts read the same environment file the unit reads.
#
# They arrive in $deploy_path/.incoming, not in /tmp: the environment file
# holds the SMTP password once it is filled in, and /tmp is readable by every
# user on the box.

# On the server only the checks below decide, and a command that breaks must
# not be skipped over: a flip that never happened used to look like a deploy.
set -euo pipefail

parse_args "$@"

incoming="$deploy_path/.incoming"
for file in 9site.service deploy.env; do
  if [ ! -f "$incoming/$file" ]; then
    echo "no $incoming/$file: the pipeline ships it before this step" >&2
    exit 1
  fi
done

install -d -m 755 "$deploy_path"
install -d -m 755 "$HOME/.config/systemd/user"
install -m 0644 "$incoming/9site.service" "$HOME/.config/systemd/user/9site.service"

# The environment file belongs to the server: it carries the SMTP password and
# whatever was set by hand, and a deploy that overwrote it would take both
# away. The shipped file only seeds a server that has none; a difference is
# printed, so a new key here is not silently missing there.
if [ -f "$deploy_path/deploy.env" ]; then
  if ! diff -q "$incoming/deploy.env" "$deploy_path/deploy.env" >/dev/null; then
    echo "  $deploy_path/deploy.env differs from the repository, the server keeps its own:"
    diff "$incoming/deploy.env" "$deploy_path/deploy.env" | sed 's/^/    /' || true
  fi
else
  install -m 0600 "$incoming/deploy.env" "$deploy_path/deploy.env"
  echo "  $deploy_path/deploy.env seeded from the repository, mode 0600"
fi
rm -rf "$incoming"

systemctl --user daemon-reload

load_env "$deploy_path" || exit 1

printf '  unit installed, DATA_DIR=%s\n' "${data_dir:-unset}"

guard_db_location
