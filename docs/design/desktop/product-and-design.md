# Product and visual direction

Status: design proposal. Working name: **OVPN Desk**. Audience: one operator or a small team managing a self-hosted Xray fleet. The primary promise is a short, inspectable route from a new server to verified user access.

## Interaction model

The first screen answers: what works, what lacks evidence, what needs action? Prefer one actionable incident over an undifferentiated red/green dashboard. The normal path needs no terminal. Detailed commands and evidence remain available when they help diagnosis.

| Screen | Main job | Primary action |
| --- | --- | --- |
| Overview | Separate running services from working access; show the next useful action | Check access / investigate the current incident. |
| Servers | Find a node, compare desired/deployed state and inspect its checks | Add server. |
| Access | Issue, publish, rotate or revoke access with visible scope | Create a user, then deliver a subscription. |
| Checks | Compare sources, freshness, transport and observed result | Run an explicit bounded check. |
| Changes | Follow a concrete plan through apply, verification and recovery | Review a plan or inspect its evidence. |
| Recovery | Separate archive integrity from a successful restore drill | Prepare a recovery plan. |

Keep global search/actions available through `Ctrl+K` / `Cmd+K`. Use a detail drawer for evidence without losing list context. Do not make hover the only way to discover an action.

### First run

1. Open existing local state or start a new workspace. Detect vault/SSH/tool prerequisites and explain actionable blockers.
2. Name the server and select SSH authentication through native controls. Verify its host fingerprint. Keep server credentials out of frontend stores.
3. Recommend the baseline REALITY profile. TLS prerequisites appear when that profile is chosen; advanced transports are secondary.
4. Present a plan: target, inherited user scope, ports, snapshots, service restarts, verification and recovery limits. Applying the reviewed plan is the final write action.
5. Show structured progress. Finish only when selected postconditions pass; otherwise offer diagnosis/recovery rather than a green success banner.
6. Issue access and a subscription. Explain the per-server quota and global user-operation scope at the relevant decision.

### Daily scenarios

- A process is running, but traffic fails: show source/time and concrete evidence, preserve uncertainty about cause, then prepare a narrow diagnostic/remediation plan.
- A node is replaced: apply and verify; offer publication of changed subscription snapshots as an explicit next step. Publishing is not automatic in today's implementation.
- A delivery URL is disclosed: rotate the token; show that old downloads stop and imported VPN UUIDs remain active. VPN disable/removal is a different action.
- The checker disappears or a laptop wakes: show stale/unavailable observations. Never present old successful checks as current health.
- An operation is interrupted: show an uncertain state and reconciliation path. Do not turn a retry into a duplicate deployment.

## Visual system

Extend the current website: graphite, silver and a restrained cold accent. The ring-and-stem mark follows OVPN's existing visual identity. Status colors carry operational meaning, never a decorative category palette.

| Token | Dark | Light | Purpose |
| --- | --- | --- | --- |
| Canvas | `#0B0E10` | `#F2F4F2` | Calm application background. |
| Surface | `#111619` | `#FFFFFF` | Tables, plans and evidence. |
| Raised | `#171D21` | `#F2F5F3` | Selection and secondary controls. |
| Primary text | `#EDF0EB` | `#1A2529` | Content and major values. |
| Secondary text | `#A0AAA9` | `#526468` | Labels and descriptions. |
| Accent | `#A6C9D5` | `#285E74` | Focus, active context and selected actions. |
| Verified | `#96C9AC` | `#256746` | Confirmed checks, with an icon and text. |
| Attention | `#E0BA7F` | `#825816` | Degradation or required review. |
| Failure/destructive | `#EFA4A5` | `#A6343D` | Failed operations and destructive actions. |

Use the system UI font stack; package any chosen brand font locally. Monospace is for identifiers, versions and measurements. Production typography target: 14px body/table text, 12px secondary labels, 24–30px page headings. Small uppercase captions are secondary navigation, never essential instructions.

Spacing uses a 4px base; main gaps are 16/24/32px. Corners: 8px controls, 12–14px panels. Keep shadows for modal elevation. Avoid glowing controls, constant motion, decorative world maps and oversized empty statistics.

### Components and states

- A status always has a label/icon and source/freshness where relevant. `unknown`, `stale`, `failed` and `not applicable` are separate states.
- Table columns keep service health and traffic apart. Important scope is visible before a write, including all enabled servers for today's user lifecycle.
- The operation panel contains impact, proof and the next action. Success means the stated verification passed, not that every possible client capability is supported.
- A subscription secret appears only after deliberate generation/reveal. Existing token hashes do not imply the old URL can be shown again.
- Empty, busy, partial failure, locked workspace, unavailable vault, unreachable host and recovered connection need designed states before implementation is considered complete.
- Respect OS title bars/menus, keyboard conventions and reduced motion. Use a native title bar first; do not ship imitation macOS window controls on every platform.

### Accessibility and usability gates

Keyboard-complete navigation, visible focus, focus return after dialogs and screen-reader names are required. Target WCAG AA text contrast and text/icon status redundancy. Support 125/150/200% scaling, long translations and 1024×720 windows without horizontal page scrolling; wide tables may scroll within their own container. English is the default interface language. Russian is selected in Settings. Production uses keyed message catalogs with plural rules, separate from business logic, and persists the language preference independently of server state. The prototype has a small display-only translation layer; it is not the production localization implementation.

The prototype demonstrates both themes, keyboard actions, first run, node setup, incident evidence, simulated apply, access and recovery planning. Its fixed fixture values are not measurements or product performance claims. Some recovery screens describe proposed workflows; the architecture/roadmap defines the work required to make them real.

## Where to aim beyond parity

These are hypotheses to validate, not claims that competitors lack the features:

| Proposed advantage | User outcome | Evidence required |
| --- | --- | --- |
| One operation: plan → snapshot → apply → verify | Understand exactly what changed and whether access works | Changed scope/revision invalidates the plan; a failed postcondition remains visibly unresolved. |
| Linked evidence from runtime, source and client checks | Distinguish a server failure from a route/checker/client problem | Fault-injection scenarios classified correctly, including unknown causes. |
| Rehearsed recovery | Recover operator control, not merely download an archive | Fresh-workspace restore and fresh tunneled connection in a disposable lab. |
| Subscription change preview | Replace a node without redistributing every client profile | Same URL yields the replacement after publication; old-token and old-UUID revocation tested separately. |
| Local desktop ownership | Operate without a public management panel | No public admin listener; secret-free view models; three-platform native validation. |

Node-group assignments, compatibility evidence by app/core version, canary rollout and topology rehearsal are valuable follow-ups. Introduce them with backend invariants and operational tests, rather than adding decorative controls to unsupported behavior.
