# Upgrades, rollback, and cleanup

This document defines low-risk runtime and host change procedures.

## Principles

- explicit version pins
- no unattended runtime upgrades
- backup before change
- validate after each change

## Xray version policy

The repository default is currently **26.7.28**. Keep Xray versions explicitly
pinned. Do not promote a prerelease merely because it has a larger version
number; test it on an unused server first and check the client versions and
transport profiles that the server must support.

The Xray image is selected per server, so an isolated upgrade does not change
the image used by other servers. The repository default remains the stable
pin until a newer upstream release is reviewed and the compatibility tests
pass.

For a REALITY upgrade, read the Xray startup log after deployment. Xray may
warn about its minimum client version, the configured REALITY target, or
client compatibility. These warnings are operational input, not proof that a
client link is invalid. Keep the previous image version available for a
rollback and do not rotate `reality_target` or `reality_server_name` in the
same change as an image upgrade. Those values are per-server and can be tested
on an unused server without changing another server's target.

## Ownership boundaries

- `ovpn` runtime layer: Xray pin, runtime compose, monitoring stack, backup/restore, cleanup
- Ansible host layer: Docker packages, SSH/firewall/fail2ban policy
- CI/tooling layer: Actions, linters, scanners

## Xray upgrade runbook

```bash
./ovpn server backup <server>
./ovpn server set-xray-version <server> 26.7.28
./ovpn config validate --server <server>
./ovpn deploy <server>
./ovpn doctor <server>
./ovpn server status <server>
```

Replace `26.7.28` with the reviewed release tag when upgrading later. The
value must be an Xray image tag without the leading `v`. `set-xray-version`
changes local desired state only; `deploy` is what updates the remote image.
The stack restarts during deployment, so expect a short interruption. Existing
links do not need to be regenerated for an image-only upgrade.

Ansible is not required for an image-only Xray upgrade. Run the Ansible
maintenance playbook separately only when host packages, firewall rules, or
other host policy also changed.

For a failed REALITY connection, collect evidence before changing links:

```bash
./ovpn server logs <server> --tail 120
./ovpn config validate --server <server>
./ovpn user diagnose --server <server> --username <user> --since 30m
```

From the server, verify that the configured target is reachable and that its
certificate/SNI are consistent. Use the Xray `tls ping` diagnostic described
in `docs/transports.md`. A successful target check does not prove that every
operator will pass the REALITY handshake: some networks drop large or
fragmented ClientHello packets or classify the transport independently.

If the upgrade is not suitable, roll back the image without changing the
server's users or transport state:

```bash
./ovpn server set-xray-version <server> <known-good-version>
./ovpn config validate --server <server>
./ovpn deploy <server>
./ovpn doctor <server>
```

Existing client links should be preserved during an image-only rollback. A
change to REALITY target, SNI, keys, or profile parameters is a separate
rollout and requires newly generated links.

For example, changing the REALITY target to `www.trip.com` is a separate
server configuration change: deploy it, verify the target from the server, and
generate new REALITY links. It does not affect clients using
`vless-xhttp-plain`.

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
ANSIBLE_CONFIG=ansible.cfg ansible-playbook -i inventories/production/hosts.yml playbooks/host-maintenance.yml --limit <host> --check --diff
ANSIBLE_CONFIG=ansible.cfg ansible-playbook -i inventories/production/hosts.yml playbooks/host-maintenance.yml --limit <host>
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
