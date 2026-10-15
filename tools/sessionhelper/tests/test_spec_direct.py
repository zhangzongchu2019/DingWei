"""Direct helper control-plane contract; real local HTTP, no production writes."""
import asyncio
import json
import threading
import types
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest.mock import patch
from sessionhelper.app import SessionHelper
from sessionhelper import mcp_shim


class SpecDirectTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.requests=[]; outer=self
        class Handler(BaseHTTPRequestHandler):
            def log_message(self,*args):pass
            def do_POST(self):self.handle_write()
            def do_PATCH(self):self.handle_write()
            def handle_write(self):
                outer.requests.append((self.command,self.path,dict(self.headers),json.loads(self.rfile.read(int(self.headers['Content-Length'])))))
                status=403 if self.path.endswith('denied') else 201 if self.command=='POST' else 200
                self.send_response(status);self.end_headers();self.wfile.write(json.dumps({'id':'spec-local','current_revision':1,'revision':2}).encode())
        self.server=ThreadingHTTPServer(('127.0.0.1',0),Handler)
        self.thread=threading.Thread(target=self.server.serve_forever,daemon=True);self.thread.start()
        self.owner=types.SimpleNamespace(enroll_credential=object(),_connection=lambda:('ws://127.0.0.1:'+str(self.server.server_port)+'/ws/agent',{'Authorization':'Bearer synthetic-local-credential'}))
        self.handler=SessionHelper._make_spec_write_handler(self.owner,None)
    async def asyncTearDown(self):
        await asyncio.to_thread(self.server.shutdown);self.server.server_close();self.thread.join()
    def args(self,action='create',id='spec-local'):
        return dict(action=action,protocol='direct-v1',spec_id=id,body={'title':'local','expected_revision':1})
    async def test_first_call_writes_once_no_grant_or_capability(self):
        result=await self.handler(self.args())
        self.assertEqual(result['status'],'ok');self.assertEqual(len(self.requests),1)
        headers=self.requests[0][2]
        self.assertEqual(headers.get('X-Dw-Spec-Write'),'direct-v1')
        self.assertTrue(headers.get('Authorization')=='Bearer synthetic-local-credential')
        self.assertNotIn('X-Dw-Console-Capability',headers)
        self.assertNotIn('synthetic-local-credential',json.dumps(result))
    async def test_old_ipc_or_missing_agent_identity_has_zero_http(self):
        for args in (dict(action='create',body={}),dict(self.args(),grant_request_id='old-request')):
            self.assertEqual((await self.handler(args))['status'],'upgrade_required')
        self.owner.enroll_credential=None
        self.assertEqual((await self.handler(self.args()))['status'],'upgrade_required')
        self.assertEqual(self.requests,[])
    async def test_fixed_chinese_other_creator_error(self):
        result=await self.handler(self.args('update','denied'))
        self.assertEqual(result['http_status'],403);self.assertIn('当前 Agent 自建',result['message'])
    async def test_transport_error_is_unknown_and_not_retryable(self):
        with patch('urllib.request.urlopen',side_effect=OSError('synthetic')):
            result=await self.handler(self.args())
        self.assertEqual(result['outcome'],'unknown');self.assertFalse(result['retryable'])
    async def test_model_schema_has_no_retired_grant(self):
        for name in ('dw_spec_create','dw_spec_update'):
            # Actual public tools/list payload, not a duplicate schema fixture.
            listing=mcp_shim._dispatch(mcp_shim.Request('2.0','tools/list',{},1))
            tool=next(t for t in listing.result['tools'] if t['name']==name)
            self.assertNotIn('grant_request_id',tool['inputSchema']['properties'])
            self.assertNotIn('capability',tool['inputSchema']['properties'])
