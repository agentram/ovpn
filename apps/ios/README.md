# O Tunnel for iOS

O Tunnel uses SwiftUI and a Network Extension built against the repository's pinned Xray core. The extension uses the public packet-flow API and obtains the selected profile from the shared Keychain. The account session and remaining profiles stay in the app's Keychain.

Build with Xcode, the iOS device and simulator SDKs, XcodeGen and the Go version in the root module:

```sh
cd apps/ios
./build-core.sh
xcodegen generate
xcodebuild -project O-Tunnel.xcodeproj -scheme O-Tunnel \
  -destination 'generic/platform=iOS Simulator' ARCHS=arm64 CODE_SIGNING_ALLOWED=NO build
```

The unsigned build is for simulator UI and build verification. For device testing, configure the signing team and provisioning for both targets with Network Extension and the shared Keychain group. Set the issuer, client ID and HTTPS profile service using the `OVPN_ACCOUNT_ISSUER`, `OVPN_ACCOUNT_CLIENT_ID` and `OVPN_ACCOUNT_ORIGIN` build settings. Register `io.github.agentram.ovpn.tunnel:/oauth` with the iOS public OIDC client.

Sign in, paste the administrator-issued code, select a compatible profile, tap **Connect**, and approve the iOS VPN configuration. A signed device acceptance run must verify Safari traffic and its public exit IP, DNS, a network change, and restoration after disconnect. The development client stops the tunnel on an uplink change. Choose **Problem report** to preview and share recent connection results.
