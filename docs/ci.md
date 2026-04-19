# CI/CD workflows

This document describes the **public** repository automation and its safety boundaries.

## Principles

- validation on pull requests and `main`
- immutable-pinned third-party GitHub Actions
- least-privilege workflow permissions
- no public repository deploy, backup, or restore workflows
- automatic public release creation only from `VERSION` + `CHANGELOG.md`

## Workflow map

- `ci.yml`: repository hygiene, template validation, actionlint, Go checks, Compose validation, and Ansible syntax/lint
- `security.yml`: gitleaks working-tree scan plus Trivy filesystem scanning and optional SARIF upload
- `release.yml`: validate `VERSION`/`CHANGELOG.md`, build artifacts, and publish a new release when both files move together on `main`

Operational infrastructure workflows are intentionally kept out of the public repository.
Use local CLI automation or a separate private ops repository for deployment, backup, and restore.

## Release rule

Repository releases use plain semantic versions without a `v` prefix.

Required inputs:

- `VERSION`
- top `CHANGELOG.md` heading

They must match exactly, for example `1.1.0`.

Automatic release flow:

1. update code for the release
2. bump `VERSION`
3. prepend the matching changelog entry
4. merge to `main`
5. `release.yml` validates both files
6. if both files changed together and the tag does not already exist, the workflow builds artifacts and creates the GitHub release

Manual rebuild flow:

- use `workflow_dispatch` on `release.yml`
- the requested version must match `VERSION`
- the target release must already exist
- assets are rebuilt and re-uploaded with `--clobber`

## Required secrets and repository settings

Public workflows do not require infrastructure secrets.

Recommended repository settings:

- enable Dependabot alerts and security updates
- enable private vulnerability reporting
- enable code scanning only if you want SARIF uploads from `security.yml`
- protect `main` with required pull requests and required checks:
  - `Go Quality`
  - `Ansible Quality`
  - `Gitleaks Tree Scan`
  - `Trivy FS Scan`
- keep default GitHub Actions permissions read-only
- protect semver release tags (`*.*.*`) from update and deletion

## Local parity checks

Run before opening a PR:

```bash
./scripts/repo_hygiene_check.sh
./scripts/validate_github_templates.sh
go test ./...
go test -covermode=atomic -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
go vet ./...
gofmt -l $(git ls-files '*.go')
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 -color
```

Ansible checks:

```bash
ANSIBLE_CONFIG=ansible/ansible.cfg ansible-lint -c .ansible-lint ansible
ANSIBLE_CONFIG=ansible/ansible.cfg ansible-playbook -i ansible/inventories/example/hosts.yml ansible/playbooks/bootstrap.yml --syntax-check
ANSIBLE_CONFIG=ansible/ansible.cfg ansible-playbook -i ansible/inventories/example/hosts.yml ansible/playbooks/security.yml --syntax-check
```

Security checks:

```bash
gosec -severity high -confidence high ./...
gitleaks git --source . --redact --no-banner
trivy fs --scanners vuln,misconfig,secret --severity HIGH,CRITICAL --ignore-unfixed .
```
