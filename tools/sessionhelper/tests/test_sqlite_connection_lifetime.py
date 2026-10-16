"""Deterministic connection ownership, including GC-off/low-FD processes."""
from contextlib import closing
import gc
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
from sessionhelper.sqlite_utils import connection


def fixture_fd_count(root):
    result = 0
    for entry in Path('/proc/self/fd').iterdir():
        try:
            result += os.readlink(entry).startswith(str(root))
        except FileNotFoundError:
            pass
    return result


def fd_worker():
    import resource
    from sessionhelper.inbox import DeliveryInbox
    from sessionhelper.codexDriver import _DeliveryStore as CodexStore
    from sessionhelper.claudeDriver import _DeliveryStore as ClaudeStore
    from sessionhelper.openCodeDriver import _DeliveryStore as OpenCodeStore
    from sessionhelper.desktop import DesktopAdapter
    from sessionhelper.desktop_stop_hook import continuation
    from sessionhelper.cli import OpenCodeDBReader
    from sessionhelper.provision import Provisioner, ProvisionResult

    with tempfile.TemporaryDirectory(prefix='dw-fd-') as tmp:
        root = Path(tmp)
        inbox = DeliveryInbox(str(root / 'inbox.db'), session_name='fixture')
        codex = CodexStore(str(root / 'codex.db'))
        claude = ClaudeStore(str(root / 'claude.db'))
        opencode = OpenCodeStore(str(root / 'opencode.db'))
        desktop = DesktopAdapter(SimpleNamespace(inbox_db=str(root / 'desktop.db')))
        desktop.handle({'id': 'unread', 'body': 'fixture', 'from': 'fixture#fixture'})
        binding = root / 'binding.json'
        binding.write_text(json.dumps({'enabled': True, 'session_id': 'fixture',
                                       'inbox_path': desktop.path}))
        binding.chmod(0o600)
        event = {'hook_event_name': 'Stop', 'stop_hook_active': False,
                 'session_id': 'fixture', 'turn_id': 'turn'}
        legacy = root / 'legacy.db'
        with closing(sqlite3.connect(legacy)) as db, db:
            db.execute('CREATE TABLE delivery_inbox(delivery_id, envelope_json, state, attempts, error, received_at)')
        read_db = root / 'read.db'
        with closing(sqlite3.connect(read_db)) as db, db:
            db.execute('CREATE TABLE message(id, session_id, time_created, data)')
            db.execute('CREATE TABLE part(id, message_id, time_created, data)')
        reader = OpenCodeDBReader(str(read_db))
        provisioner = object.__new__(Provisioner)
        provisioner.audit_db = root / 'audit.db'
        result = ProvisionResult('fixture', 'fixture', '0', True)

        def inbox_lifecycle(i):
            key = str(i)
            inbox.accept(key, {'body': 'fixture'})
            inbox.accept(key, {'body': 'fixture'})
            assert inbox.claim_next()[0] == key
            inbox.release(key)
            assert inbox.claim_next()[0] == key
            inbox.finish(key)
            inbox.accept('cancel-' + key, {})
            inbox.cancel('message', 'cancel-' + key)

        def codex_lifecycle(i):
            codex.upsert('thread', 'envelope', 'turn', 'hash', 'processing')
            assert codex.get('thread', 'envelope') is not None
            assert codex.get_by_envelope('envelope') is not None
            codex.update_state('thread', 'envelope', 'completed')

        def claude_lifecycle(i):
            claude.upsert('envelope', 'turn', 'hash', 'processing')
            assert claude.get('envelope') is not None
            claude.update_state('envelope', 'completed')

        def opencode_lifecycle(i):
            opencode.upsert('envelope', 'session', 'message', 'processing', '')
            assert opencode.get('envelope') is not None

        def desktop_lifecycle(i):
            desktop.request({'operation': 'status'})
            desktop.request({'operation': 'list'})
            desktop.request({'operation': 'read', 'message_id': 'unread'})
            desktop.request({'operation': 'pause'})
            desktop.request({'operation': 'resume'})
            assert desktop.wait_message({'wait_seconds': 1})['state'] == 'message'

        workloads = {
            'inbox': inbox_lifecycle,
            'legacy': lambda i: inbox._pending_legacy_rows(str(legacy)),
            'codex': codex_lifecycle, 'claude': claude_lifecycle,
            'opencode': opencode_lifecycle, 'desktop': desktop_lifecycle,
            'cli_recent': lambda i: reader.recent_messages(0),
            'cli_parts': lambda i: reader.message_text('missing'),
            'audit': lambda i: provisioner.audit({}, result),
            'stop_hook': lambda i: continuation(str(binding), event),
        }
        gc.collect()
        gc.disable()
        old_limit = resource.getrlimit(resource.RLIMIT_NOFILE)
        resource.setrlimit(resource.RLIMIT_NOFILE, (128, old_limit[1]))
        baseline = fixture_fd_count(root)
        total_baseline = len(os.listdir('/proc/self/fd'))
        counts = {}
        try:
            for name, operation in workloads.items():
                peak = baseline
                for i in range(200):
                    operation(i)
                    current = fixture_fd_count(root)
                    peak = max(peak, current)
                    assert current == baseline, (name, i, baseline, current)
                counts[name] = {'calls': 200, 'peak_db_fd': peak}
            milestones = {}
            for i in range(1, 10001):
                assert inbox.claim_next() is None
                codex.get('thread', 'missing')
                current = fixture_fd_count(root)
                assert current == baseline, ('long_run', i, baseline, current)
                if i in (1, 100, 1000, 10000):
                    milestones[str(i)] = current
            assert not gc.isenabled()
            assert len(os.listdir('/proc/self/fd')) == total_baseline
            print(json.dumps({'gc_enabled': False, 'nofile': 128,
                              'baseline_db_fd': baseline, 'final_db_fd': fixture_fd_count(root),
                              'total_fd_baseline': total_baseline,
                              'total_fd_final': len(os.listdir('/proc/self/fd')),
                              'workloads': counts, 'long_run_iterations': 10000,
                              'long_run_db_fds': milestones}), flush=True)
        finally:
            resource.setrlimit(resource.RLIMIT_NOFILE, old_limit)
            gc.enable()
            gc.collect()


