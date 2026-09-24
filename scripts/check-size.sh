#!/usr/bin/env bash
# Проверяет бюджет HTML+CSS. Порог задаётся через SIZE_LIMIT (в байтах).
set -euo pipefail

LIMIT="${SIZE_LIMIT:-61440}"

total=0
for f in public/index.html public/style.css; do
  if [ ! -f "$f" ]; then
    echo "нет файла: $f" >&2
    exit 1
  fi
  bytes=$(wc -c <"$f" | tr -d ' ')
  printf '  %-12s %7d B\n' "$f" "$bytes"
  total=$((total + bytes))
done

printf '  %-12s %7d B  (лимит %d B)\n' "ИТОГО" "$total" "$LIMIT"

if [ "$total" -gt "$LIMIT" ]; then
  echo "ПРЕВЫШЕНИЕ: HTML+CSS больше лимита на $((total - LIMIT)) B" >&2
  exit 1
fi

echo "OK: запас $((LIMIT - total)) B"
