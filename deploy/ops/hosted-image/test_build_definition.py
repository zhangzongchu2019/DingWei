"""Offline source/context/startup fixtures. Never invokes Docker, pip or helper."""
import base64
import contextlib
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import socket
import tarfile
import tempfile
import tomllib
import unittest
from unittest import mock
import zipfile

HERE = Path(__file__).resolve().parent

def load(name):
    spec = importlib.util.spec_from_file_location('fixture_'+name, HERE/(name+'.py'))
    mod = importlib.util.module_from_spec(spec); spec.loader.exec_module(mod)
    return mod

pc = load('prepare_context')
start = load('start')


def wheel(name='websockets', version='15.0.1'):
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, 'w') as z:
        z.writestr(name+'-'+version+'.dist-info/METADATA', 'Name: '+name+'\nVersion: '+version+'\n')
    return buf.getvalue()


def native(members):
    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode='w:gz') as tar:
        for name, kind in members:
            info = tarfile.TarInfo(name); info.mode = 0o755
            if kind == 'link':
                info.type = tarfile.SYMTYPE; info.linkname = '/private/fixture'
                tar.addfile(info)
            else:
                data = b'synthetic native fixture, never execute'; info.size = len(data)
                tar.addfile(info, io.BytesIO(data))
    blob = buf.getvalue()
    return blob, dict(dist=dict(integrity='sha512-'+base64.b64encode(hashlib.sha512(blob).digest()).decode(), fileCount=len(members)))


