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
        import sys,threading
        from sessionhelper.claudeDriver import ClaudeProtocolDriver,_TurnState
        driver=ClaudeProtocolDriver(store_path=str(Path(self.tmp.name)/'previous.db'),cwd=self.tmp.name,
            cmd=[sys.executable,'-c','import time;time.sleep(20)'])
        self.addCleanup(driver.stop)
        driver.start()
        previous=_TurnState(envelope_id='previous',turn_id='previous-turn',accepted=threading.Event(),terminal_seen=threading.Event())
        previous.accepted.set();driver._active=previous
        replacement=SimpleNamespace(start=lambda:None)
        adapter=self.adapter(driver,'claude');adapter._build_driver=lambda which:replacement;adapter._admission_timeout=.15
        adapter._commands.enqueue(self.env)
        command=adapter._commands.start_next();adapter._deliver_regular_command(command)
        self.assertEqual(self.inbox.lifecycle(command.delivery_id)['state'],'admission_unknown')
        self.assertTrue(adapter._transport_recovery.wait(3),'等待旧回合的deliver也必须响应abort并释放旧写线程')
        self.assertIs(adapter.driver,replacement)
        self.assertFalse(previous.terminal_seen.is_set(),'abort不得伪造旧回合终态')

    def test_clock_includes_time_since_persisted_dispatch_intent(self):
        import time
        calls=[]
        driver=SimpleNamespace(deliver=lambda did,body:calls.append(did) or DeliveryReceipt(did,'processing','fixture','submitted'),
            poll_receipt=lambda did:DeliveryReceipt(did,'processing','fixture','submitted'),abort_transport=lambda:None,stop=lambda:None,start=lambda:None)
        adapter=self.adapter(driver,'claude');adapter._admission_timeout=.1;adapter._build_driver=lambda which:driver
        original=self.inbox.begin_dispatch
        def slow_intent(did):
            won=original(did);time.sleep(.15);return won
        self.inbox.begin_dispatch=slow_intent
        adapter._commands.enqueue(self.env)
        started=time.monotonic()
        adapter._deliver_regular_command(adapter._commands.start_next())
        self.assertLess(time.monotonic()-started,.23,'admission截止须自intent落盘起计，不能在deliver返回后重新起计')
        self.assertEqual(calls,[],'admission截止已过的intent不得再派发')
        self.assertEqual(self.inbox.lifecycle(self.env['meta']['delivery_id'])['state'],'admission_unknown','admission截止须自intent落盘起计')
        self.assertTrue(adapter._transport_recovery.wait(1))

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

if __name__=='__main__':unittest.main()
