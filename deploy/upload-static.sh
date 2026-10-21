#!/usr/bin/env bash
# Static files land next to the live set, in $DEPLOY_PATH/public.next, and the
# deploy swaps them in only after the new binary passed its checks: a release
# that never comes up must not leave its pages on a site that still runs the
# previous binary. The service reads STATIC_ROOT from the unit, which stays
# $DEPLOY_PATH/public across a swap.
#
# The root pages and styles travel by pattern, not by a list of names: a
# new page lands on the server by itself. A list of names here is a site
# that lags the repository, and the first start of the new binary then
# fails on its startup check for a file nobody added.

set -euo pipefail

next="$DEPLOY_PATH/public.next"
$SSH_OPTS "$TARGET" \
  "rm -rf '$next' && mkdir -p '$next' && chmod 755 '$next'"

rsync -avz -e "$SSH_OPTS" \
  public/*.html public/*.css \
  "$TARGET:$next/"

# The style layers travel separately with --delete-after: it removes files
# left on the server by an older layout, so public/css stops collecting
# junk that is served to the outside anyway.
rsync -avz --delete-after -e "$SSH_OPTS" \
  public/css/ "$TARGET:$next/css/"
rsync -avz --delete-after -e "$SSH_OPTS" \
  public/assets/ "$TARGET:$next/assets/"

# The script built from src/*.ts travels the same way: old modules must
# not stay on the server.
rsync -avz --delete-after -e "$SSH_OPTS" \
  public/js/ "$TARGET:$next/js/"
