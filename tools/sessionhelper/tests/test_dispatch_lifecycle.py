"""Unit fixtures for persistence windows; these do not replace real CLI E2E."""
import json
import threading
import time
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

from sessionhelper.inbox import DeliveryInbox
from sessionhelper.busy_admission import BusyAdmission
from sessionhelper.driver_adapter import DriverAdapter
from sessionhelper.contract import DeliveryReceipt, SessionEvent


class DispatchLifecycleTest(unittest.TestCase):
    def setUp(self):
        import os
        clean={k:v for k,v in os.environ.items() if not k.startswith(("SH_","DW_","DINGWEI_","DATAD_"))}
        self.env_patch=patch.dict(os.environ,clean,clear=True);self.env_patch.start();self.addCleanup(self.env_patch.stop)
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

    def test_absent_blocker_journal_is_not_reopened_on_each_idle_tick(self):
        adapter=self.adapter(SimpleNamespace())
        with patch('sessionhelper.blocker_journal.BlockerJournal.exists',return_value=False) as probe:
            adapter._restore_blocker_commands();adapter._restore_blocker_commands()
        self.assertEqual(probe.call_count,1,'空闲探测不得重建已结件inbox的WAL旁文件')
        journal=SimpleNamespace(recover=lambda:[])
        adapter._blocker_store=journal
        with patch.object(journal,'recover',return_value=[]) as recover:
            adapter._restore_blocker_commands()
        self.assertEqual(recover.call_count,1,'运行中新建journal仍须进入恢复')

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

    def test_busy_wait_persists_before_one_hub_busy_requeue_request(self):
        did='dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
        self.inbox.begin_dispatch(did)
        driver=SimpleNamespace(_busy_admission=BusyAdmission())
        adapter=self.adapter(driver)
        requested=[]
        adapter.on_busy_wait=requested.append
        self.assertTrue(driver._busy_admission.waiting(did))
        self.assertTrue(driver._busy_admission.waiting(did))
        row=self.inbox.lifecycle(did)
        self.assertIsNotNone(row['busy_wait_started_at'],'N1C_BUSY_MARKER_NOT_PERSISTED')
        self.assertIsNone(row['submission_may_have_started_at'],'N1C_BUSY_CROSSED_SUBMIT')
        self.assertEqual(requested,[did],'N1C_BUSY_ACK_NOT_ONCE')
        self.assertEqual(row['state'],'processing','N1C_BUSY_ACK_TERMINALIZED_LOCAL_INBOX')

    def test_presubmit_binding_mismatch_stays_fifo_head_even_when_driver_unhealthy(self):
        driver=SimpleNamespace(presubmit_validation_hold=lambda did:'invalid_thread_binding',
                               health=lambda:False,
                               _busy_admission=BusyAdmission())
        adapter=self.adapter(driver)
        command=SimpleNamespace(delivery_id='dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')
        self.assertEqual(adapter._busy_admission_status(command,driver),'waiting',
                         'N1C_BINDING_MISMATCH_WATCHDOG_DROPPED_FIFO_HEAD')

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
        with patch('time.time', return_value=99):
            self.inbox.begin_dispatch('dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')
        self.inbox.record_admitted('dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','turn',100)
        event=SessionEvent(kind='state_change',delivery_id='dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',turn_id='turn',data={'change':'turn_completed','status':'completed','admitted_at':100,'processed_at':__import__('time').time()})
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

    def test_failure_receipt_without_original_times_cannot_reopen_unknown(self):
        did='dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
        self.inbox.begin_dispatch(did)
        self.inbox=DeliveryInbox(self.path,session_name='seat')
        store=SimpleNamespace(get=lambda ident:SimpleNamespace(turn_id='original'))
        driver=SimpleNamespace(_active=None,_store=store,poll_receipt=lambda ident:DeliveryReceipt(ident,'failed','pipe','result failed: error_max_turns'))
        adapter=self.adapter(driver,'claude');adapter._restore_delivery_commands()
        command=adapter._commands.start_next();adapter._deliver_regular_command(command)
        row=self.inbox.lifecycle(did)
        self.assertEqual((row['state'],row['turn_id'],row['turn_outcome']),('admission_unknown',None,None))
        self.assertIsNone(row['admitted_at']);self.assertIsNone(row['processed_at'])

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
            adapter._record_event_lifecycle(SessionEvent(kind='state_change',delivery_id=did,turn_id='native-turn',data={'change':'turn_completed','status':'completed','admitted_at':self.inbox.lifecycle(did)['dispatch_intent_at'],'processed_at':__import__('time').time()}))
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

    def test_persisted_terminal_consumes_admission_callback_before_release(self):
        did='dw-dlv_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa';callbacks=[]
        driver=SimpleNamespace(poll_receipt=lambda identifier:DeliveryReceipt(did,'processing','fixture','submitted, awaiting ack, turn=native-turn'))
        adapter=self.adapter(driver,'claude')
        adapter._commands.enqueue(self.env)
        command=adapter._commands.start_next()
        self.inbox.begin_dispatch(did)
        adapter._record_event_lifecycle(SessionEvent(kind='state_change',delivery_id=did,turn_id='native-turn',data={'change':'turn_completed','status':'completed','admitted_at':self.inbox.lifecycle(did)['dispatch_intent_at'],'processed_at':__import__('time').time()}))
        adapter._admission_callbacks[did]=(lambda:callbacks.append(adapter._commands.current.delivery_id),None)
        adapter._settle_command(command,DeliveryReceipt(did,'processing','fixture','submitted, awaiting ack, turn=native-turn'))
        self.assertEqual(callbacks,[did],'持久原生证据推进前须完成admission回调')
        self.assertNotIn(did,adapter._admission_callbacks)
        self.assertIsNone(adapter._commands.current)

    def test_historical_processed_is_not_native_terminal_evidence(self):
        did='legacy-processed'
        row={'dispatch_intent_at':None,'state':'processed','turn_id':'','admitted_at':None,'processed_at':None,'turn_outcome':None}
        calls=[]
        adapter=self.adapter(SimpleNamespace(poll_receipt=lambda identifier:DeliveryReceipt(did,'processing','fixture','submitted, awaiting ack, turn=local')),'claude')
        adapter.delivery_lifecycle=SimpleNamespace(lifecycle=lambda identifier:row,record_transport_unknown=lambda identifier:calls.append(identifier),expire_admission=lambda identifier,timeout:calls.append(identifier) or True)
        adapter._terminal_timeout=.01;adapter._admission_timeout=.01
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
            if did==first and not admitted:adapter._admission_timeout=.1
            if did==second and which=='codex':
                # A done receipt alone no longer fabricates native completion.
                adapter._record_event_lifecycle(SessionEvent(kind='state_change', delivery_id=did, turn_id='turn-second',
                    data={'change':'turn_completed','status':'completed',
                          'admitted_at':self.inbox.lifecycle(did)['dispatch_intent_at'],
                          'processed_at':__import__('time').time()}))
            return DeliveryReceipt(did,'processing' if did==first else 'done','fixture','turn=turn-first' if did==first else 'turn=turn-second')
        driver=SimpleNamespace(deliver=deliver,poll_receipt=lambda did:DeliveryReceipt(did,'processing','fixture','turn=turn-first'))
        driver.abort_transport=lambda:None;driver.stop=lambda:None;driver.start=lambda:None
        adapter=self.adapter(driver,which)
        def rebuild(which):
            adapter._admission_timeout=60
            return driver
        adapter._build_driver=rebuild
        adapter._terminal_timeout=.05;adapter._admission_timeout=60
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
        if adapter._transport_recovery is not None:self.assertTrue(adapter._transport_recovery.wait(1))
        self.assertEqual(self.inbox.claim_next()[0],second)
        adapter._deliver_regular_command(adapter._commands.start_next())
        self.assertEqual(calls,[first,second],'watchdog未知离场必须推进下一件')
        self.assertEqual(self.inbox.lifecycle(second)['state'],'processed')
        adapter._record_event_lifecycle(SessionEvent(kind='state_change',delivery_id=first,turn_id='turn-first',data={'change':'turn_completed','status':'completed','admitted_at':self.inbox.lifecycle(first)['dispatch_intent_at'],'processed_at':__import__('time').time()}))
        after=self.inbox.lifecycle(first)
        self.assertEqual(after['state'],'processed','迟到真实终态仍须补记原件')
        self.assertIsNone(after['error'],'真实终态须清除旧transport unknown诊断')
        if admitted:self.assertEqual(after['admitted_at'],saved_admission)

