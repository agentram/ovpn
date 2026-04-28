# Upgrades, rollback, and cleanup

This document defines low-risk runtime and host change procedures.

## Principles

- explicit version pins
- no unattended runtime upgrades
- backup before change
- validate after each change

## Ownership boundaries

- `ovpn` runtime layer: Xray pin, runtime compose, monitoring stack, backup/restore, cleanup
- Ansible host layer: Docker packages, SSH/firewall/fail2ban policy
- CI/tooling layer: Actions, linters, scanners

## Runtime upgrade runbook

```bash
./ovpn server backup <server>
./ovpn server set-xray-version <server> <version>
./ovpn config validate --server <server>
./ovpn deploy <server>
./ovpn doctor <server>
./ovpn server status <server>
```

## Monitoring image update runbook

Set image overrides and roll monitoring:

```bash
export OVPN_PROMETHEUS_IMAGE=<image:tag>
export OVPN_ALERTMANAGER_IMAGE=<image:tag>
export OVPN_GRAFANA_IMAGE=<image:tag>
export OVPN_NODE_EXPORTER_IMAGE=<image:tag>
export OVPN_CADVISOR_IMAGE=<image:tag>
export OVPN_TELEGRAM_BOT_IMAGE=<image:tag>

./ovpn deploy <server>
./ovpn server monitor down <server>
./ovpn server monitor up <server>
./ovpn server monitor status <server>
```

## Runtime security profile rollback

If deploy validation fails on geosite resources in your selected Xray image:

```bash
export OVPN_SECURITY_PROFILE=off
./ovpn deploy <server>
```

Re-enable default profile after image/config is fixed:

```bash
export OVPN_SECURITY_PROFILE=minimal
./ovpn deploy <server>
```

Ensure Telegram token secret exists before monitoring restart:

```bash
ssh <ssh-user>@<server-ip> 'test -s /opt/ovpn/monitoring/secrets/telegram_bot_token'
```

## Host update runbook (Ansible)

For already-deployed hosts, use the maintenance playbook. It applies common packages, Docker daemon defaults, SSH/firewall/fail2ban policy, optional declared cleanup, and runtime file permission hardening without re-rendering the OVPN runtime scaffold.

```bash
cd ansible
ANSIBLE_CONFIG=ansible.cfg ansible-playbook -i inventories/example/hosts.yml playbooks/host-maintenance.yml --syntax-check
ANSIBLE_CONFIG=ansible.cfg ansible-playbook -i inventories/example/hosts.yml playbooks/host-maintenance.yml --limit <host> --check --diff
ANSIBLE_CONFIG=ansible.cfg ansible-playbook -i inventories/example/hosts.yml playbooks/host-maintenance.yml --limit <host>
```

Then re-validate runtime:

```bash
./ovpn doctor <server>
./ovpn server status <server>
```

The maintenance playbook must not reboot the host. If `/var/run/reboot-required` exists, schedule a separate maintenance window.

## Rollback runbook

```bash
./ovpn deploy <server>
./ovpn doctor <server>
./ovpn server status <server>
```

If still unhealthy, restore from backup:

```bash
./ovpn server restore <server> --remote-path /opt/ovpn-backups/<archive>.tgz
./ovpn restart <server>
./ovpn doctor <server>
```

## Decommission runbook

1. Backup old server.
2. Verify replacement server is healthy.
3. Preview cleanup.
4. Execute cleanup with explicit confirmation.

```bash
./ovpn server backup <server>
./ovpn --dry-run server cleanup <server>
./ovpn server cleanup <server> --confirm CLEANUP
```

Optional destructive cleanup:

```bash
./ovpn server cleanup <server> \
  --remove-backups \
  --remove-local \
  --confirm CLEANUP
```

Safety boundary:

- `ovpn server cleanup` removes runtime artifacts only.
- Host package/policy cleanup remains Ansible-managed.

## Breaking telemetry/API rename (quota window)

Quota semantics are rolling 30d. Updated public fields/metrics:

- API:
  - `window_30d_usage_byte`
  - `window_30d_quota_byte`
  - `window_30d_start`
  - `window_30d_end`
- Metrics:
  - `ovpn_agent_user_window_30d_usage_bytes`
  - `ovpn_agent_user_window_30d_quota_bytes`
