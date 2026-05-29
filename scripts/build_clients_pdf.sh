#!/usr/bin/env bash
set -euo pipefail

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$repo_root"

chrome="${CHROME_BIN:-}"

if [ -z "$chrome" ]; then
  for candidate in \
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
    google-chrome \
    chromium \
    chromium-browser; do
    if command -v "$candidate" >/dev/null 2>&1; then
      chrome="$candidate"
      break
    fi
  done
fi

build_pdf() {
  local src="$1"
  local out="$2"

  mkdir -p "$(dirname "$out")"
  local generated="${src%.md}.pdf"
  rm -f "$generated"

  if [ -n "$chrome" ]; then
    npx -y md-to-pdf "$src" --launch-options "{\"executablePath\":\"$chrome\"}"
  else
    npx -y md-to-pdf "$src"
  fi

  if [ "$generated" != "$out" ]; then
    mv "$generated" "$out"
  fi

  echo "built $out"
}

if [ "$#" -gt 0 ]; then
  build_pdf "${OVPN_CLIENTS_MD:-docs/clients.md}" "$1"
else
  build_pdf docs/clients.md docs/clients.pdf
  build_pdf docs/clients-ru.md docs/clients-ru.pdf
fi