class PresubmitRecoveryStartRetryTest(unittest.TestCase):
    setUp = DispatchLifecycleTest.setUp
    adapter = DispatchLifecycleTest.adapter

    def _run_recovery(self, failures, *, marker_set=False):
        from sessionhelper.codexDriver import JsonRpcError

        first_id = self.env['meta']['delivery_id']
        second_id = 'dw-dlv_' + 'c' * 32
        second_env = {'id': 'later', 'body': 'later', 'meta': dict(self.env['meta'], delivery_id=second_id)}
        self.inbox.begin_dispatch(first_id)
        self.inbox.mark_busy_waiting(first_id)
        if marker_set:
            self.inbox.mark_submission_may_have_started(first_id)
        self.inbox.accept(second_id, second_env)

        class OldDriver:
            transport_recovery_required = staticmethod(lambda _timeout: True)
            abort_transport = staticmethod(lambda: None)
            stop = staticmethod(lambda: None)

        class Replacement:
            def __init__(self):
                self.start_calls = 0
                self.deliveries = []
                self.turn_starts = []
                self.on_activity = None

            def start(self):
                self.start_calls += 1
                if self.start_calls <= len(failures):
                    error = failures[self.start_calls - 1]
                    raise error

            def stop(self):
                pass

            def deliver(self, delivery_id, body):
                self.deliveries.append(delivery_id)
                self.turn_starts.append(delivery_id)
                return DeliveryReceipt(delivery_id, 'processing', 'fixture', f'turn=turn-{delivery_id[-1]}')

        old = OldDriver()
        replacement = Replacement()
        adapter = self.adapter(old, 'codex')
        adapter._lane_stop.clear()
        adapter._build_driver = lambda _which: replacement
        adapter._bridge_sleep = lambda _seconds: None
        adapter._commands.enqueue(self.env)
        current = adapter._commands.start_next()
        adapter._commands.defer_current(current.delivery_id, preserve_fifo_head=True)
        adapter._commands.enqueue(second_env)
        current._delivery_worker = threading.Event()
        current._delivery_worker.set()
        adapter._terminal_timeout = 0.05

        with patch('sessionhelper.driver_adapter.PRESUBMIT_RECOVERY_BACKOFF', (0,)):
            adapter._recover_transport(current)
            recovery = adapter._transport_recovery
            thread = adapter._transport_recovery_thread
            thread.join(2)
        self.assertFalse(thread.is_alive(), 'N1C_RECOVERY_THREAD_DID_NOT_SETTLE')
        return adapter, replacement, recovery, first_id, second_id

    def test_repeated_timeout_then_initialize_rpc_error_recovers_fifo_once(self):
        from sessionhelper.codexDriver import JsonRpcError
        adapter, replacement, recovery, first_id, second_id = self._run_recovery([
            TimeoutError('synthetic recovery timeout'),
            JsonRpcError('initialize', {'code': -32603, 'message': 'synthetic temporary initialize error'}),
        ])
        self.assertTrue(recovery.is_set(), 'N1C_TRANSIENT_RECOVERY_DID_NOT_RESUME_FIFO')
        self.assertIs(adapter.driver, replacement)
        self.assertEqual(replacement.start_calls, 3, 'N1C_RECOVERY_DID_NOT_RETRY_BOTH_TRANSIENT_FAILURES')
        for expected in (first_id, second_id):
            command = adapter._commands.start_next()
            self.assertEqual(command.delivery_id, expected, 'N1C_FIFO_HEAD_ORDER_CHANGED')
            replacement.deliver(command.delivery_id, command.text)
            adapter._commands.finish(command.delivery_id, 'completed')
        self.assertEqual(replacement.turn_starts, [first_id, second_id],
                         'N1C_LATER_FIFO_ITEM_NOT_SUBMITTED_EXACTLY_ONCE')

    def test_authentication_rejection_is_not_retried_or_fifo_released(self):
        from sessionhelper.codexDriver import JsonRpcError
        adapter, replacement, recovery, first_id, second_id = self._run_recovery([
            JsonRpcError('initialize', {'code': 401, 'message': '401 Unauthorized'}),
        ])
        self.assertFalse(recovery.is_set(), 'N1C_AUTH_REJECTION_WAS_RETRIED_AS_TRANSIENT')
        self.assertEqual(replacement.start_calls, 1, 'N1C_AUTH_REJECTION_WAS_RETRIED_AS_TRANSIENT')
        head = adapter._commands.start_next()
        self.assertEqual(head.delivery_id, first_id)
        self.assertEqual(replacement.deliveries, [], 'N1C_AUTH_REJECTION_RELEASED_FIFO')

    def test_submission_marker_prevents_startup_retry(self):
        from sessionhelper.codexDriver import JsonRpcError
        adapter, replacement, recovery, first_id, _second_id = self._run_recovery([
            TimeoutError('synthetic recovery timeout'),
        ], marker_set=True)
        self.assertFalse(recovery.is_set(), 'N1C_UNKNOWN_SUBMISSION_WAS_AUTOMATICALLY_RECOVERED')
        self.assertEqual(replacement.start_calls, 1, 'N1C_UNKNOWN_SUBMISSION_WAS_AUTOMATICALLY_RECOVERED')

    def test_event_bridge_resubscribes_after_recovery_event_is_set(self):
        calls = []
        recovery = threading.Event()
        sleeps = []
        def events():
            calls.append(('subscribed', recovery.is_set()))
            return ()
        driver = SimpleNamespace(events=events,
                                 healthy=lambda: True, on_activity=None)
        adapter = self.adapter(driver, 'codex')
        adapter._transport_recovery = recovery
        def bridge_sleep(_seconds):
            sleeps.append(None)
            if len(sleeps) == 1:
                recovery.set()
            else:
                adapter._lane_stop.set()
        adapter._bridge_sleep = bridge_sleep
        adapter._event_bridge()
        self.assertEqual(calls, [('subscribed', True)], 'N1C_EVENT_BRIDGE_SUBSCRIBED_BEFORE_RECOVERY')

    def test_set_recovery_event_does_not_mask_unhealthy_driver_reconnect(self):
        recovery = threading.Event()
        recovery.set()
        driver_state = {'healthy': False, 'starts': 0, 'events': 0}
        sleeps = []
        adapter = None

        def events():
            driver_state['events'] += 1
            if driver_state['healthy']:
                adapter._lane_stop.set()
            return ()

        def start():
            driver_state['starts'] += 1
            driver_state['healthy'] = True

        driver = SimpleNamespace(events=events, healthy=lambda: driver_state['healthy'],
                                 start=start, on_activity=None)
        adapter = self.adapter(driver, 'codex')
        adapter._transport_recovery = recovery  # Set, but intentionally not cleared.

        def bridge_sleep(_seconds):
            sleeps.append(None)
            # Bound a regressed loop that only checks recovery-object presence.
            if len(sleeps) >= 3:
                adapter._lane_stop.set()

        adapter._bridge_sleep = bridge_sleep
        adapter._bridge_clock = lambda: float(len(sleeps) + 1)
        adapter._event_bridge()

        self.assertEqual(driver_state['starts'], 1,
                         'N1C_SET_RECOVERY_MASKED_UNHEALTHY_DRIVER_RECONNECT')
        self.assertTrue(driver_state['healthy'], 'N1C_SET_RECOVERY_DID_NOT_RESTORE_DRIVER')
        self.assertGreaterEqual(driver_state['events'], 2,
                                'N1C_SET_RECOVERY_EVENT_BRIDGE_DID_NOT_RESUBSCRIBE')


