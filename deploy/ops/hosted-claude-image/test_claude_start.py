"""Offline launcher fixtures; never invokes Claude, helper, Docker, or real credentials."""
import importlib.util
import json
import os
import hashlib
import io
import tarfile
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
prepare_spec = importlib.util.spec_from_file_location('claude_prepare_fixture', HERE / 'prepare_context.py')
prepare = importlib.util.module_from_spec(prepare_spec)
prepare_spec.loader.exec_module(prepare)


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

    def test_context_fixture_uses_only_verified_inputs(self):
        def package(blob):
            out = io.BytesIO()
            with tarfile.open(fileobj=out, mode='w:gz') as tar:
                info = tarfile.TarInfo('package/file'); info.size = len(blob); tar.addfile(info, io.BytesIO(blob))
            return out.getvalue()
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); cli, native, node = (root/'cli.tgz', root/'native.tgz', root/'node-v22.23.3-linux-x64.tar.xz')
            cli.write_bytes(package(b'cli')); native.write_bytes(package(b'native')); node.write_bytes(b'node')
            def npmrow(p):
                b=p.read_bytes(); import base64
                return {'integrity':'sha512-'+base64.b64encode(hashlib.sha512(b).digest()).decode(),'shasum':hashlib.sha1(b).hexdigest(),'fileCount':1}
            lock={'name':'@anthropic-ai/claude-code','version':'2.1.285','dist':npmrow(cli),
                  'native':dict(name='@anthropic-ai/claude-code-linux-x64',version='2.1.285',**npmrow(native)),
                  'runtime':{'name':'node','version':'22.23.3','platform':'linux-x64','file':node.name,'sha256':hashlib.sha256(node.read_bytes()).hexdigest()}}
            lock_path=root/'lock.json'; lock_path.write_text(json.dumps(lock))
            base=root/'base'; base.mkdir()
            (base/'start.py').write_text('fixture common guard')
            for name in ('helper','wheels','runtime-debs','security-debs'):(base/name).mkdir()
            receipt={'commit':'a'*40,'tree':'b'*40,'files':{'start.py':hashlib.sha256((base/'start.py').read_bytes()).hexdigest()}}
            (base/'BUILD-INPUTS.json').write_text(json.dumps(receipt))
            out=root/'out'; prepare.prepare(lock_path,cli,native,node,base,out)
            self.assertTrue((out/'claude'/'package'/'file').is_file())
            self.assertTrue((out/'native'/'package'/'file').is_file())
            self.assertEqual((out/'node'/node.name).read_bytes(),b'node')

    def test_context_rejects_receipted_symlink_before_copy(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); base = root/'base'; base.mkdir(); outside = root/'synthetic-secret'; outside.write_text('not copied')
            (base/'helper').mkdir(); (base/'helper'/'link').symlink_to(outside)
            receipt = {'commit':'a'*40, 'tree':'b'*40, 'files':{'helper/link':hashlib.sha256(outside.read_bytes()).hexdigest(), 'start.py':'0'*64}}
            (base/'start.py').write_text('guard'); receipt['files']['start.py']=hashlib.sha256((base/'start.py').read_bytes()).hexdigest()
            (base/'BUILD-INPUTS.json').write_text(json.dumps(receipt))
            with self.assertRaises(prepare.Rejected):
                prepare._h3_files(base)

    def test_restricted_mcp_and_json_object_credential(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            cred = root / 'account-credential'
            cred.write_text('{"opaque":"synthetic"}')
            cred.chmod(0o600)
            env = {'SH_WS_BASE': 'wss://hub.invalid', 'SH_ENROLL_TOKEN_FILE': str(root/'enroll'),
                   'SH_CRED_FILE': str(cred), 'SH_CRED_KIND': 'token_file', 'SH_CLI': 'claude',
                   'HOME': str(root/'work')}
            with mock.patch.object(start, '_shared_guard'), mock.patch.object(start, 'SEAT_UID', os.getuid()), \
                 mock.patch.object(start, 'IPC_DIR', str(root/'ipc')):
                out = start.prepare(env)
            self.assertEqual((out['SH_IPC_UPLOAD_ONLY'], out['SH_IPC_SEND_BODY_ONLY']), ('0', '1'))
            self.assertIn('--mcp-config', out['SH_CLI_CMD'])
            self.assertTrue(out['SH_ENROLL'])
            self.assertEqual(out['SH_MCP_IPC_SOCK'], out['DINGWEI_MCP_IPC_SOCK'])
            self.assertEqual(json.loads((root/'work'/'.claude'/'.credentials.json').read_text()), {'opaque': 'synthetic'})
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
