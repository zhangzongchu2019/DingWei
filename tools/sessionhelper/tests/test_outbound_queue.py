import asyncio
import json
import os
import tempfile
import time
import unittest
from sessionhelper.outbound_queue import OutboundQueue, QueueRejected
from sessionhelper.outbound_recovery import OutboundRecovery
from sessionhelper.outbound_receipt import OutboundReceiptError, OutboundRejected


class DurableQueueTest(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup)
        self.path=os.path.join(self.tmp.name,'out.db');self.now=100000
        self.q=OutboundQueue(self.path,clock=lambda:self.now)
        self.q.update_peer('sender',[{'agent_id':'target','full_name':'target.owner','aliases':['target']}])
    def test_automatic_replay_window_stays_within_hub_retention_contract(self):
        # This is a protocol bound, not merely a tunable cache duration. A larger
        # helper window requires a coordinated Hub retention-floor change.
        self.assertEqual(OutboundQueue.WINDOW,24*3600)

    def test_offline_admission_restart_and_conflict(self):
        a=self.q.submit('request','target','body',{})
        self.assertEqual(a['phase'],'local_queued');self.assertEqual(os.stat(self.path).st_mode&0o777,0o600)
        q=OutboundQueue(self.path,clock=lambda:self.now)
        self.assertEqual(q.submit('request','target','body',{}),a)
        with self.assertRaises(QueueRejected):q.submit('request','target','different',{})
        r=q.claim('writer','sender');self.assertEqual(r['source_id'],a['source_message_id'])
        self.assertIsNone(q.claim('other','sender'))
        self.assertFalse(q.finish('request','other',phase='rejected'))
        q.finish('request','writer',phase='unknown')
        self.now+=31;r=q.claim('restart','sender')
        self.assertEqual(r['source_id'],a['source_message_id']);self.assertEqual(r['body'],'body')
        q.finish('request','restart',phase='hub_durable_queued',delivery_id='dw-dlv_'+'a'*32)
        self.assertEqual(q.status('request')['accepted_by'],'hub')
        self.assertEqual(q.submit('request','target','body',{})['delivery_id'],'dw-dlv_'+'a'*32)
        with q.db() as db:self.assertEqual(db.execute('SELECT body FROM outbound_request').fetchone()[0],'')
    def test_explicit_owner_key_is_not_silently_discarded(self):
        with self.assertRaises(QueueRejected):self.q.submit('wrong','target#wrong-owner','body',{})
        self.q.update_peer('sender',[{'agent_id':'target','full_name':'target.owner','aliases':['target','target#verified-owner']}])
        accepted=self.q.submit('right','target#verified-owner','body',{})
        self.assertEqual(accepted['to'],'target.owner')
        with self.assertRaises(QueueRejected):self.q.submit('still-wrong','target#wrong-owner','body',{})
    def test_capacity_and_expiry_preserve_unknown(self):
        self.q.MAX_PENDING=1;self.q.submit('one','target','secret business body',{})
        with self.assertRaises(QueueRejected):self.q.submit('two','target','body',{})
        self.q.claim('writer','sender');self.now+=self.q.WINDOW+31
        self.assertIsNone(self.q.claim('writer2','sender'))
        status=self.q.status('one');self.assertEqual(status['phase'],'unknown');self.assertEqual(status['reason'],'outbound_recovery_expired')
        with self.q.db() as db:self.assertEqual(db.execute('SELECT body FROM outbound_request').fetchone()[0],'secret business body')
    def test_new_identity_cannot_claim_old_sender_or_target(self):
        self.q.submit('one','target','body',{})
        self.q.update_peer('new-sender',[{'agent_id':'new-target','full_name':'target.owner','aliases':['target']}])
        self.assertIsNone(self.q.claim('writer','new-sender'))
        denied=self.q.status('one')
        self.assertEqual(denied['reason'],'sender_identity_changed')
        self.assertEqual(denied['phase'],'rejected')
        self.assertNotIn('source_message_id',denied)
        self.assertNotIn('delivery_id',denied)


    def test_absent_status_is_unknown_not_proof_of_non_acceptance(self):
        result=self.q.status('may-be-submitting')
        self.assertEqual(result['outcome'],'unknown')
        self.assertEqual(result['status_ref'],'may-be-submitting')


    def test_directory_failure_preserves_only_same_sender_cached_binding(self):
        self.q.update_peer('sender',None)
        try:result=self.q.submit('one','target','body',{})
        except QueueRejected:self.fail('temporary directory failure destroyed cached same-sender binding')
        self.assertEqual(result['phase'],'local_queued')
        self.q.update_peer('different-sender',None)
        with self.assertRaises(QueueRejected) as e:self.q.submit('two','target','body',{})
        self.assertEqual(e.exception.reason,'target_identity_unavailable')


    def test_attempt_audit_is_body_free_bounded_and_separate_from_fence(self):
        self.q.MAX_AUDIT=2
        original=self.q.submit('one','target','PRIVATE-BODY-SENTINEL',{})
        for n in range(3):
            row=self.q.claim('writer','sender')
            self.assertIsNotNone(row)
            self.q.finish('one','writer',phase='unknown')
            self.now+=31
        result=self.q.status('one')
        self.assertEqual(len(result['recent_attempts']),2)
        self.assertNotIn('PRIVATE-BODY-SENTINEL',json.dumps(result))
        self.assertEqual(result['source_message_id'],original['source_message_id'])
        with self.q.db() as db:self.assertEqual(db.execute('SELECT body FROM outbound_request').fetchone()[0],'PRIVATE-BODY-SENTINEL')


    def test_replacement_sender_cannot_recover_prior_receipt_before_catalog_tick(self):
        self.q.submit('one','target','body',{})
        self.assertNotIn('source_message_id',self.q.status('one',actor_id='new-sender'))
        with self.assertRaises(QueueRejected):self.q.submit('one','target','body',{},actor_id='new-sender')


    def test_byte_budget_is_utf8_and_rejects_new_body_without_evicting_old(self):
        self.q.MAX_BYTES=8  # 中文 is six UTF-8 bytes, plus two metadata bytes.
        first=self.q.submit('one','target','中文',{})
        with self.assertRaises(QueueRejected) as e:self.q.submit('two','target','🙂',{})
        self.assertEqual(e.exception.reason,'local_queue_full')
        self.assertEqual(self.q.status('one')['source_message_id'],first['source_message_id'])
        with self.q.db() as db:self.assertEqual(db.execute('SELECT body FROM outbound_request').fetchone()[0],'中文')