class AdmissionDeadlineTest(unittest.TestCase):
    setUp = DispatchLifecycleTest.setUp
    adapter = DispatchLifecycleTest.adapter

    def test_default_and_independent_environment_configuration(self):
        from sessionhelper.config import load_config
        base={'SH_SESSION_NAME':'test','SH_OWNER':'zzc','SH_KEY_ID':'fixture','SH_SECRET':'fixture'}
        self.assertEqual(load_config(base).admission_timeout_seconds,60)
        cfg=load_config(dict(base,SH_ADMISSION_TIMEOUT_SECONDS='7',SH_TURN_TERMINAL_TIMEOUT_SECONDS='900'))
        self.assertEqual((cfg.admission_timeout_seconds,cfg.turn_terminal_timeout_seconds),(7,900))

    def exercise(self, blocking):
        import threading,time
        first=self.env['meta']['delivery_id'];second='dw-dlv_deadline-next'
        next_env={'id':'next','body':'next','meta':dict(self.env['meta'],delivery_id=second)}
        self.inbox.accept(second,next_env)
        stopped=threading.Event();entered=threading.Event();calls=[];callbacks=[]
        def deliver(did,body):
            calls.append(('old',did));entered.set()
            if blocking:stopped.wait(5)
            return DeliveryReceipt(did,'processing','fixture','submitted, awaiting ack, turn=old-turn')
        old=SimpleNamespace(deliver=deliver,poll_receipt=lambda did:DeliveryReceipt(did,'processing','fixture','submitted'),
                            abort_transport=stopped.set,stop=lambda:None)
        new=SimpleNamespace(start=lambda:callbacks.append('restart'),deliver=lambda did,body:calls.append(('new',did)) or DeliveryReceipt(did,'done','fixture','turn=new-turn'))
        adapter=self.adapter(old,'claude');adapter._build_driver=lambda which:new
        adapter._admission_timeout=.1;adapter._terminal_timeout=90
        adapter.handle_delivery(self.env,on_admitted=lambda:callbacks.append('admitted'))
        adapter._commands.enqueue(next_env)
        command=adapter._commands.start_next();finished=threading.Event();errors=[]
        def run():
            try:adapter._deliver_regular_command(command)
            except Exception as exc:errors.append(exc)
            finally:finished.set()
        task=threading.Thread(target=run,daemon=True);task.start()
        on_time=finished.wait(1)
        if not on_time:
            # Bound negative-control cleanup; a missing deadline must fail an
            # assertion instead of hanging the test process for 90 seconds.
            adapter._terminal_timeout=.001;adapter._admission_timeout=.001
            stopped.set();task.join(1)
        self.assertTrue(on_time,'独立admission截止必须越过阻塞deliver')
        self.assertEqual(errors,[])
        self.assertTrue(entered.is_set())
        row=self.inbox.lifecycle(first)
        self.assertEqual(row['state'],'admission_unknown','未接纳截止必须持久化unknown')
        self.assertIsNone(row['admitted_at']);self.assertIsNone(row['processed_at'])
        self.assertTrue(adapter._transport_recovery.wait(1),'下一件派发前必须恢复CLI')
        self.assertNotIn('admitted',callbacks)
        self.inbox.claim_next()
        adapter._deliver_regular_command(adapter._commands.start_next())
        self.assertEqual(calls,[('old',first),('new',second)],'恢复后推进下一件且不得重派原件')
        self.assertEqual(self.inbox.lifecycle(second)['state'],'processed')

    def test_missing_accepted_uses_admission_deadline_and_recovers(self):self.exercise(False)
    def test_blocked_deliver_is_aborted_before_next_dispatch(self):self.exercise(True)

    def test_real_blocked_stdin_killed_before_buffered_close_and_next_dispatch(self):
        import sys,time
        from sessionhelper.claudeDriver import ClaudeProtocolDriver
        did=self.env['meta']['delivery_id']
        self.env['body']='x'*(2*1024*1024)
        driver=ClaudeProtocolDriver(store_path=str(Path(self.tmp.name)/'blocked-native.db'),cwd=self.tmp.name,
                                   cmd=[sys.executable,'-c','import time;time.sleep(20)'])
        self.addCleanup(driver.stop)
        next_calls=[]
        replacement=SimpleNamespace(start=lambda:None,deliver=lambda ident,text:next_calls.append(ident) or DeliveryReceipt(ident,'done','fixture','turn=next'))
        adapter=self.adapter(driver,'claude');adapter._build_driver=lambda which:replacement;adapter._admission_timeout=.15
        adapter._commands.enqueue(self.env)
        start=time.monotonic();adapter._deliver_regular_command(adapter._commands.start_next())
        self.assertLess(time.monotonic()-start,1,'真实阻塞stdin必须能越过admission截止')
        self.assertEqual(self.inbox.lifecycle(did)['state'],'admission_unknown')
        self.assertTrue(adapter._transport_recovery.wait(3),'须kill旧进程、写线程退出后再启动新CLI')
        self.assertIsNotNone(driver._proc.poll())
        adapter._commands.enqueue({'id':'next','body':'next'})
        adapter._deliver_regular_command(adapter._commands.start_next())
        self.assertEqual(next_calls,['dw-next'])

    def test_prior_nonterminal_wait_is_aborted_before_replacement_dispatch(self):
        import sys,threading,time
        from sessionhelper.claudeDriver import ClaudeProtocolDriver,_TurnState
        driver=ClaudeProtocolDriver(store_path=str(Path(self.tmp.name)/'previous.db'),cwd=self.tmp.name,
            cmd=[sys.executable,'-c','import time;time.sleep(20)'])
        self.addCleanup(driver.stop)
        driver.start()
        previous=_TurnState(envelope_id='previous',turn_id='previous-turn',accepted=threading.Event(),terminal_seen=threading.Event())
        previous.accepted.set();driver._active=previous
        replacement=SimpleNamespace(start=lambda:None)
        adapter=self.adapter(driver,'claude');adapter._build_driver=lambda which:replacement;adapter._admission_timeout=.15
        adapter._busy_admission_max=.15
        adapter._commands.enqueue(self.env)
        command=adapter._commands.start_next()
        worker=threading.Thread(target=lambda:adapter._deliver_regular_command(command))
        worker.start()
        deadline=time.monotonic()+2
        while self.inbox.lifecycle(command.delivery_id)['busy_wait_started_at'] is None and time.monotonic()<deadline:
            time.sleep(.005)
        self.assertIsNotNone(self.inbox.lifecycle(command.delivery_id)['busy_wait_started_at'],'等待必须持久化')
        time.sleep(.3)
        self.assertTrue(worker.is_alive(),'确定未提交的旧回合等待不得由 admission timeout 中止')
        self.assertIsNone(adapter._transport_recovery,'健康忙等待不得触发传输恢复')
        self.assertEqual(self.inbox.lifecycle(command.delivery_id)['state'],'processing')
        previous.terminal_seen.set()
        worker.join(1)
        self.assertTrue(adapter._transport_recovery.wait(3),'提交后无 ACK 应进入 unknown 并释放旧写线程')
        self.assertEqual(self.inbox.lifecycle(command.delivery_id)['state'],'admission_unknown')
        self.assertIs(adapter.driver,replacement)

    def test_clock_includes_time_since_persisted_dispatch_intent(self):
        calls=[]
        driver=SimpleNamespace(deliver=lambda did,body:calls.append(did) or DeliveryReceipt(did,'unknown','fixture','no admission'),
            poll_receipt=lambda did:DeliveryReceipt(did,'unknown','fixture','no admission'),abort_transport=lambda:None,stop=lambda:None,start=lambda:None)
        adapter=self.adapter(driver,'claude');adapter._admission_timeout=.1;adapter._build_driver=lambda which:driver
        original=self.inbox.begin_dispatch
        clock={'wall':1000.0,'monotonic':200.0}
        def delayed_intent(did):
            won=original(did)
            # Simulate time spent after the real SQLite intent commit, before
            # deliver is considered. No scheduler-dependent sleep or duration.
            clock['wall']+=1.0;clock['monotonic']+=1.0
            return won
        self.inbox.begin_dispatch=delayed_intent
        adapter._commands.enqueue(self.env)
        command=adapter._commands.start_next()
        with patch('time.time',side_effect=lambda:clock['wall']), patch('time.monotonic',side_effect=lambda:clock['monotonic']):
            adapter._deliver_regular_command(command)
            self.assertEqual(calls,[],'PERSISTED_INTENT_DEADLINE_RESET: expired intent must not reach deliver')
            row=self.inbox.lifecycle(command.delivery_id)
            self.assertEqual(row['dispatch_intent_at'],1000.0,'fixture must persist the original intent clock')
            self.assertEqual(adapter._admission_elapsed(command),1.0,'deadline must include the time since persisted intent')
            self.assertEqual(row['state'],'admission_unknown','admission截止须自intent落盘起计')
            self.assertTrue(adapter._transport_recovery.wait(1),'transport recovery must finish before clock patches leave scope')

    def test_failed_recovery_quarantines_following_pipe_writes(self):
        did=self.env['meta']['delivery_id'];calls=[]
        def deliver(*args):
            calls.append(args)
            # Start with a safe dispatch budget, shorten only once the writer
            # was entered; filesystem speed cannot turn this into zero calls.
            adapter._admission_timeout=.01
            return DeliveryReceipt(did,'processing','fixture','submitted')
        driver=SimpleNamespace(deliver=deliver,poll_receipt=lambda did:DeliveryReceipt(did,'processing','fixture','submitted'))
        adapter=self.adapter(driver,'claude');adapter._admission_timeout=60
        adapter._commands.enqueue(self.env);adapter._commands.enqueue({'id':'next','body':'next'})
        adapter._deliver_regular_command(adapter._commands.start_next())
        adapter._deliver_regular_command(adapter._commands.start_next())
        self.assertEqual(len(calls),1,'CLI未恢复时不得复用旧stdin')
        self.assertEqual(len(adapter._commands.pending),1)
        self.assertIsNone(adapter._commands.current)



