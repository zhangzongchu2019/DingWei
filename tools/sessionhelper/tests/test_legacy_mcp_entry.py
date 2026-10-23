"""Isolated legacy-slot selection and stdio tests; never access a live helper."""
import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import socket
import stat
import subprocess
import sys
import tarfile
import tempfile
import threading
import time
import types
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
REPO = ROOT.parents[1]
sys.path.insert(0, str(ROOT))
import legacy_mcp_entry as entry

OLD = 'b5faf73c8fe4d5c71720daf4eed5b133b8d45dbd'
CALLS = ''.join(json.dumps(x) + '\n' for x in (
    {'jsonrpc': '2.0', 'id': 1, 'method': 'initialize', 'params': {}},
    {'jsonrpc': '2.0', 'method': 'notifications/initialized'},
    {'jsonrpc': '2.0', 'id': 2, 'method': 'tools/list', 'params': {}},
))


def git(*args):
    return subprocess.check_output(['git', '-C', str(REPO), *args], stderr=subprocess.PIPE)


def load_old(name, file):
    code = git('show', OLD + ':tools/sessionhelper/' + file)
    mod = types.ModuleType(name)
    exec(compile(code, '<git:' + OLD + ':' + file + '>', 'exec'), mod.__dict__)
    return mod


def snapshot(root):
    return {str(p.relative_to(root)): (p.lstat().st_mode, p.lstat().st_ino,
            p.lstat().st_mtime_ns, hashlib.sha256(p.read_bytes()).hexdigest()
            if p.is_file() and not p.is_symlink() else None)
            for p in [root, *root.rglob('*')]}


class LegacyEntryTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.base = Path(self.tmp.name).resolve()
        self.state = self.base / 'state'
        self.update = self.state / 'update'
        self.update.mkdir(parents=True, mode=0o700)
        self.state.chmod(0o700)
        self.factory = self.base / 'mount/factory'
        self.make_package(self.factory, 'factory')
        self.slots = self.update / 'slots'
        self.slots.mkdir(mode=0o700)
        self.a, self.b = [self.slots / (s * 64) for s in 'ab']
        for slot, label in ((self.a, 'a'), (self.b, 'b')):
            self.make_package(slot, label)
        self.pointer(self.a)

    def make_package(self, root, label):
        package = root / 'sessionhelper'
        package.mkdir(parents=True, mode=0o700)
        root.chmod(0o700)
        for name, text in {'__init__.py': '__version__ = ' + repr(label),
                           '__main__.py': 'raise RuntimeError("HELPER_MUST_NOT_START")',
                           'mcp_shim.py': 'print(' + repr(label) + ')\n'}.items():
            p = package / name; p.write_text(text); p.chmod(0o600)

    def pointer(self, slot=None, **extra):
        data = {'format': 1, 'current': {'sha256': slot.name} if slot else None,
                'previous': {'sha256': 'b' * 64},
                'pending': {'sha256': 'b' * 64, 'starts': 1}, 'terminal': {}}
        data.update(extra)
        self.journal(data)

    def journal(self, data):
        p = self.update / 'journal.json'
        p.write_text(json.dumps(data)); p.chmod(0o600)

    def selected(self):
        return entry.selected_root(self.state, self.factory)

    def run_entry(self, input=CALLS, cwd=None, timeout=3, ipc=None):
        # Install the standalone bytes outside slots, as the migration does.
        installed = self.state / 'mcp-entry/forwarder.py'
        installed.parent.mkdir(exist_ok=True, mode=0o700)
        installed.write_bytes((ROOT / 'legacy_mcp_entry.py').read_bytes())
        installed.chmod(0o600)
        env = {'HOME': str(self.base / 'isolated-home'), 'PATH': '/usr/bin:/bin',
               'PYTHONDONTWRITEBYTECODE': '1', 'PYTHONPATH': str(self.factory),
               'DINGWEI_MCP_IPC_SOCK': str(ipc or self.base / 'nonexistent-private-ipc')}
        return subprocess.run([sys.executable, '-B', str(installed),
                               '--state-dir', str(self.state), '--factory-root', str(self.factory)],
                              input=input, text=True, capture_output=True,
                              env=env, cwd=cwd or self.base, timeout=timeout)

    def test_confirmed_only_and_readonly_without_guard_or_locks(self):
        before = snapshot(self.base)
        self.assertEqual(self.selected(), (self.a, False), 'CURRENT_NOT_SELECTED')
        self.assertEqual(snapshot(self.base), before, 'SELECTION_WROTE_STATE')
        self.assertFalse((self.update / 'state.lock').exists())
        self.pointer(None)
        self.assertEqual(self.selected(), (self.factory, True), 'PENDING_OR_PREVIOUS_SELECTED')
        self.pointer(self.b)
        self.assertEqual(self.selected(), (self.b, False), 'NEW_CURRENT_NOT_SELECTED')

    def test_invalid_journal_shapes_fall_back(self):
        for data in (None, [], {}, {'format': True, 'current': {'sha256': 'a' * 64}}, {'format': 2, 'current': {'sha256': 'a' * 64}},
                     {'format': 1, 'current': 'a'}, {'format': 1, 'current': {'sha256': '../escape'}},
                     {'format': 1, 'current': {'sha256': 'A' * 64}},
                     {'format': 1, 'current': {'sha256': 'f' * 64}}):
            with self.subTest(data=data):
                self.journal(data)
                self.assertEqual(self.selected(), (self.factory, True), 'INVALID_JOURNAL_ACCEPTED')
        for raw in (b'{broken', b'\xff', json.dumps({'format': 1, 'current': {'sha256': 'a' * 64}}).encode() + b' ' * (entry.MAX_JOURNAL_BYTES + 1)):
            (self.update / 'journal.json').write_bytes(raw)
            self.assertEqual(self.selected(), (self.factory, True), 'INVALID_JOURNAL_ACCEPTED')
        bad_slot = self.slots / ('g' * 64)
        self.make_package(bad_slot, 'g')
        self.pointer(bad_slot)
        self.assertEqual(self.selected(), (self.factory, True), 'INVALID_SHA_ACCEPTED')
        (self.update / 'journal.json').unlink()
        self.assertEqual(self.selected(), (self.factory, True))

    def test_special_journals_nonblocking(self):
        path = self.update / 'journal.json'
        for kind in ('fifo', 'directory', 'socket', 'symlink'):
            with self.subTest(kind=kind):
                path.unlink(missing_ok=True)
                sock = None
                if kind == 'fifo': os.mkfifo(path, 0o600)
                if kind == 'directory': path.mkdir()
                if kind == 'symlink': path.symlink_to(self.base / 'absent')
                if kind == 'socket':
                    sock = socket.socket(socket.AF_UNIX); sock.bind(str(path))
                try:
                    start = time.monotonic()
                    try: result = self.run_entry(timeout=2)
                    except subprocess.TimeoutExpired: self.fail('JOURNAL_NONBLOCK_TIMEOUT')
                    self.assertLess(time.monotonic() - start, 2.5)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertEqual(result.stdout, 'factory\n', 'SPECIAL_JOURNAL_NOT_FALLBACK')
                    self.assertEqual(result.stderr, 'mcp_slot_pin: factory_fallback\n')
                finally:
                    if sock: sock.close()
                    if path.is_dir() and not path.is_symlink(): path.rmdir()
                    else: path.unlink()

    def test_path_and_permission_guards(self):
        for p, badmode in ((self.state, 0o755), (self.update, 0o770),
                           (self.slots, 0o777), (self.a, 0o777),
                           (self.a / 'sessionhelper', 0o777),
                           (self.update / 'journal.json', 0o644)):
            with self.subTest(path=p.name):
                old = p.stat().st_mode; p.chmod(badmode)
                try: self.assertEqual(self.selected(), (self.factory, True), 'UNSAFE_MODE_ACCEPTED')
                finally: p.chmod(stat.S_IMODE(old))
        journal = self.update / 'journal.json'
        saved = self.base / 'valid-journal'; journal.rename(saved); journal.symlink_to(saved)
        try: self.assertEqual(self.selected(), (self.factory, True), 'JOURNAL_SYMLINK_ACCEPTED')
        finally: journal.unlink(); saved.rename(journal)
        parent_alias = self.base / 'parent-alias'; parent_alias.symlink_to(self.base, target_is_directory=True)
        self.assertEqual(entry.selected_root(parent_alias / 'state', self.factory), (self.factory, True), 'PARENT_ALIAS_ACCEPTED')
        alias = self.base / 'alias'; alias.symlink_to(self.state, target_is_directory=True)
        self.assertEqual(entry.selected_root(alias, self.factory), (self.factory, True), 'STATE_ALIAS_ACCEPTED')
        with patch.object(entry.os, 'getuid', return_value=os.getuid() + 10000):
            self.assertFalse(entry.safe_directory(self.state, private=True), 'FOREIGN_DIRECTORY_OWNER_ACCEPTED')
            self.assertIsNone(entry.confirmed_slot(self.state), 'FOREIGN_OWNER_ACCEPTED')

    def test_slot_symlinks_incomplete_and_special_files_fall_back(self):
        moved = self.slots / 'saved'
        self.a.rename(moved); self.a.symlink_to(moved, target_is_directory=True)
        self.assertEqual(self.selected(), (self.factory, True), 'SLOT_SYMLINK_ACCEPTED')
        self.a.unlink(); moved.rename(self.a)
        for name in entry.REQUIRED_FILES:
            p = self.a / 'sessionhelper' / name; data = p.read_bytes(); p.unlink()
            self.assertEqual(self.selected(), (self.factory, True), 'INCOMPLETE_SLOT_ACCEPTED')
            p.symlink_to(self.factory / 'sessionhelper' / name)
            self.assertEqual(self.selected(), (self.factory, True), 'PAYLOAD_SYMLINK_ACCEPTED')
            p.unlink(); os.mkfifo(p, 0o600)
            self.assertEqual(self.selected(), (self.factory, True), 'PAYLOAD_SPECIAL_ACCEPTED')
            p.unlink(); p.write_bytes(data)

    def test_no_valid_factory_visible_failure(self):
        self.pointer(None)
        (self.factory / 'sessionhelper/mcp_shim.py').unlink()
        result = self.run_entry()
        self.assertEqual(result.returncode, 78, 'INVALID_FACTORY_EXECUTED')
        self.assertEqual(result.stdout, '')
        self.assertEqual(result.stderr, 'mcp_slot_pin: no_valid_mcp_package\n')

    def test_real_stdio_current_factory_versions_and_ipc_failure(self):
        # Exact current source shim, not a mocked JSON-RPC server.
        shutil.rmtree(self.a / 'sessionhelper')
        shutil.copytree(ROOT / 'sessionhelper', self.a / 'sessionhelper',
                        ignore=shutil.ignore_patterns('__pycache__', '*.pyc'))
        (self.a / 'sessionhelper').chmod(0o700)
        before = snapshot(self.update)
        result = self.run_entry(cwd=self.factory)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stderr, '')
        self.assertTrue(result.stdout.startswith('{'), 'CWD_OR_PYTHONPATH_SHADOWED_SLOT')
        replies = {r['id']: r for r in map(json.loads, result.stdout.splitlines())}
        from sessionhelper import __version__
        self.assertEqual(replies[1]['result']['serverInfo']['version'], __version__, 'WRONG_STDIO_VERSION')
        self.assertIn('dw_inbox', {t['name'] for t in replies[2]['result']['tools']})
        self.assertEqual(snapshot(self.update), before, 'STDIO_WROTE_UPDATE_STATE')
        self.assertFalse((self.base / 'nonexistent-private-ipc').exists())

    def test_selected_shim_failure_never_reexecutes_factory(self):
        marker = self.base / 'factory-called'
        (self.factory / 'sessionhelper/mcp_shim.py').write_text(
            'from pathlib import Path;Path(' + repr(str(marker)) + ').touch()\n')
        (self.a / 'sessionhelper/mcp_shim.py').write_text('raise SystemExit(23)\n')
        result = self.run_entry()
        self.assertEqual(result.returncode, 23, 'FAILED_SHIM_RETRIED')
        self.assertFalse(marker.exists(), 'FAILED_SHIM_RETRIED')

    def test_process_keeps_snapshot_until_next_start(self):
        # A live shim remains pinned; a new MCP process selects the new current.
        (self.a / 'sessionhelper/mcp_shim.py').write_text(
            'import sys;print("a",flush=True);sys.stdin.readline();print("a",flush=True)\n')
        installed = self.state / 'mcp-entry/forwarder.py'
        self.run_entry(input='go\n')  # install bytes
        proc = subprocess.Popen([sys.executable, '-B', str(installed), '--state-dir', str(self.state),
                                 '--factory-root', str(self.factory)], stdin=subprocess.PIPE,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
                                env={'HOME': str(self.base), 'PATH': '/usr/bin:/bin'})
        try:
            self.assertEqual(proc.stdout.readline(), 'a\n')
            self.pointer(self.b)
            out, err = proc.communicate('go\n', timeout=3)
            self.assertEqual(out, 'a\n', 'LIVE_SHIM_HOT_SWITCHED')
            self.assertEqual(proc.returncode, 0, err)
            self.assertEqual(self.run_entry().stdout, 'b\n', 'RESTART_DID_NOT_FOLLOW_CURRENT')
        finally:
            if proc.poll() is None: proc.kill(); proc.communicate()

    def test_actual_shim_uses_only_supplied_fake_ipc(self):
        shutil.rmtree(self.a / 'sessionhelper')
        shutil.copytree(ROOT / 'sessionhelper', self.a / 'sessionhelper',
                        ignore=shutil.ignore_patterns('__pycache__', '*.pyc'))
        (self.a / 'sessionhelper').chmod(0o700)
        ipc = self.base / 'fake.sock'; requests = []; errors = []
        with socket.socket(socket.AF_UNIX) as listener:
            listener.bind(str(ipc)); listener.listen(1); listener.settimeout(3)
            def serve():
                try:
                    conn, _ = listener.accept()
                    with conn:
                        conn.settimeout(2); data = b''
                        while b'\n' not in data:
                            chunk = conn.recv(65536)
                            if not chunk: break
                            data += chunk
                        requests.append(json.loads(data))
                        conn.sendall(b'{"ok":true,"outcome":{"mode":"driver","fixture":true}}\n')
                except Exception as exc: errors.append(type(exc).__name__)
            worker = threading.Thread(target=serve); worker.start()
            try:
                request = {'jsonrpc': '2.0', 'id': 3, 'method': 'tools/call',
                           'params': {'name': 'dw_inbox', 'arguments': {'operation': 'status'}}}
                result = self.run_entry(input=CALLS + json.dumps(request) + '\n', ipc=ipc)
            finally: worker.join(timeout=4)
        self.assertFalse(worker.is_alive())
        self.assertEqual(errors, [], 'FAKE_IPC_FAILED')
        self.assertEqual(result.returncode, 0, result.stderr)
        reply = {r['id']: r for r in map(json.loads, result.stdout.splitlines())}[3]
        self.assertIn('driver', json.dumps(reply), 'IPC_ENV_NOT_PRESERVED')
        self.assertEqual(len(requests), 1, 'IPC_REPLAYED')
        self.assertEqual(requests[0]['type'], 'desktop_inbox_v1')
        self.assertEqual(requests[0]['operation'], 'status')

    def test_old_factory_real_stdio_reports_its_actual_version(self):
        # b5's real legacy shim and init: old tools are not relabelled as new.
        for name in ('__init__.py', 'mcp_shim.py'):
            p = self.factory / 'sessionhelper' / name
            p.write_bytes(git('show', OLD + ':tools/sessionhelper/sessionhelper/' + name))
        self.pointer(None)
        result = self.run_entry()
        self.assertEqual(result.returncode, 0, result.stderr)
        replies = {r['id']: r for r in map(json.loads, result.stdout.splitlines())}
        self.assertEqual(replies[1]['result']['serverInfo']['version'], '0.2.3.2', 'FACTORY_VERSION_RELABELLED')
        names = {t['name'] for t in replies[2]['result']['tools']}
        self.assertNotIn('dw_spec_review_reply', names)
        self.assertEqual(result.stderr, 'mcp_slot_pin: factory_fallback\n')

    def test_atomic_journal_replacements_keep_whole_snapshot(self):
        errors = []
        def replace():
            for i in range(80):
                p = self.update / '.incoming'
                p.write_text(json.dumps({'format': 1, 'current': {'sha256': ('a' if i % 2 else 'b') * 64}}))
                p.chmod(0o600)
                os.replace(p, self.update / 'journal.json')
        worker = threading.Thread(target=replace); worker.start()
        try:
            for _ in range(80):
                if self.selected() not in ((self.a, False), (self.b, False)): errors.append('partial')
        finally: worker.join(timeout=3)
        self.assertFalse(worker.is_alive())
        self.assertEqual(errors, [], 'ATOMIC_SNAPSHOT_TORN')

    def test_exec_failure_is_visible_without_retries(self):
        argv = ['forwarder', '--state-dir', str(self.state), '--factory-root', str(self.factory)]
        err = io.StringIO(); out = io.StringIO()
        with patch.object(sys, 'argv', argv), patch.object(sys, 'stderr', err), patch.object(sys, 'stdout', out):
            with patch.object(entry.os, 'execve', side_effect=OSError('private text must not leak')) as execute:
                self.assertEqual(entry.main(), 78, 'EXEC_FAILURE_HIDDEN')
                self.assertEqual(execute.call_count, 1, 'EXEC_RETRIED')
        self.assertEqual(out.getvalue(), '')
        self.assertEqual(err.getvalue(), 'mcp_slot_pin: exec_failed\n', 'EXEC_ERROR_LEAKED')

    def test_real_legacy_verifier_and_guard_confirm_rollback(self):
        verifier = load_old('old_package', 'package_release.py')
        guard = load_old('old_guard', 'update_launcher/guard.py')
        # A genuine old-format package, checked by the unmodified stable verifier.
        files = {'factory/sessionhelper/' + name: (self.b / 'sessionhelper' / name).read_bytes()
                 for name in entry.REQUIRED_FILES}
        for name in ('sessionhelper/__init__.py', 'sessionhelper/__main__.py',
                     'helper_update_guard.py', 'bootstrap.py', 'helper_update_package.py'):
            files[name] = b'# inert fixture\n'
        files['requirements-runtime.lock'] = b'# inert fixture lock\n'
        pins = {k: {'bytes': len(v), 'sha256': verifier.digest(v)} for k, v in files.items()}
        manifest = {'format': 1, 'version': '0.2.3.11', 'files': pins,
                    'lock_sha256': verifier.digest(files['requirements-runtime.lock'])}
        artifact = self.base / 'legacy.tgz'
        def pack(values):
            with tarfile.open(artifact, 'w:gz') as archive:
                for name, data in values.items():
                    info = tarfile.TarInfo('helper-release/' + name); info.size = len(data)
                    archive.addfile(info, io.BytesIO(data))
        pack({**files, 'MANIFEST.json': json.dumps(manifest).encode()})
        meta, payload = verifier.verify(artifact, hashlib.sha256(artifact.read_bytes()).hexdigest())
        self.assertEqual(meta['version'], '0.2.3.11')
        self.assertEqual(set(payload), set(files))
        # Even a pinned root MCP forwarder is still rejected by the old whitelist.
        files['sessionhelper/mcp_shim.py'] = b'# not allowed in old layout\n'
        manifest['files'] = {k: {'bytes': len(v), 'sha256': verifier.digest(v)} for k, v in files.items()}
        pack({**files, 'MANIFEST.json': json.dumps(manifest).encode()})
        with self.assertRaisesRegex(verifier.PackageError, 'manifest_invalid'):
            verifier.verify(artifact)
        # Real old journal/guard transitions, no helper process or live IPC.
        self.journal({'format': 1, 'current': None, 'previous': None, 'pending': None, 'terminal': {}})
        state = guard.State(self.update, self.factory)
        def item(slot):
            return dict(job_id='job-' + slot.name[0], release_id='release', agent_id='agent',
                        owner_id='owner', version='0.2.3.11', sha256=slot.name)
        state.arm(item(self.a)); state.select()
        self.assertEqual(self.selected(), (self.factory, True), 'TRIAL_SELECTED')
        state.confirm(item(self.a))
        self.assertEqual(self.run_entry().stdout, 'a\n', 'CONFIRMED_SLOT_NOT_EXECUTED')
        state.arm(item(self.b)); state.select()
        self.assertEqual(self.selected(), (self.a, False), 'TRIAL_SELECTED')
        state.fail(item(self.b), 'inert_trial_failed')
        self.assertEqual(self.selected(), (self.a, False), 'ROLLBACK_LOST_CURRENT')
        state.arm(dict(item(self.b), job_id='job-b-again')); state.select()
        state.confirm(dict(item(self.b), job_id='job-b-again'))
        self.assertEqual(self.run_entry().stdout, 'b\n', 'SECOND_CONFIRM_NOT_FOLLOWED')


if __name__ == '__main__':
    unittest.main()
