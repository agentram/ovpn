# Private Production Inventory

Do not commit real production inventory to this repository.

Recommended flow:

1. Copy `ansible/inventories/example` to `ansible/inventories/production` locally.
2. Replace the example hostnames, IPs, and host variables with your real values.
3. Keep the resulting files private and untracked.

This repository intentionally tracks only the example inventory so the public tree never exposes live infrastructure details.
