#!/usr/bin/env bash
set -euo pipefail

: "${ANDROID_HOME:?Set ANDROID_HOME to your Android SDK}"
apk="${1:?Usage: check-alignment.sh APK}"
ndk="$ANDROID_HOME/ndk/28.2.13676358"
readelf="$(find "$ndk/toolchains/llvm/prebuilt" -path '*/bin/llvm-readelf' -print -quit)"
[[ -x "$readelf" ]]
"$ANDROID_HOME/build-tools/36.0.0/zipalign" -c -P 16 4 "$apk"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
unzip -q "$apk" 'lib/*/*.so' -d "$work"
for abi in arm64-v8a x86_64; do
    test -f "$work/lib/$abi/libgojni.so"
done
for lib in "$work"/lib/*/*.so; do
    "$readelf" -lW "$lib" | python3 -c '
import sys
segments = [line.split() for line in sys.stdin if line.lstrip().startswith("LOAD ")]
if not segments or any(int(line[-1], 0) < 16384 for line in segments):
    sys.exit("Native LOAD segment alignment below 16 KiB: " + sys.argv[1])
print("16 KiB ELF alignment: " + sys.argv[1])
' "${lib#"$work"/}"
done
