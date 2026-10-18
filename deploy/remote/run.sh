#!/usr/bin/env bash
# Entry point for the scripts that run on the server: lib.sh and the script
# are concatenated and piped into bash -s, so the static checker reads the
# script as it is. Values are quoted with printf %q, which keeps spaces and
# newlines intact across ssh.

set -euo pipefail

name="$1"
shift

script="deploy/remote/$name.sh"
if [ ! -f "$script" ]; then
  echo "no such remote script: $name" >&2
  exit 1
fi

args="bash -s --"
for arg in "$@"; do
  args+=" $(printf '%q' "$arg")"
done

cat deploy/lib.sh "$script" | ${SSH_OPTS} "$TARGET" "$args"
