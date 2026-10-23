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

    def test_restart_and_historical_processed_are_unknown_without_replay(self):
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
        self.assertEqual(snapshot['dispatch_unknown_count'], 2)
        self.assertEqual(snapshot['deliveries'][a], 'unknown')
        self.assertEqual(snapshot['deliveries'][b], 'unknown')
        self.assertIsNone(restarted.claim_next())

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


if __name__ == '__main__': unittest.main()
