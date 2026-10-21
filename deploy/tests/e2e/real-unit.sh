#!/usr/bin/env bash
# The real binary under the real user unit, the way the deploy leaves it: one
# release behind the current symlink, EnvironmentFile, Type=notify. The e2e
# rehearses the same path with a stub; this runs the Go binary itself, because
# only it answers whether readiness is really reported and /healthz names this
# build. Run it after a change to main.go, the unit or deploy.env.
#
#   GOOS=linux go build -trimpath -ldflags="-X main.version=$sha" \
#     -o /tmp/site-server-linux ./server
#   bash deploy/tests/e2e/real-unit.sh
#
# The build has to carry the sha below: the deploy reads the version of the
# running binary from /healthz, and a check that only asks whether the field
# exists would pass on a build nobody stamped.
#
# Needs docker with a systemd container; the container is removed afterwards.
set -uo pipefail

repo="$(cd "$(dirname "$0")/../../.." && pwd)"
name=9site-real-unit
sha=abcdefabcdefabcdefabcdefabcdefabcdefabcd
binary=/tmp/site-server-linux

if [ ! -f "$binary" ]; then
  echo "no $binary: GOOS=linux go build -o $binary ./server" >&2
  exit 1
fi

docker rm -f "$name" >/dev/null 2>&1 || true
trap 'docker rm -f "$name" >/dev/null 2>&1 || true' EXIT

docker build -q -t 9site-e2e - <"$repo/deploy/tests/e2e/Dockerfile" >/dev/null
docker run -d --name "$name" --privileged --cgroupns=host \
  -v /sys/fs/cgroup:/sys/fs/cgroup:rw --tmpfs /run --tmpfs /run/lock \
  -v "$repo:/repo:ro" 9site-e2e /sbin/init >/dev/null
# Not /tmp: the container's systemd mounts a tmpfs there, which hides a file
# copied into the layer underneath.
docker cp "$binary" "$name":/opt/site-server-linux

for _ in $(seq 60); do
  state="$(docker exec "$name" systemctl is-system-running 2>/dev/null || true)"
  case "$state" in running|degraded) break ;; esac
  sleep 1
done

docker exec "$name" bash -euc "
  set -euo pipefail
  sha=$sha
  apt-get update -qq
  apt-get install -y -qq curl python3 libpam-systemd >/dev/null
  id deploy >/dev/null 2>&1 || useradd --system --create-home --shell /bin/bash deploy
  usermod -aG systemd-journal deploy 2>/dev/null || true
  install -d -o deploy -g deploy -m 755 /var/www/9site /var/www/9site/var
  cp -r /repo/public/. /var/www/9site/public/
  chown -R deploy:deploy /var/www/9site/public
  install -d -o deploy -g deploy -m 755 /home/deploy/.config/systemd/user
  install -m 0644 /repo/deploy/deploy.env /var/www/9site/deploy.env
  install -m 0644 /repo/deploy/9site.service /home/deploy/.config/systemd/user/9site.service
  install -d -o deploy -g deploy -m 755 \"/var/www/9site/releases/\$sha\"
  install -o deploy -g deploy -m 755 /opt/site-server-linux \"/var/www/9site/releases/\$sha/site-server\"
  ln -sfn \"/var/www/9site/releases/\$sha\" /var/www/9site/current
  loginctl enable-linger deploy
  systemctl start \"user@\$(id -u deploy).service\"
"

uid="$(docker exec "$name" id -u deploy)"
as_deploy() { docker exec "$name" runuser -u deploy -- env XDG_RUNTIME_DIR=/run/user/"$uid" bash -c "$1"; }

fail=0
say() { printf '\n== %s\n' "$1"; }
expect() { if [ "$1" = 0 ]; then echo "  ok   $2"; else echo "  FAIL $2"; fail=1; fi; }

say 'start the release through the unit, Type=notify'
rc=0
as_deploy 'systemctl --user start 9site' >/dev/null 2>&1 || rc=$?
expect "$rc" 'the start succeeds'
state="$(as_deploy 'systemctl --user is-active 9site')"
rc=0
[ "$state" = active ] || rc=1
expect "$rc" 'the service is active, which for notify means it is listening'

say 'healthz answers with the version of this build'
hz="$(as_deploy 'for i in $(seq 20); do curl -sf http://127.0.0.1:8080/healthz && break; sleep 0.3; done')"
printf '%s\n' "$hz" | grep -q "\"version\":\"$sha\""
expect "$?" "healthz reports the build the deploy shipped: $hz"

say 'restart the same release'
rc=0
as_deploy 'systemctl --user restart 9site' >/dev/null 2>&1 || rc=$?
expect "$rc" 'the restart returns zero'

say 'a release that never reports readiness fails the restart'
docker exec "$name" bash -c '
  install -d -o deploy -g deploy -m 755 /var/www/9site/releases/dead
  printf "#!/usr/bin/env bash\nsleep 600\n" >/var/www/9site/releases/dead/site-server
  chmod 755 /var/www/9site/releases/dead/site-server
  chown deploy:deploy /var/www/9site/releases/dead/site-server
  ln -sfn /var/www/9site/releases/dead /var/www/9site/current'
start=$SECONDS
rc=0
as_deploy 'systemctl --user restart 9site' >/dev/null 2>&1 || rc=$?
status=0
[ "$rc" != 0 ] || status=1
expect "$status" "the restart failed after $((SECONDS - start))s, not forever"

say 'back to the good release'
docker exec "$name" ln -sfn "/var/www/9site/releases/$sha" /var/www/9site/current
as_deploy 'systemctl --user reset-failed 9site 2>/dev/null || true; systemctl --user restart 9site' >/dev/null 2>&1
as_deploy 'for i in $(seq 20); do curl -sf http://127.0.0.1:8080/healthz && break; sleep 0.3; done' | grep -q '"version":"'
expect "$?" 'the good release serves healthz again'

printf '\n== result: %s\n' "$([ "$fail" = 0 ] && echo 'all ok' || echo 'FAILED')"
exit "$fail"
