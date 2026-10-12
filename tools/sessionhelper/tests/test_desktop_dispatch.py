import os
import tempfile
import unittest
from types import SimpleNamespace
from sessionhelper.desktop import DesktopAdapter
from sessionhelper.desktop_dispatch import DesktopDispatcher


class Provider:
    bound = True
    idle = True
    failure = False
    def __init__(self):
        self.calls = []
    def bound_to_original_desktop(self, thread):
        return self.bound and thread == 'original'
    def loaded_and_idle(self, thread):
        return self.idle
    def start_if_idle(self, thread, message, body):
        self.calls.append((thread, message, body))
        if self.failure:
            raise TimeoutError('synthetic private error')
        return 'turn-' + message


class DispatchTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.cfg = SimpleNamespace(inbox_db=os.path.join(self.tmp.name, 'inbox'))
        self.adapter = DesktopAdapter(self.cfg)
        self.provider = Provider()
        self.engine = DesktopDispatcher(self.adapter, self.provider, 'original')
        self.adapter.handle({'id': 'a', 'body': 'synthetic question'})
    def tearDown(self):
        self.tmp.cleanup()
    def test_admission_is_not_completion_or_read(self):
        self.assertEqual(self.engine.step()['state'], 'admitted')
        self.assertEqual(self.engine.step()['state'], 'blocked')
        self.assertEqual(self.adapter.request({'operation': 'status'})['unread'], 1)
        self.assertFalse(self.adapter.request({'operation': 'status'})['native_auto_notify_implemented'])
    def test_unbound_busy_pause_no_native_effect(self):
        self.provider.bound = False
        self.assertEqual(self.engine.step()['state'], 'unbound')
        self.provider.bound = True
        self.provider.idle = False
        self.assertEqual(self.engine.step()['state'], 'busy')
        self.provider.idle = True
        self.adapter.request({'operation': 'pause'})
        self.assertEqual(self.engine.step()['state'], 'paused')
        self.assertEqual(self.provider.calls, [])
    def test_uncertain_never_replayed_after_restart(self):
        self.provider.failure = True
        with self.assertRaises(TimeoutError):
            self.engine.step()
        reopened = DesktopDispatcher(DesktopAdapter(self.cfg), self.provider, 'original')
        self.assertEqual(reopened.step()['admission_state'], 'uncertain')
        self.assertEqual(len(self.provider.calls), 1)
    def test_crash_before_native_response_remains_blocked(self):
        with self.adapter._connect() as db:
            db.execute("INSERT INTO desktop_admission VALUES('a','original','starting',NULL)")
        self.assertEqual(self.engine.step()['admission_state'], 'starting')
        self.assertEqual(self.provider.calls, [])
    def test_other_dispatcher_cannot_duplicate_admission(self):
        other = DesktopDispatcher(DesktopAdapter(self.cfg), self.provider, 'original')
        self.engine.step()
        self.assertEqual(other.step()['state'], 'blocked')
        self.assertEqual(len(self.provider.calls), 1)
    def test_only_correlated_completion_releases_next(self):
        self.adapter.handle({'id': 'b', 'body': 'second synthetic question'})
        self.engine.step()
        self.assertFalse(self.engine.complete('a', 'turn-a', thread_id='other'))
        self.assertFalse(self.engine.complete('a', 'turn-b', thread_id='original'))
        self.assertEqual(self.engine.step()['state'], 'blocked')
        self.assertTrue(self.engine.complete('a', 'turn-a', thread_id='original'))
        self.assertFalse(self.engine.complete('a', 'turn-a', thread_id='original'))
        self.assertEqual(self.engine.step()['message_id'], 'b')
    def test_mcp_read_ack_does_not_release_inflight(self):
        self.engine.step()
        self.adapter.request({'operation': 'ack', 'message_id': 'a'})
        self.assertEqual(self.engine.step()['state'], 'blocked')
    def test_incoming_binding_fields_cannot_select_thread(self):
        self.adapter.handle({'id': 'b', 'body': 'fixture', 'thread_id': 'attacker', 'meta': {'thread_id': 'attacker'}})
        self.engine.step()
        self.assertEqual(self.provider.calls[0][0], 'original')


if __name__ == '__main__':
    unittest.main()
