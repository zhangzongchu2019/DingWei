"""Ordered replay/confirmation tests. Protocol peers here are unit fixtures."""
import asyncio
import json
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace

from sessionhelper.inbox import DeliveryInbox
from sessionhelper.app import SessionHelper
from sessionhelper.protocol import AddressBook


class ACKOutboxTest(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup)
        self.path=str(Path(self.tmp.name)/'inbox.sqlite')
        self.inbox=DeliveryInbox(self.path,session_name='seat')
        self.did='dw-dlv_'+'a'*32
        self.env={'id':'source','body':'fixture','meta':{'delivery_id':self.did,'delivery_ack_protocol':2,'processing_mode':'driver','ack_generation':1}}
        self.inbox.accept(self.did,self.env)

    def confirm(self, phase):
        return self.inbox.confirm_delivery_ack(dict(type='delivery_ack_confirm',delivery_id=self.did,
            ack_protocol=2,processing_mode='driver',ack_generation=1,state=phase))

    def complete(self):
        self.inbox.claim_next();self.inbox.begin_dispatch(self.did)
        self.inbox.record_admitted(self.did,'original-turn',100)
        self.inbox.record_completed(self.did,'original-turn','completed',200)

    def test_outbox_exposes_only_predecessor_until_durable_confirmation(self):
        self.complete()
        chain=self.inbox.delivery_ack_chain()
        self.assertEqual([r['phase'] for r in chain],['queued','injected','processed'])
        self.assertEqual([r['phase'] for r in self.inbox.pending_delivery_acks()],['queued'])
        self.assertFalse(self.confirm('processed'))
        self.assertTrue(self.confirm('queued'))
        self.assertEqual([r['phase'] for r in self.inbox.pending_delivery_acks()],['injected'])
        self.assertFalse(self.confirm('queued'))
        self.assertTrue(self.confirm('injected'));self.assertTrue(self.confirm('processed'))
        self.assertEqual(self.inbox.pending_delivery_acks(),[])
        recovered=DeliveryInbox(self.path,session_name='seat')
        self.assertEqual(recovered.delivery_ack_chain(),[])
        self.assertEqual(recovered.lifecycle(self.did)['turn_id'],'original-turn')

    def test_restart_before_and_after_injected_confirm_preserves_original_chain(self):
        for confirm_injected in (False,True):
            with self.subTest(confirm_injected=confirm_injected):
                # Each subcase uses a fresh isolated database.
                path=str(Path(self.tmp.name)/f'case-{confirm_injected}.db')
                self.inbox=DeliveryInbox(path,session_name='seat');self.inbox.accept(self.did,self.env)
                self.complete();original=self.inbox.delivery_ack_chain()
                self.assertTrue(self.confirm('queued'))
                if confirm_injected:self.assertTrue(self.confirm('injected'))
                recovered=DeliveryInbox(path,session_name='seat')
                actual=recovered.delivery_ack_chain()
                expected=original[2:] if confirm_injected else original[1:]
                self.assertEqual([(r['phase'],r['payload']) for r in actual],[(r['phase'],r['payload']) for r in expected])
                self.assertIsNone(recovered.claim_next())

    def test_wrong_identity_confirmation_cannot_clear_any_phase(self):
        before=self.inbox.delivery_ack_chain()
        base=dict(type='delivery_ack_confirm',delivery_id=self.did,ack_protocol=2,processing_mode='driver',ack_generation=1,state='queued')
        for change in ({'ack_generation':2},{'ack_generation':True},{'processing_mode':'desktop'},{'ack_protocol':1},{'delivery_id':'other'}):
            meta=dict(base,**change)
            self.assertFalse(self.inbox.confirm_delivery_ack(meta))
            self.assertEqual(self.inbox.delivery_ack_chain(),before)

    def test_admission_unknown_is_ordered_then_positive_turn_can_advance(self):
        self.inbox.claim_next();self.inbox.begin_dispatch(self.did)
        self.inbox.record_admission_unknown(self.did)
        self.inbox.record_admitted(self.did,'known-turn',100)
        self.inbox.record_completed(self.did,'known-turn','failed',200)
        self.assertEqual([r['phase'] for r in self.inbox.delivery_ack_chain()],['queued','admission_unknown','injected','processed'])
        for phase in ('queued','admission_unknown','injected','processed'):self.assertTrue(self.confirm(phase))
        self.assertEqual(self.inbox.pending_delivery_acks(),[])

    def test_cancel_discards_unsent_phases_without_claim_or_dispatch(self):
        self.assertEqual(self.inbox.cancel('source',self.did)['result'],'cancelled')
        self.assertEqual(self.inbox.delivery_ack_chain(),[])
        self.assertIsNone(self.inbox.claim_next())
        self.assertFalse(self.inbox.begin_dispatch(self.did))

    def test_legacy_provenance_does_not_gain_v2_terminal_fields(self):
        path=str(Path(self.tmp.name)/'legacy.db')
        inbox=DeliveryInbox(path,session_name='seat')
        with inbox._connect() as db:db.execute("INSERT INTO delivery_inbox(delivery_id,session_name,envelope_json,state,received_at,updated_at) VALUES(?,'seat','{}','processed',1,1)",(self.did,))
        recovered=DeliveryInbox(path,session_name='seat')
        self.assertEqual(recovered.delivery_ack_chain(),[])
        self.assertIsNone(recovered.lifecycle(self.did)['processed_at'])

    def test_v1_wire_keeps_legacy_names_and_original_real_times(self):
        path=str(Path(self.tmp.name)/'v1.db')
        self.inbox=DeliveryInbox(path,session_name='seat');env=dict(self.env,meta=dict(self.env['meta'],delivery_ack_protocol=1))
        self.inbox.accept(self.did,env);self.complete()
        chain=self.inbox.delivery_ack_chain()
        self.assertEqual([r['payload']['state'] for r in chain],['delivered','injected','processed'])
        self.assertEqual([r['payload']['ack_protocol'] for r in chain],[1,1,1])
        self.assertFalse(self.confirm('queued'))
        self.assertEqual(len(self.inbox.delivery_ack_chain()),3)

    def test_recovery_after_intent_without_admission_adds_unknown_not_injected(self):
        self.inbox.claim_next();self.inbox.begin_dispatch(self.did)
        recovered=DeliveryInbox(self.path,session_name='seat')
        self.assertEqual(recovered.lifecycle(self.did)['state'],'admission_unknown')
        self.assertEqual([r['phase'] for r in recovered.delivery_ack_chain()],['queued','admission_unknown'])
        self.assertIsNone(recovered.claim_next())

    def test_flush_without_hub_redelivery_replays_entire_confirmed_chain(self):
        self.complete()
        helper=object.__new__(SessionHelper);helper.cfg=SimpleNamespace(mode='driver');helper.book=AddressBook('seat','fixture','bot');helper.delivery_inbox=self.inbox
        sent=[]
        class WS:
            async def send(inner,raw):
                meta=json.loads(raw)['meta'];sent.append(meta['state'])
                self.assertTrue(self.confirm(meta['state']))
        async def probe():
            task=asyncio.create_task(helper.delivery_ack_outbox_loop(WS()))
            try:
                for _ in range(30):
                    if len(sent)==3:break
                    await asyncio.sleep(.02)
                self.assertEqual(sent,['queued','injected','processed'])
            finally:
                task.cancel()
                try:await task
                except asyncio.CancelledError:pass
        asyncio.run(probe())

    def test_send_failure_never_rolls_back_execution_or_changes_replay_payload(self):
        self.complete();before=self.inbox.delivery_ack_chain()
        helper=object.__new__(SessionHelper);helper.cfg=SimpleNamespace(mode='driver');helper.book=AddressBook('seat','fixture','bot');helper.delivery_inbox=self.inbox
        class WS:
            async def send(inner,raw):raise OSError('transport disconnected')
        with self.assertRaises(OSError):asyncio.run(helper.delivery_ack_outbox_loop(WS()))
        self.assertEqual(self.inbox.delivery_ack_chain(),before)
        self.assertEqual(self.inbox.lifecycle(self.did)['state'],'processed')

    def test_confirm_control_uses_authenticated_connection_identity_and_never_injects(self):
        helper=object.__new__(SessionHelper);helper.book=AddressBook('seat','enrolled','bot');helper.delivery_inbox=self.inbox
        helper._connection_agent_id='u_fixture'
        meta=dict(type='delivery_ack_confirm',delivery_id=self.did,ack_protocol=2,processing_mode='driver',ack_generation=1,state='queued')
        class WS:
            def __aiter__(inner):
                async def frames():
                    for sender in ('peer#u_fixture','workpulse#enrolled','workpulse#u_fixture'):
                        yield json.dumps({'meta':meta,'from':sender})
                        if sender != 'workpulse#u_fixture':
                            self.assertEqual(len(self.inbox.pending_delivery_acks()),1)
                return frames()
        asyncio.run(helper.recv_loop(WS()))
        self.assertEqual(self.inbox.pending_delivery_acks(),[])



if __name__=='__main__':unittest.main()
