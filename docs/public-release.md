# Public Release Checklist

This repository is prepared for a public source release, but **do not** make the current private git history public as-is.
The old private history contained real inventory data and must stay private unless you intentionally rewrite it.

## 1. Publish from a clean public history

Recommended options:

- create a fresh public repository from the sanitized current tree
- or rewrite the existing private repository history to one clean initial commit, then force-push it to a new public remote

If you choose the rewrite route, do it only after verifying the current tree is clean and after backing up the private repository first.

## 2. Keep private data out of the tree

- Keep real Ansible inventory private under `ansible/inventories/production` only on your local machine.
- The repository intentionally tracks only `ansible/inventories/example`.
- Do not commit `.env`, `monitoring/secrets/*`, backup archives, local databases, private keys, generated binaries, generated PDFs, or local docs build output.
- Run `./scripts/repo_hygiene_check.sh` before publishing and in CI.
- Run a history-capable secret scanner such as `gitleaks git <repo>` against the final public repository before pushing it.

## 3. GitHub repository settings

Recommended settings before publishing:

- enable `Issues`
- enable `Discussions`
- enable `Private vulnerability reporting`
- enable `Dependabot alerts`
- enable `Dependabot security updates`
- enable `Secret scanning` if your GitHub plan includes it
- enable `Code scanning` if you want SARIF uploads from `security.yml`
- protect `main` with required pull requests and required status checks
- restrict who can push directly to `main`
- keep GitHub Actions default permissions read-only

## 4. GitHub Discussions categories

The discussion forms in `.github/DISCUSSION_TEMPLATE` expect these category slugs:

- `announcements`
- `ideas`
- `q-a`

Create matching categories in repository Discussions settings.

## 5. Sponsorship best practices

This repository does not currently ship a sponsor button.

If you add sponsorship later:

- prefer GitHub Sponsors or OpenCollective first
- link to a public donation page instead of embedding raw crypto wallet addresses in the repository
- keep donations separate from commercial support or licensing
- explain what sponsorship funds: maintenance, bug fixes, docs, hosting, or support time

## 6. Public workflow boundary

The public repository intentionally includes only:

- CI
- security scanning
- release automation

Operational workflows such as deploy, backup, and restore should stay in a private ops repository or remain local-only.