TEST_RECOVERY_CAP={'sender_id':'sender','targets':[{'agent_id':'target','full_name':'target.owner','aliases':['target']}]}

class RecoveryWorkerTest(unittest.IsolatedAsyncioTestCase):
    async def test_offline_unattempted_request_expires(self):
        class Broker:connection=None;negotiated=False;recovery=None
        with tempfile.TemporaryDirectory() as tmp:
            w=OutboundRecovery(os.path.join(tmp,'q'),Broker(),lambda:'source');now=[100000];w.queue.clock=lambda:now[0]
            w.queue.update_peer('sender',TEST_RECOVERY_CAP['targets']);w.queue.submit('caller','target','body',{})
            now[0]+=w.queue.WINDOW+31;await w.step()
            self.assertEqual(w.queue.status('caller')['phase'],'rejected','offline queue did not enforce lifetime')
    async def test_missing_capability_deadline_is_explicit(self):
        class Broker:connection=object();negotiated=False;recovery=None
        with tempfile.TemporaryDirectory() as tmp:
            w=OutboundRecovery(os.path.join(tmp,'q'),Broker(),lambda:'source');w.queue.update_peer('sender',TEST_RECOVERY_CAP['targets']);w.queue.submit('caller','target','body',{})
            await w.step();w._cap_wait_started=time.monotonic()-11;await w.step()
            self.assertEqual(w.queue.status('caller')['reason'],'outbound_recovery_unsupported','old Hub kept local acceptance pending forever')
    async def test_unsupported_unknown_is_not_automatically_replayed_on_upgrade(self):
        class Broker:
            connection=object();negotiated=True;recovery=TEST_RECOVERY_CAP
            def __init__(self):self.calls=0
            async def send(self,*args,**kwargs):
                self.calls+=1
                if self.calls==1:raise OutboundReceiptError('unknown after prior write')
                return {'delivery_id':'dw-dlv_'+'b'*32}
        with tempfile.TemporaryDirectory() as tmp:
            b=Broker();w=OutboundRecovery(os.path.join(tmp,'q'),b,lambda:'source');await w.step();w.queue.submit('caller','target','body',{});await w.step()
            b.recovery=None;await w.step()
            with w.queue.db() as db:db.execute('UPDATE outbound_request SET next_attempt=0')
            b.connection=object();b.recovery=TEST_RECOVERY_CAP;await w.step()
            self.assertEqual(b.calls,1,'unsupported unknown was blindly replayed after upgrade')

    async def test_unsupported_generation_does_not_overwrite_prior_receipt(self):
        class Broker:connection=object();negotiated=True;recovery=TEST_RECOVERY_CAP
        with tempfile.TemporaryDirectory() as tmp:
            b=Broker();w=OutboundRecovery(os.path.join(tmp,'q'),b,lambda:'source');await w.step()
            original=w.queue.submit('caller','target','body',{});w.queue.claim('writer','sender')
            w.queue.finish('caller','writer',phase='hub_durable_queued',delivery_id='dw-dlv_'+'d'*32)
            b.recovery=None;await w.step()
            result=await w.handler().submit_request('target','body',{},'caller')
            self.assertEqual(result['phase'],'hub_durable_queued')
            self.assertEqual(result['source_message_id'],original['source_message_id'])
            refused=await w.handler().submit_request('target','body',{},'new-caller')
            self.assertEqual(refused['reason'],'outbound_recovery_unsupported')
            self.assertEqual(refused['outcome'],'definitive_failure')

    async def test_disconnect_then_same_id_and_lost_receipt_recovery(self):
        class Broker:
            connection=object();negotiated=True
            recovery={'sender_id':'sender','targets':[{'agent_id':'target','full_name':'target.owner','aliases':['target']}]}
            def __init__(self):self.ids=[];self.accepted={}
            async def send(self,ws,to,body,source,meta,**kw):
                sid=kw['source_id'];self.ids.append(sid)
                # Simulate Hub commit followed by disconnect before receipt.
                self.accepted.setdefault(sid,'dw-dlv_'+'b'*32)
                if len(self.ids)==1:raise OutboundReceiptError('connection lost')
                return {'delivery_id':self.accepted[sid]}
        with tempfile.TemporaryDirectory() as d:
            b=Broker();w=OutboundRecovery(os.path.join(d,'queue'),b,lambda:'source#owner')
            await w.step();handler=w.handler()
            out=await handler.submit_request('target','body',{},'caller')
            await w.step();self.assertEqual(w.queue.status('caller')['phase'],'unknown')
            # Restart with the same private queue; no new source ID.
            w=OutboundRecovery(os.path.join(d,'queue'),b,lambda:'source#owner')
            with w.queue.db() as db:db.execute('UPDATE outbound_request SET next_attempt=0')
            await w.step();result=await w.handler().status('caller')
            self.assertEqual(result['phase'],'hub_durable_queued')
            self.assertEqual(b.ids,[out['source_message_id']]*2);self.assertEqual(len(b.accepted),1)
    async def test_current_refusal_after_lost_receipt_preserves_original_unknown(self):
        class Broker:
            connection=object();negotiated=True
            recovery={'sender_id':'sender','targets':[{'agent_id':'target','full_name':'target.owner','aliases':['target']}]}
            def __init__(self,prior):self.prior=prior;self.ids=[];self.accepted={}
            async def send(self,ws,to,body,source,meta,**kw):
                sid=kw['source_id'];self.ids.append(sid)
                if self.prior and len(self.ids)==1:
                    self.accepted[sid]='dw-dlv_'+'c'*32
                    raise OutboundReceiptError('receipt lost after Hub commit')
                if not self.prior or len(self.ids)==2:
                    err=OutboundRejected('current permission denied');err.reason='permission_denied';raise err
                return {'delivery_id':self.accepted[sid]}
        for prior in (False,True):
            with self.subTest(prior=prior), tempfile.TemporaryDirectory() as tmp:
                b=Broker(prior);w=OutboundRecovery(os.path.join(tmp,'queue.db'),b,lambda:'source#owner')
                await w.step();first=w.queue.submit('caller','target','original pending body',{})
                await w.step()
                if not prior:
                    self.assertEqual(w.queue.status('caller')['phase'],'rejected')
                    continue
                with w.queue.db() as db:db.execute('UPDATE outbound_request SET next_attempt=0')
                await w.step()
                self.assertEqual(w.queue.status('caller')['phase'],'unknown','current refusal cannot prove prior acceptance absent')
                self.assertEqual(w.queue.status('caller')['reason'],'outbound_rejected_after_uncertainty')
                with w.queue.db() as db:
                    self.assertEqual(db.execute('SELECT body FROM outbound_request').fetchone()[0],'original pending body')
                    db.execute('UPDATE outbound_request SET next_attempt=0')
                await w.step()
                resolved=w.queue.status('caller')
                self.assertEqual(resolved['phase'],'hub_durable_queued')
                self.assertEqual(resolved['delivery_id'],'dw-dlv_'+'c'*32)
                self.assertEqual(b.ids,[first['source_message_id']]*3)
                self.assertEqual(len(b.accepted),1,'recovery created a second business identity')

    async def test_old_or_missing_capability_never_writes(self):
        class Broker:
            connection=object();negotiated=True;recovery=None
            async def send(self,*a,**kw):raise AssertionError('old Hub received body')
        with tempfile.TemporaryDirectory() as d:
            w=OutboundRecovery(os.path.join(d,'queue'),Broker(),lambda:'source')
            w.queue.update_peer('sender',[{'agent_id':'target','full_name':'target.owner','aliases':['target']}])
            await w.handler().submit_request('target','body',{},'request')
            await w.step();self.assertEqual(w.queue.status('request')['phase'],'rejected')
            self.assertEqual(w.queue.status('request')['reason'],'outbound_recovery_unsupported')

    async def test_only_one_process_writer_can_hold_outbox(self):
        class Broker:connection=None;negotiated=False;recovery=None
        with tempfile.TemporaryDirectory() as directory:
            path=os.path.join(directory,'queue')
            first=OutboundRecovery(path,Broker(),lambda:'source')
            second=OutboundRecovery(path,Broker(),lambda:'source')
            task=asyncio.create_task(first.run())
            try:
                await asyncio.sleep(0.01)
                try:
                    with self.assertRaisesRegex(RuntimeError,'writer already active'):
                        await asyncio.wait_for(second.run(),0.5)
                except TimeoutError:
                    self.fail('second writer was not rejected while the first owned the outbox')
            finally:
                task.cancel()
                try:await task
                except asyncio.CancelledError:pass