class ContextFixtures(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(); self.root = Path(self.tmp.name)
        self.addCleanup(self.tmp.cleanup)
        self.wheels = self.root/'wheels'; self.wheels.mkdir()
        self.blob = wheel(); (self.wheels/'websockets-15.0.1-py3-none-any.whl').write_bytes(self.blob)
        self.lock = ('websockets==15.0.1 --hash=sha256:'+hashlib.sha256(self.blob).hexdigest()+'\n').encode()

    def test_locked_wheel_matches(self):
        self.assertEqual(list(pc.checked_wheels(self.wheels, self.lock)), ['websockets-15.0.1-py3-none-any.whl'])

    def test_wrong_hash_or_version_or_missing_pin_rejected(self):
        for lock in (self.lock.replace(b'15.0.1', b'17.0.1'), self.lock.replace(b'sha256:', b'sha256:00'), b'websockets==15.0.1\n'):
            with self.subTest(lock=lock), self.assertRaises(pc.Rejected): pc.checked_wheels(self.wheels, lock)
        (self.wheels/'websockets-15.0.1-py3-none-any.whl').unlink()
        with self.assertRaises(pc.Rejected): pc.checked_wheels(self.wheels, self.lock)

    def test_extra_file_duplicate_distribution_symlink_rejected(self):
        for name, data in (('.env', b'fixture-do-not-package'), ('duplicate.whl', self.blob)):
            p = self.wheels/name; p.write_bytes(data)
            with self.assertRaises(pc.Rejected): pc.checked_wheels(self.wheels, self.lock)
            p.unlink()
        target = self.root/'fixture.whl'; target.write_bytes(self.blob)
        (self.wheels/'link.whl').symlink_to(target)
        with self.assertRaises(pc.Rejected): pc.checked_wheels(self.wheels, self.lock)

    def test_exact_deb_set_and_hash(self):
        d = self.root/'debs'; d.mkdir(); (d/'fixture.deb').write_bytes(b'synthetic')
        rows = [dict(file='fixture.deb', sha256=hashlib.sha256(b'synthetic').hexdigest())]
        self.assertEqual(pc.checked_debs(d, rows), {'fixture.deb': b'synthetic'})
        (d/'fixture.deb').write_bytes(b'corrupt')
        with self.assertRaises(pc.Rejected): pc.checked_debs(d, rows)
        (d/'extra.deb').write_bytes(b'extra')
        with self.assertRaises(pc.Rejected): pc.checked_debs(d, rows)

    def test_native_integrity_and_file_count(self):
        blob, lock = native([('package/bin/codex', 'file')]); p = self.root/'native.tgz'; p.write_bytes(blob)
        self.assertEqual(set(pc.checked_native(p, lock)), {'package/bin/codex'})
        lock['dist']['fileCount'] = 2
        with self.assertRaises(pc.Rejected): pc.checked_native(p, lock)
        p.write_bytes(b'changed')
        with self.assertRaises(pc.Rejected): pc.checked_native(p, lock)

    def test_native_traversal_links_and_duplicate_rejected(self):
        for entries in ([('../private', 'file')], [('/absolute', 'file')], [('package/./alias', 'file')], [('package/link', 'link')],
                        [('package/bin/codex', 'file'), ('package/bin/codex', 'file')]):
            blob, lock = native(entries); p = self.root/'native.tgz'; p.write_bytes(blob)
            with self.subTest(entries=entries), self.assertRaises(pc.Rejected): pc.checked_native(p, lock)

    def test_dirty_source_or_wrong_commit_stops_before_inputs(self):
        commit = 'a'*40
        with mock.patch.object(pc, 'git', side_effect=[(commit+'\n').encode(), b' M VERSION']), mock.patch.object(pc, 'source_kit') as source:
            with self.assertRaises(pc.Rejected): pc.prepare(self.root, commit, self.wheels, self.root/'native', self.root, self.root, self.root/'out')
            source.assert_not_called()
        with mock.patch.object(pc, 'git', return_value=b'b'*40):
            with self.assertRaises(pc.Rejected): pc.prepare(self.root, commit, self.wheels, self.root/'native', self.root, self.root, self.root/'out')

    def test_context_allowlist_assembly_no_source_directory_copy(self):
        # Explicitly synthetic orchestration: input format validators above run independently.
        commit = 'a'*40
        rows = json.dumps([dict(file='fixture.deb', sha256='f'*64)]).encode()
        def git(repo, *args):
            if args == ('rev-parse', 'HEAD'): return commit.encode()
            if args == ('status', '--porcelain'): return b''
            if args[0] == 'rev-parse': return b'b'*40
            if args[-1].endswith('requirements-runtime.lock'): return self.lock
            if args[-1].endswith('-lock.json'): return rows
            return b'synthetic checked source'
        with mock.patch.object(pc, 'git', side_effect=git), mock.patch.object(pc, 'LOCK_SHA', hashlib.sha256(self.lock).hexdigest()), \
             mock.patch.object(pc, 'source_kit', return_value={'factory/sessionhelper/__init__.py': b'fixture'}), \
             mock.patch.object(pc, 'checked_debs', return_value={'fixture.deb': b'fixture'}), \
             mock.patch.object(pc, 'checked_native', return_value={'package/bin/codex': (b'fixture', 0o755)}):
            out = self.root/'out'; pc.prepare(self.root, commit, self.wheels, self.root/'native', self.root, self.root, out)
        files = {str(p.relative_to(out)) for p in out.rglob('*') if p.is_file()}
        self.assertIn('helper/factory/sessionhelper/__init__.py', files)
        self.assertIn('security-debs/fixture.deb', files)
        self.assertNotIn('.env', files)
        self.assertNotIn('auth.json', files)
        self.assertEqual(json.loads((out/'BUILD-INPUTS.json').read_text())['commit'], commit)


class StartupAndDefinition(unittest.TestCase):
    def test_mcp_and_helper_share_restricted_root_and_state(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp); cred = root/'account'; cred.write_text('{"tokens":{"access_token":"synthetic"}}'); cred.chmod(0o600)
            home = root/'.codex'
            env = dict(SH_WS_BASE='wss://fixture.invalid', SH_ENROLL_TOKEN_FILE=str(root/'enroll'),
                       SH_CRED_FILE=str(cred), SH_CRED_KIND='token_file', CODEX_HOME=str(home),
                       SH_IPC_UPLOAD_ONLY='1', SH_IPC_SEND_BODY_ONLY='0', PYTHONPATH='/wrong', SH_MODE='driver')
            with mock.patch.object(start, 'SEAT_UID', os.getuid()), mock.patch.object(start, 'IPC_DIR', str(root/'ipc')), mock.patch.object(start, 'STATE_DIR', str(root/'state')):
                result = start.prepare(env)
                cfg = tomllib.loads((home/'config.toml').read_text())
                mcp = cfg['mcp_servers']['dingwei-send']
                self.assertEqual(mcp['command'], '/opt/dingwei/sessionhelper-venv/bin/python')
                self.assertEqual(mcp['args'], ['-B', '-m', 'sessionhelper.mcp_shim'])
                self.assertEqual(mcp['env']['PYTHONPATH'], '/opt/dingwei/sessionhelper-src')
                self.assertEqual(mcp['env']['SH_STATE_DIR'], '/work/.dingwei')
                self.assertEqual(mcp['env']['SH_IPC_SEND_BODY_ONLY'], '1')
                self.assertEqual(mcp['env']['DINGWEI_MCP_IPC_SOCK'], result['SH_MCP_IPC_SOCK'])
                self.assertEqual((result['SH_IPC_UPLOAD_ONLY'], result['SH_IPC_SEND_BODY_ONLY']), ('0', '1'))
                self.assertEqual(result['SH_MODE'], 'driver')
                self.assertEqual(result['PYTHONPATH'], mcp['env']['PYTHONPATH'])
                auth = (home/'auth.json').read_bytes()
                update = root/'state'/'update'; update.mkdir(parents=True)
                lock = update/'state.lock'; lock.touch(mode=0o600)  # inert normal guard residue
                start.prepare(env)
                self.assertEqual((home/'auth.json').read_bytes(), auth)
                self.assertEqual(tomllib.loads((home/'config.toml').read_text())['mcp_servers'], cfg['mcp_servers'])

    def test_existing_update_state_stops_before_credentials_without_read_or_reset(self):
        cases = ('current032', 'pending', 'previous', 'null', 'malformed', 'slot', 'symlink', 'fifo')
        for case in cases:
            with self.subTest(case=case), tempfile.TemporaryDirectory() as tmp:
                state = Path(tmp)/'state'; update = state/'update'; update.mkdir(parents=True)
                journal = update/'journal.json'
                if case == 'slot':
                    (update/'slots'/'synthetic-old-slot').mkdir(parents=True)
                elif case == 'symlink':
                    journal.symlink_to(Path(tmp)/'private-do-not-read')
                elif case == 'fifo':
                    os.mkfifo(journal)
                else:
                    data = {'format': 1, 'current': None}
                    if case == 'current032': data['current'] = {'sha256': 'a'*64, 'version': '0.2.3.2'}
                    if case in ('pending', 'previous'): data[case] = {'sha256': 'b'*64}
                    journal.write_text('broken fixture' if case == 'malformed' else json.dumps(data))
                before = sorted(str(p.relative_to(state)) for p in state.rglob('*'))
                diagnostic = io.StringIO()
                with contextlib.redirect_stderr(diagnostic), mock.patch.object(start, 'STATE_DIR', str(state)), \
                     mock.patch.object(Path, 'read_bytes', side_effect=AssertionError('must not read credentials or journal')), \
                     mock.patch.object(start.subprocess, 'run', side_effect=AssertionError('must not launch')):
                    with self.assertRaises(SystemExit) as raised:
                        start.prepare({})
                    self.assertEqual(raised.exception.code, 2)
                self.assertEqual(diagnostic.getvalue(), 'hosted-start: invalid or missing HELPER_UPDATE_STATE\n')
                self.assertEqual(sorted(str(p.relative_to(state)) for p in state.rglob('*')), before)

    def test_factory_only_state_accepts_absent_empty_rejects_redirected_state(self):
        with tempfile.TemporaryDirectory() as tmp:
            state = Path(tmp)/'state'
            with mock.patch.object(start, 'STATE_DIR', str(state)):
                start.check_factory_only_state()
                (state/'update').mkdir(parents=True)
                start.check_factory_only_state()
                (state/'update').rmdir()
                (state/'update').symlink_to(Path(tmp)/'elsewhere')
                with self.assertRaises(SystemExit): start.check_factory_only_state()
            linked = Path(tmp)/'linked'; linked.symlink_to(state)
            with mock.patch.object(start, 'STATE_DIR', str(linked)), self.assertRaises(SystemExit):
                start.check_factory_only_state()

    def test_lock_only_restart_metadata_boundary(self):
        with tempfile.TemporaryDirectory() as tmp:
            state = Path(tmp)/'state'; update = state/'update'; update.mkdir(parents=True)
            lock = update/'state.lock'; lock.touch(mode=0o600)
            with mock.patch.object(start, 'STATE_DIR', str(state)), mock.patch.object(start, 'SEAT_UID', os.getuid()):
                start.check_factory_only_state()
                start.check_factory_only_state()  # restart check is repeatable and nonmutating
                self.assertEqual(lock.stat().st_size, 0)
                for kind in ('content', 'permissions', 'symlink', 'fifo', 'directory', 'hardlink', 'journal'):
                    with self.subTest(kind=kind):
                        if kind == 'content': lock.write_text('not a normal lock')
                        elif kind == 'permissions': lock.chmod(0o644)
                        elif kind == 'symlink': lock.unlink(); lock.symlink_to(state/'outside')
                        elif kind == 'fifo': lock.unlink(); os.mkfifo(lock)
                        elif kind == 'directory': lock.unlink(); lock.mkdir()
                        elif kind == 'hardlink': os.link(lock, state/'alias')
                        elif kind == 'journal': (update/'journal.json').write_text('{}')
                        with self.assertRaises(SystemExit): start.check_factory_only_state()
                        if kind == 'directory': lock.rmdir()
                        else: lock.unlink()
                        (update/'journal.json').unlink(missing_ok=True)
                        (state/'alias').unlink(missing_ok=True)
                        lock.touch(mode=0o600)
                with mock.patch.object(start, 'SEAT_UID', os.getuid()+1), self.assertRaises(SystemExit):
                    start.check_factory_only_state()
                self.assertEqual(sorted(p.name for p in update.iterdir()), ['state.lock'])

    def test_runtime_metadata_exact_lock_no_build_distributions(self):
        from types import SimpleNamespace
        runtime = load('verify_runtime')
        with tempfile.TemporaryDirectory() as tmp:
            lock = Path(tmp)/'requirements.lock'; lock.write_text('websockets==15.0.1 --hash=sha256:fixture\n')
            dist = SimpleNamespace(metadata={'Name':'websockets'}, version='15.0.1')
            with mock.patch.object(runtime.importlib.metadata, 'distributions', return_value=[dist]):
                runtime.verify(lock)
            extra = SimpleNamespace(metadata={'Name':'pip'}, version='24.0')
            for actual in ([], [dist, extra], [SimpleNamespace(metadata={'Name':'websockets'}, version='17.0.1')]):
                with mock.patch.object(runtime.importlib.metadata, 'distributions', return_value=actual), self.assertRaises(SystemExit):
                    runtime.verify(lock)

    def test_shell_missing_variables_and_default_engine_refuse_without_docker(self):
        env = {'PATH': os.defpath}
        first = subprocess.run(['sh', str(HERE/'build-image.sh')], env=env, capture_output=True)
        self.assertNotEqual(first.returncode, 0)
        env.update(SOURCE_COMMIT='a'*40, HOSTED_BUILD_CONTEXT='/tmp/never-created-hosted-context',
                   HOSTED_BUILD_ENGINE='unix:///var/run/docker.sock', HOSTED_IMAGE_TAG='fixture')
        second = subprocess.run(['sh', str(HERE/'build-image.sh')], env=env, capture_output=True)
        self.assertEqual(second.returncode, 2)
        self.assertIn(b'default host Engine forbidden', second.stderr)
        with tempfile.TemporaryDirectory() as tmp:
            env['HOSTED_BUILD_ENGINE'] = 'unix:///run/docker.sock'
            direct = subprocess.run(['sh', str(HERE/'build-image.sh')], env=env, capture_output=True)
            self.assertNotEqual(direct.returncode, 0)
            self.assertIn(b'default host Engine alias forbidden', direct.stderr)
            alias = Path(tmp)/'engine.sock'; alias.symlink_to('/var/run/docker.sock')
            env['HOSTED_BUILD_ENGINE'] = 'unix://' + str(alias)
            third = subprocess.run(['sh', str(HERE/'build-image.sh')], env=env, capture_output=True)
            self.assertNotEqual(third.returncode, 0)
            self.assertIn(b'default host Engine alias forbidden', third.stderr)


    def test_engine_existing_socket_and_approved_identity_preflight(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); endpoint = root/'engine.sock'; calls = root/'calls'
            fake = root/'docker'
            fake.write_text('#!/bin/sh\necho "$*" >> "$FIXTURE_CALLS"\nprintf "%s\\n" "$FIXTURE_ENGINE_ID"\n')
            fake.chmod(0o755)
            env = dict(PATH=str(root)+':'+os.defpath, SOURCE_COMMIT='a'*40,
                       HOSTED_BUILD_CONTEXT=str(root/'never-context'), HOSTED_IMAGE_TAG='fixture',
                       HOSTED_BUILD_ENGINE='unix://'+str(endpoint), HOSTED_BUILD_ENGINE_ID='approved-isolated-fixture',
                       FIXTURE_CALLS=str(calls), FIXTURE_ENGINE_ID='different-fixture')
            def run(): return subprocess.run(['sh', str(HERE/'build-image.sh')], env=env, capture_output=True)
            result = run()
            self.assertNotEqual(result.returncode, 0)
            self.assertIn(b'socket missing', result.stderr)
            self.assertFalse(calls.exists())
            endpoint.write_text('not socket')
            result = run()
            self.assertNotEqual(result.returncode, 0)
            self.assertIn(b'not a socket', result.stderr)
            self.assertFalse(calls.exists()); endpoint.unlink()
            with socket.socket(socket.AF_UNIX) as inert:
                inert.bind(str(endpoint))  # No listen/connect; Docker is a text-only substitute.
                result = run()
                self.assertEqual(result.returncode, 2)
                self.assertIn(b'identity mismatch', result.stderr)
                self.assertEqual(calls.read_text().count('info --format'), 1)
                self.assertNotIn(' build ', calls.read_text())
                self.assertFalse((root/'never-context').exists())
                calls.unlink(); env.pop('HOSTED_BUILD_ENGINE_ID')
                result = run()
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(calls.exists())

    def test_offline_build_security_contract(self):
        docker = (HERE/'Dockerfile').read_text(); shell = (HERE/'build-image.sh').read_text()
        self.assertIn('--require-hashes --only-binary=:all:', docker)
        self.assertIn('--no-index --find-links=/wheels', docker)
        self.assertIn('USER 10001:10001', docker)
        self.assertIn('helper/ /opt/dingwei/sessionhelper-src/', docker)
        self.assertIn('security-debs/*.deb', docker)
        self.assertNotIn('apt-get', docker)
        self.assertNotIn('/opt/client', docker)
        self.assertIn('--network=none --pull=false', shell)
        self.assertIn('default host Engine forbidden', shell)
        self.assertIn('os.execve(VENV_PYTHON, [VENV_PYTHON', (HERE/'start.py').read_text())
        subprocess.run(['sh', '-n', str(HERE/'build-image.sh')], check=True)


if __name__ == '__main__':
    unittest.main()