class AdmissionDeadlineRaceTest(unittest.TestCase):
    setUp=DispatchLifecycleTest.setUp
    adapter=DispatchLifecycleTest.adapter

    def exercise_order(self,mode):
        import time,threading
        did=self.env['meta']['delivery_id'];count=0;stop=threading.Event();calls=[]
        def deliver(ident,body):
            calls.append(ident);stop.wait(.30)
            return DeliveryReceipt(ident,'accepted','fixture','isReplay ack, turn=original')
        def poll(ident):
            nonlocal count
            count+=1
            if mode=='late_poll':time.sleep(.12)
            if mode=='late_deliver':return DeliveryReceipt(ident,'processing','fixture','submitted')
            if mode=='early_admitted' and count==2:time.sleep(.12)
            state='done' if mode=='early_admitted' and count>2 else 'accepted'
            return DeliveryReceipt(ident,state,'fixture','isReplay ack, turn=original')
        old=SimpleNamespace(deliver=deliver,poll_receipt=poll,abort_transport=stop.set,stop=lambda:None)
        new=SimpleNamespace(start=lambda:None)
        adapter=self.adapter(old,'claude');adapter._admission_timeout=.10;adapter._terminal_timeout=2;adapter._build_driver=lambda which:new
        adapter._commands.enqueue(self.env);adapter._deliver_regular_command(adapter._commands.start_next())
        row=self.inbox.lifecycle(did);phases=[x['phase'] for x in self.inbox.delivery_ack_chain() if x['delivery_id']==did]
        if mode=='early_admitted':
            self.assertEqual(phases,['queued','injected','processed'],'截止前admitted不得再产生admission unknown')
            self.assertEqual(row['state'],'processed')
        else:
            self.assertEqual(phases,['queued','admission_unknown'],'截止后accepted-only不得写injected或造成ACK矛盾')
            self.assertEqual(row['state'],'admission_unknown')
            self.assertIsNone(row['admitted_at']);self.assertIsNone(row['processed_at'])
            # Direct receipt re-observation is also fenced by the durable row.
            self.assertFalse(adapter._admit_from_receipt(adapter._commands._by_delivery[did],DeliveryReceipt(did,'accepted','fixture','turn=original')))
            after_phases=[x['phase'] for x in self.inbox.delivery_ack_chain() if x['delivery_id']==did]
            self.assertEqual(after_phases,phases,'截止后accepted-only不得写injected或造成ACK矛盾')
            self.assertEqual(self.inbox.lifecycle(did)['state'],'admission_unknown','截止后accepted-only不得重开admission')
        if adapter._transport_recovery is not None:self.assertTrue(adapter._transport_recovery.wait(1))
        stop.set()
        self.assertEqual(calls,[did],'截止或迟到回执不得重派原件')

    def test_slow_poll_accepted_after_deadline_is_not_admission(self):self.exercise_order('late_poll')
    def test_blocked_deliver_accepted_after_deadline_is_not_admission(self):self.exercise_order('late_deliver')
    def test_admission_committed_before_deadline_wins(self):self.exercise_order('early_admitted')

    def test_unknown_terminal_without_original_times_is_evidence_only(self):
        did=self.env['meta']['delivery_id']
        with patch('time.time', return_value=122):self.inbox.begin_dispatch(did)
        self.inbox.expire_admission(did,0)
        adapter=self.adapter(SimpleNamespace(),'claude')
        before=self.inbox.lifecycle(did)
        for extra in ({},{'admitted_at':123},{'processed_at':124}):
            adapter._record_event_lifecycle(SessionEvent(kind='state_change',delivery_id=did,turn_id='original',data=dict(change='turn_completed',status='completed',**extra)))
            self.assertEqual(self.inbox.lifecycle(did),before,'缺原始双时刻的终态不得补造完整证据')
            self.assertEqual([x['phase'] for x in self.inbox.delivery_ack_chain()],['queued','admission_unknown'])
        adapter._record_event_lifecycle(SessionEvent(kind='state_change',delivery_id=did,turn_id='original',data=dict(change='turn_completed',status='completed',admitted_at=123,processed_at=124)))
        after=self.inbox.lifecycle(did)
        self.assertEqual((after['state'],after['admitted_at'],after['processed_at']),('processed',123,124),'完整原始双时刻的终态可补账')

    def test_unknown_requires_complete_native_evidence_to_reconcile_atomically(self):
        did=self.env['meta']['delivery_id']
        with patch('time.time', return_value=122):self.inbox.begin_dispatch(did)
        self.assertTrue(self.inbox.expire_admission(did,0))
        adapter=self.adapter(SimpleNamespace(),'claude')
        adapter._commands.enqueue(self.env);command=adapter._commands.start_next()
        self.assertFalse(adapter._admit_from_receipt(command,DeliveryReceipt(did,'accepted','fixture','turn=original')))
        self.assertEqual(self.inbox.lifecycle(did)['state'],'admission_unknown')
        for fields in [('original',None,124,'completed'),('original',123,None,'completed'),('',123,124,'completed'),('original',123,124,'unknown')]:
            with self.assertRaises(ValueError):self.inbox.record_native_terminal(did,*fields)
        self.inbox.record_native_terminal(did,'original',123,124,'completed')
        row=self.inbox.lifecycle(did)
        self.assertEqual((row['state'],row['turn_id'],row['admitted_at'],row['processed_at'],row['turn_outcome']),('processed','original',123,124,'completed'))
        phases=[x['phase'] for x in self.inbox.delivery_ack_chain() if x['delivery_id']==did]
        self.assertEqual(phases,['queued','admission_unknown','injected','processed'],'完整原生证据须原子补账并保留原时刻')
        self.inbox.record_native_terminal(did,'original',200,201,'completed')
        self.assertEqual(self.inbox.lifecycle(did),row,'重放完整证据不得改写原时刻')