class CallerReceiptTest(unittest.TestCase):
    def test_caller_id_is_journaled_before_ipc_write_and_unknown_recovers_id(self):
        from unittest import mock
        from sessionhelper import send_dingwei
        with tempfile.TemporaryDirectory() as directory, mock.patch.dict(os.environ,{'HOME':directory}):
            rid='caller-stable'
            first=send_dingwei.remember_request(rid,'target','body',{})
            self.assertEqual(first,rid)
            self.assertEqual(send_dingwei.remember_request(rid,'target','body',{}),rid)
            with self.assertRaises(ValueError):send_dingwei.remember_request(rid,'target','other',{})
            self.assertTrue(os.path.isfile(os.path.join(directory,'.dingwei','outbound-caller.db')))
            with mock.patch.object(send_dingwei.socket,'socket') as factory:
                factory.return_value.connect.side_effect=ConnectionRefusedError()
                result=send_dingwei.ipc_send('target','body','next-request')
            self.assertEqual(result['outcome']['outcome'],'unknown')
            self.assertEqual(result['outcome']['request_id'],'next-request')
    def test_ipc_submission_passes_caller_id_and_queries_without_resending(self):
        from sessionhelper.ipc_listener import _handle_connection
        class Pipe:
            def __init__(self,msg):self.msg=(json.dumps(msg)+'\n').encode();self.written=b''
            async def readline(self):return self.msg
            def write(self,data):self.written+=data
            async def drain(self):pass
        with tempfile.TemporaryDirectory() as directory:
            class Broker: connection=None;negotiated=False;recovery=None
            w=OutboundRecovery(os.path.join(directory,'queue'),Broker(),lambda:'source')
            w.queue.update_peer('sender',[{'agent_id':'target','full_name':'target.owner','aliases':['target']}])
            p=Pipe({'type':'mcp_send','to':'target','body':'body','request_id':'caller'})
            asyncio.run(_handle_connection(p,p,w.handler()))
            r=json.loads(p.written)['outcome'];self.assertEqual(r['phase'],'local_queued')
            p=Pipe({'type':'mcp_send_status','request_id':'caller'})
            asyncio.run(_handle_connection(p,p,w.handler(),send_body_only=True))
            self.assertEqual(json.loads(p.written)['outcome']['source_message_id'],r['source_message_id'])

    def test_cli_ipc_ok_without_responsibility_is_not_success(self):
        import io
        import sys
        from unittest import mock
        from sessionhelper import send_dingwei
        with mock.patch.object(sys,'argv',['send.py','target.owner','body']),mock.patch.object(send_dingwei,'ipc_send',return_value={'ok':True,'request_id':'stable','outcome':{'sent':True}}),mock.patch('sys.stdout',new_callable=io.StringIO) as output:
            with self.assertRaises(SystemExit) as e:send_dingwei.main()
            self.assertEqual(e.exception.code,2)
            result=json.loads(output.getvalue());self.assertEqual(result['outcome'],'unknown');self.assertEqual(result['request_id'],'stable')


    def test_mcp_status_transport_error_keeps_original_query_id(self):
        from unittest import mock
        from sessionhelper import mcp_shim
        with mock.patch.object(mcp_shim,'_ipc_request',side_effect=mcp_shim._IPCFailure('ipc_unavailable')):
            r=mcp_shim._dispatch(mcp_shim.Request('2.0','tools/call',{'name':'dw_send_status','arguments':{'request_id':'original'}},1))
        data=json.loads(r.result['content'][0]['text'])
        self.assertTrue(r.result['isError']);self.assertEqual(data['request_id'],'original')
    def test_mcp_caller_journal_failure_never_dispatches(self):
        from unittest import mock
        from sessionhelper import mcp_shim
        with mock.patch('sessionhelper.send_dingwei.remember_request',side_effect=OSError('private path')),mock.patch.object(mcp_shim,'_ipc_request',side_effect=AssertionError('IPC must not run')):
            data=mcp_shim._send_message({'to':'target','body':'body','request_id':'original'})
        self.assertEqual(data['outcome'],'definitive_failure');self.assertTrue(data['retryable']);self.assertEqual(data['request_id'],'original')


