# Contributing

## Before you open a PR

- use Discussions for questions and design exploration
- use Issues for confirmed bugs and concrete feature requests
- report vulnerabilities through private vulnerability reporting, not public issues

## Local setup

```bash
go build -o ovpn ./cmd/ovpn
go test ./...
golangci-lint run --timeout=5m
```

For Ansible checks, use the example inventory shipped in the repository:

```bash
ANSIBLE_CONFIG=ansible/ansible.cfg ansible-playbook -i ansible/inventories/example/hosts.yml ansible/playbooks/bootstrap.yml --syntax-check
ANSIBLE_CONFIG=ansible/ansible.cfg ansible-playbook -i ansible/inventories/example/hosts.yml ansible/playbooks/deploy.yml --syntax-check
ANSIBLE_CONFIG=ansible/ansible.cfg ansible-playbook -i ansible/inventories/example/hosts.yml ansible/playbooks/security.yml --syntax-check
```

## Private inventory and generated artifacts

Do not commit:

- real production inventory
- `.env`
- Telegram tokens, SSH keys, vault passwords, or private keys
- generated binaries, PDFs, SARIF reports, coverage files, or local databases
- real customer or user data

Keep real Ansible inventory under `ansible/inventories/production` locally. That path is intentionally ignored by git.
Start from `ansible/inventories/example` and copy it privately.

Generate the client guide PDF locally only when you need it for Telegram `/guide` deployment:

```bash
make docs-pdf
```

## Pull request expectations

- keep changes scoped and explain operator impact
- add or update tests when behavior changes
- update docs for public interfaces, workflows, or operational procedures
- preserve backward compatibility unless a breaking change is intentional and documented
