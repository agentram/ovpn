#!/usr/bin/env bash
set -euo pipefail

android_dir="$(cd -- "$(dirname -- "$0")" && pwd)"
cd "$android_dir/../.."
: "${ANDROID_HOME:?Set ANDROID_HOME to your Android SDK}"
: "${JAVA_HOME:?Set JAVA_HOME to a JDK 17 installation}"
export ANDROID_NDK_HOME="$ANDROID_HOME/ndk/28.2.13676358"
if [[ ! -f "$ANDROID_NDK_HOME/source.properties" ]]; then
    echo "Install Android NDK 28.2.13676358" >&2
    exit 1
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
export GOBIN="$work"
export PATH="$work:$JAVA_HOME/bin:$PATH"
go install golang.org/x/mobile/cmd/gomobile golang.org/x/mobile/cmd/gobind
# Match upstream Xray Android builds for the pinned anet dependency.
gomobile bind -target=android/arm64,android/amd64 -androidapi 29 \
    -javapkg io.github.agentram.ovpn -trimpath -ldflags="-s -w -checklinkname=0" \
    -o "$work/androidbridge.aar" ./mobile/androidbridge
xray_dir="$(go list -m -f '{{.Dir}}' github.com/xtls/xray-core)"
mkdir -p "$work/assets/licenses" "$android_dir/app/libs"
cp LICENSE "$work/assets/licenses/OVPN.txt"
cp "$xray_dir/LICENSE" "$work/assets/licenses/Xray-core.txt"
go list -m -f '{{.Path}} {{.Version}}' github.com/xtls/xray-core > "$work/assets/licenses/Xray-core-version.txt"
"$JAVA_HOME/bin/jar" uf "$work/androidbridge.aar" -C "$work" assets
mv "$work/androidbridge.aar" "$android_dir/app/libs/androidbridge.aar"
