"""Offline launcher fixtures; never invokes Claude, helper, Docker, or real credentials."""
import importlib.util
import json
import os
import hashlib
from pathlib import Path
import tempfile
import unittest
from unittest import mock

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('claude_start_fixture', HERE / 'claude-start.py')
start = importlib.util.module_from_spec(spec)
spec.loader.exec_module(start)
verify_spec = importlib.util.spec_from_file_location('claude_verify_fixture', HERE / 'verify_inputs.py')
verify = importlib.util.module_from_spec(verify_spec)
verify_spec.loader.exec_module(verify)


class ClaudeStart(unittest.TestCase):
    def test_definition_keeps_combo_unverified_and_builder_fail_closed(self):
        combo = json.loads((HERE / 'combos.json').read_text())
        self.assertEqual(combo, [{'cli': 'claude', 'llm': 'claude', 'cred_kind': 'token_file', 'verified': False}])
        dockerfile = (HERE / 'Dockerfile').read_text()
        for required in ('USER 10001:10001', 'COPY wheels /wheels',
                         'COPY native/package/ /opt/claude/node_modules/@anthropic-ai/claude-code-linux-x64/',
                         'COPY node/node-v22.23.3-linux-x64.tar.xz /tmp/node.tar.xz',
                         'dingwei.claude.version="2.1.285"'):
            self.assertIn(required, dockerfile)
        out = __import__('subprocess').run(['sh', str(HERE / 'build-image.sh')], text=True, capture_output=True)
        self.assertEqual(out.returncode, 2)
        self.assertNotIn('docker', out.stdout.lower())

    def test_package_and_native_metadata_are_pinned(self):
        lock = json.loads((HERE / 'claude-package-lock.json').read_text())
        self.assertEqual((lock['name'], lock['version']), ('@anthropic-ai/claude-code', '2.1.285'))
        self.assertEqual((lock['native']['name'], lock['native']['version'], lock['native']['platform']),
                         ('@anthropic-ai/claude-code-linux-x64', '2.1.285', 'linux-x64'))
        for part in (lock['dist'], lock['native']):
            self.assertTrue(part['integrity'].startswith('sha512-'))
            self.assertEqual(len(part['shasum']), 40)
        self.assertEqual((lock['runtime']['name'], lock['runtime']['version'], lock['runtime']['file']),
                         ('node', '22.23.3', 'node-v22.23.3-linux-x64.tar.xz'))
        self.assertEqual(len(lock['runtime']['sha256']), 64)

    def test_offline_fixture_hashes_and_missing_node_lock_fail_closed(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            blobs = [b'claude fixture', b'native fixture', b'node fixture']
            files = []
            for i, blob in enumerate(blobs):
                p = root / ('node-v22.23.3-linux-x64.tar.xz' if i == 2 else 'input' + str(i)); p.write_bytes(blob); files.append(p)
            def row(blob):
                import base64, hashlib
                return {'integrity': 'sha512-' + base64.b64encode(hashlib.sha512(blob).digest()).decode(),
                        'shasum': hashlib.sha1(blob).hexdigest()}
            lock = {'name': '@anthropic-ai/claude-code', 'version': '2.1.285', 'dist': row(blobs[0]),
                    'native': dict(name='@anthropic-ai/claude-code-linux-x64', version='2.1.285', **row(blobs[1])),
                    'runtime': dict(name='node', version='22.23.3', platform='linux-x64',
                                    file=files[2].name, sha256=hashlib.sha256(blobs[2]).hexdigest())}
            verify.verify(lock, *files)
            del lock['runtime']['sha256']
            with self.assertRaises(verify.Rejected):
                verify.verify(lock, *files)

    def test_restricted_mcp_and_json_object_credential(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            cred = root / 'account-credential'
            cred.write_text('{"opaque":"synthetic"}')
            cred.chmod(0o600)
            env = {'SH_WS_BASE': 'wss://hub.invalid', 'SH_ENROLL_TOKEN_FILE': str(root/'enroll'),
                   'SH_CRED_FILE': str(cred), 'SH_CRED_KIND': 'token_file', 'SH_CLI': 'claude',
                   'HOME': str(root/'work')}
            with mock.patch.object(start, '_shared_guard'), mock.patch.object(start, 'SEAT_UID', os.getuid()):
                out = start.prepare(env)
            self.assertEqual((out['SH_IPC_UPLOAD_ONLY'], out['SH_IPC_SEND_BODY_ONLY']), ('0', '1'))
            self.assertEqual(out['SH_CLI_CMD'], start.CLAUDE_BIN)
            self.assertEqual(json.loads((root/'work'/'.claude.json').read_text()), {'opaque': 'synthetic'})
            mcp = json.loads((root/'work'/'.claude'/'mcp.json').read_text())['mcpServers']['dingwei-send']
            self.assertEqual(mcp['env']['SH_IPC_SEND_BODY_ONLY'], '1')
            self.assertNotIn('file', json.dumps(mcp))

    def test_rejects_non_object_or_wrong_cli(self):
        for raw, cli in (('[]', 'claude'), ('{}', 'codex')):
            with self.subTest(raw=raw, cli=cli), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp); cred = root/'credential'; cred.write_text(raw); cred.chmod(0o600)
                env = {'SH_WS_BASE': 'wss://hub.invalid', 'SH_ENROLL_TOKEN_FILE': str(root/'enroll'),
                       'SH_CRED_FILE': str(cred), 'SH_CRED_KIND': 'token_file', 'SH_CLI': cli, 'HOME': str(root/'work')}
                with mock.patch.object(start, '_shared_guard'), mock.patch.object(start, 'SEAT_UID', os.getuid()), self.assertRaises(SystemExit):
                    start.prepare(env)


if __name__ == '__main__':
    unittest.main()
