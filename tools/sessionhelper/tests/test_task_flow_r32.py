import asyncio
import json
import os
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest.mock import patch

from sessionhelper.inbox import DeliveryInbox
from sessionhelper.task_flow import task_request
from sessionhelper import mcp_shim

class R32TaskFlowTest(unittest.TestCase):
    def test_driver_read_ack_never_consumes_delivery_and_is_session_scoped(self):
        with tempfile.TemporaryDirectory() as root:
            path=os.path.join(root,'inbox.sqlite')
            a=DeliveryInbox(path,session_name='a');b=DeliveryInbox(path,session_name='b')
            a.accept('delivery',{'body':'a-private'});b.accept('delivery',{'body':'b-private'})
            self.assertEqual(a.request({'operation':'status'})['unread'],1)
            self.assertEqual(a.request({'operation':'read','message_id':'delivery'})['message']['body'],'a-private')
            self.assertTrue(a.request({'operation':'ack','message_id':'delivery'})['local_only'])
            self.assertEqual(a.request({'operation':'status'})['unread'],0)
            self.assertEqual(b.request({'operation':'status'})['unread'],1)
            claimed=a.claim_next();self.assertIsNotNone(claimed,'DRIVER_READ_ACK_CONSUMED_DELIVERY');self.assertEqual(claimed[0],'delivery')
            self.assertEqual(a.request({'operation':'pause'})['error'],'mode_operation_unsupported')
            reopened=DeliveryInbox(path,session_name='a')
            self.assertEqual(reopened.request({'operation':'status'})['unread'],0)
    def test_task_notification_is_durable_deduplicated_and_not_executable(self):
        with tempfile.TemporaryDirectory() as root:
            inbox=DeliveryInbox(os.path.join(root,'inbox.sqlite'),session_name='a')
            notification={'body':'notification only','meta':{'kind':'task_assigned'}}
            inbox.accept_notification('task-event',notification);inbox.accept_notification('task-event',notification)
            self.assertIsNone(inbox.claim_next(),'TASK_NOTIFICATION_INJECTED')
            self.assertEqual(len(inbox.request({'operation':'list'})['messages']),1)
            self.assertEqual(inbox.request({'operation':'read','message_id':'task-event'})['message']['body'],'notification only')
    def test_mcp_task_tools_have_no_credentials_and_keep_request_id(self):
        self.assertIn('dw_task_list',mcp_shim._tools);self.assertIn('dw_task_update',mcp_shim._tools)
        with patch.object(mcp_shim,'_ipc_request',return_value={'status':'ok'}) as call:
            mcp_shim._dw_task_update({'task_id':'task:1','operation':'start','expected_version':2,'request_id':'stable-id'})
            args=call.call_args.args[0]
            self.assertEqual(args['body']['request_id'],'stable-id')
            self.assertNotIn('credential',json.dumps(args))
        with self.assertRaises(ValueError):
            mcp_shim._dw_task_update({'task_id':'x','operation':'start','expected_version':2,'request_id':'r','credential':'forged'})
    def test_http_uses_dedicated_header_and_never_follows_redirect(self):
        received=[]
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                received.append((self.path,self.headers.get('Authorization')))
                self.send_response(302);self.send_header('Location','/leak');self.end_headers()
            def log_message(self,*args):pass
        server=ThreadingHTTPServer(('127.0.0.1',0),Handler);thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
        try:
            result=task_request('ws://127.0.0.1:'+str(server.server_port)+'/ws',{'Authorization':'Bearer fixture-only'}, {'action':'list','body':{'limit':2}})
            self.assertEqual(result['http_status'],302)
            self.assertEqual(len(received),1,'TASK_CREDENTIAL_REDIRECTED')
            self.assertEqual(received[0][1],'Bearer fixture-only')
        finally:server.shutdown();server.server_close();thread.join()
    def test_http_503_mutation_is_unknown_not_rejected(self):
        import urllib.error
        failure=urllib.error.HTTPError('https://hub.invalid',503,'unavailable',{},None)
        with patch('urllib.request.OpenerDirector.open',side_effect=failure):
            result=task_request('wss://hub.invalid/ws',{}, {'action':'update','task_id':'task','body':{'request_id':'r','expected_version':1,'operation':'start'}})
            self.assertEqual(result.get('outcome'),'unknown','HTTP_503_CLAIMED_REJECTION')
            self.assertFalse(result['retryable'])
    def test_http_mutation_uncertainty_does_not_retry(self):
        with patch('urllib.request.OpenerDirector.open',side_effect=OSError('offline')) as call:
            result=task_request('wss://hub.invalid/ws',{}, {'action':'update','task_id':'task','body':{'request_id':'r','expected_version':1,'operation':'start'}})
            self.assertEqual(result['error'],'task_write_outcome_unknown');self.assertFalse(result['retryable']);self.assertEqual(call.call_count,1)

if __name__=='__main__':unittest.main()
