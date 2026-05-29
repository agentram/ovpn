# ovpn

[![Go Quality](https://github.com/agentram/ovpn/actions/workflows/ci.yml/badge.svg)](https://github.com/agentram/ovpn/actions/workflows/ci.yml)
[![Security](https://github.com/agentram/ovpn/actions/workflows/security.yml/badge.svg)](https://github.com/agentram/ovpn/actions/workflows/security.yml)
[![Release](https://github.com/agentram/ovpn/actions/workflows/release.yml/badge.svg)](https://github.com/agentram/ovpn/actions/workflows/release.yml)

`ovpn` operates self-hosted Xray (`VLESS + REALITY`) VPN servers from a local Go CLI over SSH.

The CLI keeps server and user state in `~/.ovpn`, renders Docker Compose runtime files, and pushes them to Linux hosts. Optional HAProxy support can route traffic to a VPN backend pool.

## Fast navigation

- [Architecture](#architecture)
- [Design points](#design-points)
- [Security and quota defaults](#security-and-quota-defaults)
- [Quick start](#quick-start)
- [Production flow](#production-flow)
- [HA proxy rollout](#4a-optional-ha-proxy-rollout)
- [User operations](#6-add-users-and-deliver-links)
- [Monitoring](#7-optional-monitoring)
- [Documentation map](#documentation-map)

## Production model

- Ansible prepares the host baseline: packages, Docker, SSH, firewall, unattended upgrades, and hardening.
- `ovpn` owns the VPN runtime: Xray config, users, quotas, monitoring, backups, and cleanup.
- Desired state stays local under `~/.ovpn`; deploys render runtime files and push them over SSH/SCP.
- Remote services run as Docker Compose under `/opt/ovpn`, which keeps maintenance and recovery predictable.
- Xray REALITY listens on `443/tcp`; internal agent and monitoring endpoints stay private by default.
- Real inventory, hostnames, IPs, tokens, and private keys stay outside the public repository.

## Architecture

```mermaid
flowchart LR
  classDef operator fill:#e8f5ef,stroke:#167054,stroke-width:2px,color:#17231d
  classDef host fill:#eef4fb,stroke:#225b8d,stroke-width:2px,color:#17231d
  classDef runtime fill:#fff7e8,stroke:#8f4f23,stroke-width:2px,color:#17231d
  classDef optional fill:#f7f0ff,stroke:#7050a8,stroke-width:2px,color:#17231d
  classDef client fill:#f2f2ec,stroke:#6a6f68,stroke-width:1.5px,color:#17231d

  subgraph local["Operator machine"]
    cli["ovpn CLI"]
    state["~/.ovpn\nservers, users, quotas"]
    ansible["Ansible inventory\nhost hardening"]
    ci["GitHub Actions\nCI, security, release"]
  end

  subgraph vpn["VPN host(s)"]
    hostA["VPN host A\n/opt/ovpn\nXray 443 + agent"]
    hostN["VPN host B..N\nsame runtime model"]
    monitoring["optional monitoring\nPrometheus/Grafana\nTelegram bot"]
    backups["backup + cleanup\nsnapshots"]
  end

  subgraph ha["Optional HA entrypoint"]
    proxy["HAProxy proxy\ncountry preset routing"]
    backends["VPN backend pool\nhost A..N"]
  end

  clients["Clients\nmobile / desktop\nVLESS link or QR"]

  state --> cli
  ansible -->|bootstrap| hostA
  ansible -->|bootstrap| hostN
  ci -->|validated release binary| cli
  cli -->|SSH/SCP deploy| hostA
  cli -->|SSH/SCP deploy| hostN
  hostA --> monitoring
  hostN --> monitoring
  hostA --> backups
  hostN --> backups
  proxy -->|foreign relay| backends
  backends --> hostA
  backends --> hostN
  clients -->|443/tcp| proxy
  clients -->|direct 443/tcp| hostA
  clients -->|direct 443/tcp| hostN

  class cli,state,ansible,ci operator
  class hostA,hostN host
  class backups runtime
  class monitoring,proxy,backends optional
  class clients client
```

## Design points

- Local state: `~/.ovpn` stores servers, users, quotas, and metadata used for rendering.
- SSH/SCP deploy path: commands render files locally and apply them through normal server access.
- Docker runtime: Xray, `ovpn-agent`, optional proxy, and monitoring run under `/opt/ovpn`.
- Multi-host user operations: user add/remove/enable/disable/quota commands apply to all enabled VPN servers by default.
- Optional proxy role: HAProxy can front a backend pool and use country presets for split routing.
- Security defaults: Xray routing blocks BitTorrent and public tracker domains; Ansible can add host-level Tor exit filtering.
- Maintenance commands: `doctor`, `status`, logs, backups, restore, cleanup, monitoring, and release checks are part of the CLI.

## Key features

- Local desired state in `~/.ovpn`
- Remote Docker Compose runtime in `/opt/ovpn`
- User lifecycle commands and VLESS link generation
- Global-by-default user provisioning across enabled servers
- Rolling `30d` traffic quota enforcement
- Optional Prometheus, Alertmanager, Grafana, and Telegram bot monitoring
- Optional HA proxy entrypoint with backend failover
- Backup, restore, and decommission commands

## Versioning

- Current pinned version: `1.4.4`
- Check locally: `./ovpn version`
- Release source of truth:
  - `VERSION`
  - top entry in `CHANGELOG.md`
- Runtime image defaults source of truth:
  - [`internal/defaults/images.go`](internal/defaults/images.go)
- Versioning policy:
  - major: breaking CLI, API, or operator workflow changes
  - minor: new commands, monitoring surfaces, or operator features
  - patch: fixes and small UX improvements

## Community

- Issues: confirmed bugs and concrete feature requests
- Discussions: questions, usage help, design ideas, and announcements
- Security: use private vulnerability reporting, not public issues

## Requirements

- Release binary, or Go `1.26.2+` when building from source
- SSH key access to the target host
- Target host running Debian `12+` or Ubuntu `22.04+`
- Clean Linux host recommended
- Root SSH access is the simplest supported setup
- Docker/Compose on the host, usually installed by the Ansible bootstrap

## Security and quota defaults

- Default security profile: `OVPN_SECURITY_PROFILE=minimal`
- Minimal profile blocks BitTorrent protocol traffic and public tracker domains in Xray routing.
- Threat DNS defaults to `9.9.9.9,149.112.112.112`.
- If the selected Xray image cannot validate geosite resources, temporarily roll back with:

```bash
export OVPN_SECURITY_PROFILE=off
./ovpn deploy <server>
```

- Default quota: rolling `30d`, `300 GB` per user when no per-user limit is set.
- Optional host-level Tor exit-node blocking exists in Ansible and is off by default.

See [`docs/security.md`](docs/security.md) for the full security model.

## Capacity defaults

- Remote server backups: keep latest `7`
- Local backups: keep latest `7`
- Remote pre-deploy snapshots: keep latest `7`
- Monitoring defaults are sized for small hosts:
  - Prometheus scrape/evaluation interval: `30s`
  - Prometheus retention: `10d`
  - cAdvisor housekeeping: `30s`

## Quick start

Use this flow for a first small server. Replace placeholders with your own values.

```bash
# Use the release archive for your workstation OS/arch, or build from source for development.
# Release archives contain one executable: ovpn.
# Source build:
# go build -o ovpn ./cmd/ovpn
./ovpn version

./ovpn server add \
  --name <server> \
  --host <server-ip> \
  --domain <domain> \
  --ssh-user root \
  --ssh-port 22 \
  --xray-version 26.3.27

./ovpn server init <server>
./ovpn doctor <server>
./ovpn user add --username <user>
./ovpn user link --server <server> --username <user>
```

`user link` prints the client link and a terminal QR code by default for mobile onboarding. Use `--qr=false` when you need link-only output for scripts.

`server init` performs the first bootstrap/deploy for the VPN runtime. Use `deploy` later when you change runtime settings:

```bash
./ovpn deploy <server>
```

## Production flow

Recommended order:

1. Prepare the host with Ansible.
2. Register the server in local `ovpn` state.
3. Initialize and validate the runtime.
4. Add users and share client links.
5. Enable monitoring if you need it.
6. Back up before risky changes.

### 1. Pre-flight checks

Before changing a host, confirm SSH, listening ports, and Docker state:

```bash
ssh root@<server-ip> 'hostnamectl'
ssh root@<server-ip> 'sudo ss -lntup'
ssh root@<server-ip> 'sudo docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"'
```

### 2. Host bootstrap with Ansible

Start from the example inventory and keep your real inventory private.

```bash
cd ansible
mkdir -p inventories/production
cp -R inventories/example/. inventories/production/
```

Edit `inventories/production/hosts.yml`, then validate and apply:

```bash
ANSIBLE_CONFIG=ansible.cfg ansible-playbook -i inventories/production/hosts.yml playbooks/bootstrap.yml --syntax-check
ANSIBLE_CONFIG=ansible.cfg ansible-playbook -i inventories/production/hosts.yml playbooks/bootstrap.yml --limit <host> --check --diff
ANSIBLE_CONFIG=ansible.cfg ansible-playbook -i inventories/production/hosts.yml playbooks/bootstrap.yml --limit <host>
```

For already deployed hosts, use the maintenance playbook instead:

```bash
ANSIBLE_CONFIG=ansible.cfg ansible-playbook -i inventories/production/hosts.yml playbooks/host-maintenance.yml --limit <host> --check --diff
ANSIBLE_CONFIG=ansible.cfg ansible-playbook -i inventories/production/hosts.yml playbooks/host-maintenance.yml --limit <host>
```

See [`README.ansible.md`](README.ansible.md) for inventory and hardening details.

### 3. Register server in local state

```bash
./ovpn server add \
  --name <server> \
  --host <server-ip> \
  --domain <domain> \
  --ssh-user root \
  --ssh-port 22 \
  --xray-version 26.3.27
```

### 4. Initialize and deploy runtime

```bash
./ovpn server init <server>
./ovpn doctor <server>
./ovpn server status <server>
```

Use `deploy` after config or code changes:

```bash
./ovpn deploy <server>
./ovpn doctor <server>
```

Official release binaries embed the Linux `ovpn-agent` and `ovpn-telegram-bot` runtime binaries used by deploys, so release archives contain only the `ovpn` executable. Normal installed usage does not require Go, a source checkout, or keeping sidecar binaries in the same directory. Use `OVPN_REPO_ROOT` only for development builds without embedded runtime assets, or when you intentionally deploy an unsupported runtime architecture through source fallback:

```bash
export OVPN_REPO_ROOT=/absolute/path/to/ovpn
ovpn deploy <server>
```

### 4a. Optional HA proxy rollout

Use a proxy only when you want one entrypoint in front of existing VPN backends.
Available proxy presets are `ru` and `cn` (`china` is accepted as an alias for `cn`).

```bash
./ovpn server add \
  --name <proxy> \
  --role proxy \
  --proxy-preset ru \
  --host <proxy-ip> \
  --domain <proxy-domain> \
  --ssh-user root \
  --ssh-port 22

./ovpn server init <proxy>
./ovpn server backend attach --proxy <proxy> --backend <vpn-backend>
./ovpn deploy <vpn-backend>
./ovpn config validate --server <proxy>
./ovpn deploy <proxy>
./ovpn doctor <proxy>
```

Deploy the backend after attaching it so the backend runtime receives the proxy relay identity.

See [`docs/ha.md`](docs/ha.md) for the full HA design and troubleshooting guide.

### 5. Validate runtime

```bash
./ovpn doctor <server>
./ovpn server status <server>
./ovpn server logs <server> --service xray --tail 200
./ovpn server logs <server> --service ovpn-agent --tail 200
```

### 6. Add users and deliver links

```bash
./ovpn user add --username <user>
./ovpn user add --username <user> --expiry 2026-05-01
./ovpn user quota-set --username <user> --monthly-gb 400
./ovpn user quota-set --username <user> --monthly-bytes <bytes>
./ovpn user enable --username <user>
./ovpn user rm --username <user>

./ovpn user link --server <server> --username <user>
./ovpn user link --server <server> --username <user> --qr=false
./ovpn user link --server <server> --username <user> --qr-file ~/Desktop/<user>.png
./ovpn user show --server <server> --username <user>
./ovpn user list --server <server>
```

Mutating user commands apply to all enabled servers by default. Link and read commands stay server-scoped. QR output is enabled by default and contains the same full client credential as the `vless://` link; treat saved QR files as secrets.

Use `--monthly-gb` for human-sized quota changes. `ovpn` stores quota internally as bytes; `--monthly-gb 400` means `400 * 1024^3` bytes. `--monthly-bytes` remains available for exact automation.

When `--email` is omitted, new users get stable identity `username@global`. User expiry is cluster-wide and uses UTC end-of-day semantics.

### 7. Optional monitoring

Start monitoring:

```bash
./ovpn deploy <server>
./ovpn server monitor up <server>
./ovpn server monitor status <server>
```

Telegram setup can be done in one command:

```bash
OVPN_TELEGRAM_BOT_TOKEN=<token> \
./ovpn server monitor telegram-setup <server> \
  --owner-user-id <owner-user-id>
```

The Telegram bot is read-only by default. Owner-only recovery actions require `OVPN_TELEGRAM_ADMIN_TOKEN`.

See [`docs/monitoring.md`](docs/monitoring.md) for dashboards, alerts, and Telegram behavior.

### 8. Backups

```bash
./ovpn server backup <server>
ls -lah ~/.ovpn/backups
```

### 9. Decommission old runtime

Preview first:

```bash
./ovpn --dry-run server cleanup <server>
```

Then run cleanup with explicit confirmation:

```bash
./ovpn server cleanup <server> --confirm CLEANUP
```

By default, cleanup removes remote runtime files and disables local server metadata. Add `--remove-local` only when you also want to remove local metadata.

## Minimal operational commands

```bash
./ovpn version
./ovpn deploy <server>
./ovpn restart <server>
./ovpn doctor <server>
./ovpn server status <server>
./ovpn server logs <server> --service xray --tail 200
./ovpn stats --server <server>
./ovpn user list --server <server>
./ovpn user add --username <user>
./ovpn user quota-set --username <user> --monthly-gb 400
./ovpn user link --server <server> --username <user>
./ovpn server monitor up <server>
./ovpn server backup <server>
./ovpn server restore <server> --remote-path /opt/ovpn-backups/<archive>.tgz
./ovpn server cleanup <server> --confirm CLEANUP
```

## VPN clients

End-user setup instructions are in Russian and cover iOS, Android, Windows, and macOS:

- [`docs/clients.md`](docs/clients.md)

Generate the optional PDF guide locally when you need it for Telegram `/guide`:

```bash
make docs-pdf
```

## Documentation map

- [`README.md`](README.md): main operator entrypoint
- [`README.ansible.md`](README.ansible.md): host bootstrap and hardening
- [`DEVELOPMENT.md`](DEVELOPMENT.md): contributor and architecture guide
- [`docs/security.md`](docs/security.md): security and hardening model
- [`docs/ha.md`](docs/ha.md): HA proxy topology and rollout
- [`docs/monitoring.md`](docs/monitoring.md): monitoring operations
- [`docs/ci.md`](docs/ci.md): GitHub Actions workflows
- [`docs/testing.md`](docs/testing.md): testing strategy
- [`docs/upgrades.md`](docs/upgrades.md): upgrades, rollback, and cleanup

## Release process

1. Update `VERSION`.
2. Prepend the matching version entry to `CHANGELOG.md`.
3. Merge to `main`.
4. GitHub Actions validates both files, creates the semver tag if needed, and publishes the release.

## Protected main

Recommended required checks:

- `Go Quality`
- `Ansible Quality`
- `Gitleaks Tree Scan`
- `Trivy FS Scan`

Release tags matching `*.*.*` should be protected from update and deletion.

## Support

This repository includes an optional sponsor button and public donation page:

- sponsor button config: [`.github/FUNDING.yml`](.github/FUNDING.yml)
- donation page source: [`docs/donate/index.html`](docs/donate/index.html)
- project page URL: `https://agentram.github.io/ovpn/donate/`

## License

This repository is source-available under the `Attribution-NonCommercial Source License 1.0 (ovpn)`.
You may use, modify, and share it for non-commercial purposes with attribution. Commercial use requires separate permission.
