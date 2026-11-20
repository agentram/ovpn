#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
repo="$PWD"
xcrun --sdk iphoneos --show-sdk-path >/dev/null
xcrun --sdk iphonesimulator --show-sdk-path >/dev/null
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
export GOBIN="$work"
export PATH="$work:$PATH"
go install golang.org/x/mobile/cmd/gomobile golang.org/x/mobile/cmd/gobind
mkdir -p apps/ios/Frameworks
go mod download github.com/xtls/xray-core
xray_dir="$(go list -m -f '{{.Dir}}' github.com/xtls/xray-core)"
# Work in a temporary module so the iOS patch never changes the source tree
# or the shared Go module cache.
mkdir "$work/source"
cp go.mod go.sum "$work/source/"
ln -s "$repo/mobile" "$repo/internal" "$work/source/"
cp -R "$xray_dir" "$work/xray"
chmod u+w "$work/xray/proxy/tun/tun_darwin.go"
# The pinned Darwin TUN wraps the supplied fd in os.File without owning it.
# Give Xray its own duplicate so Close cancels Go reads and releases it safely.
python3 - "$work/xray/proxy/tun/tun_darwin.go" <<'PY'
from pathlib import Path
import sys

source = Path(sys.argv[1])
raw = source.read_text()
old = """		if err = unix.SetNonblock(fd, true); err != nil {
			return nil, err
		}
"""
new = """		fd, err = unix.FcntlInt(uintptr(fd), unix.F_DUPFD_CLOEXEC, 3)
		if err != nil {
			return nil, err
		}
		if err = unix.SetNonblock(fd, true); err != nil {
			_ = unix.Close(fd)
			return nil, err
		}
"""
ownership = "ownsFd:  false,"
if raw.count(old) != 1 or raw.count(ownership) != 1:
    raise SystemExit("Recheck Darwin descriptor ownership for the pinned Xray version")
source.write_text(raw.replace(old, new).replace(ownership, "ownsFd:  true,"))
PY
cd "$work/source"
export GOWORK=off
go mod edit -replace "github.com/xtls/xray-core=$work/xray"
go list golang.org/x/mobile/bind >/dev/null
gomobile bind -target=ios/arm64,iossimulator/arm64 -iosversion=16.0 -trimpath \
    -ldflags="-s -w -checklinkname=0" -o "$work/MobileCore.xcframework" ./mobile/androidbridge
rm -rf "$repo/apps/ios/Frameworks/MobileCore.xcframework"
mv "$work/MobileCore.xcframework" "$repo/apps/ios/Frameworks"
