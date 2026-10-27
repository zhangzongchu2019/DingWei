"""Unit fixtures for persistence windows; these do not replace real CLI E2E."""
import json
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

from sessionhelper.inbox import DeliveryInbox
from sessionhelper.driver_adapter import DriverAdapter
from sessionhelper.contract import DeliveryReceipt, SessionEvent


class DispatchLifecycleTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(); self.addCleanup(self.tmp.cleanup)
        self.path = str(Path(self.tmp.name)/'inbox.sqlite')
        self.inbox = DeliveryInbox(self.path, session_name='seat')
        self.env = {'id':'source','body':'fixture','meta':{'delivery_id':'dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','delivery_ack_protocol':2,'processing_mode':'driver','ack_generation':1}}
        self.inbox.accept('dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', self.env); self.inbox.claim_next()

    def adapter(self, driver, which='codex'):
        cfg = SimpleNamespace(busy_buffer_max=10, turn_terminal_timeout_seconds=10)
        with patch.object(DriverAdapter, '_build_driver', return_value=driver):
            adapter = DriverAdapter(cfg)
        adapter._which=which;adapter._started=True;adapter.delivery_lifecycle=self.inbox
        return adapter

    def test_intent_precedes_rpc_and_exception_never_replays(self):
        calls=[]
        def deliver(did,body):
            row=self.inbox.lifecycle(did)
            self.assertEqual(row['request_id'],did)
            self.assertIsNotNone(row['dispatch_intent_at'])
            calls.append(did)
            raise OSError('connection lost after request')
        driver=SimpleNamespace(deliver=deliver,poll_receipt=lambda did: DeliveryReceipt(did,'unknown','protocol','no evidence'))
        adapter=self.adapter(driver)
        adapter.handle_delivery(self.env)
        adapter._deliver_regular_command(adapter._commands.start_next())
        self.assertEqual(self.inbox.lifecycle('dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')['state'],'admission_unknown')
        reopened=DeliveryInbox(self.path,session_name='seat')
        self.assertIsNone(reopened.claim_next())
        adapter=self.adapter(driver)
        adapter.handle_delivery(self.env)
        adapter._deliver_regular_command(adapter._commands.start_next())
        self.assertEqual(calls,['dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'])

    def test_enqueue_and_duplicate_never_fire_admission_callback(self):
        driver=SimpleNamespace()
        adapter=self.adapter(driver);seen=[]
        adapter.handle_delivery(self.env,lambda: seen.append('injected'),lambda:seen.append('admitted'))
        adapter.handle_delivery(self.env,lambda: seen.append('duplicate'))
        self.assertEqual(seen,[])
        self.assertIsNone(self.inbox.lifecycle('dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')['admitted_at'])

    def test_claude_submitted_local_uuid_is_not_admission(self):
        driver=SimpleNamespace()
        adapter=self.adapter(driver,'claude')
        adapter.handle_delivery(self.env);command=adapter._commands.start_next()
        self.inbox.begin_dispatch('dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')
        self.assertFalse(adapter._admit_from_receipt(command,DeliveryReceipt('dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','processing','pipe','submitted, awaiting ack, turn=local')))
        self.assertIsNone(self.inbox.lifecycle('dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')['admitted_at'])
        self.assertTrue(adapter._admit_from_receipt(command,DeliveryReceipt('dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','accepted','pipe','isReplay ack, turn=local')))
        self.assertEqual(self.inbox.lifecycle('dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')['turn_id'],'local')

    def test_native_terminal_preserves_turn_and_original_times(self):
        adapter=self.adapter(SimpleNamespace())
        self.inbox.begin_dispatch('dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')
        self.inbox.record_admitted('dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','turn',100)
        event=SessionEvent(kind='state_change',delivery_id='dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',turn_id='turn',data={'change':'turn_completed','status':'completed'})
        adapter._record_event_lifecycle(event)
        first=self.inbox.lifecycle('dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')
        adapter._record_event_lifecycle(event)
        after=self.inbox.lifecycle('dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')
        self.assertEqual(after,first)
        self.assertEqual(after['admitted_at'],100)
        self.assertEqual(after['turn_outcome'],'completed')
        self.assertIsNotNone(after['processed_at'])
        with self.assertRaises(ValueError):self.inbox.record_admitted('dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','other')

    def test_watchdog_event_does_not_fabricate_turn_terminal(self):
        adapter=self.adapter(SimpleNamespace())
        self.inbox.begin_dispatch('dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa');self.inbox.record_admitted('dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','turn')
        event=SessionEvent(kind='state_change',delivery_id='dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',turn_id='turn',data={'change':'turn_completed','reason':'terminal_timeout','outcome':'failed'})
        adapter._record_event_lifecycle(event)
        self.assertEqual(self.inbox.lifecycle('dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')['state'],'admitted')
        self.assertIsNone(self.inbox.lifecycle('dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')['processed_at'])

    def test_cancel_and_dispatch_have_single_atomic_winner(self):
        import threading
        for n in range(20):
            did=f'race-{n}'
            self.inbox.accept(did,self.env)
            barrier=threading.Barrier(2);result={}
            def cancel():
                barrier.wait();result['cancel']=self.inbox.cancel('source',did)
            def dispatch():
                barrier.wait();claim=self.inbox.claim_next()
                result['intent']=bool(claim and self.inbox.begin_dispatch(claim[0]))
            a=threading.Thread(target=cancel);b=threading.Thread(target=dispatch)
            a.start();b.start();a.join();b.join()
            row=self.inbox.lifecycle(did)
            if result['cancel']['result']=='cancelled':
                self.assertIsNone(row['dispatch_intent_at'])
                self.assertFalse(result['intent'])
            else:
                self.assertTrue(result['intent'])
                self.assertEqual(row['state'],'processing')
            if row['state']=='processing':self.inbox.record_admission_unknown(did)

    def test_recovery_query_does_not_dispatch_admitted_or_uncertain_rows(self):
        self.inbox.begin_dispatch('dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa');self.inbox.record_admitted('dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','original')
        reopened=DeliveryInbox(self.path,session_name='seat')
        recovered=reopened.recoverable_lifecycles()
        self.assertEqual(len(recovered),1)
        self.assertEqual(recovered[0]['turn_id'],'original')
        self.assertFalse(reopened.begin_dispatch('dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'))
        self.assertIsNone(reopened.claim_next())

    def test_restore_only_queries_bound_request_and_never_calls_deliver(self):
        self.inbox.begin_dispatch('dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')
        self.inbox.record_admission_unknown('dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')
        calls=[]
        driver=SimpleNamespace(deliver=lambda *args: self.fail('recovery dispatched again'),
            poll_receipt=lambda did: calls.append(did) or DeliveryReceipt(did,'unknown','protocol','no remote proof'))
        adapter=self.adapter(driver)
        adapter._restore_delivery_commands()
        command=adapter._commands.start_next()
        self.assertIsNotNone(command)
        adapter._deliver_regular_command(command)
        self.assertEqual(calls,['dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'])
        self.assertIsNone(self.inbox.lifecycle(calls[0])['admitted_at'])

    def test_non_durable_human_command_does_not_require_hub_ledger(self):
        driver=SimpleNamespace(deliver=lambda did,body: DeliveryReceipt(did,'done','protocol','turn=human-turn'))
        adapter=self.adapter(driver)
        adapter.handle_delivery({'id':'human','body':'local input'})
        command=adapter._commands.start_next()
        adapter._deliver_regular_command(command)
        self.assertEqual(command.state,'completed')
        self.assertIsNone(self.inbox.lifecycle(command.delivery_id))

    def test_lost_claude_pipe_recovery_isolates_without_blocking_fifo(self):
        did='dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
        self.inbox.begin_dispatch(did)
        self.inbox=DeliveryInbox(self.path,session_name='seat')
        driver=SimpleNamespace(_active=None,poll_receipt=lambda ident: DeliveryReceipt(ident,'processing','pipe','sent'))
        adapter=self.adapter(driver,'claude');adapter._restore_delivery_commands()
        command=adapter._commands.start_next();adapter._deliver_regular_command(command)
        self.assertEqual(self.inbox.lifecycle(did)['state'],'admission_unknown')
        self.assertIsNone(self.inbox.lifecycle(did)['admitted_at'])
        self.assertIsNone(adapter._commands.current)

    def test_positive_native_failure_receipt_can_recover_original_turn(self):
        did='dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
        self.inbox.begin_dispatch(did)
        self.inbox=DeliveryInbox(self.path,session_name='seat')
        store=SimpleNamespace(get=lambda ident:SimpleNamespace(turn_id='original'))
        driver=SimpleNamespace(_active=None,_store=store,poll_receipt=lambda ident:DeliveryReceipt(ident,'failed','pipe','result failed: error_max_turns'))
        adapter=self.adapter(driver,'claude');adapter._restore_delivery_commands()
        command=adapter._commands.start_next();adapter._deliver_regular_command(command)
        row=self.inbox.lifecycle(did)
        self.assertEqual((row['state'],row['turn_id'],row['turn_outcome']),('processed','original','failed'))

    def test_claude_error_result_cannot_be_persisted_as_successful_done(self):
        from sessionhelper.claudeDriver import ClaudeProtocolDriver, _TurnState
        did='dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
        driver=ClaudeProtocolDriver(store_path=str(Path(self.tmp.name)/'claude.db'))
        driver._store.upsert(did,'original','hash','accepted','isReplay ack')
        driver._active=_TurnState(envelope_id=did,turn_id='original',accepted=__import__('threading').Event(),terminal_seen=__import__('threading').Event())
        driver._handle_frame({'type':'result','subtype':'error_max_turns','is_error':True})
        receipt=driver.poll_receipt(did)
        self.assertEqual(receipt.state,'failed')
        self.assertTrue(receipt.detail.startswith('result failed:'))

    def test_claude_partial_stream_only_refreshes_progress_without_admission_or_output(self):
        from sessionhelper.claudeDriver import ClaudeProtocolDriver, _TurnState, DEFAULT_CMD
        import threading
        self.assertIn('--include-partial-messages',DEFAULT_CMD)
        driver=ClaudeProtocolDriver(store_path=str(Path(self.tmp.name)/'claude-partial.db'))
        active=_TurnState(envelope_id='input',turn_id='turn',accepted=threading.Event(),terminal_seen=threading.Event())
        driver._active=active
        before=active.last_event_at
        driver._handle_frame({'type':'stream_event','event':{'type':'content_block_delta','delta':{'type':'text_delta','text':'partial'}}})
        self.assertGreater(active.last_event_at,before)
        self.assertFalse(active.accepted.is_set())
        self.assertFalse(active.terminal_seen.is_set())
        self.assertEqual(driver._history,[])




