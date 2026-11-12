#!/usr/bin/env python3
"""Archive a Desk build and its matching worker using the repository VERSION."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import platform
import plistlib
import shutil
import struct
import subprocess
import sys
import tarfile
import tempfile
import time
import zipfile

ROOT = Path(__file__).resolve().parents[1]


def executable_target(path):
    with path.open('rb') as source:
        header = source.read(64)
        if header[:4] == b'\x7fELF' and len(header) >= 20:
            machine = struct.unpack('<H' if header[5] == 1 else '>H', header[18:20])[0]
            return 'linux', {62: 'amd64', 183: 'arm64'}.get(machine)
        if header[:4] in (b'\xcf\xfa\xed\xfe', b'\xfe\xed\xfa\xcf'):
            machine = struct.unpack('<I' if header[0] == 0xcf else '>I', header[4:8])[0]
            return 'darwin', {0x1000007: 'amd64', 0x100000c: 'arm64'}.get(machine)
        if header[:2] == b'MZ' and len(header) == 64:
            source.seek(struct.unpack('<I', header[60:64])[0])
            pe = source.read(6)
            if pe[:4] == b'PE\x00\x00':
                return 'windows', {0x8664: 'amd64', 0xaa64: 'arm64'}.get(struct.unpack('<H', pe[4:6])[0])
    raise ValueError('Unrecognized executable format')


def sha256(path):
    with path.open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def build_settings(path):
    raw = subprocess.check_output(['go', 'version', '-m', '-json', str(path)], text=True)
    return {entry['Key']: entry['Value'] for entry in json.loads(raw)['Settings']}


def verify_build(path, commit):
    settings = build_settings(path)
    if settings.get('vcs.revision') != commit or settings.get('vcs.modified') != 'false':
        raise ValueError('Executable source differs from the clean source commit; rebuild before packaging')


def package(root, target, arch, commit, epoch, output):
    version = (root / 'VERSION').read_text().strip()
    if not re.fullmatch(r'\d+\.\d+\.\d+', version):
        raise ValueError('VERSION must be plain semantic version')
    if not re.fullmatch(r'[0-9a-f]{40}|[0-9a-f]{64}', commit):
        raise ValueError('A full source commit is required')
    source = root / 'bin/desktop' / target
    record = json.loads((source / 'build-desktop.json').read_text())
    if record.get('version') != version:
        raise ValueError('Executable version does not match VERSION; rebuild before packaging')
    if any(record.get(key) != expected for key, expected in [('sourceCommit', commit), ('os', target), ('arch', arch)]):
        raise ValueError('Build record does not match the source or target; rebuild before packaging')
    bundle = Path('OVPN Desk.app')
    binary_dir = bundle / 'Contents/MacOS' if target == 'darwin' else Path('ovpn-desk')
    suffix = '.exe' if target == 'windows' else ''
    binaries = [binary_dir / ('ovpn-desk' + suffix), binary_dir / ('ovpn-worker' + suffix)]
    # Copy only build outputs; stray workspace files never enter the archive.
    files = list(binaries)
    if target == 'darwin':
        files += [bundle / 'Contents/Info.plist', bundle / 'Contents/Resources/OVPN.icns',
                  bundle / 'Contents/_CodeSignature/CodeResources']
    source_paths = {relative: source / (relative if target == 'darwin' else relative.name) for relative in files}
    for relative, path in source_paths.items():
        if path.is_symlink() or not path.is_file():
            raise ValueError(f'Missing or unsafe build output: {relative}')
    for relative in binaries:
        if executable_target(source_paths[relative]) != (target, arch):
            raise ValueError('Executable target does not match the archive target')
        verify_build(source_paths[relative], commit)
    if any(record.get(key) != sha256(source_paths[path]) for key, path in zip(["appSHA256", "workerSHA256"], binaries)):
        raise ValueError("Build record hash does not match the executable pair")
    if target == "darwin":
        with source_paths[bundle / "Contents/Info.plist"].open("rb") as source_info:
            info = plistlib.load(source_info)
        if info.get("CFBundleShortVersionString") != version or info.get("CFBundleVersion") != version:
            raise ValueError("Bundle version does not match VERSION")
    metadata = {'version': version, 'sourceCommit': commit, 'os': target, 'arch': arch,
                'developmentBuild': True, 'workerVersionVerified': False,
                'files': {path.as_posix(): sha256(source_paths[path]) for path in binaries}}
    name = f'ovpn-desk-{version}-{target}-{arch}-{commit[:12]}'
    output.mkdir(parents=True, exist_ok=True)
    extension = '.zip' if target == 'windows' else '.tar.gz'
    archive = output / (name + extension)
    with tempfile.TemporaryDirectory(prefix='ovpn-desk-package-') as temporary:
        staging = Path(temporary)
        for relative, path in source_paths.items():
            destination = staging / relative
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(path, destination)
            destination.chmod(0o755 if relative in binaries else 0o644)
        host = {'darwin': 'darwin', 'win32': 'windows'}.get(sys.platform, 'linux')
        host_arch = {'x86_64': 'amd64', 'AMD64': 'amd64', 'aarch64': 'arm64', 'arm64': 'arm64'}.get(platform.machine())
        if (target, arch) == (host, host_arch):
            with tempfile.TemporaryDirectory(prefix='ovpn-desk-version-') as state:
                actual = subprocess.check_output([str(staging / binaries[1]), '--data-dir', state, 'version'], text=True, timeout=15).strip()
            if actual != version:
                raise ValueError('Worker runtime version does not match VERSION')
            metadata['workerVersionVerified'] = True
        shutil.copyfile(root / 'LICENSE', staging / 'LICENSE')
        (staging / 'BUILD.json').write_text(json.dumps(metadata, indent=2) + '\n')
        instructions = {
            'darwin': 'Extract the archive and open OVPN Desk.app. This development bundle is ad-hoc signed and not notarized.',
            'windows': 'Extract all files and open ovpn-desk/ovpn-desk.exe. Keep ovpn-worker.exe beside it. Microsoft WebView2 Runtime is required.',
            'linux': 'Extract all files and run ./ovpn-desk/ovpn-desk. GTK4 and WebKitGTK 6 are required (Ubuntu 24.04: libgtk-4-1 and libwebkitgtk-6.0-4).',
        }
        (staging / 'README.txt').write_text(f'OVPN Desk {version}\n\n{instructions[target]}\n\nThe matching worker and server-side runtime binaries are bundled.\nVersion follows the repository VERSION; source and binary hashes are in BUILD.json.\nhttps://github.com/agentram/ovpn/blob/main/docs/desktop.md\n')
        if target == 'darwin' and sys.platform == 'darwin':
            subprocess.run(['codesign', '--verify', '--deep', '--strict', str(staging / bundle)], check=True)
        entries = sorted(staging.rglob('*'))
        if target == 'windows':
            date = time.gmtime(max(epoch, 315532800))[:6]
            with zipfile.ZipFile(archive, 'w', compression=zipfile.ZIP_DEFLATED) as zipped:
                for entry in entries:
                    if not entry.is_file():
                        continue
                    item = zipfile.ZipInfo(entry.relative_to(staging).as_posix(), date_time=date)
                    item.create_system = 3
                    item.external_attr = (0o100755 if entry.relative_to(staging) in binaries else 0o100644) << 16
                    item.compress_type = zipfile.ZIP_DEFLATED
                    zipped.writestr(item, entry.read_bytes())
        else:
            with tarfile.open(archive, 'w:gz', format=tarfile.PAX_FORMAT) as tar:
                for entry in entries:
                    item = tar.gettarinfo(str(entry), arcname=entry.relative_to(staging).as_posix())
                    item.uid = item.gid = 0
                    item.uname = item.gname = ''
                    item.mtime = epoch
                    item.mode = 0o755 if entry.is_dir() or entry.relative_to(staging) in binaries else 0o644
                    if entry.is_file():
                        with entry.open('rb') as contents:
                            tar.addfile(item, contents)
                    else:
                        tar.addfile(item)
    (output / 'BUILD.json').write_text(json.dumps(metadata, indent=2) + '\n')
    (output / 'SHA256SUMS.txt').write_text(f'{sha256(archive)}  {archive.name}\n')
    return name, archive


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--target', required=True, choices=['darwin', 'linux', 'windows'])
    parser.add_argument('--arch', required=True, choices=['amd64', 'arm64'])
    parser.add_argument('--github-output', type=Path)
    args = parser.parse_args()
    commit = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    epoch = int(subprocess.check_output(['git', 'show', '-s', '--format=%ct', 'HEAD'], cwd=ROOT, text=True))
    directory = Path('bin/desktop/artifacts') / args.target / args.arch / commit[:12]
    name, archive = package(ROOT, args.target, args.arch, commit, epoch, ROOT / directory)
    if args.github_output:
        with args.github_output.open('a') as output:
            output.write(f'name={name}\ndirectory={directory.as_posix()}\n')
    print(f'Packaged {archive.relative_to(ROOT).as_posix()}')


if __name__ == '__main__':
    main()
