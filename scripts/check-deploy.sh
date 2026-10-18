#!/usr/bin/env bash
# Deploy checks that must be seconds, not minutes: static linters for the
# scripts and the workflows, plus the sudo contract and the unit behaviour
# with stubbed server commands. The end-to-end run with real systemd lives
# in the e2e job, this one is what every branch pays for.

set -uo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root" || exit 1

fail=0
skipped=""
note() { printf '  %s\n' "$1"; }
bad() { printf 'FAIL: %s\n' "$1" >&2; fail=1; }

# --- shellcheck on the scripts the deploy runs ---------------------------
if command -v shellcheck >/dev/null; then
  # The remote scripts are the concatenation of lib.sh and the file itself,
  # so shellcheck reads them as separate files and the library values come
  # from a directive in each file.
  if shellcheck -S warning deploy/lib.sh deploy/upload-static.sh \
    deploy/remote/*.sh deploy/tests/fakessh scripts/check-size.sh; then
    note 'shellcheck: clean'
  else
    bad 'shellcheck reported something'
  fi
else
  skipped+='shellcheck '
fi

# --- actionlint, which lints the workflows and their inline bash ---------
if command -v actionlint >/dev/null; then
  if actionlint .github/workflows/ci.yml .github/workflows/rollback.yml; then
    note 'actionlint: clean'
  else
    bad 'actionlint reported something'
  fi
else
  skipped+='actionlint '
fi

# --- the deploy scripts, with stubbed server commands --------------------
if command -v bats >/dev/null; then
  if bats deploy/tests/lib.bats deploy/tests/remote.bats; then
    note 'bats: clean'
  else
    bad 'bats reported something'
  fi
else
  skipped+='bats '
fi

if [ -n "$skipped" ]; then
  printf '  partial run, tools missing (install to check locally): %s\n' "$skipped" >&2
fi

if [ "$fail" != 0 ]; then
  echo 'deploy checks: FAILED'
  exit 1
fi
echo 'deploy checks: OK'
