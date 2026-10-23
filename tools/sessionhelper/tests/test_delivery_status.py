"""Read-only correlated status, no replay or unauthenticated control admission."""
import asyncio
import json
import unittest
from types import SimpleNamespace
from unittest.mock import patch
from sessionhelper.outbound_receipt import OutboundReceiptBroker
from sessionhelper.app import SessionHelper
from sessionhelper import mcp_shim

DID = 'dw-dlv_' + 'a' * 32

class DeliveryStatusTest(unittest.IsolatedAsyncioTestCase):
    def setUp(self):
        self.broker = OutboundReceiptBroker(['seat'])
        self.frames = []
        outer = self
        class WS:
            async def send(inner, raw):
                env = json.loads(raw);outer.frames.append(env)
                if outer.on_send: outer.on_send(inner, env)
        self.ws = WS(); self.on_send = None
        cap = self.broker.prepare(self.ws)
        self.broker.receive(self.ws, {'from':'workpulse#fixture','to':'seat#fixture','meta':{'type':'outbound_capabilities','version':1,'nonce':cap['outbound_receipt_cap_nonce'],'durable_agent_queue':True}})

    def status_response(self, env):
        return {'from':'workpulse#fixture','to':'seat#fixture','meta':{'type':'delivery_status_result_v1','nonce':env['meta']['nonce'],'delivery_id':DID,'result':{'ok':True,'delivery_id':DID,'phase':'adapter_queued','inbox_lag':{'disk_not_admitted_count':1}}}}

    async def test_query_correlated_and_immediate_callback_safe(self):
        self.on_send = lambda ws,env:self.broker.receive(ws,self.status_response(env))
        result = await self.broker.query_status(self.ws,DID)
        self.assertEqual(result['phase'],'adapter_queued')
        self.assertEqual(self.broker.status_pending,{})
        self.assertEqual(len(self.frames),1)
        self.assertEqual(self.frames[0]['body'],'')

    async def test_wrong_nonce_connection_sender_or_delivery_cannot_resolve(self):
        def bad(ws,env):
            for change in ('nonce','connection','sender','delivery'):
                r=self.status_response(env);conn=ws
                if change=='nonce':r['meta']['nonce']='b'*32
                if change=='connection':conn=object()
                if change=='sender':r['from']='peer#fixture'
                if change=='delivery':r['meta']['delivery_id']='dw-dlv_'+'b'*32
                self.assertTrue(self.broker.receive(conn,r))
        self.on_send=bad
        self.assertEqual(await self.broker.query_status(self.ws,DID,timeout=.02),{'ok':False,'status':'unknown'})
        self.assertEqual(len(self.frames),1);self.assertEqual(self.broker.status_pending,{})

    async def test_status_connection_close_is_unknown_and_not_resent(self):
        from websockets.exceptions import ConnectionClosedError
        def closed(ws,env):raise ConnectionClosedError(None,None)
        self.on_send=closed
        self.assertEqual(await self.broker.query_status(self.ws,DID),{'ok':False,'status':'unknown'})
        self.assertEqual(len(self.frames),1);self.assertEqual(self.broker.status_pending,{})

    async def test_cancellation_and_reconnect_never_resend(self):
        task=asyncio.create_task(self.broker.query_status(self.ws,DID));await asyncio.sleep(0);task.cancel()
        with self.assertRaises(asyncio.CancelledError):await task
        self.assertEqual(self.broker.status_pending,{})
        self.broker.prepare(object())
        self.assertEqual((await self.broker.query_status(self.ws,DID))['status'],'unknown')
        self.assertEqual(len(self.frames),1)

    async def test_real_send_handler_returns_phase_after_durable_receipt(self):
        def respond(ws,env):
            if env['meta'].get('type')=='delivery_status_query_v1':r=self.status_response(env)
            else:r={'from':'workpulse#fixture','to':'seat#fixture','meta':{'type':'outbound_receipt','version':1,'nonce':env['meta']['outbound_receipt_nonce'],'source_message_id':env['id'],'delivery_id':DID,'state':'queued','durable':True}}
            self.broker.receive(ws,r)
        self.on_send=respond
        helper=SessionHelper.__new__(SessionHelper);helper.outbound_receipts=self.broker;helper.book=SimpleNamespace(key_id='fixture',self_addr='seat#fixture')
        got=await helper._make_send_handler(self.ws)('peer','synthetic',{})
        self.assertTrue(got['sent']);self.assertEqual(got['phase'],'adapter_queued');self.assertEqual(got['delivery_id'],DID)
        self.assertEqual(len(self.frames),2)

    async def test_legacy_send_marks_unknown_once(self):
        self.broker.prepare(self.ws)
        helper=SessionHelper.__new__(SessionHelper);helper.outbound_receipts=self.broker;helper.book=SimpleNamespace(key_id='fixture',self_addr='seat#fixture')
        got=await helper._make_send_handler(self.ws)('peer','synthetic',{})
        self.assertEqual(got['phase'],'unknown');self.assertTrue(got['sent']);self.assertEqual(len(self.frames),1)

    async def test_real_ipc_shim_roundtrip_and_restricted_modes(self):
        import tempfile
        from pathlib import Path
        from sessionhelper.ipc_listener import serve_ipc
        calls=[]
        async def status(args):calls.append(args);return {'ok':True,'delivery_id':DID,'phase':'adapter_queued'}
        async def send(*args):raise AssertionError('read-only query invoked sender')
        with tempfile.TemporaryDirectory(prefix='lag-ipc-') as root:
            sock=str(Path(root)/'ipc.sock')
            for mode in ('normal','upload_only','send_body_only'):
                bound=asyncio.Event();task=asyncio.create_task(serve_ipc(sock,send,bound=bound,delivery_status_handler=status,upload_only=mode=='upload_only',send_body_only=mode=='send_body_only'))
                try:
                    await asyncio.wait_for(bound.wait(),2)
                    with patch.object(mcp_shim,'_IPC_SOCK',sock):
                        if mode=='normal':
                            got=await asyncio.to_thread(mcp_shim._delivery_status,{'delivery_id':DID})
                            self.assertEqual(got['phase'],'adapter_queued')
                        else:
                            with self.assertRaises(mcp_shim._IPCFailure):await asyncio.to_thread(mcp_shim._delivery_status,{'delivery_id':DID})
                finally:
                    task.cancel()
                    try:await task
                    except asyncio.CancelledError:pass
        self.assertEqual(len(calls),1)

class StatusToolTest(unittest.TestCase):
    def test_strict_readonly_query_no_extra_priority_or_cross_agent_selector(self):
        with patch.object(mcp_shim,'_ipc_request',return_value={'ok':True}) as ipc:
            self.assertTrue(mcp_shim._delivery_status({'delivery_id':DID})['ok'])
            ipc.assert_called_once_with({'type':'delivery_status_v1','delivery_id':DID})
            for args in ({}, {'delivery_id':'bad'}, {'delivery_id':DID,'urgent':True}, {'delivery_id':DID,'agent':'other'}):
                with self.assertRaises(ValueError):mcp_shim._delivery_status(args)
        self.assertNotIn('urgent',mcp_shim._SEND_TOOL_SCHEMA['properties'])

if __name__=='__main__':unittest.main()
