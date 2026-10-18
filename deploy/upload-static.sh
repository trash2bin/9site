#!/usr/bin/env bash
# Static files live in $DEPLOY_PATH/public, the binary in $DEPLOY_PATH: the
# service reads them from STATIC_ROOT in the unit.
#
# The root pages and styles travel by pattern, not by a list of names: a
# new page lands on the server by itself. A list of names here is a site
# that lags the repository, and the first start of the new binary then
# fails on its startup check for a file nobody added.

set -euo pipefail

$SSH_OPTS "$TARGET" "mkdir -p '$DEPLOY_PATH/public' && chmod 755 '$DEPLOY_PATH/public'"

rsync -avz -e "$SSH_OPTS" \
  public/*.html public/*.css \
  "$TARGET:$DEPLOY_PATH/public/"

# The style layers travel separately with --delete-after: it removes files
# left on the server by an older layout, so public/css stops collecting
# junk that is served to the outside anyway.
rsync -avz --delete-after -e "$SSH_OPTS" \
  public/css/ "$TARGET:$DEPLOY_PATH/public/css/"
rsync -avz --delete-after -e "$SSH_OPTS" \
  public/assets/ "$TARGET:$DEPLOY_PATH/public/assets/"

# The script built from src/*.ts travels the same way: old modules must
# not stay on the server.
rsync -avz --delete-after -e "$SSH_OPTS" \
  public/js/ "$TARGET:$DEPLOY_PATH/public/js/"
