import asyncio
import json
import unittest
from sessionhelper.outbound_receipt import OutboundReceiptBroker, OutboundReceiptError


class ReceiptTest(unittest.IsolatedAsyncioTestCase):
    def setUp(self):
        self.broker = OutboundReceiptBroker(['desktop.zzc'])
    def response(self, outgoing, **overrides):
        meta = {'type':'outbound_receipt','version':1,'source_message_id':outgoing['id'],
                'nonce':outgoing['meta']['outbound_receipt_nonce'],'state':'queued','durable':True,
                'delivery_id':'dw-dlv_'+'a'*32}
        meta.update(overrides)
        return {'from':'workpulse#u_fixture','to':'desktop.zzc#u_fixture','body':'','meta':meta}
    def negotiate(self, ws):
        capability=self.broker.prepare(ws)
        self.broker.receive(ws,{'from':'workpulse#u_fixture','to':'desktop.zzc#u_fixture','meta':{
            'type':'outbound_capabilities','version':1,'nonce':capability['outbound_receipt_cap_nonce'],'durable_agent_queue':True}})
    def ws(self, handler):
        class SyntheticWS:
            def __init__(self): self.frames=[]
            async def send(inner, frame):
                outgoing=json.loads(frame);inner.frames.append(outgoing)
                handler(inner, outgoing)
        ws=SyntheticWS()
        self.negotiate(ws)
        return ws
    async def test_positive_immediate_receipt_after_waiter_registration(self):
        ws=self.ws(lambda conn, env: self.broker.receive(conn,self.response(env)))
        result=await self.broker.send(ws,'peer.zzc#u_fixture','synthetic reply','desktop#enrolled',{'kind':'reply','in_reply_to':'fixture'})
        self.assertIs(result['ok'],True)
        self.assertEqual(result['stage'],'hub_durable_queued')
        self.assertEqual(ws.frames[0]['meta']['in_reply_to'],'fixture')
        self.assertEqual(self.broker.pending,{})
        self.assertNotIn('nonce',result)
    async def test_rejected_not_accepted(self):
        ws=self.ws(lambda conn,env:self.broker.receive(conn,self.response(env,state='rejected',durable=False,delivery_id='')))
        with self.assertRaisesRegex(OutboundReceiptError,'Hub rejected'):
            await self.broker.send(ws,'peer#fixture','synthetic','desktop#fixture')
        self.assertEqual(self.broker.pending,{})
    async def test_wrong_nonce_or_other_connection_not_accepted(self):
        def handler(conn,env):
            self.assertTrue(self.broker.receive(conn,self.response(env,nonce='wrong')))
            self.assertTrue(self.broker.receive(object(),self.response(env)))
        ws=self.ws(handler)
        with self.assertRaisesRegex(OutboundReceiptError,'uncertain'):
            await self.broker.send(ws,'peer#fixture','synthetic','desktop#fixture',timeout=.02)
        self.assertEqual(len(ws.frames),1)
    async def test_wrong_source_wrong_target_and_weak_ack_not_accepted(self):
        def handler(conn,env):
            response=self.response(env);response['from']='other.zzc#u_fixture';self.broker.receive(conn,response)
            response=self.response(env);response['to']='other.zzc#u_fixture';self.broker.receive(conn,response)
            self.broker.receive(conn,self.response(env,durable=1))
            self.broker.receive(conn,self.response(env,delivery_id='direct_fixture'))
        ws=self.ws(handler)
        with self.assertRaises(OutboundReceiptError):
            await self.broker.send(ws,'peer#fixture','synthetic','desktop#fixture',timeout=.02)
    async def test_cancel_cleanup_no_resend(self):
        ws=self.ws(lambda *_:None)
        task=asyncio.create_task(self.broker.send(ws,'peer#fixture','synthetic','desktop#fixture'))
        await asyncio.sleep(0)
        task.cancel()
        with self.assertRaises(asyncio.CancelledError): await task
        self.assertEqual(self.broker.pending,{})
        self.assertEqual(len(ws.frames),1)
    async def test_abort_during_blocked_send_then_cancel_has_no_unhandled_future(self):
        seen=[]
        loop=asyncio.get_running_loop()
        previous=loop.get_exception_handler()
        loop.set_exception_handler(lambda _loop,context:seen.append(context))
        broker=self.broker
        class BlockedWS:
            async def send(inner,_raw):
                broker.abort(inner)
                await asyncio.Future()
        ws=BlockedWS()
        self.negotiate(ws)
        task=asyncio.create_task(broker.send(ws,'peer#fixture','synthetic','desktop#fixture'))
        try:
            await asyncio.sleep(0)
            task.cancel()
            with self.assertRaises(asyncio.CancelledError):await task
            import gc;gc.collect()
            await asyncio.sleep(0)
            self.assertEqual(seen,[])
            self.assertEqual(broker.pending,{})
        finally:loop.set_exception_handler(previous)

    async def test_disconnect_only_own_connection(self):
        ws=self.ws(lambda conn,_:self.broker.abort(conn))
        with self.assertRaisesRegex(OutboundReceiptError,'connection closed'):
            await self.broker.send(ws,'peer#fixture','synthetic','desktop#fixture')
        self.assertEqual(self.broker.pending,{})
    async def test_actual_helper_send_and_receive_control_wiring(self):
        from types import SimpleNamespace
        from sessionhelper.app import SessionHelper
        helper=SessionHelper.__new__(SessionHelper)
        helper.outbound_receipts=self.broker
        helper.book=SimpleNamespace(key_id='enrolled',self_addr='desktop#enrolled')
        broker=self.broker
        outer=self
        class SyntheticDuplexWS:
            def __init__(inner):inner.frames=[];inner.response=None
            async def send(inner,raw):
                env=json.loads(raw);inner.frames.append(env)
                inner.response=json.dumps(outer.response(env))
            def __aiter__(inner):return inner
            async def __anext__(inner):
                if inner.response is None:raise StopAsyncIteration
                raw=inner.response;inner.response=None;return raw
        ws=SyntheticDuplexWS()
        self.negotiate(ws)
        task=asyncio.create_task(helper._make_confirmed_send_handler(ws)('peer.zzc','synthetic answer',{'in_reply_to':'fixture'}))
        await asyncio.sleep(0)
        await helper.recv_loop(ws)
        result=await task
        self.assertEqual(result['stage'],'hub_durable_queued')
        self.assertEqual(ws.frames[0]['to'],'peer.zzc#enrolled')
        self.assertEqual(self.broker.pending,{})

    async def test_raw_send_receipt_is_not_durable(self):
        ws=self.ws(lambda conn,env:self.broker.receive(conn,{'sent':True,'to':env['to']}))
        with self.assertRaises(OutboundReceiptError):
            await self.broker.send(ws,'peer#fixture','synthetic','desktop#fixture',timeout=.02)
    async def test_old_hub_or_wrong_capability_cannot_send_body(self):
        ws=self.ws(lambda *_:None)
        cap=self.broker.prepare(ws)  # no current-generation Hub response yet
        self.broker.receive(ws,{'from':'workpulse#u_fixture','to':'desktop.zzc#u_fixture','meta':{
            'type':'outbound_capabilities','version':1,'nonce':'wrong','durable_agent_queue':True}})
        with self.assertRaisesRegex(OutboundReceiptError,'not negotiated'):
            await self.broker.send(ws,'peer#fixture','must not write','desktop#fixture')
        self.assertEqual(ws.frames,[])
        self.assertEqual(self.broker.pending,{})
        self.broker.receive(object(),{'from':'workpulse#u_fixture','to':'desktop.zzc#u_fixture','meta':{
            'type':'outbound_capabilities','version':1,'nonce':cap['outbound_receipt_cap_nonce'],'durable_agent_queue':True}})
        self.assertFalse(self.broker.negotiated)

    async def test_unknown_control_consumed_without_inbox_or_ai(self):
        self.assertTrue(self.broker.receive(object(),{'meta':{'type':'outbound_receipt','source_message_id':'unknown'}}))
        self.assertFalse(self.broker.receive(object(),{'meta':{'type':'delivery'}}))


if __name__=='__main__':unittest.main()
