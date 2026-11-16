import asyncio
import json
import os
import tempfile
import unittest
from sessionhelper.outbound_queue import OutboundQueue, QueueRejected
from sessionhelper.outbound_recovery import OutboundRecovery
from sessionhelper.outbound_receipt import OutboundReceiptError


class DurableQueueTest(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup)
        self.path=os.path.join(self.tmp.name,'out.db');self.now=100000
        self.q=OutboundQueue(self.path,clock=lambda:self.now)
        self.q.update_peer('sender',[{'agent_id':'target','full_name':'target.owner','aliases':['target']}])
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
        r=self.q.claim('writer','sender');self.assertEqual(r['target_id'],'target')


class RecoveryWorkerTest(unittest.IsolatedAsyncioTestCase):
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


class OfflineListenerLifetimeTest(unittest.IsolatedAsyncioTestCase):
    async def test_real_run_forever_accepts_during_transport_outage(self):
        from pathlib import Path
        try:import test_file_upload as fx
        except ImportError:from tests import test_file_upload as fx
        with tempfile.TemporaryDirectory() as directory:
            layout=fx.Layout(Path(directory))
            helper=fx.make_helper(layout)
            async def offline_transport():
                helper.outbound_recovery.queue.update_peer('sender',[{'agent_id':'target','full_name':'target.owner','aliases':['target']}])
                await asyncio.Event().wait()
            helper._reconnect_forever=offline_transport
            task=asyncio.create_task(helper.run_forever())
            try:
                await asyncio.wait_for(helper.ipc_bound.wait(),3)
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