class OfflineListenerLifetimeTest(unittest.IsolatedAsyncioTestCase):
    async def test_real_run_forever_accepts_during_transport_outage(self):
        from pathlib import Path
        from unittest import mock
        from sessionhelper import send_dingwei as caller
        try:import test_file_upload as fx
        except ImportError:from tests import test_file_upload as fx
        with tempfile.TemporaryDirectory() as directory, mock.patch.dict(os.environ,{'HOME':directory}):
            layout=fx.Layout(Path(directory))
            helper=fx.make_helper(layout)
            helper.cfg.outbound_caller_retention_seconds=48*3600
            async def offline_transport():
                helper.outbound_recovery.queue.update_peer('sender',[{'agent_id':'target','full_name':'target.owner','aliases':['target']}])
                await asyncio.Event().wait()
            helper._reconnect_forever=offline_transport
            task=asyncio.create_task(helper.run_forever())
            try:
                await asyncio.wait_for(helper.ipc_bound.wait(),3)
                with caller._caller_db(time.time()) as db:
                    self.assertIn((helper.outbound_recovery.queue.path,),db.execute('SELECT path FROM caller_outbox').fetchall(),'production startup did not register the actual outbox')
                    self.assertEqual(db.execute('SELECT retention FROM caller_policy WHERE id=1').fetchone()[0],48*3600)
                self.assertEqual(helper.outbound_recovery.queue.retention,48*3600)
                result=await asyncio.to_thread(fx.raw_ipc,layout.sock,{'type':'mcp_send','to':'target','body':'body','request_id':'offline'})
                self.assertEqual(result['outcome']['phase'],'local_queued')
                self.assertIsNone(helper.outbound_receipts.connection)
                recovered=OutboundQueue(helper.cfg.inbox_db+'.outbound.db').status('offline')
                self.assertEqual(recovered['source_message_id'],result['outcome']['source_message_id'])
            finally:
                task.cancel()
                try:await task
                except asyncio.CancelledError:pass
                layout.cleanup()


