#!/usr/bin/env bash
set -euo pipefail

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$repo_root"

fail=0

report_blocker() {
  printf 'BLOCKER: %s\n' "$1" >&2
  fail=1
}

tracked_forbidden="$({
  git ls-files -- \
    .env .envrc coverage.out test-report.jsonl trivy-results.sarif 'docs/*.pdf' '*.pem' '*.key' '*.crt' '*.db' '*.sqlite' '*.tgz' 'monitoring/secrets/*' 'ansible/inventories/production/*' \
    ':!monitoring/secrets/.gitkeep' ':!monitoring/secrets/README.md' ':!ansible/inventories/production/.gitkeep' ':!ansible/inventories/production/README.md' \
    | while IFS= read -r path; do
        [ -e "$path" ] && printf '%s\n' "$path"
      done
  true
} | sed '/^$/d' | sort -u)"
if [ -n "$tracked_forbidden" ]; then
  report_blocker "tracked sensitive, generated, or private-only files detected"
  printf '%s\n' "$tracked_forbidden" >&2
fi

if git grep -n -I -E '-----BEGIN ([A-Z ]+ )?PRIVATE KEY-----' -- . ':!scripts/repo_hygiene_check.sh' >/tmp/ovpn-hygiene-private-keys.txt 2>/dev/null; then
  report_blocker "private key material detected in tracked text files"
  cat /tmp/ovpn-hygiene-private-keys.txt >&2
fi

if git grep -n -I -E 'AKIA[0-9A-Z]{16}|ghp_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{20,}|xox[baprs]-[A-Za-z0-9-]{10,}' -- . ':!scripts/repo_hygiene_check.sh' >/tmp/ovpn-hygiene-token-patterns.txt 2>/dev/null; then
  report_blocker "token-like patterns detected in tracked text files"
  cat /tmp/ovpn-hygiene-token-patterns.txt >&2
fi

if git grep -n -I -E '/Users/|C:\\\\Users\\\\' -- . ':!scripts/repo_hygiene_check.sh' >/tmp/ovpn-hygiene-local-paths.txt 2>/dev/null; then
  report_blocker "local workstation paths detected in tracked text files"
  cat /tmp/ovpn-hygiene-local-paths.txt >&2
fi

rm -f /tmp/ovpn-hygiene-private-keys.txt /tmp/ovpn-hygiene-token-patterns.txt /tmp/ovpn-hygiene-local-paths.txt

if [ "$fail" -ne 0 ]; then
  exit 1
fi

echo "repo hygiene check passed"
