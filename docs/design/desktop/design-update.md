# Operational visual system update

The desktop extends OVPN's graphite canvas and ring mark with stronger semantic color. Color is paired with text/icons and evidence scope. The native title bar and system fonts remain; the design does not copy CELERITY assets or layout code.

| Semantic role | Dark text/accent | Light text/accent | Treatment |
| --- | --- | --- | --- |
| Verified measurement / allowed policy | `#4ADE80` | `#146C3A` | Green label and tinted surface; specify whether this is policy or measured traffic. |
| Failed measurement / identity conflict / quota block | `#FB7185` | `#A91D3A` | Red label, icon and restrained tinted incident card. |
| Partial assignment / expiry / stale evidence | `#FBBF24` | `#85540C` | Amber, with an explanation and next action. |
| Information / integration context | `#72D5E8` | `#135C76` | Cyan selection, links and design-preview labels. |
| Primary action | `#63E6A1` on dark | `#176C42` on light | Mint/green button with contrasting text; action color is not a health claim. |
| No evidence / disabled observation | Existing neutral text | Existing neutral text | Gray; never substitute a failed red or healthy green. |

Keep backgrounds dark/neutral rather than coloring the entire workspace. Use color at status pills, row avatars, incident edges, selected navigation and bounded summary cards. Reserve red buttons for destructive actions. Verification warnings are amber; a failed traffic measurement is red even if service health is green.

## Information hierarchy

- Users is a primary navigation destination. Its table presents policy, configured nodes, per-server quota, expiry and connectivity separately. A detail dialog exposes the component policy counts and the next action.
- Monitoring groups runtime, traffic and user reconciliation in separate cards, followed by a per-node/source table, one actionable incident and the observation policy/budget.
- Integrations describes Native, Checker and Knife by responsibility and permission scope. Unconfigured providers must not appear installed or connected in Local workspace.
- Changes & recovery links user actions, delivery and restore journeys to their implementation gates.
- Every unfinished workflow carries a design-preview label. Demonstration operations affect fixtures only. Local user policy is a live read-only projection, without invented online status or remote metrics.

## Density and accessibility

Use 12–14 px content, 24–30 px page headings, 40–44 px controls and stable table columns. Wide tables scroll inside their panels. At 1024×720, navigation remains reachable and the page has no horizontal overflow. Support reduced motion and visible keyboard focus. Screen-reader action names include the target user.

Check both themes, semantic foreground/tinted-background pairs, primary/destructive buttons, long Russian labels and keyboard dialog focus. Green/red status text must remain readable in addition to the colored indicator. Representative contrast checks are evidence, not a complete accessibility certification.

## Validation for this increment

Browser checks covered user detail, demo create/suspend with quota preservation, monitoring cycle/evidence, integration preview, English/Russian and both themes. At 1024×720 the page width matched the viewport; wide tables retained their own scrolling. Final console review reported no errors. Twelve representative semantic/button contrast pairs passed 4.5:1; the lowest was 5.32:1. This is not a full accessibility certification.

The new worker user projection was compared with the existing workspace's distinct-user count without exporting identities or credentials. Go unit/race checks, lint, frontend build/tests, macOS build and Windows x64 cross-build passed. The expanded native window was not manually verified in this pass; earlier native launch evidence belongs to the preceding development slice.
