# Ansible bootstrap for ovpn

This document covers host-layer automation only.

## Responsibility split

- Ansible: host bootstrap and security baseline
- `ovpn` CLI: VPN runtime, users, monitoring, and backup/restore commands

This split also applies to `proxy` hosts used for HA.
The proxy host is prepared by Ansible the same way as a regular VPN host, then configured as `--role proxy` by `ovpn`.
The first built-in proxy preset is `ru`.

## Supported targets

- Debian `12+` (including `13+`)
- Ubuntu `22.04+` (including `24.04+`)

## Safety defaults

- Root SSH is allowed by default.
- SSH remains on port `22` in the recommended flow.
- Firewall automation is enabled by default for clean ovpn-only hosts.
- Unattended upgrades install security updates only by default.
- Normal Ubuntu updates, Docker updates, and reboots remain manual maintenance tasks.
- Journald disk limits are enforced (`200M` system, `100M` runtime).
- Swapfile is enabled by default (`1GB`, `vm.swappiness=10`).

## Minimal bootstrap runbook

### 1. Configure inventory

Example `ansible/inventories/example/hosts.yml` (copy it privately to `ansible/inventories/production` before real deployments):

```yaml
all:
  children:
    vpn_servers:
      hosts:
        <vpn-host-fqdn>:
          ansible_host: <vpn-server-ip>
          ansible_user: root
          ansible_port: 22
    proxy_servers:
      hosts:
        <proxy-host-fqdn>:
          ansible_host: <proxy-server-ip>
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
ovpn_enable_swapfile: true
ovpn_swapfile_size_mb: 1024
ovpn_swapfile_swappiness: 10
ovpn_journald_system_max_use: "200M"
ovpn_journald_runtime_max_use: "100M"

# Optional host-level Tor exit blocking for Xray 443 only (default off).
ovpn_block_tor_exit_nodes: false

# Optional unattended security updates. Keep downtime-producing reboots under
# manual maintenance by default. Add host-specific package blacklists only when needed.
ovpn_enable_unattended_upgrades: true
ovpn_unattended_enable_ubuntu_updates: false
ovpn_unattended_auto_reboot: false
ovpn_unattended_package_blacklist: []
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

If you temporarily keep a non-ovpn service, add only a host-specific firewall exception:

```yaml
ovpn_firewall_extra_tcp_ports:
  - 10050
```

Unattended upgrades policy:

- security and ESM security updates are automatic when `ovpn_enable_unattended_upgrades=true`
- normal Ubuntu `-updates` are manual unless `ovpn_unattended_enable_ubuntu_updates=true`
- Docker repository packages are manual maintenance by default
- automatic reboot is disabled unless `ovpn_unattended_auto_reboot=true`
- host-specific package blacklists can be set with `ovpn_unattended_package_blacklist`

Manual maintenance window:

```bash
apt update
apt list --upgradable
apt full-upgrade
reboot
```

## Handoff to ovpn runtime

After host bootstrap:

```bash
./ovpn server add --name <server> --host <server-ip> --domain <domain> --ssh-user root --ssh-port 22
./ovpn server init <server>
./ovpn deploy <server>
./ovpn doctor <server>
```

Proxy host handoff:

```bash
./ovpn server add --name <proxy> --role proxy --proxy-preset ru --host <proxy-ip> --domain <proxy-domain> --ssh-user root --ssh-port 22
./ovpn server init <proxy>
./ovpn server backend attach --proxy <proxy> --backend <vpn-backend>
./ovpn deploy <vpn-backend>
./ovpn deploy <proxy>
./ovpn doctor <proxy>
```

Attach-first is not enough on its own. The backend should be redeployed after attachment so its Xray runtime includes the proxy relay service identity before the proxy starts sending traffic to it.

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
- [`docs/ha.md`](docs/ha.md)
- [`docs/security.md`](docs/security.md)
- [`docs/upgrades.md`](docs/upgrades.md)
