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
        self.requests=[]; self.error_response=None; outer=self
        class Handler(BaseHTTPRequestHandler):
            def log_message(self,*args):pass
            def do_POST(self):self.handle_write()
            def do_PATCH(self):self.handle_write()
            def handle_write(self):
                outer.requests.append((self.command,self.path,dict(self.headers),json.loads(self.rfile.read(int(self.headers['Content-Length'])))))
                if outer.error_response is not None:
                    status, body = outer.error_response
                    self.send_response(status); self.end_headers(); self.wfile.write(body)
                    return
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

    async def test_frozen_409_create_and_update_are_explicit_rejections(self):
        message = "旧规格已冻结，请在仓库 OpenSpec 中提交新需求；本请求未写入。"
        for action in ("create", "update"):
            for supplied in (message, None, "", 123):
                with self.subTest(action=action, message=supplied):
                    self.error_response = (409, json.dumps({"error": "legacy_spec_frozen", "message": supplied}, ensure_ascii=False).encode())
                    before = len(self.requests)
                    result = await self.handler(self.args(action))
                    self.assertEqual(len(self.requests), before + 1)
                    self.assertEqual(result["status"], "error")
                    self.assertEqual(result["http_status"], 409)
                    self.assertEqual(result["code"], "legacy_spec_frozen")
                    self.assertEqual(result["outcome"], "rejected")
                    self.assertIs(result["retryable"], False)
                    if supplied == message:
                        self.assertEqual(result["message"], message)
                    else:
                        self.assertIn("旧规格已冻结", result["message"])
                        self.assertIn("OpenSpec", result["message"])
                    self.assertNotIn("revision", result["message"])
                    self.assertIn("legacy_spec_frozen", result["hub_error"])

    async def test_revision_conflict_409_keeps_existing_message(self):
        for action in ("create", "update"):
            with self.subTest(action=action):
                self.error_response = (409, b'{"error":"revision_conflict","message":"legacy_spec_frozen is only text"}')
                result = await self.handler(self.args(action))
                self.assertEqual(result["http_status"], 409)
                self.assertEqual(result["message"], "spec 已被修改，请读取最新 revision 后重试")
                self.assertNotIn("code", result)
                self.assertNotIn("outcome", result)
                self.assertNotIn("retryable", result)

    async def test_unparseable_409_uses_conservative_message(self):
        for body in (b"", b"not json legacy_spec_frozen", b'{"error":"legacy_spec_frozen",',
                     b"[]", b"null", b'"legacy_spec_frozen"', b"{}", b'{"error":123}'):
            for action in ("create", "update"):
                with self.subTest(body=body, action=action):
                    self.error_response = (409, body)
                    result = await self.handler(self.args(action))
                    self.assertEqual(result["http_status"], 409)
                    self.assertIn("原因未能解析", result["message"])
                    self.assertIn("勿直接重试", result["message"])
                    self.assertNotIn("revision", result["message"])
                    self.assertNotIn("code", result)

    async def test_frozen_detail_is_redacted_before_top_level_message(self):
        self.error_response = (409, json.dumps({"error": "legacy_spec_frozen",
            "message": "旧规格已冻结，请改用仓库 OpenSpec synthetic-local-credential"}).encode())
        result = await self.handler(self.args())
        self.assertEqual(result["code"], "legacy_spec_frozen")
        self.assertNotIn("synthetic-local-credential", json.dumps(result))
        self.assertIn("[redacted]", result["message"])

    async def test_frozen_error_code_on_non409_does_not_use_frozen_mapping(self):
        self.error_response = (403, b'{"error":"legacy_spec_frozen"}')
        result = await self.handler(self.args())
        self.assertEqual(result["http_status"], 403)
        self.assertIn("当前 Agent 自建", result["message"])
        self.assertNotIn("code", result)
