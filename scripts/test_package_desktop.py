"""Regression checks for distributable contents, permissions, identity and hashes."""
import hashlib
import json
from pathlib import Path
import struct
import plistlib
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import zipfile

from package_desktop import package

COMMIT = 'a' * 40


class PackageTests(unittest.TestCase):
    def fixture(self, target='linux'):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        (root / 'VERSION').write_text('7.8.9\n')
        (root / 'LICENSE').write_text('Fixture license\n')
        reader = patch('package_desktop.build_settings', return_value={'vcs.revision': COMMIT, 'vcs.modified': 'false'})
        reader.start()
        self.addCleanup(reader.stop)
        runner = patch('package_desktop.subprocess.check_output', return_value='7.8.9\n')
        runner.start()
        self.addCleanup(runner.stop)
        source = root / 'bin/desktop' / target
        executable = source / 'OVPN Desk.app/Contents/MacOS' if target == 'darwin' else source
        executable.mkdir(parents=True)
        suffix = '.exe' if target == 'windows' else ''
        header = bytearray(70)
        if target == 'linux':
            header[:6] = b'\x7fELF\x02\x01'
            header[18:20] = struct.pack('<H', 62)
        elif target == 'windows':
            header[:2] = b'MZ'
            header[60:64] = struct.pack('<I', 64)
            header[64:70] = b'PE\0\0' + struct.pack('<H', 0x8664)
        else:
            header[:8] = b'\xcf\xfa\xed\xfe' + struct.pack('<I', 0x1000007)
            for relative in ['Info.plist', 'Resources/OVPN.icns', '_CodeSignature/CodeResources']:
                path = source / 'OVPN Desk.app/Contents' / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                if relative == 'Info.plist':
                    path.write_bytes(plistlib.dumps({'CFBundleShortVersionString': '7.8.9', 'CFBundleVersion': '7.8.9'}))
                else:
                    path.write_text('fixture')
        for name in ['ovpn-desk', 'ovpn-worker']:
            (executable / (name + suffix)).write_bytes(header + name.encode())
        record = {'version': '7.8.9', 'sourceCommit': COMMIT, 'os': target, 'arch': 'amd64',
                  'appSHA256': hashlib.sha256((executable / ('ovpn-desk' + suffix)).read_bytes()).hexdigest(),
                  'workerSHA256': hashlib.sha256((executable / ('ovpn-worker' + suffix)).read_bytes()).hexdigest()}
        (source / 'build-desktop.json').write_text(json.dumps(record))
        (executable / 'private-state.json').write_text('fixture-private-marker')
        return root, executable

    def test_linux_archive_preserves_launch_permissions_without_private_inventory(self):
        root, _ = self.fixture()
        name, archive = package(root, 'linux', 'amd64', COMMIT, 1700000000, root / 'packages')
        self.assertIn('7.8.9-linux-amd64', name)
        with tarfile.open(archive) as tar:
            names = tar.getnames()
            self.assertNotIn('ovpn-desk/private-state.json', names)
            for member in tar.getmembers():
                self.assertEqual((member.uid, member.gid, member.uname, member.gname), (0, 0, '', ''))
            for binary in ['ovpn-desk/ovpn-desk', 'ovpn-desk/ovpn-worker']:
                self.assertEqual(tar.getmember(binary).mode, 0o755)
            metadata = json.load(tar.extractfile('BUILD.json'))
            self.assertEqual(metadata['version'], '7.8.9')
            for binary, expected in metadata['files'].items():
                self.assertEqual(hashlib.sha256(tar.extractfile(binary).read()).hexdigest(), expected)
        expected = hashlib.sha256(archive.read_bytes()).hexdigest()
        self.assertEqual((archive.parent / 'SHA256SUMS.txt').read_text(), f'{expected}  {archive.name}\n')

    def test_windows_archive_keeps_both_executables_and_source_identity(self):
        root, _ = self.fixture('windows')
        _, archive = package(root, 'windows', 'amd64', COMMIT, 1700000000, root / 'packages')
        with zipfile.ZipFile(archive) as zipped:
            self.assertIn('ovpn-desk/ovpn-worker.exe', zipped.namelist())
            self.assertNotIn('ovpn-desk/private-state.json', zipped.namelist())
            self.assertEqual(json.loads(zipped.read('BUILD.json'))['sourceCommit'], COMMIT)

    def test_mac_bundle_keeps_signature_resources_and_worker(self):
        root, _ = self.fixture('darwin')
        with patch('package_desktop.subprocess.run'):
            _, archive = package(root, 'darwin', 'amd64', COMMIT, 1700000000, root / 'packages')
        with tarfile.open(archive) as tar:
            self.assertIn('OVPN Desk.app/Contents/_CodeSignature/CodeResources', tar.getnames())
            self.assertNotIn('OVPN Desk.app/Contents/MacOS/private-state.json', tar.getnames())

    def test_missing_worker_wrong_architecture_and_invalid_version_fail_closed(self):
        root, executable = self.fixture()
        with self.assertRaisesRegex(ValueError, 'target'):
            package(root, 'linux', 'arm64', COMMIT, 1700000000, root / 'packages')
        (executable / 'ovpn-worker').unlink()
        with self.assertRaisesRegex(ValueError, 'output'):
            package(root, 'linux', 'amd64', COMMIT, 1700000000, root / 'packages')
        (root / 'VERSION').write_text('../outside')
        with self.assertRaisesRegex(ValueError, 'VERSION'):
            package(root, 'linux', 'amd64', COMMIT, 1700000000, root / 'packages')

    def test_old_version_and_dirty_or_different_source_are_not_mislabeled(self):
        root, _ = self.fixture()
        record_path = root / 'bin/desktop/linux/build-desktop.json'
        record = json.loads(record_path.read_text())
        record['version'] = '1.0.0'
        record_path.write_text(json.dumps(record))
        with self.assertRaisesRegex(ValueError, 'version'):
            package(root, 'linux', 'amd64', COMMIT, 1700000000, root / 'packages')
        record['version'] = '7.8.9'
        record_path.write_text(json.dumps(record))
        for settings in [{'vcs.revision': 'b' * 40, 'vcs.modified': 'false'},
                         {'vcs.revision': COMMIT, 'vcs.modified': 'true'}]:
            with self.subTest(settings=settings), patch('package_desktop.build_settings', return_value=settings):
                with self.assertRaises(ValueError):
                    package(root, 'linux', 'amd64', COMMIT, 1700000000, root / 'packages')
        record['workerSHA256'] = '0' * 64
        record_path.write_text(json.dumps(record))
        with self.assertRaisesRegex(ValueError, 'hash'):
            package(root, 'linux', 'amd64', COMMIT, 1700000000, root / 'packages')

    def test_native_worker_version_must_match_the_build_record(self):
        root, _ = self.fixture()
        with patch('package_desktop.sys.platform', 'linux'), patch('package_desktop.platform.machine', return_value='x86_64'), patch('package_desktop.subprocess.check_output', return_value='1.0.0\n'):
            with self.assertRaisesRegex(ValueError, 'runtime version'):
                package(root, 'linux', 'amd64', COMMIT, 1700000000, root / 'packages')

    def test_generated_runtime_assets_are_ignored_in_a_fresh_checkout(self):
        with tempfile.TemporaryDirectory(prefix='ovpn-build-ignore-') as directory:
            root = Path(directory)
            subprocess.run(['git', 'init', '-q', directory], check=True)
            destination = root / 'internal/runtimeassets'
            destination.mkdir(parents=True)
            ignore = Path(__file__).resolve().parents[1] / 'internal/runtimeassets/.gitignore'
            (destination / '.gitignore').write_text(ignore.read_text())
            generated = destination / 'assets/linux_amd64/ovpn-agent'
            generated.parent.mkdir(parents=True)
            generated.write_bytes(b'fixture generated binary')
            result = subprocess.run(['git', '-C', directory, 'check-ignore', 'internal/runtimeassets/assets/linux_amd64/ovpn-agent'], capture_output=True)
            self.assertEqual(result.returncode, 0)


if __name__ == '__main__':
    unittest.main()
