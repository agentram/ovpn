# Transport Profiles

`ovpn` keeps the original TCP/REALITY profile for compatibility:

```text
vless-reality-tcp-vision = VLESS / TCP / REALITY / xtls-rprx-vision on 443/tcp
```

That profile is widely supported by current clients and existing links keep working. It is still one traffic shape, though, and in degraded networks it has not been the most reliable option.

Transport profiles make that explicit: keep the deprecated compatibility profile live, enable one fallback profile, test it with a small set of users, and switch the server primary profile only if the tradeoff is acceptable. In current field tests, plain XHTTP has been the most reliable fallback shape on some degraded paths, but it is not transport-encrypted by itself.

## Profiles

| Profile | Status | Port | Use case |
| --- | --- | ---: | --- |
| `vless-reality-tcp-vision` | deprecated | `443/tcp` | Original VLESS + REALITY profile. Best compatibility, less reliable on affected paths. |
| `vless-reality-xhttp` | experimental | `8443/tcp` | XHTTP + REALITY for clients that support XHTTP. Useful as a different traffic shape. |
| `vless-xhttp-plain` | fallback | `13179/tcp` | Plain XHTTP shape for affected networks; no transport security unless fronted by TLS separately. |
| `vless-ws-tls-web` | planned | n/a | WebSocket/TLS behind a real HTTPS site. Not deployable yet because it needs certificate and web-front handling. |

The fallback profiles are not magic. They give you controlled A/B testing and faster rotation when a network path degrades.
The `vless-xhttp-plain` profile deliberately has `security=none`, `path=/`, and no REALITY parameters because it matches simple XHTTP profiles seen working in the field. That means Xray does not add REALITY/TLS transport security on this profile. HTTPS destinations inside the tunnel are still protected by HTTPS, but the VLESS/XHTTP transport itself is not protected like the REALITY profiles. Use it only when that tradeoff is acceptable, and keep monitoring whether it remains reliable for your users.

## Enable and Test

List profiles:

```bash
./ovpn server profile list <server>
```

Enable an extra profile:

```bash
./ovpn server profile enable <server> vless-reality-xhttp
./ovpn server profile enable <server> vless-xhttp-plain
./ovpn deploy <server>
./ovpn doctor <server>
```

Generate a profile-specific link:

```bash
./ovpn user link --server <server> --username alice --profile vless-reality-xhttp
./ovpn user qr --server <server> --username alice --profile vless-reality-xhttp --out ~/Downloads/alice-xhttp.png
./ovpn user qr --server <server> --username alice --profile vless-xhttp-plain --out ~/Downloads/alice-plain-xhttp.png
```

Export all enabled profiles for a user:

```bash
./ovpn user export --server <server> --username alice --all-profiles --out ~/Downloads
```

Optionally switch the default profile used by `user link` when no `--profile` is passed:

```bash
./ovpn server profile switch <server> vless-xhttp-plain
./ovpn deploy <server>
```

Do this only after testing with real clients. Switching the primary profile does not change already issued links; it only changes future link generation when `--profile` is omitted.

Keep the old profile enabled during testing.
After users have migrated, disable a non-primary profile and redeploy:

```bash
./ovpn server profile disable <server> vless-reality-tcp-vision
./ovpn deploy <server>
./ovpn doctor <server>
```

`disable` updates local desired state only. Old links for that profile keep working until the next successful deploy removes the Xray inbound.
The CLI refuses to disable the current primary profile; switch primary first, deploy, verify, then disable the old profile.

Link and QR commands fail early when a requested profile is unknown, planned, or disabled. That is intentional: they should not print a secret that cannot work on the selected server.

## Client Compatibility

Client support differs by transport and version:

- TCP + REALITY + vision is the compatibility baseline.
- XHTTP is newer. Test the exact client and version before sending it broadly.
- Plain XHTTP may import in clients that fail REALITY/XHTTP, but it does not provide the same stream-security layer unless you put it behind a separate TLS front.
- WS/TLS with a real web front is a separate implementation step, not just another link parameter.

When a user reports “connected, but nothing loads”, compare profiles instead of guessing:

```bash
./ovpn user diagnose --server <server> --username alice --since 24h
./ovpn user debug start --server <server> --username alice --duration 15m
./ovpn user debug show --server <server> --username alice --since 15m
```

Look for whether the server sees accepted connections, rejected connections, destination attempts, and source-network changes. If accepted counts rise but traffic stalls, the problem is likely after the profile reaches Xray.

## Operational Notes

- Do not promise that any profile is unblockable. Treat profiles as resilience tools.
- Avoid opening ports you do not use; `ovpn` exposes extra Xray ports only for enabled deployable profiles.
- Use separate users for separate people/devices when you need meaningful diagnostics.
- REALITY failed-auth traffic is handled by its configured target. A local fake website is only technically correct for a separate TLS/WebSocket web-front design.

References:

- Xray REALITY transport: https://xtls.github.io/en/config/transports/reality.html
- Xray fallbacks overview: https://xtls.github.io/en/document/level-1/fallbacks-lv1.html
- Xray releases: https://github.com/XTLS/Xray-core/releases
