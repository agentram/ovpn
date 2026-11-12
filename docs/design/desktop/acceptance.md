# Desk 1.13 acceptance

Use temporary accounts on two disposable VPN servers for access lifecycle
acceptance, plus a proxy for HA acceptance. Never use an operator workspace or
customer installation.

## Platform gates

On native macOS, Windows/WebView2 and Linux/GTK, verify: startup with the
matching worker; SSH-agent and key-file access; reviewed installation on a new
server; opening an existing installation; user add/share/disable; profile and
backend edits followed by deploy; bounded logs/debug; verified backup, failed
apply and restore. Repeat after sleep and reopening the app. Check both themes,
all three languages, keyboard focus, large text, a small window and long names.
Record actual outcomes per OS; browser tests and cross-compilation do not close
these gates. Public signing, installers and notarization are separate work.

## HA rollout gate

Review backend-first application and verified backups on the three-node stand.
Keep a direct client link as a comparison. With a user connected through the
proxy, stop one backend and verify new connections use the remaining node.
Stop both and verify relay traffic fails closed; preset-direct destinations
retain their configured behavior. Restore a backend and recheck access.
Existing TCP sessions can close during failover; reconnecting is a separate
client behavior. Stop the proxy and confirm that direct links still work while
proxy access is unavailable. Retain per-node outcomes and traffic history.

The Docker HA regression proves pool TCP behavior and metric projection. It
does not close this full VLESS/HTTPS rollout gate or native platform acceptance.

## Five-person pilot

Give each participant an empty workspace, two disposable hosts and their SSH
access instructions. Do not coach navigation. Ask them to:

1. Add and install a server, reviewing its SSH fingerprint.
2. Create a user with an end date and a traffic allowance.
3. Share working access by QR or link.
4. Enable automatic checks and explain the difference between access and presence.
5. Find the reason for a prepared service failure and verify its repair.

Pass when at least four of five participants independently complete server
setup, user creation, delivery and failure diagnosis. Record completion,
blocking labels and mistaken assumptions without participant personal data.
No pilot or native-platform outcome is implied by automated test success.
