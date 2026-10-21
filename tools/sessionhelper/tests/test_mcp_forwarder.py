"""Real packaged stdio MCP checks in isolated mounts/state; no live Agent IO."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import sys
import time
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path[:0] = [str(ROOT), str(ROOT / 'tests')]
import bootstrap
import package_release
import test_update_release_core as core

REQUESTS = ''.join(json.dumps(item) + '\n' for item in (
    {'jsonrpc': '2.0', 'id': 1, 'method': 'initialize', 'params': {}},
    {'jsonrpc': '2.0', 'method': 'notifications/initialized'},
    {'jsonrpc': '2.0', 'id': 2, 'method': 'tools/list', 'params': {}},
))


def snapshot(root):
    """Exclude atime (reads may change it); include bytes, names, inode/mode/mtime."""
    if not root.exists():
        return None
    return {str(p.relative_to(root)): (p.lstat().st_ino, p.lstat().st_mode,
            p.lstat().st_mtime_ns, hashlib.sha256(p.read_bytes()).hexdigest()
            if p.is_file() else None) for p in [root, *root.rglob('*')]}


def probe(mount, state, shape='module', cwd=None, timeout=10):
    env = {'PATH': os.environ.get('PATH', ''), 'HOME': str(state.parent / 'home'),
           'SH_STATE_DIR': str(state), 'PYTHONPATH': str(mount),
           'PYTHONDONTWRITEBYTECODE': '1'}
    if shape == 'module':
        args = ['-m', 'sessionhelper.mcp_shim']
    elif shape == 'factory-script':
        args = [str(mount / 'factory/sessionhelper/mcp_shim.py')]
    else:
        env['PYTHONPATH'] = str(mount / 'factory')
        args = ['-m', 'sessionhelper.mcp_shim']
    return subprocess.run([sys.executable, '-B', *args], input=REQUESTS,
                          cwd=cwd or mount, env=env, capture_output=True,
                          text=True, timeout=timeout)


def assert_tools(test, proc, version):
    test.assertEqual(proc.returncode, 0, proc.stderr)
    test.assertEqual(proc.stderr, '')
    replies = {item['id']: item for item in map(json.loads, proc.stdout.splitlines())}
    test.assertEqual(replies[1]['result']['serverInfo']['version'], version)
    tools = replies[2]['result']['tools']
    test.assertEqual(len(tools), 22)
    test.assertEqual(len({tool['name'] for tool in tools}), 22)
    test.assertTrue({'send_message','dw_task_list','dw_task_update','dw_spec_review_requests','dw_spec_review_reply','dw_task_blocker_list','dw_task_blocker_set','dw_task_blocker_input','dw_task_blocker_recheck'}.issubset({tool['name'] for tool in tools}))


class ForwarderTests(unittest.TestCase):
    def setUp(self):
        self.fixture = core.PackageTests('runTest')
        self.fixture.setUp()
        self.addCleanup(self.fixture.doCleanups)
        self.base = Path(self.fixture.tmp.name)
        shutil.copyfile(ROOT / 'sessionhelper/mcp_shim.py',
                        self.fixture.base / 'sessionhelper/mcp_shim.py')
        shutil.copyfile(ROOT / 'sessionhelper/ipc_permissions.py',
                        self.fixture.base / 'sessionhelper/ipc_permissions.py')
        commit = self.fixture.commit()
        artifact = self.base / 'mcp-release.tgz'
        package_release.build(self.fixture.repo, commit, artifact)
        manifest, files = package_release.verify(artifact)
        self.assertIn('sessionhelper/mcp_shim.py', manifest['files'])
        self.assertEqual(files['sessionhelper/mcp_shim.py'],
                         (ROOT / 'update_launcher/mcp_forwarder.py').read_bytes())
        stage = self.base / 'stage'
        stage.mkdir()
        for name, content in files.items():
            path = stage / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(content)
        (stage / 'MANIFEST.json').write_text(json.dumps(manifest))
        self.mount, self.state = self.base / 'mount', self.base / 'private'
        self.mount.mkdir()
        bootstrap.install(stage, self.mount, self.base / 'backup')
        # A guard import or invocation must fail even though MCP still succeeds.
        (self.mount / 'helper_update_guard.py').write_text('raise RuntimeError("guard invoked")\n')

    def check_readonly(self, version='0.2.3.1', shape='module', cwd=None):
        before = snapshot(self.state), snapshot(self.mount)
        assert_tools(self, probe(self.mount, self.state, shape, cwd), version)
        self.assertEqual((snapshot(self.state), snapshot(self.mount)), before)

    def test_bootstrap_all_three_shapes_and_no_state_creation(self):
        for shape in ('module', 'factory-script', 'factory-module'):
            with self.subTest(shape=shape):
                # PYTHONPATH factory only applies outside the stable package cwd.
                self.check_readonly(shape=shape, cwd=self.base)
        self.check_readonly()  # also handle cwd == mount
        self.assertFalse(self.state.exists())

    def journal(self, data):
        update = self.state / 'update'
        update.mkdir(parents=True, exist_ok=True)
        (update / 'journal.json').write_text(json.dumps(data))
        return update

    def test_current_selected_pending_previous_ignored_and_budget_unchanged(self):
        update = self.journal({'format': 1, 'current': {'sha256': 'a' * 64},
                               'pending': {'sha256': 'b' * 64, 'starts': 1},
                               'previous': {'sha256': 'c' * 64}, 'terminal': {}})
        slot = update / 'slots' / ('a' * 64)
        shutil.copytree(self.mount / 'factory', slot)
        (slot / 'sessionhelper/__init__.py').write_text('__version__ = "0.2.3.2"\n')
        self.check_readonly('0.2.3.2')

    def test_invalid_journals_and_current_slots_fall_back(self):
        for value in (None, [], {}, {'format': 2}, {'format': 1, 'current': None},
                      {'format': 1, 'current': []},
                      {'format': 1, 'current': {'sha256': '../outside'}},
                      {'format': 1, 'current': {'sha256': 'f' * 64}}):
            with self.subTest(value=value):
                self.journal(value)
                self.check_readonly()
        journal = self.state / 'update/journal.json'
        for raw in ('{broken', 'x' * (1024 * 1024 + 1)):
            journal.write_text(raw)
            self.check_readonly()
        update = self.journal({'format': 1, 'current': {'sha256': 'd' * 64}})
        slot = update / 'slots' / ('d' * 64)
        slot.mkdir(parents=True)
        self.check_readonly()  # incomplete slot
        slot.rmdir()
        slot.symlink_to(self.mount / 'factory', target_is_directory=True)
        self.check_readonly()  # escaping named slot

    def test_withdrawing_packaged_forwarder_is_red(self):
        (self.mount / 'sessionhelper/mcp_shim.py').unlink()
        result = probe(self.mount, self.state)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('No module named sessionhelper.mcp_shim', result.stderr)
        self.assertFalse(result.stdout)

    def check_special_journal(self):
        # snapshot() reads ordinary files; never use it on an intentional FIFO.
        started = time.monotonic()
        assert_tools(self, probe(self.mount, self.state, timeout=2), '0.2.3.1')
        self.assertLess(time.monotonic() - started, 2.5)
        self.assertFalse((self.state / 'update/state.lock').exists())

    def special_journal_path(self):
        update = self.state / 'update'
        update.mkdir(parents=True)
        return update / 'journal.json'

    def test_fifo_journal_without_writer_falls_back_within_timeout(self):
        os.mkfifo(self.special_journal_path())
        self.check_special_journal()

    def test_directory_journal_falls_back_within_timeout(self):
        self.special_journal_path().mkdir()
        self.check_special_journal()

    def test_socket_journal_falls_back_within_timeout(self):
        with socket.socket(socket.AF_UNIX) as listener:
            listener.bind(str(self.special_journal_path()))
            self.check_special_journal()

    def test_oversized_journal_falls_back_within_timeout(self):
        with self.special_journal_path().open('wb') as stream:
            stream.truncate(1024 * 1024 + 1)
        self.check_special_journal()


if __name__ == '__main__':
    unittest.main()
