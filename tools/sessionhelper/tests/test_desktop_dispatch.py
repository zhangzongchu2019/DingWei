import os
import tempfile
import unittest
from types import SimpleNamespace
from sessionhelper.desktop import DesktopAdapter
from sessionhelper.desktop_dispatch import DesktopDispatcher, NativeAdmissionBusy


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
    def test_user_turn_wins_after_idle_snapshot_no_steer_or_lost_item(self):
        accepted = []
        calls = []
        def conditional_start(thread, message, body):
            calls.append(message)
            if len(calls) == 1:
                # User started a turn between the metadata check and admission.
                self.provider.idle = False
                raise NativeAdmissionBusy()
            accepted.append(message)
            return 'turn-' + message
        self.provider.start_if_idle = conditional_start
        self.assertEqual(self.engine.step()['state'], 'busy')
        self.assertEqual(accepted, [])
        self.assertEqual(self.adapter.request({'operation': 'status'})['unread'], 1)
        reopened = DesktopDispatcher(DesktopAdapter(self.cfg), self.provider, 'original')
        self.assertEqual(reopened.step()['state'], 'busy')
        self.assertEqual(calls, ['a'])
        self.provider.idle = True
        self.assertEqual(reopened.step()['message_id'], 'a')
        self.assertEqual(accepted, ['a'])
        self.assertEqual(reopened.step()['admission_state'], 'admitted')
    def test_generic_busy_error_is_uncertain_not_retry_permission(self):
        calls = []
        def ambiguous(thread, message, body):
            calls.append(message)
            raise RuntimeError('busy')
        self.provider.start_if_idle = ambiguous
        with self.assertRaises(RuntimeError):
            self.engine.step()
        reopened = DesktopDispatcher(DesktopAdapter(self.cfg), self.provider, 'original')
        self.assertEqual(reopened.step()['admission_state'], 'uncertain')
        self.assertEqual(calls, ['a'])
    def test_second_scheduler_blocked_during_conditional_busy_refusal(self):
        other = DesktopDispatcher(DesktopAdapter(self.cfg), self.provider, 'original')
        def refused(thread, message, body):
            self.assertEqual(other.step()['admission_state'], 'starting')
            raise NativeAdmissionBusy()
        self.provider.start_if_idle = refused
        self.assertEqual(self.engine.step()['state'], 'busy')
        self.provider.idle = False
        self.assertEqual(other.step()['state'], 'busy')
        with self.adapter._connect() as db:
            self.assertEqual(db.execute('SELECT count(*) FROM desktop_admission').fetchone()[0], 0)
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


    def prepare_reply(self):
        with self.adapter._connect() as db:
            db.execute("UPDATE desktop_message SET reply_to='fixture-sender' WHERE id='a'")
        self.engine.step()
        return self.engine.complete_with_reply('a', 'turn-a', thread_id='original', body='synthetic answer')
    def test_correlated_output_durable_route_and_ack(self):
        self.assertTrue(self.prepare_reply())
        self.assertFalse(self.engine.complete_with_reply('a', 'turn-a', thread_id='original', body='duplicate'))
        reopened = DesktopDispatcher(DesktopAdapter(self.cfg), self.provider, 'original')
        calls = []
        def send(payload):
            calls.append(payload)
            return {'ok': True}
        self.assertEqual(reopened.flush_reply(send)['state'], 'sent')
        self.assertEqual(calls, [{'to': 'fixture-sender', 'body': 'synthetic answer', 'meta': {'in_reply_to': 'a', 'kind': 'reply'}}])
        self.assertEqual(reopened.flush_reply(send)['state'], 'empty')
        self.assertEqual(self.adapter.request({'operation': 'status'})['unread'], 0)
    def test_unrelated_output_does_not_enter_outbox(self):
        self.engine.step()
        self.assertFalse(self.engine.complete_with_reply('a', 'wrong-turn', thread_id='original', body='private unrelated fixture'))
        self.assertFalse(self.engine.complete_with_reply('a', 'turn-a', thread_id='wrong-thread', body='fixture'))
        self.assertEqual(self.engine.flush_reply(lambda _: self.fail('must not send'))['state'], 'empty')
    def test_ambiguous_send_persists_no_retry_or_read_ack(self):
        self.prepare_reply()
        calls = []
        def fail(payload):
            calls.append(payload)
            raise TimeoutError('synthetic uncertain outcome')
        with self.assertRaises(TimeoutError):
            self.engine.flush_reply(fail)
        reopened = DesktopDispatcher(DesktopAdapter(self.cfg), self.provider, 'original')
        self.assertEqual(reopened.flush_reply(fail)['reply_state'], 'uncertain')
        self.assertEqual(len(calls), 1)
        self.assertEqual(self.adapter.request({'operation': 'status'})['unread'], 1)
    def test_pause_and_missing_durable_ack(self):
        self.prepare_reply()
        self.adapter.request({'operation': 'pause'})
        self.assertEqual(self.engine.flush_reply(lambda _: self.fail('paused'))['state'], 'paused')
        self.adapter.request({'operation': 'resume'})
        with self.assertRaises(ValueError):
            self.engine.flush_reply(lambda _: {'ok': 1})
        self.assertEqual(self.engine.flush_reply(lambda _: self.fail('uncertain'))['reply_state'], 'uncertain')
    def test_missing_original_target_keeps_admission(self):
        self.engine.step()
        with self.assertRaises(ValueError):
            self.engine.complete_with_reply('a', 'turn-a', thread_id='original', body='fixture')
        self.assertEqual(self.engine.step()['admission_state'], 'admitted')
    def test_existing_helper_write_receipt_is_not_delivery_confirmation(self):
        import asyncio
        from sessionhelper.app import SessionHelper
        self.prepare_reply()
        helper = SessionHelper.__new__(SessionHelper)
        helper.book = SimpleNamespace(key_id='fixture-owner', self_addr='fixture-helper#LOCAL')
        from sessionhelper.outbound_receipt import OutboundReceiptBroker
        helper.outbound_receipts = OutboundReceiptBroker(['fixture-helper'])
        class SyntheticWS:
            def __init__(self): self.frames = []
            async def send(self, frame): self.frames.append(frame)
        ws = SyntheticWS()
        transport_send = helper._make_send_handler(ws)
        def sender(payload):
            return asyncio.run(transport_send(payload['to'], payload['body'], payload['meta']))
        with self.assertRaisesRegex(ValueError, 'durable reply ACK missing'):
            self.engine.flush_reply(sender)
        self.assertEqual(len(ws.frames), 1)
        self.assertEqual(self.engine.flush_reply(sender)['reply_state'], 'uncertain')
        self.assertEqual(len(ws.frames), 1)
        self.assertEqual(self.adapter.request({'operation': 'status'})['unread'], 1)

    def test_reply_limits_no_state_mutation(self):
        self.engine.step()
        for body in ('', ' '*10, 'x'*65537, None):
            with self.assertRaises(ValueError):
                self.engine.complete_with_reply('a', 'turn-a', thread_id='original', body=body)
        self.assertEqual(self.engine.step()['admission_state'], 'admitted')


if __name__ == '__main__':
    unittest.main()
