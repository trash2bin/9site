#!/usr/bin/env bash
# Проверяет бюджет HTML+CSS. Порог задаётся через SIZE_LIMIT (в байтах).
#
# CSS считается целиком: точка входа style.css и все слои из public/css/.
# Слои макета (public/css/design/) в бюджет не входят: они грузятся только
# на /maket и в исходной версии тоже не считались.
set -euo pipefail

LIMIT="${SIZE_LIMIT:-61440}"

files=(public/index.html public/style.css)
while IFS= read -r f; do
  files+=("$f")
done < <(find public/css -maxdepth 1 -name '*.css' | sort)

total=0
for f in "${files[@]}"; do
  if [ ! -f "$f" ]; then
    echo "нет файла: $f" >&2
    exit 1
  fi
  bytes=$(wc -c <"$f" | tr -d ' ')
  printf '  %-26s %7d B\n' "$f" "$bytes"
  total=$((total + bytes))
done

printf '  %-26s %7d B  (лимит %d B)\n' "ИТОГО" "$total" "$LIMIT"

if [ "$total" -gt "$LIMIT" ]; then
  echo "ПРЕВЫШЕНИЕ: HTML+CSS больше лимита на $((total - LIMIT)) B" >&2
  exit 1
fi

echo "OK: запас $((LIMIT - total)) B"
