#!/usr/bin/env bash
# Prepares the deploy user's own runtime for the unit: the unit directory and
# linger for the manager. No root: a user sets up its own user manager, so the
# deploy self-prepares a fresh box without a human touch.

parse_args "$@"

install -d -m 755 "$HOME/.config/systemd/user"
loginctl enable-linger "$(id -un)" 2>/dev/null || true

# enable-linger starts the manager, but not always within the same second, so
# wait for the user bus before the next step reloads the unit into it.
for _ in 1 2 3 4 5 6 7 8 9 10; do
  systemctl --user show-environment >/dev/null 2>&1 && exit 0
  sleep 1
done
echo 'the user manager did not come up, a reboot finishes enable-linger' >&2
exit 1
