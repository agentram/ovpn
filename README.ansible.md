# Ansible bootstrap for ovpn

This document covers host-layer automation only.

## Responsibility split

- Ansible: host bootstrap and security baseline
- `ovpn` CLI: VPN runtime, users, monitoring, and backup/restore commands

## Supported targets

- Debian `12+` (including `13+`)
- Ubuntu `22.04+` (including `24.04+`)

## Safety defaults

- Root SSH is allowed by default.
- SSH remains on port `22` in the recommended flow.
- Firewall automation is enabled by default for clean ovpn-only hosts.
- Legacy nginx packages are purged by default.
- Journald disk limits are enforced (`200M` system, `100M` runtime).
- Swapfile is enabled by default (`1GB`, `vm.swappiness=10`).

## Minimal bootstrap runbook

### 1. Configure inventory

Example `ansible/inventories/example/hosts.yml` (copy it privately to `ansible/inventories/production` before real deployments):

```yaml
all:
  hosts:
    <host-fqdn>:
      ansible_host: <server-ip>
      ansible_user: root
      ansible_port: 22
```

Example host vars:

```yaml
ovpn_harden_ssh: true
ovpn_ssh_permit_root_login: true
ovpn_ssh_password_auth: false
ovpn_manage_firewall: true

# Clean-server optimization defaults.
ovpn_purge_legacy_nginx: true
ovpn_enable_swapfile: true
ovpn_swapfile_size_mb: 1024
ovpn_swapfile_swappiness: 10
ovpn_journald_system_max_use: "200M"
ovpn_journald_runtime_max_use: "100M"

# Optional host-level Tor exit blocking for Xray 443 only (default off).
ovpn_block_tor_exit_nodes: false
```

### 2. Validate and apply bootstrap

```bash
cd ansible
ANSIBLE_CONFIG=ansible.cfg ansible-playbook -i inventories/example/hosts.yml playbooks/bootstrap.yml --syntax-check
ANSIBLE_CONFIG=ansible.cfg ansible-playbook -i inventories/example/hosts.yml playbooks/bootstrap.yml --limit <host> --check --diff
ANSIBLE_CONFIG=ansible.cfg ansible-playbook -i inventories/example/hosts.yml playbooks/bootstrap.yml --limit <host>
```

### 3. Optional: apply security playbook

```bash
ANSIBLE_CONFIG=ansible.cfg ansible-playbook -i inventories/example/hosts.yml playbooks/security.yml --syntax-check
ANSIBLE_CONFIG=ansible.cfg ansible-playbook -i inventories/example/hosts.yml playbooks/security.yml --limit <host> --check --diff
ANSIBLE_CONFIG=ansible.cfg ansible-playbook -i inventories/example/hosts.yml playbooks/security.yml --limit <host>
```

Optional Tor exit-node blocking profile (host layer, fail-open, daily refresh):

```yaml
ovpn_block_tor_exit_nodes: true
ovpn_tor_exit_block_port: 443
ovpn_tor_exit_list_url: "https://check.torproject.org/torbulkexitlist"
ovpn_tor_update_schedule: "daily"
```

Validation commands:

```bash
ssh -p 22 root@<server-ip> 'echo ok'
ssh -p 22 root@<server-ip> 'sudo ss -lntup'
ssh -p 22 root@<server-ip> 'sudo docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"'
```

If you temporarily keep a non-ovpn service (for example Zabbix agent on `10050/tcp`), add only a host-specific firewall exception:

```yaml
ovpn_firewall_extra_tcp_ports:
  - 10050
```

## Handoff to ovpn runtime

After host bootstrap:

```bash
./ovpn server add --name <server> --host <server-ip> --domain <domain> --ssh-user root --ssh-port 22
./ovpn server init <server>
./ovpn deploy <server>
./ovpn doctor <server>
```

Default `ovpn-agent` host bind is `127.0.0.1:19000`. If that loopback port is occupied:

```bash
export OVPN_AGENT_HOST_PORT=19001
./ovpn deploy <server>
```

## Cleanup boundary

- `ovpn server cleanup` removes runtime artifacts only.
- Host-level packages and security policies remain Ansible-owned.

## Related docs

- [`README.md`](README.md)
- [`docs/security.md`](docs/security.md)
- [`docs/upgrades.md`](docs/upgrades.md)