class NativeEvidenceTimeTest(unittest.TestCase):
    setUp=DispatchLifecycleTest.setUp

    def test_claude_times_are_first_native_receipt_and_result_observations(self):
        import threading
        from sessionhelper.claudeDriver import ClaudeProtocolDriver,_TurnState
        did=self.env['meta']['delivery_id'];driver=ClaudeProtocolDriver(store_path=str(Path(self.tmp.name)/'native-time.db'))
        driver._store.upsert(did,'turn','hash','processing','sent')
        driver._active=_TurnState(envelope_id=did,turn_id='turn',accepted=threading.Event(),terminal_seen=threading.Event())
        with patch('time.time',return_value=101):driver._handle_frame({'type':'user','isReplay':True})
        with patch('time.time',return_value=103):driver._handle_frame({'type':'user','isReplay':True})
        with patch('time.time',return_value=105):driver._handle_frame({'type':'result','subtype':'success'})
        event=driver._history[-1]
        self.assertEqual((event.data['admitted_at'],event.data['processed_at']),(101,105),'原生时刻在wire观察边界固化且重放不重置')

    def test_codex_times_require_original_started_observation(self):
        from sessionhelper.codexDriver import CodexProtocolDriver
        driver=CodexProtocolDriver(store_path=str(Path(self.tmp.name)/'codex-time.db'),workdir=self.tmp.name,transport_mode='direct')
        driver._thread_id='thread';driver._remember_delivery('delivery','turn','processing')
        with patch('time.time',return_value=101):driver._map_notification({'method':'turn/started','params':{'turn':{'id':'turn'}}})
        with patch('time.time',return_value=103):driver._map_notification({'method':'turn/started','params':{'turn':{'id':'turn'}}})
        with patch('time.time',return_value=105):
            event=driver._map_notification({'method':'turn/completed','params':{'turn':{'id':'turn','status':'completed'}}})
            missing=driver._map_notification({'method':'turn/completed','params':{'turn':{'id':'unknown','status':'completed'}}})
        self.assertEqual((event.data['admitted_at'],event.data['processed_at']),(101,105))
        self.assertIsNone(missing.data['admitted_at'],'缺原始接纳观察不得在terminal补造')


class MixedPrecisionGuardTest(unittest.TestCase):
    """The helper must not commit a terminal ACK that the Hub rejects."""
    setUp = DispatchLifecycleTest.setUp

    def original_admission(self, *, confirmed=False, at=5000.875):
        did = self.env['meta']['delivery_id']
        with patch('time.time', return_value=5000.8):
            self.inbox.begin_dispatch(did)
        with patch('time.time', return_value=at):
            self.inbox.record_admitted_before_deadline(did, 'original', 60, at)
        original = {row['phase']:row['payload_json'] for row in self.inbox.delivery_ack_chain()}
        if confirmed:
            for phase in ('queued', 'injected'):
                head = self.inbox.pending_delivery_acks()[0]['payload']
                self.assertEqual(head['state'], phase)
                self.assertTrue(self.inbox.confirm_delivery_ack(dict(head, type='delivery_ack_confirm')))
        return did, original

    def terminal(self, did, completed=5000):
        row = self.inbox.lifecycle(did)
        context = {key:row[key] for key in ('ack_generation','dispatch_intent_at','request_id')}
        with patch('time.time', return_value=5002):
            return self.inbox.record_native_terminal(did, 'original', 5000, completed,
                'completed', expected_context=context)

    def test_mixed_precision_blocks_pending_and_confirmed_injected_ack(self):
        for confirmed in (False, True):
            with self.subTest(confirmed=confirmed):
                if confirmed:
                    self.inbox = DeliveryInbox(str(Path(self.tmp.name)/'confirmed.sqlite'),session_name='seat')
                    self.inbox.accept(self.env['meta']['delivery_id'],self.env);self.inbox.claim_next()
                did, original = self.original_admission(confirmed=confirmed)
                before = dict(self.inbox.lifecycle(did))
                self.assertIs(self.terminal(did),False,'MIXED_PRECISION_MUST_REMAIN_PENDING')
                row = self.inbox.lifecycle(did)
                self.assertEqual(row['state'],'admitted','MIXED_PRECISION_MUST_REMAIN_PENDING')
                self.assertEqual(row['error'],'native_terminal_precedes_acked_injected')
                self.assertEqual((row['admitted_at'],row['processed_at'],row['turn_outcome']),
                    (before['admitted_at'],None,None),'GUARD_REWROTE_ORIGINAL_TIME')
                chain={a['phase']:a['payload_json'] for a in self.inbox.delivery_ack_chain()}
                self.assertNotIn('processed',chain,'REJECTABLE_PROCESSED_ACK_CREATED')
                if not confirmed:self.assertEqual(chain,original,'INJECTED_ACK_ORIGINAL_REWRITTEN')
                else:self.assertFalse(chain)

    def test_pending_survives_restart_and_identical_evidence_without_new_ack(self):
        did, _ = self.original_admission(confirmed=True)
        self.assertIs(self.terminal(did),False)
        self.inbox=DeliveryInbox(self.path,session_name='seat')
        for _ in range(3):
            self.assertIs(self.terminal(did),False)
        row=self.inbox.lifecycle(did)
        self.assertEqual((row['state'],row['error']),('admitted','native_terminal_precedes_acked_injected'))
        self.assertIsNone(row['processed_at'])
        self.assertEqual(self.inbox.pending_delivery_acks(),[],'PENDING_CREATED_UNCONFIRMABLE_ACK_RETRY')
        self.assertIsNone(self.inbox.claim_next(),'PENDING_DELIVERY_WAS_RESUBMITTED')

    def test_consistent_original_terminal_can_close_pending(self):
        did, original = self.original_admission(confirmed=True)
        self.assertIs(self.terminal(did),False)
        self.assertIsNone(self.terminal(did,5001))
        row=self.inbox.lifecycle(did)
        self.assertEqual((row['state'],row['processed_at'],row['error']),('processed',5001,None))
        heads=self.inbox.pending_delivery_acks()
        self.assertEqual([a['phase'] for a in heads],['processed'])
        self.assertEqual(heads[0]['payload']['occurred_at'],'1970-01-01T01:23:21.000000Z')
        self.assertNotIn('injected',{a['phase'] for a in heads},'CONFIRMED_ACK_WAS_REWRITTEN')

    def test_compare_serialized_wire_time_not_unrounded_float(self):
        did, _ = self.original_admission(confirmed=True,at=5000.8750004)
        self.assertIsNone(self.terminal(did,5000.8750003),'WIRE_EQUAL_TIME_FALSELY_BLOCKED')
        self.assertEqual(self.inbox.lifecycle(did)['state'],'processed')
        self.assertEqual(self.inbox.pending_delivery_acks()[0]['payload']['occurred_at'],
            '1970-01-01T01:23:20.875000Z')

    def test_pending_wire_timestamp_is_authoritative_over_inbox_value(self):
        did, original = self.original_admission()
        with self.inbox._lock, self.inbox._connect() as db:
            db.execute('UPDATE delivery_inbox SET admitted_at=? WHERE session_name=? AND delivery_id=?',
                (5000.25,'seat',did))
        self.assertIs(self.terminal(did,5000.5),False,'PENDING_ACK_ORIGINAL_TIME_IGNORED')
        self.assertEqual({a['phase']:a['payload_json'] for a in self.inbox.delivery_ack_chain()},original)
        self.assertEqual(self.inbox.lifecycle(did)['state'],'admitted')




