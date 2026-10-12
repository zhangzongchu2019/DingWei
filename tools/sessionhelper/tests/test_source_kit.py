import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


REPO = Path(__file__).resolve().parents[3]
spec = importlib.util.spec_from_file_location('source_kit_verifier', REPO / 'scripts/verify-sessionhelper-source.py')
verifier = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verifier)


class SourceKitTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name) / 'source'
        self.root.mkdir()
        for name in verifier.REQUIRED:
            p = self.root / name
            p.parent.mkdir(parents=True, exist_ok=True)
            p.write_text('0.2.2.2\n' if name == 'VERSION' else 'synthetic build member\n')
        (self.root / 'START-HERE.txt').write_text('synthetic development kit\n')
        self.manifest = {'source_commit': 'a' * 40, 'version': '0.2.2.2', 'files': []}
        for name in sorted(verifier.REQUIRED):
            b = (self.root / name).read_bytes()
            self.manifest['files'].append({'path': name, 'bytes': len(b), 'sha256': hashlib.sha256(b).hexdigest()})
        self.write_manifest()

    def tearDown(self):
        self.tmp.cleanup()

    def write_manifest(self):
        (self.root / 'SOURCE-MEMBER-PINS.json').write_text(json.dumps(self.manifest))

    def test_exact_kit_passes_without_authenticity_or_release_claim(self):
        before = {p.relative_to(self.root).as_posix(): p.read_bytes() for p in self.root.rglob('*') if p.is_file()}
        r = verifier.verify_source(self.root)
        self.assertEqual(r['source_members'], len(verifier.REQUIRED))
        self.assertFalse(r['archive_authenticity_verified'])
        self.assertFalse(r['release_ready'])
        self.assertEqual(before, {p.relative_to(self.root).as_posix(): p.read_bytes() for p in self.root.rglob('*') if p.is_file()})

    def test_extra_code_and_stale_pycache_are_refused(self):
        for name in ('tools/sessionhelper/sessionhelper/unlisted.py', '__pycache__/stale.pyc'):
            with self.subTest(name=name):
                p = self.root / name
                p.parent.mkdir(parents=True, exist_ok=True)
                p.write_bytes(b'synthetic unexpected file')
                with self.assertRaisesRegex(verifier.SourceKitError, 'unlisted_source_member'):
                    verifier.verify_source(self.root)
                self.assertEqual(p.read_bytes(), b'synthetic unexpected file')
                p.unlink()

    def test_listed_missing_tampered_and_required_omission_refused(self):
        p = self.root / 'tools/sessionhelper/sessionhelper/setup.py'
        old = p.read_bytes()
        p.unlink()
        with self.assertRaisesRegex(verifier.SourceKitError, 'listed_source_member_missing'):
            verifier.verify_source(self.root)
        p.write_bytes(b'changed')
        with self.assertRaisesRegex(verifier.SourceKitError, 'pin_mismatch'):
            verifier.verify_source(self.root)
        p.write_bytes(old)
        self.manifest['files'] = [i for i in self.manifest['files'] if i['path'] != 'tools/sessionhelper/sessionhelper/setup.py']
        self.write_manifest()
        with self.assertRaisesRegex(verifier.SourceKitError, 'required_build_member_missing'):
            verifier.verify_source(self.root)

    def test_unsafe_and_duplicate_paths_do_not_escape_source(self):
        for name in ('../outside', '/absolute', 'VERSION/', './VERSION', 'tools\\escape.py', 'VERSION'):
            with self.subTest(name=name):
                self.manifest['files'].append({'path': name, 'bytes': 0, 'sha256': '0' * 64})
                self.write_manifest()
                with self.assertRaises(verifier.SourceKitError):
                    verifier.verify_source(self.root)
                self.manifest['files'].pop()

    def test_duplicate_json_keys_and_noninteger_size_refused(self):
        p = self.root / 'SOURCE-MEMBER-PINS.json'
        p.write_text('{"files":[],"files":[]}')
        with self.assertRaisesRegex(verifier.SourceKitError, 'duplicate_manifest_key'):
            verifier.verify_source(self.root)
        self.manifest['files'][0]['bytes'] = True
        self.write_manifest()
        with self.assertRaisesRegex(verifier.SourceKitError, 'invalid_source_pin'):
            verifier.verify_source(self.root)

    def test_file_and_directory_links_and_fifo_refused_without_read(self):
        outside = Path(self.tmp.name) / 'outside'
        outside.write_text('preserved')
        p = self.root / 'unlisted'
        for make in (lambda: p.symlink_to(outside), lambda: p.symlink_to(self.root / 'tools'), lambda: os.mkfifo(p)):
            make()
            with self.assertRaisesRegex(verifier.SourceKitError, 'nonregular_or_linked'):
                verifier.verify_source(self.root)
            p.unlink()
        self.assertEqual(outside.read_text(), 'preserved')

    def test_version_disagreement_and_missing_wrapper_refused(self):
        self.manifest['version'] = '0.2.1.105'
        self.write_manifest()
        with self.assertRaisesRegex(verifier.SourceKitError, 'source_version_mismatch'):
            verifier.verify_source(self.root)
        self.manifest['version'] = '0.2.2.2'
        self.write_manifest()
        (self.root / 'START-HERE.txt').unlink()
        with self.assertRaisesRegex(verifier.SourceKitError, 'missing_wrapper'):
            verifier.verify_source(self.root)

    def test_real_cli_refuses_extra_member_without_printing_its_body(self):
        (self.root / 'unlisted.py').write_text('PRIVATE_SYNTHETIC_VALUE_DO_NOT_PRINT')
        r = subprocess.run([sys.executable, '-B', str(REPO / 'scripts/verify-sessionhelper-source.py'), str(self.root)], capture_output=True, text=True, timeout=5)
        self.assertNotEqual(r.returncode, 0)
        self.assertNotIn('PRIVATE_SYNTHETIC_VALUE', r.stdout + r.stderr)
        self.assertNotIn('unlisted.py', r.stdout + r.stderr)


if __name__ == '__main__':
    unittest.main()
