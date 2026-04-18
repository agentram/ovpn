# Development Guide

Contributor reference for `ovpn`.

## Architecture summary

`ovpn` is a local Go CLI control plane for Xray (`VLESS + REALITY`) servers.

Core model:

- local desired state: SQLite in `~/.ovpn/ovpn.db`
- remote runtime: Docker Compose in `/opt/ovpn`
- control channel: SSH/SCP only
- runtime sidecar: `ovpn-agent` for health, stats, quota, and runtime operations
- optional monitoring stack: Prometheus, Alertmanager, Grafana, node_exporter, cAdvisor, `ovpn-telegram-bot`

## Responsibility split

- `ansible/`: host bootstrap and security baseline
- `cmd/ovpn` + `internal/cli`: runtime lifecycle and operator workflows
- `cmd/ovpn-agent`: runtime API and metrics collector
- `cmd/ovpn-telegram-bot`: monitoring and operator audit interface

## Repository map

```text
cmd/
  ovpn/
  ovpn-agent/
  ovpn-telegram-bot/
internal/
  backup/
  cli/
  deploy/
  doctor/
  model/
  ssh/
  stats/
  store/local/
  store/remote/
  util/
  xrayapi/
  xraycfg/
ansible/
  inventories/
  playbooks/
  roles/
docs/
  ci.md
  monitoring.md
  public-release.md
  security.md
  testing.md
  upgrades.md
```

Runtime image defaults are centralized in `internal/defaults/images.go`.
Example env files are checked against that source in tests.

## Runtime shape

Base runtime:

- `xray` (public `443/tcp`)
- `ovpn-agent` (loopback host bind only)

Monitoring runtime is separate (`docker-compose.monitoring.yml`) and optional.

Security-sensitive defaults:

- Xray API is internal-only.
- `ovpn-agent` host endpoint is loopback-bound.
- `ovpn-telegram-bot` defaults to owner-only access for sensitive actions.
- structured logging redacts secret-like key/value payloads and `vless://` links.
- backup retention is automatic.

## High-impact areas

- SSH command construction and quoting
- runtime API operations vs full deploy behavior
- quota synchronization and blocked-user state transitions
- Telegram callback flow and owner-only controls
- deploy sequencing and preflight safety checks
- cleanup sequencing and confirmation guards
- avoiding secret leakage in logs and command traces

## Invariants

- SSH execution path and host-key behavior
- runtime/config consistency between DB, render, and deploy
- rolling 30d quota behavior and unblock flows
- `ovpn-agent` HTTP API compatibility
- cleanup boundary: runtime cleanup must not remove host-level policy or packages

## Validation commands

```bash
./scripts/repo_hygiene_check.sh
./scripts/validate_github_templates.sh
go test ./...

cd ansible
ANSIBLE_CONFIG=ansible.cfg ansible-playbook -i inventories/example/hosts.yml playbooks/bootstrap.yml --syntax-check
ANSIBLE_CONFIG=ansible.cfg ansible-playbook -i inventories/example/hosts.yml playbooks/security.yml --syntax-check
```

## Documentation map

- operator entrypoint: `README.md`
- host bootstrap: `README.ansible.md`
- security model: `docs/security.md`
- monitoring: `docs/monitoring.md`
- public CI/release automation: `docs/ci.md`
- public release process: `docs/public-release.md`
- testing strategy: `docs/testing.md`
- upgrades and cleanup: `docs/upgrades.md`
