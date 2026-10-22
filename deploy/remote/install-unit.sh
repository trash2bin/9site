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

# The unit spells the paths out, so a DEPLOY_PATH that differs from it
# installs a unit that starts a binary which is not there.
for line in "ExecStart=$deploy_path/current/site-server" \
  "EnvironmentFile=$deploy_path/deploy.env"; do
  if ! grep -qxF "$line" "$incoming/9site.service"; then
    echo "the unit in the repository does not say $line" >&2
    echo 'fix deploy/9site.service or the DEPLOY_PATH of the pipeline' >&2
    exit 1
  fi
done

install -d -m 755 "$deploy_path"
install -d -m 755 "$HOME/.config/systemd/user"
install -m 0644 "$incoming/9site.service" "$HOME/.config/systemd/user/9site.service"

# The environment file belongs to the server: it carries the SMTP password and
# whatever was set by hand, and a deploy that overwrote it would take both
# away. The shipped file only seeds a server that has none; a difference is
# reported by key name, so a new key here is not silently missing there while
# ADMIN_PASSWORD or SMTP_PASS stays out of the deploy log.
env_keys() {
  sed -n 's/^\([A-Za-z_][A-Za-z0-9_]*\)=.*/\1/p' "$1" | sort
}

if [ -f "$deploy_path/deploy.env" ]; then
  if ! diff -q "$incoming/deploy.env" "$deploy_path/deploy.env" >/dev/null; then
    echo "  $deploy_path/deploy.env differs from the repository, the server keeps its own:"
    repo_keys="$(env_keys "$incoming/deploy.env")"
    server_keys="$(env_keys "$deploy_path/deploy.env")"
    repo_only="$(comm -23 <(printf '%s\n' "$repo_keys") <(printf '%s\n' "$server_keys") | tr '\n' ' ')"
    server_only="$(comm -13 <(printf '%s\n' "$repo_keys") <(printf '%s\n' "$server_keys") | tr '\n' ' ')"
    if [ -n "${repo_only// /}" ]; then
      echo "    only in the repository: ${repo_only% }"
    fi
    if [ -n "${server_only// /}" ]; then
      echo "    only on the server: ${server_only% }"
    fi
    if [ -z "${repo_only// /}" ] && [ -z "${server_only// /}" ]; then
      echo "    same keys, different values: the values are not printed"
    fi
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