class WatchdogQueueReleaseTest(unittest.TestCase):
    setUp = DispatchLifecycleTest.setUp
    adapter = DispatchLifecycleTest.adapter
    # Exercise real inbox/outbox transitions with the queue worker body; only
    # the never-terminal driver is a unit fixture.
    def test_unproven_claude_admission_times_out_and_advances(self):
        self.check_watchdog_release('claude', False)

    def test_admitted_turn_times_out_without_model_terminal_and_advances(self):
        self.check_watchdog_release('codex', True)

    def test_persisted_native_terminal_advances_despite_stale_poll(self):
        did='dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
        def poll(identifier):
            adapter._record_event_lifecycle(SessionEvent(kind='state_change',delivery_id=did,turn_id='native-turn',data={'change':'turn_completed','status':'completed'}))
            return DeliveryReceipt(did,'processing','fixture','turn=native-turn')
        driver=SimpleNamespace(deliver=lambda identifier,body:DeliveryReceipt(did,'processing','fixture','turn=native-turn'),poll_receipt=poll)
        adapter=self.adapter(driver)
        adapter._commands.enqueue(self.env)
        adapter._deliver_regular_command(adapter._commands.start_next())
        self.assertIsNone(adapter._commands.current,'原生终态已持久化时不得因旧poll阻塞队列')
        self.assertEqual(self.inbox.lifecycle(did)['state'],'processed')
        events=[]
        while not adapter._event_q.empty():events.append(json.loads(adapter._event_q.get_nowait()))
        self.assertFalse(any(event.get('data',{}).get('reason')=='terminal_timeout' for event in events))

    def test_historical_processed_is_not_native_terminal_evidence(self):
        did='legacy-processed'
        row={'state':'processed','turn_id':'','admitted_at':None,'processed_at':None,'turn_outcome':None}
        calls=[]
        adapter=self.adapter(SimpleNamespace(poll_receipt=lambda identifier:DeliveryReceipt(did,'processing','fixture','submitted, awaiting ack, turn=local')),'claude')
        adapter.delivery_lifecycle=SimpleNamespace(lifecycle=lambda identifier:row,record_transport_unknown=lambda identifier:calls.append(identifier))
        adapter._terminal_timeout=.01
        adapter._commands.enqueue({'id':did,'body':'fixture'})
        command=adapter._commands.start_next()
        adapter._settle_command(command,DeliveryReceipt(command.delivery_id,'processing','fixture','submitted, awaiting ack, turn=local'))
        self.assertEqual(command.state,'failed','历史processed不得冒充真实模型终态')
        self.assertEqual(calls,[command.delivery_id],'缺少原生证据须走transport unknown离场')

    def check_watchdog_release(self, which, admitted):
        first='dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'; second='dw-dlv_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb'
        second_env={'id':'next','body':'next','meta':dict(self.env['meta'],delivery_id=second)}
        self.inbox.accept(second,second_env)
        calls=[]
        def deliver(did,body):
            calls.append(did)
            return DeliveryReceipt(did,'processing' if did==first else 'done','fixture','turn=turn-first' if did==first else 'turn=turn-second')
        driver=SimpleNamespace(deliver=deliver,poll_receipt=lambda did:DeliveryReceipt(did,'processing','fixture','turn=turn-first'))
        adapter=self.adapter(driver,which);adapter._terminal_timeout=.01
        adapter._commands.enqueue(self.env);adapter._commands.enqueue(second_env)
        adapter._deliver_regular_command(adapter._commands.start_next())
        self.assertIsNone(adapter._commands.current,'watchdog未知离场必须释放当前件')
        row=self.inbox.lifecycle(first)
        self.assertIsNone(row['processed_at'],'watchdog不得造模型终态')
        self.assertEqual(row['state'],'admitted' if admitted else 'admission_unknown')
        self.assertEqual(bool(row['admitted_at']),admitted)
        saved_admission=row['admitted_at']
        chain=[item for item in self.inbox.delivery_ack_chain() if item['delivery_id']==first]
        self.assertEqual(chain[-1]['phase'],'admission_unknown')
        self.assertEqual(chain[-1]['payload']['reason'],'turn_terminal_unknown' if admitted else 'model_admission_unknown')
        self.assertFalse(any(item['phase']=='processed' for item in chain))
        self.assertEqual(self.inbox.claim_next()[0],second)
        adapter._deliver_regular_command(adapter._commands.start_next())
        self.assertEqual(calls,[first,second],'watchdog未知离场必须推进下一件')
        self.assertEqual(self.inbox.lifecycle(second)['state'],'processed')
        adapter._record_event_lifecycle(SessionEvent(kind='state_change',delivery_id=first,turn_id='turn-first',data={'change':'turn_completed','status':'completed'}))
        after=self.inbox.lifecycle(first)
        self.assertEqual(after['state'],'processed','迟到真实终态仍须补记原件')
        self.assertIsNone(after['error'],'真实终态须清除旧transport unknown诊断')
        if admitted:self.assertEqual(after['admitted_at'],saved_admission)

if __name__=='__main__':unittest.main()