class BusyAdmissionTest(unittest.TestCase):
    """真实 driver 串行门＋adapter＋临时 inbox；只替换原生传输及虚拟时钟。"""
    def setUp(self):
        import os
        import time
        import threading
        self.real_time = time
        self.threading = threading
        self.tmp = tempfile.TemporaryDirectory(prefix='ba-', dir='/tmp')
        self.addCleanup(self.tmp.cleanup)
        clean={k:v for k,v in os.environ.items() if not k.startswith(('SH_','DW_','DINGWEI_','DATAD_'))}
        self.env_patch=patch.dict(os.environ,clean,clear=True);self.env_patch.start();self.addCleanup(self.env_patch.stop)
        self.now = time.monotonic()
        self.wall = time.time()
        self.offset = 0.0
        self.clock = SimpleNamespace(monotonic=lambda:self.now+self.offset,
                                     time=lambda:self.wall+self.offset, sleep=lambda _: time.sleep(.002))
        wall_patch=patch("time.time",lambda:self.wall+self.offset)
        wall_patch.start();self.addCleanup(wall_patch.stop)
        self.busy = threading.Event(); self.busy.set()
        self.healthy = True
        self.writes = []
        self.results = []
        self.errors = []
        self.did = 'dw-dlv_'+'b'*32
        self.env = {'body':'synthetic busy input','meta':{'delivery_id':self.did,
            'delivery_ack_protocol':2,'processing_mode':'driver','ack_generation':1}}
        self.inbox=DeliveryInbox(str(Path(self.tmp.name)/'inbox.db'),session_name='fixture')
        self.inbox.accept(self.did,self.env);self.inbox.claim_next();self.inbox.begin_dispatch(self.did)

    def prepare(self, which):
        import threading
        if which == 'codex':
            from sessionhelper.codexDriver import CodexProtocolDriver
            driver=CodexProtocolDriver(workdir=self.tmp.name,store_path=str(Path(self.tmp.name)/'driver.db'))
            driver._thread_id=driver._session_id='fixture-root';driver._initialized=True
            driver._active_turn_id='old-turn'
            def request(method,params,timeout=0):
                if method=='turn/start':
                    driver._turn_start_writing(params)
                    self.writes.append(params)
                    return {'turn':{'id':'new-turn','status':'inProgress'}}
                if method in ('thread/read','thread/resume'):
                    return {'thread':{'id':'fixture-root','cwd':self.tmp.name,'status':{'type':'active'},'canAcceptDirectInput':True}}
                if method=='thread/turns/list':return {'data':[]}
                return {}
            driver._rpc=SimpleNamespace(healthy=lambda:self.healthy,request=request)
            driver._read_turn_state=lambda _:('processing','busy') if self.busy.is_set() else ('done','complete')
            time_module='sessionhelper.codexDriver.time'
        else:
            from sessionhelper.claudeDriver import ClaudeProtocolDriver, _TurnState
            driver=ClaudeProtocolDriver(cmd=['unused'],cwd=self.tmp.name,store_path=str(Path(self.tmp.name)/'driver.db'))
            self.old=driver._active=_TurnState(envelope_id='old',turn_id='old-turn',accepted=threading.Event(),terminal_seen=threading.Event())
            def write(frame):
                self.writes.append(frame)
                driver._active.accepted.set()
            driver._proc=SimpleNamespace(poll=lambda:None if self.healthy else 1,stdin=SimpleNamespace(write=write,flush=lambda:None))
            driver._reader_thread=SimpleNamespace(is_alive=lambda:True)
            time_module='sessionhelper.claudeDriver.time'
        self.driver=driver
        self.addCleanup(self.release)
        for module in ('sessionhelper.driver_adapter.time',time_module):
            timer=patch(module,self.clock);timer.start();self.addCleanup(timer.stop)
        cfg=SimpleNamespace(busy_buffer_max=5,turn_terminal_timeout_seconds=1800,admission_timeout_seconds=60,busy_admission_max_seconds=1800)
        self.adapter=DriverAdapter(cfg,driver=driver);self.adapter._which=which;self.adapter._started=True
        self.adapter.delivery_lifecycle=self.inbox
        self.recoveries=[];self.adapter._recover_transport=lambda cmd, **kwargs:self.recoveries.append(cmd.delivery_id)
        self.adapter.handle_delivery(self.env)
        self.command=self.adapter._commands.start_next()
        def run():
            try:self.results.append(self.adapter._bounded_deliver(self.command))
            except Exception as exc:self.errors.append(exc)
        self.worker=threading.Thread(target=run);self.worker.start()
        self.addCleanup(lambda:self.worker.join(2))
        self.until(lambda:hasattr(self.command,'_busy_wait_event_at'),'BA_WAIT_NOT_OBSERVED')

    def until(self, check, marker):
        deadline=self.real_time.monotonic()+2
        while not check() and self.real_time.monotonic()<deadline:self.real_time.sleep(.005)
        self.assertTrue(check(),marker)

    def release(self):
        self.busy.clear()
        if hasattr(self,'old'):self.old.terminal_seen.set()

    def events(self):
        rows=[]
        while not self.adapter._event_q.empty():rows.append(json.loads(self.adapter._event_q.get_nowait()))
        return rows

    def busy90(self,which):
        self.prepare(which)
        self.offset=90
        self.real_time.sleep(.06)
        self.assertTrue(self.worker.is_alive(),'BA_BUSY90_EARLY_UNKNOWN')
        self.assertEqual(self.inbox.lifecycle(self.did)['state'],'processing','BA_BUSY90_EARLY_UNKNOWN')
        self.assertEqual(self.writes,[],'BA_BUSY90_SUBMITTED_WHILE_BUSY')
        self.assertEqual(self.recoveries,[],'BA_HEALTHY_TRANSPORT_REPLACED')
        events=[e for e in self.events() if e.get('data',{}).get('change')=='delivery_waiting_agent_idle']
        self.assertTrue(events,'BA_WAIT_EVENT_MISSING')
        self.assertTrue(all(e['data']['lifecycle']=='waiting' and e['delivery_id']==self.did and e['data']['status_text']=='排队等待 Agent 空闲' for e in events),'BA_WAIT_EVENT_INVALID')
        self.release();self.worker.join(2)
        self.assertFalse(self.worker.is_alive(),'BA_BUSY90_NOT_ADMITTED')
        self.assertEqual(self.errors,[],'BA_WORKER_ERROR')
        self.assertIsNotNone(self.results[0],'BA_BUSY90_NOT_ADMITTED')
        self.assertTrue(self.adapter._admit_from_receipt(self.command,self.results[0]),'BA_ADMISSION_DEADLINE_NOT_EXTENDED')
        self.assertEqual(self.inbox.lifecycle(self.did)['state'],'admitted','BA_ADMISSION_DEADLINE_NOT_EXTENDED')
        self.driver.deliver(self.did,'synthetic busy input')
        self.assertEqual(len(self.writes),1,'BA_DUPLICATE_SUBMISSION')

    def cap(self,which):
        import io
        from contextlib import redirect_stdout
        self.prepare(which)
        self.offset=1799
        self.real_time.sleep(.06)
        self.assertTrue(self.worker.is_alive(),'BA_LIMIT_EARLY_UNKNOWN')
        self.assertEqual(self.inbox.lifecycle(self.did)['state'],'processing','BA_LIMIT_EARLY_UNKNOWN')
        output=io.StringIO()
        with redirect_stdout(output):
            self.offset=1801
            self.real_time.sleep(.06)
        self.assertTrue(self.worker.is_alive(),'BA_LIMIT_STILL_TERMINATED_WAITER')
        self.assertEqual(self.errors,[])
        self.assertEqual(self.inbox.lifecycle(self.did)['state'],'processing','BA_LIMIT_BECAME_UNKNOWN')
        self.assertIsNotNone(self.inbox.lifecycle(self.did)['busy_wait_started_at'],'BA_WAIT_NOT_DURABLE')
        self.assertIsNone(self.inbox.lifecycle(self.did)['submission_may_have_started_at'],'BA_WAIT_CROSSED_BOUNDARY')
        self.assertIn('[delivery][ALERT][BUSY_ADMISSION_LONG_WAIT] id='+self.did+' driver='+which,output.getvalue(),'BA_LONG_WAIT_WARNING_MISSING')
        self.release();self.worker.join(2)
        self.assertFalse(self.worker.is_alive(),'BA_BUSY_DID_NOT_RESUME')
        self.assertEqual(len(self.writes),1,'BA_RESUMED_SUBMISSION_MISSING')
        self.assertEqual(self.recoveries,[],'BA_LIMIT_REPLACED_HEALTHY_TRANSPORT')

    def test_restart_recovers_proven_busy_head_before_later_delivery(self):
        first=self.did;second='dw-dlv_'+'c'*32
        self.inbox.accept(second,dict(self.env,meta=dict(self.env['meta'],delivery_id=second)))
        self.inbox.mark_busy_waiting(first)
        reopened=DeliveryInbox(str(Path(self.tmp.name)/'inbox.db'),session_name='fixture')
        self.assertEqual(reopened.claim_next()[0],first,'BA_RESTART_SKIPPED_BUSY_HEAD')
        self.assertEqual(reopened.claim_next()[0],second,'BA_TEST_REQUIRES_ADAPTER_FIFO')

    def unhealthy(self,which):
        self.prepare(which);self.healthy=False;self.offset=90
        self.worker.join(2)
        self.assertFalse(self.worker.is_alive(),'BA_UNHEALTHY_WAIT_EXTENDED')
        self.assertEqual(self.writes,[],'BA_UNHEALTHY_SUBMISSION')
        if which == 'claude' and self.results and self.results[0] is not None:
            self.adapter._settle_command(self.command, self.results[0])
        if which=='codex':
            self.assertEqual(self.recoveries,[self.did],'BA_BAD_TRANSPORT_NOT_RECOVERED')
            self.assertEqual(self.command.state,'queued','BA_UNSENT_TRANSPORT_NOT_DEFERRED')
        else:
            self.assertEqual(self.inbox.lifecycle(self.did)['state'],'admission_unknown','BA_BAD_TRANSPORT_NOT_UNKNOWN')

    def test_codex_busy90_once(self):self.busy90('codex')
    def test_claude_busy90_once(self):self.busy90('claude')
    def test_codex_limit_warns_without_cancelling_late_submission(self):self.cap('codex')
    def test_claude_limit_warns_without_cancelling_late_submission(self):self.cap('claude')
    def test_codex_bad_transport_old_handling(self):self.unhealthy('codex')
    def test_claude_bad_transport_old_handling(self):self.unhealthy('claude')

    def test_submission_boundary_cannot_be_cancelled(self):
        from sessionhelper.busy_admission import BusyAdmission
        gate=BusyAdmission();gate.waiting(self.did)
        self.assertTrue(gate.before_submit(self.did))
        self.assertFalse(gate.cancel_waiting(self.did),'BA_POSSIBLE_SUBMISSION_CANCELLED')
        self.assertFalse(gate.is_waiting(self.did),'BA_POSSIBLE_SUBMISSION_REQUEUED')
        gate.waiting(self.did)
        self.assertFalse(gate.is_waiting(self.did),'BA_SUBMISSION_REOPENED')

    def test_cancelled_waiter_cannot_cross_submission_boundary(self):
        from sessionhelper.busy_admission import BusyAdmission
        gate=BusyAdmission();gate.waiting(self.did)
        self.assertTrue(gate.cancel_waiting(self.did))
        self.assertFalse(gate.before_submit(self.did),'BA_CANCELLED_CROSSED_BOUNDARY')

    def test_busy_configuration_default_and_override(self):
        from sessionhelper.config import load_config
        base={'SH_SESSION_NAME':'fixture','SH_KEY_ID':'fixture','SH_SECRET':'synthetic'}
        self.assertEqual(load_config(base).busy_admission_max_seconds,1800,'BA_DEFAULT_LIMIT')
        self.assertEqual(load_config(dict(base,SH_BUSY_ADMISSION_MAX_SECONDS='2400')).busy_admission_max_seconds,2400,'BA_CONFIG_LIMIT_IGNORED')

    def exemption_probe(self,which,*,submitted=None,healthy=True,stuck=False):
        from sessionhelper.busy_admission import BusyAdmission
        gate=BusyAdmission();gate.waiting(self.did)
        driver=SimpleNamespace(_busy_admission=gate,submission_started=lambda _:submitted,
            health=lambda:healthy,transport_recovery_required=lambda _:stuck)
        adapter=DriverAdapter(SimpleNamespace(busy_buffer_max=5,turn_terminal_timeout_seconds=1800),driver=driver)
        adapter._which=which
        command=SimpleNamespace(delivery_id=self.did,text='synthetic',queue_seq=1)
        return adapter._busy_admission_status(command,driver)

    def test_possible_submission_has_no_busy_exemption(self):
        for which in ('codex','claude'):
            with self.subTest(driver=which):
                self.assertIsNone(self.exemption_probe(which,submitted=self.now-61),'BA_POSSIBLE_SUBMISSION_EXTENDED')

    def test_codex_stuck_writer_has_no_busy_exemption(self):
        self.assertIsNone(self.exemption_probe('codex',stuck=True),'BA_STUCK_WRITER_EXTENDED')

    def test_claude_stuck_writer_has_no_busy_exemption(self):
        self.assertIsNone(self.exemption_probe('claude',stuck=True),'BA_STUCK_WRITER_EXTENDED')

    def test_disconnected_transport_has_no_busy_exemption(self):
        for which in ('codex','claude'):
            with self.subTest(driver=which):
                self.assertIsNone(self.exemption_probe(which,healthy=False),'BA_DISCONNECTED_EXTENDED')


