#!/usr/bin/env bash
# Variables come from parse_args in lib.sh.
# shellcheck disable=SC2154
parse_args "$@"

install -m 755 /tmp/site-server.new "$deploy_path/site-server.new"
if [ -f "$deploy_path/site-server" ]; then
  mv -f "$deploy_path/site-server" "$deploy_path/site-server.prev"
fi
mv -f "$deploy_path/site-server.new" "$deploy_path/site-server"
rm -f /tmp/site-server.new

printf '  binary swapped, previous kept as %s/site-server.prev\n' "$deploy_path"
