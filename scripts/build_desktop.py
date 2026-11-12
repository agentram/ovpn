#!/usr/bin/env python3
"""Build the development desktop and its matching, verified CLI worker."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import plistlib
import shutil
import subprocess
import sys

root = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
parser.add_argument('--target', choices=['darwin', 'linux', 'windows'], default={'darwin':'darwin','win32':'windows'}.get(sys.platform,'linux'))
parser.add_argument('--arch', choices=['arm64','amd64'], default=None)
parser.add_argument('--skip-frontend', action='store_true')
args = parser.parse_args()
version = (root/'VERSION').read_text().strip()
env = os.environ.copy()
env['GOOS'] = args.target
if args.arch:
    env['GOARCH'] = args.arch
if args.target == 'darwin':
    env['MACOSX_DEPLOYMENT_TARGET'] = '13.0'
    env['CGO_CFLAGS'] = env.get('CGO_CFLAGS','')+' -mmacosx-version-min=13.0'
    env['CGO_CXXFLAGS'] = env.get('CGO_CXXFLAGS','')+' -mmacosx-version-min=13.0'
    env['CGO_LDFLAGS'] = env.get('CGO_LDFLAGS','')+' -mmacosx-version-min=13.0'
if args.target == 'windows':
    env['CGO_ENABLED'] = '0'

def run(command, cwd=root, **kwargs):
    subprocess.run(command, cwd=cwd, check=True, **kwargs)

if not args.skip_frontend:
    run(['npm','ci','--ignore-scripts','--no-fund','--no-audit'],cwd=root/'apps/desktop/frontend')
    run(['npm','run','build'],cwd=root/'apps/desktop/frontend')

out = root/'bin/desktop'/args.target
bundle = out/'OVPN Desk.app'
exedir = bundle/'Contents/MacOS' if args.target == 'darwin' else out
exedir.mkdir(parents=True,exist_ok=True)
suffix = '.exe' if args.target == 'windows' else ''
worker = exedir/('ovpn-worker'+suffix)
app = exedir/('ovpn-desk'+suffix)
pin = '-s -w -X ovpn/internal/version.pinnedVersion='+version
if args.target == 'darwin':
    pin += ' -extldflags=-mmacosx-version-min=13.0'
# Build matching server-side binaries; the packaged Desk never requires Go sources.
for server_arch in ['amd64', 'arm64']:
    asset_dir = root/'internal/runtimeassets/assets'/('linux_'+server_arch)
    asset_dir.mkdir(parents=True, exist_ok=True)
    server_env = dict(os.environ, GOOS='linux', GOARCH=server_arch, CGO_ENABLED='0')
    for component in ['ovpn-agent', 'ovpn-telegram-bot']:
        run(['go','build','-trimpath','-ldflags','-s -w -X ovpn/internal/version.pinnedVersion='+version,
             '-o',str(asset_dir/component),'./cmd/'+component],env=server_env)
run(['go','build','-trimpath','-tags','runtimeassets','-ldflags',pin,'-o',str(worker),'./cmd/ovpn'],env=env)
if args.target == 'darwin':
    if sys.platform != 'darwin':
        raise SystemExit('Build the macOS desktop on a native macOS host.')
    run(['codesign','--force','--sign','-',str(worker)])
digest = hashlib.sha256(worker.read_bytes()).hexdigest()
run(['go','build','-trimpath','-tags','production','-ldflags',pin+' -X main.workerSHA256='+digest,'-o',str(app),'.'],cwd=root/'apps/desktop',env=env)
if args.target == 'darwin':
    resources=bundle/'Contents/Resources'
    resources.mkdir(parents=True,exist_ok=True)
    shutil.copy2(root/'apps/desktop/assets/icon.icns',resources/'OVPN.icns')
    info = {'CFBundleIconFile':'OVPN.icns','CFBundleName':'OVPN Desk','CFBundleDisplayName':'OVPN Desk','CFBundleIdentifier':'io.github.agentram.ovpn.desk','CFBundleExecutable':'ovpn-desk','CFBundlePackageType':'APPL','CFBundleShortVersionString':version,'CFBundleVersion':version,'NSHighResolutionCapable':True,'LSMinimumSystemVersion':'13.0'}
    with (bundle/'Contents/Info.plist').open('wb') as f:
        plistlib.dump(info,f)
    run(['codesign','--force','--sign','-',str(bundle)])
    run(['codesign','--verify','--deep','--strict',str(bundle)])
    print('Development app: bin/desktop/darwin/OVPN Desk.app (ad-hoc signed; not notarized)')
else:
    print('Development binaries: bin/desktop/'+args.target+' (native runtime and packaging still require validation)')

# The record binds the product version to the final signed binary pair.
record = {
    'version': version,
    'sourceCommit': subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),
    'os': args.target,
    'arch': subprocess.check_output(['go','env','GOARCH'],cwd=root,env=env,text=True).strip(),
    'appSHA256': hashlib.sha256(app.read_bytes()).hexdigest(),
    'workerSHA256': hashlib.sha256(worker.read_bytes()).hexdigest(),
}
(out/'build-desktop.json').write_text(json.dumps(record,indent=2)+'\n')
