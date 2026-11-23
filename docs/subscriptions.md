# Client subscriptions

A subscription groups a user's active server profiles behind one URL. Publish a new snapshot after deploying endpoint or profile changes; clients receive it when they refresh the subscription. Publishing changes delivery files only.

## Managed subscriptions

In Desk, open **Users → Create subscription** to create a VPN user and issue either a one-time setup code or a personal HTTPS link. The user imports it from **Add subscription** in O Tunnel. The same link works in clients that accept a Base64 list of VLESS links. Subscriptions use the existing per-server traffic quotas.

1. Run the delivery service with access to the workspace's `delivery/bundles` and `delivery/invites` directories behind HTTPS.
2. Opt in each public entrypoint with `ovpn server subscription <server>`, then deploy it. Pass `--publish=false` to remove a server from managed subscriptions. For HA, opt in to the proxy and any backends you intend to offer as separate direct connections.
3. Create a subscription with `ovpn user subscription create --username <user> --base-url https://subscriptions.example.net`. Add `--format link` for a reusable HTTPS link.

OVPN publishes profiles from the most recent verified deploy. A new or changed provider endpoint is published after deployment, and O Tunnel refreshes when it opens and the saved list is stale. Its background schedule is subject to the operating system. Desk retries transient failures and reconciles quota state every five minutes while open. To keep reconciliation running on a headless control host, schedule `ovpn subscription sync` every five minutes in that workspace. The HTTP delivery service can continue serving existing files while the publisher is offline.

Use `ovpn subscription list --username <user>` to compare the support ID and profile revision in a problem report with the published list. Use `ovpn user subscription issue` to give another phone a code. With `--format link`, issuing a new link also stops updates for earlier device codes. Use `ovpn user subscription disable --username <user>` to stop both delivery and VPN access. Check the operation's per-node results and retry any failed step. Keep the bundles and invitation directory private and exclude them from backups that are shared publicly.

## Export a file

```bash
./ovpn user subscription export --username alice --out ./alice-subscription.txt
```

The default format is a Base64-encoded list of VLESS links, one primary profile per enabled server. Use `--server <server>` to select one node, `--all-profiles` to include all enabled profiles on the selected nodes, or `--format links` for plain text. A bundle supports up to 256 profiles. Missing, disabled, expired and locally quota-blocked users are excluded. Profiles retain their existing transport compatibility limits; including a profile does not add support to a client.

Files contain client credentials and are written atomically with mode `0600`. Static exports must be regenerated when access changes. No SSH connection or VPN user mutation is involved.

## Publish a stable URL

Prepare a private directory and create a subscription:

```bash
mkdir -m 700 subscription-bundles
./ovpn user subscription publish --username alice \
  --bundle-dir ./subscription-bundles \
  --base-url https://subscriptions.example.net
```

The command prints a subscription ID and a secret URL once. Save the ID for updates; give the URL only to its intended user. The delivery token is stored as a SHA-256 hash and cannot be recovered from a bundle. Bundles contain client credentials, node labels and per-profile expiry, without server private keys or agent tokens.

After deploying a replacement node or changing access, refresh the same ID:

```bash
./ovpn user subscription publish --username alice \
  --bundle-dir ./subscription-bundles --id <subscription-id>
```

The URL stays unchanged. Repeat the intended `--server` or `--all-profiles` selection on each publication. Refreshing with no eligible profiles makes the URL return `404`; publishing eligible profiles again restores the same URL. Updates are atomic, and concurrent publishers or revocations for the same ID are rejected. If a publisher is interrupted, confirm it has stopped before removing its `<subscription-id>.lock` directory.

## Deliver prepared bundles

Run the delivery service on a separate account or host with only the prepared directory, CLI executable and optional TLS files:

```bash
./ovpn subscription serve --bundle-dir ./subscription-bundles
```

The default listener is `127.0.0.1:8088`, suitable for a local TLS reverse proxy. Preserve the `/sub/` path when proxying. For direct HTTPS delivery:

```bash
./ovpn subscription serve --bundle-dir ./subscription-bundles \
  --listen 0.0.0.0:8443 --tls-cert <fullchain.pem> --tls-key <private-key.pem>
```

Use a base URL matching the public HTTPS origin and port. Plain HTTP is permitted only on literal loopback addresses, including a base URL such as `http://127.0.0.1:8088` for local testing. The default service accepts `GET` and `HEAD`, exposes `/health`, and requires no local control database, inventory or SSH keys. Mount only the bundle directory read-only when isolating it in a container; publish or synchronize individual files in that directory. Requests are not logged by the service. Configure any reverse proxy to suppress `/sub/` URLs in access logs and tracing: the URL is a credential.

Unexpired profiles are selected at request time. Other state changes require republishing or revocation; the service has no connection to the control database. Responses disable caching and advertise a six-hour refresh interval; client behavior can differ. No combined quota or traffic header is advertised because quotas are per server.

## Activate O Tunnel

To issue a one-time code for a published subscription, create a separate private invitation directory and pass it to `subscription serve --invite-dir ./invites` in addition to the bundle directory. The service needs write access to the invitation directory and read access to the bundles.

```sh
mkdir -m 700 invites
ovpn subscription invite --bundle-dir ./subscription-bundles --invite-dir ./invites \
  --id <subscription-id> --base-url https://subscriptions.example.net
```

Give the code to the intended user for **Enter code**. Add `--out ./invite.png` to issue a new code as a QR image. A code expires after 30 minutes by default and activates one app installation. The app can retry the same activation after an interrupted request. Preserve the `/activate` and `/device/` paths at the HTTPS reverse proxy. Exclude activation request bodies and secret URLs from logs.

The installation follows the same published profile list. Rotating or revoking the parent subscription stops updates for its activated installations. Server-side access uses the underlying VPN user's credentials.

## Rotate or revoke delivery

Rotate a lost or disclosed URL without changing the VPN user's UUID:

```bash
./ovpn user subscription publish --username alice \
  --bundle-dir ./subscription-bundles --id <subscription-id> \
  --rotate-token --base-url https://subscriptions.example.net
```

The old token stops working and the command prints a new URL. To stop delivery entirely:

```bash
./ovpn subscription revoke --bundle-dir ./subscription-bundles --id <subscription-id>
```

Revoking or rotating a subscription token stops future downloads. Already imported VPN credentials remain usable until the VPN user is disabled, removed or its credentials are changed. Use `user disable --username alice` or `user rm --username alice` to revoke VPN access across enabled servers, then refresh or revoke its published subscriptions.

## Compatibility

The Base64 link-list format is consumed by [v2rayN](https://github.com/2dust/v2rayN/blob/master/v2rayN/ServiceLib/Handler/SubscriptionHandler.cs) and documented by [Xray Checker](https://xray-checker.kutovoy.dev/configuration/subscription/). Add the URL as a subscription rather than as one VLESS node. The automated regression covers HTTPS delivery, Base64 decoding and native Xray traffic for REALITY/TCP and self-SNI TLS; import in individual GUI app versions still needs verification. Existing [transport compatibility limits](transports.md) apply, especially to experimental profiles. Automatic failover depends on the client configuration and is not provided by a link list alone.

## Upgrading from account-based delivery

Remove the legacy account flags from the delivery service configuration before restarting it.
Update the HTTPS delivery service before issuing access. Keep the bundle and invitation directories in place. HTTPS subscription URLs and already
claimed anonymous device subscriptions continue to refresh. Issue a fresh code for any
unclaimed legacy invitation or device previously enrolled through account sign-in.
Cached profiles can remain usable until access expires or is revoked on the VPN servers.