class SQLiteLifetimeTests(unittest.TestCase):
    @unittest.skipUnless(sys.platform.startswith('linux'), 'requires Linux FD accounting and rlimit')
    def test_gc_disabled_rlimit128_all_openers_and_long_run(self):
        child = subprocess.run([sys.executable, '-B', str(Path(__file__).resolve()), '--fd-worker'],
                               capture_output=True, text=True, timeout=180)
        self.assertEqual(child.returncode, 0, child.stderr)
        result = json.loads(child.stdout)
        self.assertFalse(result['gc_enabled'])
        self.assertEqual(result['nofile'], 128)
        self.assertEqual(result['final_db_fd'], 0)
        self.assertEqual(set(result['long_run_db_fds'].values()), {0})
        print('SQLITE_FD_READINGS ' + json.dumps(result, sort_keys=True), flush=True)

    def test_commit_rollback_and_early_return_persist_correctly(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / 'test.db'
            def early_return():
                with connection(path) as db:
                    db.execute('CREATE TABLE value(n INTEGER UNIQUE)')
                    db.execute('INSERT INTO value VALUES(1)')
                    return 'done'
            self.assertEqual(early_return(), 'done')
            with self.assertRaisesRegex(RuntimeError, 'fixture rollback'):
                with connection(path) as db:
                    db.execute('INSERT INTO value VALUES(2)')
                    raise RuntimeError('fixture rollback')
            with self.assertRaises(sqlite3.IntegrityError):
                with connection(path) as db:
                    db.execute('INSERT INTO value VALUES(3)')
                    db.execute('INSERT INTO value VALUES(1)')
            with closing(sqlite3.connect(path)) as verifier:
                self.assertEqual(verifier.execute('SELECT n FROM value').fetchall(), [(1,)])

    def test_close_once_on_success_body_and_transaction_exit_failure(self):
        for body_failure, exit_failure, row_failure in (
                (False, False, False), (True, False, False),
                (False, True, False), (True, True, False), (False, False, True)):
            with self.subTest(body=body_failure, transaction=exit_failure, row=row_failure):
                class DB:
                    closes = 0
                    @property
                    def row_factory(self): return None
                    @row_factory.setter
                    def row_factory(self, value):
                        if row_failure: raise RuntimeError('row configuration')
                    def __enter__(self): return self
                    def __exit__(self, *args):
                        if exit_failure: raise RuntimeError('transaction exit')
                    def close(self): self.closes += 1
                db = DB()
                def exercise():
                    with connection('fixture', row_factory=sqlite3.Row):
                        if body_failure: raise RuntimeError('body')
                with patch('sessionhelper.sqlite_utils.sqlite3.connect', return_value=db):
                    if body_failure or exit_failure or row_failure:
                        with self.assertRaises(RuntimeError): exercise()
                    else: exercise()
                self.assertEqual(db.closes, 1)


if __name__ == '__main__':
    if sys.argv[1:] == ['--fd-worker']:
        fd_worker()
    else:
        unittest.main()
