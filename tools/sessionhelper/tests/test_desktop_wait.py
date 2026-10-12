import asyncio
import json
import os
from pathlib import Path
import sys
import tempfile
import threading
from types import SimpleNamespace
import unittest

from sessionhelper.desktop import DesktopAdapter
from sessionhelper.app import SessionHelper
from sessionhelper.ipc_listener import serve_ipc
from sessionhelper import mcp_shim


class DesktopWaitTest(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.tmp=tempfile.TemporaryDirectory(prefix='dww-',dir=os.path.realpath('/tmp'))
        self.adapter=DesktopAdapter(SimpleNamespace(inbox_db=str(Path(self.tmp.name)/'inbox')))
        self.helper=SessionHelper.__new__(SessionHelper)
        self.helper.cfg=SimpleNamespace(mode='desktop');self.helper.adapter=self.adapter
        self.path=str(Path(self.tmp.name)/'mcp.sock')
        self.bound=asyncio.Event()
        async def no_send(*_):raise AssertionError('wait cannot send to Hub')
        self.server=asyncio.create_task(serve_ipc(self.path,no_send,bound=self.bound,inbox_handler=self.helper._make_desktop_inbox_handler()))
        await asyncio.wait_for(self.bound.wait(),2)
        self.children=[]
    async def asyncTearDown(self):
        for child in self.children:
            if child.returncode is None:
                child.stdin.close()
                try:await asyncio.wait_for(child.wait(),2)
                except asyncio.TimeoutError:child.kill();await child.wait()
        self.server.cancel()
        await asyncio.gather(self.server,return_exceptions=True)
        self.assertFalse(self.adapter._wait_guard.locked())
        self.tmp.cleanup()
    def post(self,ident='fixture-message'):
        self.adapter.handle({'id':ident,'body':'synthetic question','from':'fixture-sender#LOCAL'})
    async def wait_guard(self,locked):
        async with asyncio.timeout(2):
            while self.adapter._wait_guard.locked()!=locked:await asyncio.sleep(.01)
    async def child(self):
        env={k:v for k,v in os.environ.items() if not k.startswith(('SH_','DW_','CODEX_','CLAUDE_'))}
        env['PYTHONPATH']=str(Path(__file__).resolve().parents[1]);env['DINGWEI_MCP_IPC_SOCK']=self.path;env['HOME']=self.tmp.name
        child=await asyncio.create_subprocess_exec(sys.executable,'-m','sessionhelper.mcp_shim',stdin=asyncio.subprocess.PIPE,stdout=asyncio.subprocess.PIPE,stderr=asyncio.subprocess.PIPE,env=env)
        self.children.append(child);return child
    async def send(self,child,ident,method,params=None):
        packet={'jsonrpc':'2.0','method':method}
        if ident is not None:packet['id']=ident
        if params is not None:packet['params']=params
        child.stdin.write((json.dumps(packet)+'\n').encode());await child.stdin.drain()
    async def read(self,child):
        return json.loads(await asyncio.wait_for(child.stdout.readline(),2))
    async def test_real_mcp_pending_call_returns_later_message_same_id(self):
        child=await self.child()
        await self.send(child,1,'initialize');self.assertEqual((await self.read(child))['id'],1)
        await self.send(child,20,'tools/call',{'name':'dw_wait_message','arguments':{'wait_seconds':5}})
        await self.wait_guard(True)
        # Normal request dispatch stays alive while the original wait is pending.
        await self.send(child,21,'tools/list');self.assertEqual((await self.read(child))['id'],21)
        self.post()
        packet=await self.read(child);self.assertEqual(packet['id'],20)
        result=json.loads(packet['result']['content'][0]['text'])
        self.assertEqual(result['message']['id'],'fixture-message')
        self.assertEqual(result['message']['body'],'synthetic question')
        self.assertFalse(result['model_execution_verified'])
        self.assertEqual(self.adapter.request({'operation':'status'})['unread'],1)
    async def test_cancel_notification_releases_helper_wait_and_keeps_next_message(self):
        child=await self.child()
        await self.send(child,10,'tools/call',{'name':'dw_wait_message','arguments':{'wait_seconds':5}})
        await self.wait_guard(True)
        await self.send(child,None,'notifications/cancelled',{'requestId':10})
        await self.wait_guard(False)
        self.post()
        await self.send(child,11,'tools/list');self.assertEqual((await self.read(child))['id'],11)
        self.assertEqual(self.adapter.request({'operation':'status'})['unread'],1)

    async def test_malformed_request_and_cancel_do_not_strand_pending_wait(self):
        child=await self.child()
        await self.send(child,10,'tools/call',{'name':'dw_wait_message','arguments':{'wait_seconds':5}})
        await self.wait_guard(True)
        for raw in ([], {'jsonrpc':'2.0','id':[],'method':'tools/list'},
                    {'jsonrpc':'2.0','id':11,'method':'tools/call','params':[]}):
            child.stdin.write((json.dumps(raw)+'\n').encode());await child.stdin.drain()
            self.assertEqual((await self.read(child))['error']['code'],-32600)
        await self.send(child,None,'notifications/cancelled',{'requestId':[]})
        self.post()
        self.assertEqual((await self.read(child))['id'],10)
    async def test_second_wait_refused_and_pause_returns_without_consumption(self):
        child=await self.child()
        await self.send(child,10,'tools/call',{'name':'dw_wait_message','arguments':{'wait_seconds':5}})
        await self.wait_guard(True)
        await self.send(child,11,'tools/call',{'name':'dw_wait_message','arguments':{'wait_seconds':5}})
        packet=await self.read(child);self.assertEqual(packet['id'],11);self.assertIn('error',packet)
        self.adapter.request({'operation':'pause'})
        packet=await self.read(child);self.assertEqual(packet['id'],10)
        self.assertEqual(json.loads(packet['result']['content'][0]['text'])['state'],'paused')
    def reply_ws(self, *, reject=False, no_ack=False):
        from sessionhelper.outbound_receipt import OutboundReceiptBroker
        from sessionhelper.protocol import AddressBook
        self.helper.book=AddressBook('desktop','fixture-key')
        broker=OutboundReceiptBroker(('desktop',))
        self.helper.outbound_receipts=broker
        class SyntheticHubWS:
            def __init__(inner):inner.frames=[]
            async def send(inner,raw):
                env=json.loads(raw);inner.frames.append(env)
                if no_ack:raise ConnectionError('synthetic lost connection')
                broker.receive(inner,{'from':'workpulse#fixture-key','to':'desktop#fixture-key','meta':{
                    'type':'outbound_receipt','version':1,'source_message_id':env['id'],
                    'nonce':env['meta']['outbound_receipt_nonce'],'state':'rejected' if reject else 'queued',
                    'durable':not reject,'delivery_id':'dw-dlv_'+'a'*32}})
        ws=SyntheticHubWS()
        cap=broker.prepare(ws)
        broker.receive(ws,{'from':'workpulse#fixture-key','to':'desktop#fixture-key','meta':{
            'type':'outbound_capabilities','version':1,'nonce':cap['outbound_receipt_cap_nonce'],'durable_agent_queue':True}})
        return ws
    async def test_original_target_confirmed_reply_cached_without_second_send(self):
        ws=self.reply_ws();self.post()
        handler=self.helper._make_desktop_inbox_handler(ws)
        request={'operation':'reply','message_id':'fixture-message','body':'synthetic answer'}
        result=await handler(request)
        self.assertTrue(result['ok']);self.assertEqual(result['outcome']['stage'],'hub_durable_queued')
        self.assertEqual(ws.frames[0]['to'],'fixture-sender#LOCAL')
        self.assertEqual(ws.frames[0]['meta']['in_reply_to'],'fixture-message')
        self.assertTrue(ws.frames[0]['meta']['conversation_terminal'])
        self.assertEqual(self.adapter.request({'operation':'status'})['unread'],0)
        self.assertTrue((await handler(request))['ok']);self.assertEqual(len(ws.frames),1)
        self.assertFalse((await handler({**request,'body':'changed reply'}))['ok'])
    async def test_ambiguous_reply_blocks_resend_and_next_wait(self):
        ws=self.reply_ws(no_ack=True);self.post()
        handler=self.helper._make_desktop_inbox_handler(ws)
        request={'operation':'reply','message_id':'fixture-message','body':'fixture answer'}
        self.assertEqual((await handler(request))['error'],'reply_outcome_uncertain')
        self.assertFalse((await handler(request))['ok']);self.assertEqual(len(ws.frames),1)
        self.assertEqual(self.adapter.wait_message({'wait_seconds':1})['state'],'reply_uncertain')
        self.assertEqual(self.adapter.request({'operation':'status'})['unread'],1)
        reopened=DesktopAdapter(SimpleNamespace(inbox_db=str(Path(self.tmp.name)/'inbox')))
        self.assertEqual(reopened.wait_message({'wait_seconds':1})['state'],'reply_uncertain')
        self.assertFalse(reopened.reserve_mcp_reply('fixture-message','fixture answer')['ok'])

    async def test_confirmed_reply_recovered_offline_after_response_loss(self):
        ws=self.reply_ws();self.post()
        handler=self.helper._make_desktop_inbox_handler(ws)
        request={'operation':'reply','message_id':'fixture-message','body':'synthetic answer'}
        # Hub ACK was persisted, but the MCP caller did not retain its response.
        original=await handler(request)
        self.assertTrue(original['ok'])
        self.helper.outbound_receipts.abort(ws)
        self.helper.adapter=DesktopAdapter(SimpleNamespace(inbox_db=str(Path(self.tmp.name)/'inbox')))
        # A recreated handler without a socket must return only the saved ACK.
        offline=self.helper._make_desktop_inbox_handler()
        self.assertEqual(await offline(request),original)
        self.assertFalse((await offline({**request,'body':'changed answer'}))['ok'])
        self.assertEqual(len(ws.frames),1)
        self.assertEqual(self.helper.adapter.request({'operation':'status'})['unread'],0)

    async def test_new_or_uncertain_reply_not_reissued_during_reconnect(self):
        ws=self.reply_ws(no_ack=True);self.post()
        handler=self.helper._make_desktop_inbox_handler(ws)
        request={'operation':'reply','message_id':'fixture-message','body':'fixture answer'}
        self.assertEqual((await handler(request))['error'],'reply_outcome_uncertain')
        self.helper.outbound_receipts.abort(ws)
        reconnect=object();self.helper.outbound_receipts.prepare(reconnect)
        self.helper.adapter=DesktopAdapter(SimpleNamespace(inbox_db=str(Path(self.tmp.name)/'inbox')))
        reconnect_handler=self.helper._make_desktop_inbox_handler(reconnect)
        self.assertEqual((await reconnect_handler(request))['error'],'reply_already_attempted')
        self.post('new-message')
        self.assertEqual((await reconnect_handler({**request,'message_id':'new-message'}))['error'],'confirmed_reply_unavailable')
        self.assertIsNone(self.helper.adapter.existing_mcp_reply('new-message','fixture answer'))
        self.assertEqual(len(ws.frames),1)
        self.assertEqual(self.helper.adapter.request({'operation':'status'})['unread'],2)
    async def test_explicit_rejection_is_retained_and_not_retried(self):
        ws=self.reply_ws(reject=True);self.post()
        handler=self.helper._make_desktop_inbox_handler(ws)
        self.assertEqual((await handler({'operation':'reply','message_id':'fixture-message','body':'fixture answer'}))['error'],'reply_rejected')
        self.assertEqual(self.adapter.wait_message({'wait_seconds':1})['state'],'reply_rejected')
    async def test_real_stdio_wait_then_correlated_reply_and_duplicate_confirmation(self):
        ws=self.reply_ws()
        self.server.cancel();await asyncio.gather(self.server,return_exceptions=True)
        self.bound.clear()
        async def no_send(*_):raise AssertionError('must use confirmed inbox reply path')
        self.server=asyncio.create_task(serve_ipc(self.path,no_send,bound=self.bound,inbox_handler=self.helper._make_desktop_inbox_handler(ws)))
        await asyncio.wait_for(self.bound.wait(),2)
        child=await self.child()
        await self.send(child,10,'tools/call',{'name':'dw_wait_message','arguments':{'wait_seconds':5}})
        await self.wait_guard(True);self.post()
        self.assertEqual((await self.read(child))['id'],10)
        for ident in (11,12):
            await self.send(child,ident,'tools/call',{'name':'dw_reply_message','arguments':{'message_id':'fixture-message','body':'synthetic answer'}})
            packet=await self.read(child);self.assertEqual(packet['id'],ident)
            outcome=json.loads(packet['result']['content'][0]['text'])
            self.assertEqual(outcome['stage'],'hub_durable_queued')
            if ident == 11:
                # The duplicate real stdio/IPC call now crosses a disconnected
                # broker and reopened inbox, not only an in-memory cache hit.
                self.helper.outbound_receipts.abort(ws)
                self.helper.adapter=DesktopAdapter(SimpleNamespace(inbox_db=str(Path(self.tmp.name)/'inbox')))
        self.assertEqual(len(ws.frames),1)
        self.assertEqual(self.adapter.request({'operation':'status'})['unread'],0)

    async def test_reply_mcp_arguments_cannot_override_target(self):
        with self.assertRaises(ValueError):mcp_shim._desktop_reply({'message_id':'fixture','body':'fixture','to':'other'})

    async def test_wait_timeout_and_validation(self):
        result=await asyncio.to_thread(self.adapter.wait_message,{'wait_seconds':1})
        self.assertEqual(result['state'],'timeout')
        for seconds in (0,46,True,'5',1.5):
            with self.assertRaises(ValueError):self.adapter.wait_message({'wait_seconds':seconds})
        with self.assertRaises(ValueError):mcp_shim._desktop_wait({'wait_seconds':5,'thread_id':'forged'})
    async def test_server_stop_joins_owned_wait_without_full_timeout(self):
        child=await self.child()
        await self.send(child,10,'tools/call',{'name':'dw_wait_message','arguments':{'wait_seconds':45}})
        await self.wait_guard(True)
        self.server.cancel()
        await asyncio.wait_for(asyncio.gather(self.server,return_exceptions=True),2)
        await self.wait_guard(False)


if __name__=='__main__':unittest.main()
