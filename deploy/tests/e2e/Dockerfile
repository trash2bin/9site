# Sandbox for the e2e rehearsal: systemd as PID 1, so `systemctl --user`,
# linger and ssh behave exactly as they do on the deploy machine. The
# rehearsal itself installs the rest of the tools.
FROM debian:12-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        systemd systemd-sysv dbus iproute2 procps ca-certificates \
    && rm -rf /var/lib/apt/lists/*

STOPSIGNAL SIGRTMIN+3
