#!/usr/bin/env bash
set -euo pipefail

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$repo_root"

src="docs/clients.md"
out="${1:-docs/clients.pdf}"
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

mkdir -p "$(dirname "$out")"

if [ -n "$chrome" ]; then
  npx -y md-to-pdf "$src" --dest "$out" --launch-options "{\"executablePath\":\"$chrome\"}"
else
  npx -y md-to-pdf "$src" --dest "$out"
fi

echo "built $out"