class ReceiptSchemaTest(unittest.TestCase):
    def test_accepted_requires_correlated_responsibility_identifiers(self):
        from sessionhelper.send_dingwei import checked_outcome
        missing={'outcome':'accepted','phase':'local_queued','accepted_by':'local_helper'}
        self.assertEqual(checked_outcome(missing,'caller')['outcome'],'unknown')
        self.assertEqual(checked_outcome({**missing,'request_id':'caller'},'caller')['outcome'],'unknown')
        local={**missing,'request_id':'caller','source_message_id':'a'*32}
        self.assertEqual(checked_outcome(local,'caller')['outcome'],'accepted')
        self.assertEqual(checked_outcome(local,'different-caller')['outcome'],'unknown')
        hub={**local,'phase':'hub_durable_queued','accepted_by':'hub'}
        self.assertEqual(checked_outcome(hub,'caller')['outcome'],'unknown')
        hub['delivery_id']='dw-dlv_'+'b'*32
        self.assertEqual(checked_outcome(hub,'caller')['outcome'],'accepted')


class CallerRetentionTest(unittest.TestCase):
    def setUp(self):
        from unittest import mock
        from sessionhelper import send_dingwei as caller
        self.caller=caller; self.mock=mock; self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup)
        self.enterContext(mock.patch.dict(os.environ,{'HOME':self.tmp.name}))
        self.now=1000000
        self.enterContext(mock.patch.object(caller.time,'time',side_effect=lambda:self.now))
        self.q=OutboundQueue(os.path.join(self.tmp.name,'outbox.db'),clock=lambda:self.now)
        self.q.update_peer('sender',TEST_RECOVERY_CAP['targets'])
        caller.register_caller_outbox(self.q.path)
    def seed(self,ident,created):
        with self.caller._caller_db(self.now) as db:
            db.execute('INSERT INTO request(id,digest,created) VALUES(?,?,?)',(ident,'existing-digest',created))
    def rows(self):
        with self.caller._caller_db(self.now) as db:return dict(db.execute('SELECT id,created FROM request'))
    def test_old_schema_migration_preserves_ids_and_starts_safe_window(self):
        import sqlite3
        path=os.path.join(self.tmp.name,'.dingwei','outbound-caller.db')
        os.unlink(path)
        with sqlite3.connect(path) as db:
            db.execute('CREATE TABLE request(id TEXT PRIMARY KEY,digest TEXT NOT NULL)')
            db.executemany('INSERT INTO request VALUES(?,?)',[('legacy-1','digest-1'),('legacy-2','digest-2')])
        self.caller.register_caller_outbox(self.q.path)
        with self.caller._caller_db(self.now) as db:
            self.assertEqual(db.execute('SELECT id,digest,created FROM request ORDER BY id').fetchall(),[('legacy-1','digest-1',self.now),('legacy-2','digest-2',self.now)])
        self.caller.remember_request('new','target','body',{})
        self.assertIn('legacy-1',self.rows())
    def test_window_capacity_ignores_expired_and_cleanup_is_bounded(self):
        cutoff=self.now-self.caller.DEFAULT_CALLER_RETENTION
        for i in range(6):self.seed('expired-'+str(i),cutoff-1)
        self.seed('boundary',cutoff)
        with self.mock.patch.object(self.caller,'CALLER_MAX_RECORDS',2),self.mock.patch.object(self.caller,'CALLER_CLEANUP_BATCH',2):
            try:self.caller.remember_request('new','target','body',{})
            except self.caller.CallerJournalRejected as exc:self.fail('expired caller rows consumed the window quota: '+exc.reason)
        rows=self.rows()
        self.assertEqual(len([i for i in rows if i.startswith('expired-')]),4,'caller cleanup did not remove exactly one bounded batch')
        self.assertIn('boundary',rows,'window boundary fence was deleted')
        self.assertEqual(rows['new'],self.now)
        self.caller.remember_request('next','target','next body',{})
        self.assertIn('boundary',self.rows(),'cleanup crossed the retention window once old backlog drained')
    def test_window_same_id_and_conflicting_digest_survive(self):
        self.caller.remember_request('stable','target','body',{})
        self.now+=1
        self.assertEqual(self.caller.remember_request('stable','target','body',{}),'stable')
        with self.assertRaises(self.caller.CallerJournalRejected) as got:self.caller.remember_request('stable','target','changed',{})
        self.assertEqual((got.exception.reason,got.exception.request_id,got.exception.retryable),('idempotency_conflict','stable',False))
    def test_pending_outbox_ids_are_never_deleted(self):
        cutoff=self.now-self.caller.DEFAULT_CALLER_RETENTION
        for i,state in enumerate(('local_queued','unknown')):
            ident='pending-'+str(i);self.seed(ident,cutoff-1)
            self.q.submit(ident,'target','body',{})
            with self.q.db() as db:db.execute('UPDATE outbound_request SET state=?,created=? WHERE request_id=?',(state,cutoff-1,ident))
        self.seed('finished',cutoff-1)
        self.caller.remember_request('new','target','body',{})
        rows=self.rows();self.assertIn('pending-0',rows);self.assertIn('pending-1',rows);self.assertNotIn('finished',rows)
    def test_unregistered_outbox_does_not_allow_cleanup(self):
        self.seed('unassociated',self.now-self.caller.DEFAULT_CALLER_RETENTION-1)
        with self.caller._caller_db(self.now) as db:db.execute('DELETE FROM caller_outbox')
        self.caller.remember_request('new','target','body',{})
        self.assertIn('unassociated',self.rows(),'missing outbox association was treated as terminal')
    def test_locked_or_missing_outbox_is_not_inferred_terminal(self):
        import sqlite3
        cutoff=self.now-self.caller.DEFAULT_CALLER_RETENTION
        for mode in ('locked','missing'):
            with self.subTest(mode=mode):
                ident='keep-'+mode;self.seed(ident,cutoff-1)
                if mode=='locked':
                    conn=sqlite3.connect(self.q.path);conn.execute('BEGIN IMMEDIATE')
                else:
                    with self.caller._caller_db(self.now) as db:db.execute('INSERT INTO caller_outbox VALUES(?)',(self.q.path+'.absent',))
                try:self.caller.remember_request('new-'+mode,'target','body',{})
                finally:
                    if mode=='locked':conn.close()
                self.assertIn(ident,self.rows(),'unreadable outbox was mistaken for no pending responsibility')
    def test_pending_snapshot_holds_writer_until_caller_commit(self):
        import threading
        entered=threading.Event();done=threading.Event();errors=[]
        def submit():
            entered.set()
            try:self.q.submit('concurrent','target','body',{})
            except Exception as exc:errors.append(type(exc).__name__)
            finally:done.set()
        with self.caller._caller_db(self.now) as db:
            db.execute('BEGIN IMMEDIATE')
            with self.caller._caller_pending(db) as pending:
                self.assertIsNotNone(pending)
                t=threading.Thread(target=submit);t.start();entered.wait(1)
                self.assertFalse(done.wait(.1),'outbox writer crossed the protected cleanup snapshot')
                db.commit()
        t.join(2);self.assertTrue(done.is_set());self.assertEqual(errors,[])
    def test_caller_full_cli_and_mcp_return_structured_retryable_rejection(self):
        self.seed('recent1',self.now);self.seed('recent2',self.now)
        with self.mock.patch.object(self.caller,'CALLER_MAX_RECORDS',2):
            self.assert_refused_both('full','body','caller_journal_full',True)
    def test_caller_conflict_cli_and_mcp_do_not_become_invalid_arguments(self):
        self.caller.remember_request('conflict-cli','target','original',{'ipc_body_amr_v1':True})
        self.caller.remember_request('conflict-mcp','target','original',{})
        self.assert_refused_both('conflict','changed','idempotency_conflict',False,split=True)
    def assert_refused_both(self,rid,body,reason,retryable,split=False):
        import contextlib,io,sys
        from sessionhelper import mcp_shim
        cli_id=rid+'-cli' if split else rid
        output=io.StringIO();error=io.StringIO()
        with self.mock.patch.object(sys,'argv',['send.py','target','--request-id',cli_id,body]),contextlib.redirect_stdout(output),contextlib.redirect_stderr(error),self.mock.patch.object(self.caller.socket,'socket',side_effect=AssertionError('no IPC on journal refusal')):
            with self.assertRaises(SystemExit) as stopped:self.caller.main()
        self.assertEqual(stopped.exception.code,1)
        result=json.loads(output.getvalue());self.assertEqual((result['outcome'],result['phase'],result['reason'],result['request_id'],result['retryable']),('definitive_failure','rejected',reason,cli_id,retryable))
        self.assertIn('发送失败：'+reason,error.getvalue())
        mcp_id=rid+'-mcp' if split else rid
        with self.mock.patch.object(mcp_shim,'_ipc_request',side_effect=AssertionError('no IPC on journal refusal')):
            response=mcp_shim._dispatch(mcp_shim.Request('2.0','tools/call',{'name':'send_message','arguments':{'to':'target','body':body,'request_id':mcp_id}},123))
        self.assertIsNone(response.error,'journal refusal became JSON-RPC invalid arguments')
        self.assertTrue(response.result['isError'])
        result=json.loads(response.result['content'][0]['text']);self.assertEqual((result['outcome'],result['phase'],result['reason'],result['request_id'],result['retryable']),('definitive_failure','rejected',reason,mcp_id,retryable))
    def test_helper_retention_bounds_and_configuration_reject_at_startup(self):
        from sessionhelper.config import Config,load_config
        for raw in ('', '93601','604800','2592000'):
            value=self.caller.parse_caller_retention(raw);self.assertGreater(value,93600);self.assertLessEqual(value,2592000)
        for raw in ('93600','86400','2592001','nan','inf','-1','INVALID_PRIVATE_VALUE'):
            with self.subTest(raw=raw):
                with self.assertRaises(ValueError) as caught:self.caller.parse_caller_retention(raw)
                self.assertNotIn('INVALID_PRIVATE_VALUE',str(caught.exception))
                with self.assertRaises(ValueError):Config('fixture','synthetic','synthetic',outbound_caller_retention_seconds=raw)
                with self.assertRaises(ValueError):load_config({'SH_SESSION_NAME':'fixture','SH_KEY_ID':'synthetic','SH_SECRET':'synthetic','SH_OUTBOUND_CALLER_RETENTION_SECONDS':raw})
    def test_outbox_history_cleanup_releases_capacity_but_preserves_pending(self):
        cutoff=self.now-self.caller.DEFAULT_CALLER_RETENTION
        with self.q.db() as db:
            db.executemany("INSERT INTO outbound_request(request_id,sender_id,source_id,target_id,target,digest,body,meta,state,created,next_attempt) VALUES(?,'sender',?,'target','target.owner','digest','','{}','hub_durable_queued',?,?)",[(f'old-{i}',f'old-source-{i}',cutoff-1,cutoff-1) for i in range(259)])
            for state in ('local_queued','unknown'):
                db.execute("INSERT INTO outbound_request(request_id,sender_id,source_id,target_id,target,digest,body,meta,state,created,next_attempt) VALUES(?,'sender',?,'target','target.owner','digest','unfinished','{}',?,?,?)",(state,state,state,cutoff-1,cutoff-1))
            db.execute("INSERT INTO outbound_request(request_id,sender_id,source_id,target_id,target,digest,body,meta,state,created,next_attempt) VALUES('boundary','sender','boundary-source','target','target.owner','digest','','{}','rejected',?,?)",(cutoff,cutoff))
        self.q.MAX_RECORDS=2
        try:result=self.q.submit('new','target','new body',{})
        except QueueRejected as exc:self.fail('expired outbox rows consumed the window quota: '+exc.reason)
        self.assertEqual(result['phase'],'local_queued')
        with self.q.db() as db:
            self.assertEqual(db.execute("SELECT count(*) FROM outbound_request WHERE request_id LIKE 'old-%'").fetchone()[0],3,'outbox terminal cleanup is absent or unbounded')
            for ident in ('local_queued','unknown','boundary'):
                self.assertIsNotNone(db.execute('SELECT 1 FROM outbound_request WHERE request_id=?',(ident,)).fetchone(),'cleanup erased pending/window outbox fence')
