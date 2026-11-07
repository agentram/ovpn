# OVPN Tunnel

OVPN Tunnel is an Android client for an existing OVPN installation. Import a QR code, a VLESS link or an HTTPS subscription, save a compatible profile, and choose **Connect**. Approve Android's VPN request on first connection. The app supports TCP/TLS self-SNI Vision and TCP/REALITY Vision. Use OVPN Desk or the CLI to administer servers and users.

On the operator machine, get a link or QR for an existing user:

```sh
ovpn user link --server <server> --username <user>
ovpn user qr --server <server> --username <user>
```

Use the private [subscription URL](../../docs/subscriptions.md) for multiple profiles. A failed refresh retains the saved snapshot. If a successful refresh changes or removes the selected profile, choose a profile again before connecting. The subscription URL and server-side access can be revoked separately.

## Build and install

Install JDK 17, the Go version in the repository's `go.mod`, and Android SDK packages `platforms;android-36`, `build-tools;36.0.0`, `platform-tools` and `ndk;28.2.13676358`. Set `JAVA_HOME` and `ANDROID_HOME`.

```sh
cd apps/android
./build-core.sh
./gradlew :app:assembleDebug :app:testDebugUnitTest :app:lintDebug :app:bundleRelease
./check-alignment.sh app/build/outputs/apk/debug/app-debug.apk
"$ANDROID_HOME/platform-tools/adb" install -r app/build/outputs/apk/debug/app-debug.apk
```

The app requires Android 10 or later. The build targets API 36 and includes ARM64 and x86-64. The Go bridge uses the Xray revision pinned in the root module. App version, embedded Xray version and privacy information are in **Settings**.

## Tunnel behavior

Android routes IPv4 and IPv6 into the service's TUN. The only native outbound sockets outside that TUN are protected TCP connections to the resolved VPN endpoint on the selected physical network. Android resolves the endpoint hostname on that network. The VPN uses `1.1.1.1` for user DNS through the tunnel, and checks HTTPS against `www.gstatic.com/generate_204` on the VPN network.

After a network change the service retains the TUN while it restarts Xray on the same profile. Disconnect or revoke VPN permission to release the TUN. A failed HTTPS check can indicate an unreachable check endpoint, tunnel failure or revoked server access. The app reports an unverified tunnel when Xray is running and the HTTPS check fails.

See [release preparation](../../docs/android-release.md) and [privacy policy](../../docs/android-privacy.html).
