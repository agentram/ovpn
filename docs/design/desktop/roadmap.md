# Delivery plan and acceptance gates

Status: proposed engineering sequence. Estimates below are planning ranges for one experienced full-time engineer with review/design help; they are not delivery commitments. Native platform tooling, signing and infrastructure access can change the ranges.

| Stage | Scope | Planning range | Exit evidence |
| --- | --- | --- | --- |
| 0. Native spike | Wails packaging, bundled worker, SSH/agent/vault and basic accessibility on all three OS families | 3–5 engineering days | Native app opens and performs a redacted inventory query on each target; Windows OpenSSH and Linux WebView constraints are documented. |
| 1. Shared engine contract | Extract queries/probes, protocol handshake, DTOs, error model and operation journal/locks | 1–2 weeks | CLI behavior stays compatible; concurrent CLI/worker writes serialize; crash/reconnect reconciliation tests pass. |
| 2. Operator MVP | First-server wizard, reviewed plans, checked snapshots, target-scoped monitoring identity, user delivery and stable subscriptions | 2–3 weeks | Clean-host and existing-runtime lab journeys reach verified HTTPS; real-user state survives update and failed apply. |
| 3. Continuous observation | Optional local/remote checker, source/freshness model, sleep/wake behavior and incident evidence | 1–2 weeks | Native/checker disagreement, stale samples and checker outage are represented correctly; no default speed-test quota surprises. |
| 4. Recovery and scale | Restore drill, scoped assignments, client compatibility matrix and canary operations | Separate scoped iterations | Fresh-workspace recovery and failed-node replacement tested; group changes never broaden user access silently. |

Plan approximately **6–10 engineering weeks** for a credible operator MVP through continuous observation, including integration and review. Stage 4 is additional. Re-estimate after the native spike and extraction spike; do not hide the core work behind a desktop-shell estimate.

## First implementation slice

Progress: the read-only Wails/React host, bundled-worker verification, handshake, server/user policy inventory, shared native probe path and English/Russian settings are implemented in [the development build](../../desktop.md). This does not complete Stage 0: native Windows/Linux, SSH/vault and accessibility gates remain.

Build a signed-development native shell and a minimal machine worker that returns `engine.hello`, redacted inventory and one native HTTPS check. Include a demo workspace. No real remote write actions are enabled until the operation engine gates pass.

First extraction targets:

1. `internal/cli/root_cmd.go`: separate CLI wiring from workspace/application lifecycle.
2. `internal/cli/runtime_helpers.go`: move orchestration, notification policy and user-scope decisions into use cases; expose structured progress, not stdout parsing.
3. `internal/deploy/deploy.go`: per-operation staging, shared host locks, revision preconditions and explicit verification/recovery phases.
4. `internal/probe` and `internal/subscription`: reuse existing implementations behind ports; do not reimplement protocol/link generation in TypeScript.
5. `internal/ssh`: native platform tool discovery, trusted executable paths and host-key workflows, preserving strict verification.

## MVP acceptance journeys

| Scenario | Required outcome |
| --- | --- |
| Import existing state | Inventory appears with secret-free DTOs; no implicit deployment, key rotation or encryption rewrite. |
| First server | Prerequisites → host identity → concrete plan → provisioning → a real HTTPS request → access delivery. No redundant deploy after init. |
| Older agent / incorrect runtime account | Runtime may be healthy while traffic fails; preserve uncertainty, collect evidence, then verify a narrow remedy. |
| Global user add | UI states the full enabled-server scope and per-server quota before execution. |
| Node replacement | Config and traffic verified; explicit publication updates the same subscription URL; client refresh receives the replacement. |
| Delivery token compromise | Old URL fails after rotation; imported UUIDs remain valid until a separate user revocation. |
| Concurrent CLI and desktop write | One operation owns the host; no mixed staging or lost local updates. |
| Failure after remote apply | Show `applied_unverified` / `recovery_required`; reconcile actual state before rollback/retry. |
| Desktop/worker crash | Operation journal persists; restart cannot silently repeat a remote write. |
| Checker process stopped / laptop asleep | Samples expire; source unavailable is distinct from proxy failure. |
| Restore | Verify archive, decrypt in the intended vault/recovery context, restore a disposable workspace, check a new tunneled connection and test revocation. |
| Secret handling | Private keys/tokens absent from frontend snapshots, operation logs, screenshots and exported support packs. |

## Product measures

All values here are **targets for a pilot**, not current measurements:

- At least 4 of 5 first-time pilot operators complete the clean-host journey without terminal assistance. Target median: ≤10 minutes excluding provider provisioning and package-download delays.
- Every write exposes scope and result; no scripted acceptance case presents unverified state as successful.
- All three native OS families pass onboarding, probe, subscription and recovery scenarios before the desktop release is called supported.
- Confirm accessibility with keyboard-only use and 200% scaling; contrast checks alone are insufficient.
- Measure startup/RSS/package size on reference machines after embedding Xray and the actual worker. Set performance budgets from those measurements.
- Pilot users can distinguish “runtime healthy / traffic failed,” “checker unavailable,” and “sample stale” without reading raw logs.

## Release and growth

Keep the current CLI available and compatible throughout extraction. Desktop artifact versions, worker protocol and supported CLI ranges are explicit. Signing, notarization, hash verification, update rollback and native installer smoke tests are release gates.

Use a short reproducible demo and a disposable lab to recruit pilot users. A Habr Part 2 can show one complete journey and a failure/recovery case; an English adaptation can follow. Publish measured results and clearly scoped compatibility instead of unsupported superiority claims. Design patterns and screenshots should link to reproducible examples and small contributor tasks.

The current design work is included as a proposal under the existing PR. It does not turn the 1.12.0 CLI work into a claim that desktop binaries have shipped.

The [complete application plan](application-plan.md) refines the next increments: reviewed user lifecycle first, then scheduled observation/Checker, Knife diagnostics, fleet assignments and recovery. Monitoring and integration screens are explicit design previews until their backend gates pass.