class FakeBusyDriver:
    def __init__(self):
        self._busy_admission = BusyAdmission()
        self.busy = threading.Event()
        self.busy.set()
        self.writes = []
        self._submitted = {}
        self._lock = threading.Lock()
        self._health = True

    def health(self):
        return self._health

    def submission_started(self, delivery_id):
        return self._submitted.get(delivery_id)

    def transport_recovery_required(self, _timeout):
        return False

    def poll_receipt(self, delivery_id):
        return DeliveryReceipt(delivery_id, 'unknown', 'fixture', 'manual reconciliation required')

    def deliver(self, delivery_id, _body):
        self._busy_admission.waiting(delivery_id)
        while self.busy.wait(.01):
            pass
        if not self._busy_admission.before_submit(delivery_id):
            return DeliveryReceipt(delivery_id, 'unknown', 'fixture', 'lifecycle CAS refused')
        with self._lock:
            self._submitted[delivery_id] = time.monotonic()
            self.writes.append(delivery_id)
        return DeliveryReceipt(delivery_id, 'accepted', 'fixture', 'isReplay ack, turn=fixture-turn')


class BusyWaitRecoveryTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='busy-wait-', dir='/tmp')
        self.addCleanup(self.tmp.cleanup)
        self.path = str(Path(self.tmp.name) / 'inbox.sqlite')
        self.session = 'fixture'
        self.first = 'dw-dlv_' + 'a' * 32
        self.second = 'dw-dlv_' + 'b' * 32
        self.env1 = {'from': 'sender', 'body': 'first', 'meta': {
            'delivery_id': self.first, 'delivery_ack_protocol': 2,
            'processing_mode': 'driver', 'ack_generation': 1}}
        self.env2 = {'from': 'sender', 'body': 'second', 'meta': {
            'delivery_id': self.second, 'delivery_ack_protocol': 2,
            'processing_mode': 'driver', 'ack_generation': 1}}
        self.inbox = DeliveryInbox(self.path, session_name=self.session, capacity=5)
        self.inbox.accept(self.first, self.env1)
        self.inbox.accept(self.second, self.env2)
        self.assertEqual(self.inbox.claim_next()[0], self.first)
        self.assertTrue(self.inbox.begin_dispatch(self.first))
        self.assertTrue(self.inbox.mark_busy_waiting(self.first))

    def test_restart_restores_busy_head_fifo_and_writer_runs_once_after_release(self):
        # A fresh inbox instance models helper restart. The marker is sufficient
        # to restore the same durable head; dispatch_intent alone is not used.
        inbox = DeliveryInbox(self.path, session_name=self.session, capacity=5)
        row = inbox.lifecycle(self.first)
        self.assertEqual(row['state'], 'received')
        self.assertIsNotNone(row['dispatch_intent_at'])
        self.assertIsNotNone(row['busy_wait_started_at'])
        self.assertIsNone(row['submission_may_have_started_at'])
        self.assertEqual(inbox.lifecycle(self.second)['state'], 'received')

        driver = FakeBusyDriver()
        adapter = DriverAdapter(SimpleNamespace(busy_buffer_max=5, turn_terminal_timeout_seconds=30,
                                                 admission_timeout_seconds=.1, busy_admission_max_seconds=.02),
                                driver=driver)
        adapter._which = 'claude'
        adapter.delivery_lifecycle = inbox
        adapter._restore_delivery_commands()
        head = adapter._commands.start_next()
        self.assertEqual(head.delivery_id, self.first)
        self.assertEqual([item.delivery_id for item in adapter._commands.pending], [self.second],
                         'BUSY_HEAD_DID_NOT_BLOCK_LATER_DELIVERY')
        result = []
        worker = threading.Thread(target=lambda: result.append(adapter._bounded_deliver(head)))
        worker.start()
        deadline = time.monotonic() + 2
        while inbox.lifecycle(self.first)['submission_may_have_started_at'] is not None and time.monotonic() < deadline:
            time.sleep(.005)
        time.sleep(.08)  # longer than both admission and legacy busy caps
        self.assertTrue(worker.is_alive(), 'BUSY_WAIT_TIMEOUT_TERMINATED_PROVEN_WAITER')
        self.assertEqual(driver.writes, [], 'BUSY_WAIT_WRITER_CALLED_BEFORE_RELEASE')
        self.assertIsNone(inbox.lifecycle(self.first)['submission_may_have_started_at'])
        self.assertEqual(inbox.progress.snapshot()['deliveries'][self.first], 'adapter_queued')
        driver.busy.clear()
        worker.join(2)
        self.assertFalse(worker.is_alive())
        self.assertEqual(driver.writes, [self.first])
        self.assertIsNotNone(inbox.lifecycle(self.first)['submission_may_have_started_at'])
        self.assertEqual(inbox.lifecycle(self.second)['state'], 'processing')

    def test_submission_boundary_restart_stays_manual_and_never_calls_writer(self):
        self.assertTrue(self.inbox.mark_submission_may_have_started(self.first))
        reopened = DeliveryInbox(self.path, session_name=self.session, capacity=5)
        row = reopened.lifecycle(self.first)
        self.assertEqual(row['state'], 'admission_unknown')
        self.assertIsNotNone(row['submission_may_have_started_at'])
        driver = FakeBusyDriver()
        adapter = DriverAdapter(SimpleNamespace(busy_buffer_max=5, turn_terminal_timeout_seconds=30,
                                                 admission_timeout_seconds=.1, busy_admission_max_seconds=.02),
                                driver=driver)
        adapter._which = 'claude'
        adapter.delivery_lifecycle = reopened
        adapter._restore_delivery_commands()
        head = adapter._commands.start_next()
        self.assertEqual(head.delivery_id, self.first)
        adapter._deliver_regular_command(head)
        self.assertEqual(driver.writes, [], 'UNKNOWN_SUBMISSION_WAS_REPLAYED')
        self.assertEqual(reopened.lifecycle(self.first)['state'], 'admission_unknown')
        self.assertEqual([item.delivery_id for item in adapter._commands.pending], [self.second])

    def test_busy_timeout_setting_applies_after_boundary_not_during_wait(self):
        driver = FakeBusyDriver()
        adapter = DriverAdapter(SimpleNamespace(busy_buffer_max=5, turn_terminal_timeout_seconds=30,
                                                 admission_timeout_seconds=60, busy_admission_max_seconds=1800),
                                driver=driver)
        adapter._which = 'claude'
        adapter.delivery_lifecycle = self.inbox
        command = SimpleNamespace(delivery_id=self.first, text='first', queue_seq=1)
        self.assertEqual(adapter._admission_timeout_for(command), 1800)
        self.assertEqual(adapter._admission_elapsed(command), 0)
        self.assertTrue(self.inbox.mark_submission_may_have_started(self.first))
        self.assertEqual(adapter._admission_timeout_for(command), 1800)

    def test_submission_gate_persistence_failure_refuses_writer(self):
        gate = BusyAdmission()
        calls = []
        gate.set_callbacks(on_waiting=lambda _did: True,
                           before_submit=lambda _did: False)
        gate.waiting(self.first)
        self.assertFalse(gate.before_submit(self.first))
        self.assertEqual(calls, [])
        self.assertTrue(gate.is_waiting(self.first))


if __name__=='__main__':
    unittest.main()
