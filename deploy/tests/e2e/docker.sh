#!/usr/bin/env bash
# Rehearses the deploy inside a systemd container: the e2e needs root, the
# user manager and ssh, which the laptop has no other way to give it.
#
#   bash deploy/tests/e2e/docker.sh
#
# The repository is mounted read-only, the e2e only writes inside the
# container, so the run leaves the checkout untouched.
set -euo pipefail

repo="$(cd "$(dirname "$0")/../../.." && pwd)"
image=9site-e2e
name=9site-e2e-run

docker rm -f "$name" >/dev/null 2>&1 || true

ctx="$(mktemp -d)"
trap 'rm -rf "$ctx"' EXIT
cp "$repo/deploy/tests/e2e/Dockerfile" "$ctx/"
docker build -q -t "$image" "$ctx"

docker run -d --name "$name" \
    --privileged --cgroupns=host \
    -v /sys/fs/cgroup:/sys/fs/cgroup:rw \
    --tmpfs /run --tmpfs /run/lock \
    -v "$repo:/repo:ro" \
    "$image" /sbin/init >/dev/null

trap 'docker rm -f "$name" >/dev/null 2>&1 || true; rm -rf "$ctx"' EXIT

for _ in $(seq 60); do
    state="$(docker exec "$name" systemctl is-system-running 2>/dev/null || true)"
    case "$state" in running|degraded) break ;; esac
    sleep 1
done
echo "container: $(docker exec "$name" systemctl is-system-running 2>&1)"

set +e
docker exec "$name" bash /repo/deploy/tests/e2e/run.sh
code=$?
set -e

exit "$code"
