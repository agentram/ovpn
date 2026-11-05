# OVPN Desk — desktop design proposal

Status: **proposal and interactive prototype**, not a shipped desktop application. The prototype uses synthetic data, performs no network operations against VPN servers, and cannot modify control state.

OVPN Desk is an operator application for Linux, macOS and Windows: connect a server, deliver access, verify traffic and recover from a change. The initial product manages servers; a system-wide VPN/TUN client is a separate scope.

- [Architecture and decisions](architecture.md)
- [Product, UX and visual system](product-and-design.md)
- [Implementation sequence and acceptance gates](roadmap.md)
- [Proposed machine contract](machine-api.json)
- [Design validation record](validation.md)
- [Interactive prototype](prototype/index.html)

Open the prototype directly, or run from the repository root:

```bash
python3 -m http.server 8938 --bind 127.0.0.1 \
  --directory docs/design/desktop/prototype
```

Open `http://127.0.0.1:8938/`. Try the workspace selector's first-run scenario, add a server, investigate the failed route, apply the simulated plan, change the measurement source, publish/revoke a subscription and switch the theme. English is the default; select Russian in Settings. `Ctrl+K` / `Cmd+K` opens actions. All mutations are in-memory demonstrations.

## What to learn from CELERITY

Reviewed CELERITY source at [`f8f7da58`](https://github.com/ClickDevTech/CELERITY-panel/tree/f8f7da58d2f0bb21fd8f0bb31a533bdbfc98ea6f). The current main-branch source and release metadata are separate snapshots.

| Observed pattern | Adaptation for OVPN Desk |
| --- | --- |
| Guided deployment choice, infrastructure setup and starter access in its [wizard](https://github.com/ClickDevTech/CELERITY-panel/blob/f8f7da58d2f0bb21fd8f0bb31a533bdbfc98ea6f/views/wizard.ejs) | Import existing local state or connect a server; show host identity and impact before setup; finish with verified traffic and delivery. |
| Groups, subscriptions and node operations in its [feature overview](https://github.com/ClickDevTech/CELERITY-panel/blob/f8f7da58d2f0bb21fd8f0bb31a533bdbfc98ea6f/README.md) | Keep the daily server/access workflow short. Show OVPN's current global user-operation scope explicitly. Introduce scoped assignments only with backend support. |
| Real end-to-end [external probes](https://github.com/ClickDevTech/CELERITY-panel/blob/f8f7da58d2f0bb21fd8f0bb31a533bdbfc98ea6f/docs/probes.md) | Distinguish service health, traffic, measurement source and freshness; connect evidence to a specific change or incident. This is an existing CELERITY capability, not a unique OVPN feature. |
| Panel update and rollback in its [maintenance guide](https://github.com/ClickDevTech/CELERITY-panel/blob/f8f7da58d2f0bb21fd8f0bb31a533bdbfc98ea6f/docs/safe-update.md) | Design a shared CLI/desktop operation journal, concrete plans, checked snapshots and post-change evidence. Treat restore rehearsal as an additional, measurable outcome. |

CELERITY already has a broad feature set, including Hysteria2, topology, CDN scenarios and S3 backups. A desktop shell alone does not establish superiority. OVPN's proposed focus is a local operator workflow with predictable changes, clear client-path evidence and tested recovery. Use the acceptance measures in the roadmap to assess that hypothesis.

The visual work here is original, based on OVPN's existing graphite/silver branding. No CELERITY code or visual assets were copied. References are for product and architecture analysis.

## Current foundation and proposed work

| Capability | Current OVPN | Desktop work required |
| --- | --- | --- |
| Inventory, users, deployment, backups, doctor | Implemented in Go; orchestration is coupled to Cobra handlers | Extract shared use cases and redacted DTOs; keep CLI behavior compatible. |
| Native HTTPS probe and checker JSON export | Implemented | Source/freshness model, scoped monitoring identity and checker adapter. |
| Stable subscription snapshots and token lifecycle | Implemented | Guided publish/rotate/revoke UI; remote delivery placement; optional publish-after-change workflow. |
| Desktop UI, machine API, durable operation execution | Not implemented | Core prerequisites before real write actions. |
| Plan fingerprints, per-host cross-process locks, isolated staging and recovery reconciliation | Not a complete shared workflow | Implement and test before safe desktop deployment. |
| Node assignments, encrypted restore drill, client import matrix | Proposed extensions | Separate milestones and compatibility evidence. |

A Windows amd64 CLI cross-build passed during this design review. That demonstrates compilation, not Windows SSH, installer or desktop compatibility. Wails, native desktop packaging and GUI application imports have not been built or certified by this prototype.
