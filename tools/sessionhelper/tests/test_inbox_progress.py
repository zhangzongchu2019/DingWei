"""Persistent observational stages, including real adapter queue/receipt wiring."""
import json
import tempfile
import threading
import time
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

from sessionhelper.inbox import DeliveryInbox
from sessionhelper.inbox_progress import InboxProgress, observe
from sessionhelper.driver_adapter import DriverAdapter


class InboxProgressTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.inbox = DeliveryInbox(str(Path(self.tmp.name) / 'inbox.db'), session_name='seat')
        self.journal = self.inbox.progress
        self.now, self.mono = 10000., 100.
        self.journal.clock = lambda: self.now
        self.journal.monotonic = lambda: self.mono

    def accept(self, char):
        did = 'dw-dlv_' + char * 32
        env = {'id': 'message-' + char, 'body': 'synthetic', 'from': 'peer#fixture', 'meta': {'delivery_id': did}}
        self.inbox.accept(did, env)
        return did, env

    def test_processed_is_still_queued_40min_age_ignores_wall_clock(self):
        did, env = self.accept('a')
        self.journal.mark(did, 'adapter_queued')
        self.inbox.claim_next()
        self.inbox.finish(did)
        self.mono += 40 * 60
        self.now -= 1000
        s = self.journal.snapshot()
        self.assertEqual(s['disk_not_admitted_count'], 1)
        self.assertEqual(s['oldest_wait_ms'], 2400000)
        self.assertFalse(s['clock_uncertain'])
        self.assertEqual(s['deliveries'][did], 'adapter_queued')
        self.journal.mark(did, 'model_admitted')
        self.journal.mark(did, 'adapter_queued')  # Late duplicate cannot regress.
        s = self.journal.snapshot()
        self.assertEqual(s['disk_not_admitted_count'], 0)
        self.assertIsNone(s['oldest_wait_ms'])
        self.assertEqual(s['deliveries'][did], 'model_admitted')

    def test_clock_rollback_new_arrival_does_not_replace_true_oldest(self):
        with patch('sessionhelper.inbox.time.time',return_value=10000):self.accept('a')
        self.mono += 60;self.now=9000
        with patch('sessionhelper.inbox.time.time',return_value=9000):self.accept('b')
        self.mono += 60
        self.assertEqual(self.journal.snapshot()['oldest_wait_ms'],120000)
        self.assertFalse(self.journal.snapshot()['clock_uncertain'])

    def test_oldest_removal_and_cancellation_and_dedup(self):
        a, env = self.accept('a')
        self.mono += 60
        b, _ = self.accept('b')
        self.inbox.accept(a, env)
        self.journal.mark(a, 'model_admitted')
        self.mono += 60
        self.assertEqual(self.journal.snapshot()['oldest_wait_ms'], 60000)
        self.inbox.cancel('message-b', b)
        self.assertEqual(self.journal.snapshot()['disk_not_admitted_count'], 0)
        self.assertIsNone(self.journal.snapshot()['oldest_wait_ms'])

    def test_restart_counts_recovered_unknown_not_historical_processed_without_replay(self):
        a, _ = self.accept('a')
        self.inbox.claim_next(); self.inbox.finish(a)
        self.journal.mark(a, 'dispatching')
        b, _ = self.accept('b')
        self.inbox.claim_next(); self.inbox.finish(b)
        # Simulate a pre-feature row without progress evidence.
        with self.inbox._connect() as db:
            db.execute('DELETE FROM delivery_progress WHERE delivery_id=?', (b,))
        restarted = DeliveryInbox(self.inbox.path, session_name='seat')
        snapshot = restarted.progress.snapshot()
        self.assertEqual(snapshot['disk_not_admitted_count'], 0)
        self.assertEqual(snapshot['dispatch_unknown_count'], 1)
        self.assertEqual(snapshot['deliveries'][a], 'unknown')
        self.assertEqual(snapshot['deliveries'][b], 'unknown')
        self.assertIsNone(restarted.claim_next())

    def test_1500_processed_history_rows_do_not_pollute_current_unknown_count(self):
        for previous_candidate in (False, True):
            with self.subTest(previous_candidate=previous_candidate), tempfile.TemporaryDirectory() as root:
                path = str(Path(root) / 'inbox.db')
                old = DeliveryInbox(path, session_name='seat')
                # Reproduce both pre-A history and the reviewed A candidate's
                # already-backfilled unscoped unknown rows / six-column schema.
                with old._connect() as db:
                    db.executemany('''INSERT INTO delivery_inbox
                        (session_name,delivery_id,envelope_json,state,received_at,updated_at)
                        VALUES('seat',?,'{}','processed',1,1)''',
                        [('dw-dlv_' + format(n, '032x'),) for n in range(1500)])
                    db.execute('DROP TABLE delivery_progress')
                    db.execute('''CREATE TABLE delivery_progress (
                        session_name TEXT NOT NULL, delivery_id TEXT NOT NULL,
                        stage TEXT NOT NULL, received_at REAL NOT NULL, updated_at REAL NOT NULL,
                        PRIMARY KEY(session_name,delivery_id))''')
                    if previous_candidate:
                        db.execute('''INSERT INTO delivery_progress
                            SELECT session_name,delivery_id,'unknown',received_at,updated_at FROM delivery_inbox''')
                upgraded = DeliveryInbox(path, session_name='seat')
                snap = upgraded.progress.snapshot()
                self.assertEqual(snap['disk_not_admitted_count'], 0)
                self.assertEqual(snap['dispatch_unknown_count'], 0)
                self.assertTrue(all(stage == 'unknown' for stage in snap['deliveries'].values()))
                self.assertIsNone(upgraded.claim_next())
                with upgraded._connect() as db:
                    self.assertEqual(db.execute("SELECT count(*) FROM delivery_inbox WHERE state='processed'").fetchone()[0], 1500)
                    self.assertEqual(db.execute('SELECT count(*) FROM delivery_progress WHERE unknown_instance IS NULL').fetchone()[0], 1500)
                did = 'dw-dlv_' + 'a' * 32
                upgraded.accept(did, {'body': 'synthetic'})
                upgraded.progress.mark(did, 'unknown')
                upgraded.progress.mark(did, 'unknown')
                self.assertEqual(upgraded.progress.snapshot()['dispatch_unknown_count'], 1)
                upgraded.progress.mark(did, 'model_admitted')
                self.assertEqual(upgraded.progress.snapshot()['dispatch_unknown_count'], 0)

    def test_previous_instance_unknown_keeps_phase_without_becoming_new_live_count(self):
        did, _ = self.accept('a')
        self.inbox.claim_next(); self.inbox.finish(did)
        self.journal.mark(did, 'unknown')
        self.assertEqual(self.journal.snapshot()['dispatch_unknown_count'], 1)
        restarted = DeliveryInbox(self.inbox.path, session_name='seat')
        self.assertNotEqual(restarted.progress.instance, self.journal.instance)
        snap = restarted.progress.snapshot()
        self.assertEqual(snap['dispatch_unknown_count'], 0)
        self.assertEqual(snap['deliveries'][did], 'unknown')
        self.assertIsNone(restarted.claim_next())

    def test_unprocessed_history_is_current_recovery_uncertainty_not_silently_zero(self):
        for had_progress in (False, True):
            with self.subTest(had_progress=had_progress):
                did, _ = self.accept('b' if had_progress else 'a')
                if had_progress:
                    self.journal.mark(did, 'unknown')
                else:
                    with self.inbox._connect() as db:
                        db.execute('DELETE FROM delivery_progress WHERE delivery_id=?', (did,))
                restarted = DeliveryInbox(self.inbox.path, session_name='seat')
                self.assertEqual(restarted.progress.snapshot()['deliveries'][did], 'unknown')
                with restarted._connect() as db:
                    scope = db.execute('SELECT unknown_instance FROM delivery_progress WHERE delivery_id=?', (did,)).fetchone()[0]
                    self.assertEqual(scope, restarted.progress.instance)
                    self.assertEqual(db.execute('SELECT state FROM delivery_inbox WHERE delivery_id=?', (did,)).fetchone()[0], 'received')
                self.assertGreaterEqual(restarted.progress.snapshot()['dispatch_unknown_count'], 1)

    def test_failed_observation_cannot_publish_old_numbers_as_fresh(self):
        did, _ = self.accept('a')
        with patch.object(self.journal, 'mark', side_effect=OSError('synthetic')):
            observe(self.journal, did, 'model_admitted')
        self.assertFalse(self.journal.snapshot()['available'])

    def test_actual_adapter_receipt_hooks_and_fifo_codex_and_claude(self):
        for driver_kind in ('codex', 'claude'):
            with self.subTest(driver=driver_kind):
                # Real adapter and durable inbox, protocol double only: no AI process.
                cfg = SimpleNamespace(busy_buffer_max=10, turn_terminal_timeout_seconds=10)
                delivered = []
                driver = SimpleNamespace(deliver=lambda did, body: delivered.append(did) or
                    SimpleNamespace(state='done', detail='turn=confirmed', source='fixture'))
                with patch.object(DriverAdapter, '_build_driver', return_value=driver), patch.dict('os.environ', {'SH_DRIVER': driver_kind}):
                    adapter = DriverAdapter(cfg)
                adapter._started = True
                adapter.delivery_progress = self.journal
                a, env_a = self.accept('c' if driver_kind == 'codex' else 'e')
                b, env_b = self.accept('d' if driver_kind == 'codex' else 'f')
                adapter.handle_delivery(env_a)
                adapter.handle_delivery(env_b)
                for did in (a, b):
                    claimed = self.inbox.claim_next(); self.assertEqual(claimed[0], did)
                    self.inbox.finish(did)
                self.assertEqual(self.journal.snapshot()['disk_not_admitted_count'], 2)
                first = adapter._commands.start_next()
                adapter._deliver_command(first)
                self.assertEqual(delivered, [a])
                snap = self.journal.snapshot()
                self.assertEqual(snap['deliveries'][a], 'terminal')
                self.assertEqual(snap['disk_not_admitted_count'], 1)
                adapter._deliver_command(adapter._commands.start_next())
                self.assertEqual(delivered, [a, b])
                self.assertEqual(self.journal.snapshot()['disk_not_admitted_count'], 0)
                adapter.handle_delivery(env_b)
                self.assertEqual(delivered, [a, b])  # No telemetry-induced replay.


    def test_real_claude_submitted_receipt_is_not_admitted_before_replay_ack(self):
        import io
        from sessionhelper.claudeDriver import ClaudeProtocolDriver
        driver = ClaudeProtocolDriver(store_path=str(Path(self.tmp.name)/'claude.db'))
        driver._proc = SimpleNamespace(stdin=io.StringIO())
        with patch.object(DriverAdapter, '_build_driver', return_value=driver), patch.dict('os.environ', {'SH_DRIVER':'claude'}):
            adapter = DriverAdapter(SimpleNamespace(busy_buffer_max=10,turn_terminal_timeout_seconds=10))
        adapter._started=True;adapter.delivery_progress=self.journal
        did,env=self.accept('a');adapter.handle_delivery(env);command=adapter._commands.start_next()
        with patch.object(driver,'health',return_value=True), patch('sessionhelper.claudeDriver.TURN_ACCEPT_TIMEOUT',0):
            receipt=driver.deliver(did,'synthetic')
        self.assertEqual(receipt.state,'processing');self.assertIn('awaiting ack',receipt.detail)
        self.assertFalse(adapter._admit_from_receipt(command,receipt)) # submitted is not admission.
        self.assertFalse(adapter._observe_model_admission(command,receipt))
        self.assertEqual(self.journal.snapshot()['disk_not_admitted_count'],1)
        self.assertFalse(adapter._observe_model_admission(command,driver.poll_receipt(did)))
        driver._handle_frame({'type':'user','isReplay':True})
        self.assertTrue(adapter._admit_from_receipt(command,driver.poll_receipt(did)))
        self.assertTrue(adapter._observe_model_admission(command,driver.poll_receipt(did)))
        self.assertEqual(self.journal.snapshot()['deliveries'][did],'model_admitted')
        self.assertEqual(self.journal.snapshot()['disk_not_admitted_count'],0)

    def test_long_turn_actual_settle_keeps_second_queued_without_replay(self):
        for kind in ('codex','claude'):
            with self.subTest(driver=kind):
                release=threading.Event();delivered=[]
                def receipt():return SimpleNamespace(state='done' if release.is_set() else 'accepted',detail='isReplay ack, turn=confirmed',source='fixture')
                def deliver(did,body):delivered.append(did);return receipt()
                driver=SimpleNamespace(deliver=deliver,poll_receipt=lambda did:receipt())
                with patch.object(DriverAdapter,'_build_driver',return_value=driver), patch.dict('os.environ',{'SH_DRIVER':kind}):
                    adapter=DriverAdapter(SimpleNamespace(busy_buffer_max=10,turn_terminal_timeout_seconds=10))
                adapter._started=True;adapter.delivery_progress=self.journal
                a,env_a=self.accept('c' if kind=='codex' else 'e');b,env_b=self.accept('d' if kind=='codex' else 'f')
                adapter.handle_delivery(env_a);adapter.handle_delivery(env_b)
                first=adapter._commands.start_next();thread=threading.Thread(target=adapter._deliver_command,args=(first,));thread.start()
                try:
                    deadline=time.monotonic()+2
                    while self.journal.snapshot()['deliveries'][a]!='model_admitted' and time.monotonic()<deadline:time.sleep(.01)
                    snap=self.journal.snapshot();self.assertEqual(snap['deliveries'][a],'model_admitted');self.assertEqual(snap['disk_not_admitted_count'],1)
                    self.mono+=2400
                    self.assertEqual(self.journal.snapshot()['oldest_wait_ms'],2400000)
                    self.assertEqual(delivered,[a]);self.assertIsNone(adapter._commands.start_next())
                finally:release.set();thread.join(2)
                self.assertFalse(thread.is_alive());adapter._deliver_command(adapter._commands.start_next());self.assertEqual(delivered,[a,b])


if __name__ == '__main__': unittest.main()
