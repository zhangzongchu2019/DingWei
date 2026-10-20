import asyncio
import inspect
import json
import os
from pathlib import Path
import tempfile
import threading
from types import SimpleNamespace
import unittest
from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer
from unittest.mock import patch

from sessionhelper.project_context import context_request,validate_args,SCHEMA,MAX_BYTES
from sessionhelper.app import SessionHelper
from sessionhelper.enroll import Credential
from sessionhelper.ipc_listener import serve_ipc
from sessionhelper import mcp_shim


def payload(scope='all'):
    data={'meta':{'schema':SCHEMA,'source':'current_hub','scope':scope,'complete':True,'next_cursor':'','generated_at':'fixture','observed_at':'fixture'}}
    for key in ('tasks','sessions','config') if scope=='all' else (scope,):
        page={'items':[],'next_cursor':''}
        data[key]={'specs':page,'context_schema':SCHEMA} if key=='config' else page
    return data


class ContextTests(unittest.TestCase):
    def server(self,body=None,status=200,location=None):
        seen=[]
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                seen.append((self.path,self.headers.get('Authorization')))
                self.send_response(status)
                if location:self.send_header('Location',location)
                self.end_headers()
                self.wfile.write(json.dumps(body if body is not None else payload()).encode())
            def log_message(self,*a):pass
        s=ThreadingHTTPServer(('127.0.0.1',0),Handler)
        t=threading.Thread(target=s.serve_forever,daemon=True);t.start()
        self.addCleanup(s.server_close);self.addCleanup(s.shutdown);self.addCleanup(lambda:t.join(timeout=1))
        return 'ws://127.0.0.1:'+str(s.server_port),seen

    def test_four_scopes_one_http_no_datad_fallback(self):
        for scope in ('tasks','sessions','config','all'):
            with self.subTest(scope=scope):
                url,seen=self.server(payload(scope))
                with patch.dict(os.environ,{'SH_DATAD_URL':'http://must-not-use.invalid','DATAD_TOKEN':'PRIVATE_FIXTURE'}):
                    out=context_request(url,{'Authorization':'Bearer local-fixture'},{'scope':scope})
                self.assertEqual(out['status'],'ok','PC_HTTP_SCOPE_FAILED')
                self.assertEqual(len(seen),1,'PC_HTTP_RETRIED')
                self.assertTrue(seen[0][0].startswith('/api/agent/console/project-context?'))
                self.assertEqual(seen[0][1],'Bearer local-fixture')
                self.assertNotIn('PRIVATE_FIXTURE',json.dumps(out))

    def test_redirect_never_forwards_identity(self):
        url,seen=self.server(status=302,location='/leaked')
        result=context_request(url,{'Authorization':'Bearer fixture'},{'scope':'all'})
        self.assertEqual(result['status'],'error')
        self.assertEqual(len(seen),1,'PC_CREDENTIAL_REDIRECTED')

    def test_old_hub_explicit_unsupported(self):
        for status in (404,426):
            url,seen=self.server(status=status)
            result=context_request(url,{}, {'scope':'all'})
            self.assertEqual(result['error'],'project_context_unsupported','PC_OLD_HUB_FAKE_EMPTY')
            self.assertEqual(len(seen),1)

    def test_invalid_arguments_never_escape_to_http(self):
        cases=({'scope':'wrong'},{'scope':'all','limit':True},{'scope':'all','limit':0},{'scope':'all','limit':101},
               {'scope':'all','cursor':1},{'scope':'all','cursor':'x'*4097},{'scope':'all','headers':{}},{'scope':'all','owner':'other'})
        for args in cases:
            with self.subTest(args=args),self.assertRaises(ValueError):validate_args(args)

    def test_bad_schema_body_and_oversize_fail_closed(self):
        cases=[[],{}, {'meta':{'schema':'datad'}}, {**payload(),'unexpected':'private'},
               {**payload(),'meta':{**payload()['meta'],'complete':False}},
               {**payload(),'tasks':{'items':'not-list','next_cursor':''}},
               {**payload(),'tasks':{'items':[{}]*51,'next_cursor':''}},
               {**payload(),'sessions':{'items':[{'oversize':'x'*(MAX_BYTES+1)}],'next_cursor':''}}]
        for body in cases:
            with self.subTest(kind=type(body).__name__):
                url,_=self.server(body)
                out=context_request(url,{}, {'scope':'all'})
                self.assertEqual(out['status'],'error','PC_BAD_SCHEMA_ACCEPTED')
                self.assertNotIn('tasks',out)

    def test_helper_dedicated_identity_and_warning_redaction(self):
        h=SessionHelper.__new__(SessionHelper)
        h.enroll_credential=None
        url,seen=self.server()
        h.cfg=SimpleNamespace(ws_url=url,secret='LOCAL_SHARED_FIXTURE',ws_base=url,tool='',os_name='linux')
        result=asyncio.run(h._make_project_context_handler()({'body':{'scope':'all'}}))
        self.assertEqual(result.get('error'),'dedicated_agent_credential_required','PC_SHARED_CREDENTIAL_FALLBACK')
        url,seen=self.server()
        h.enroll_credential=Credential('fixture','PRIVATE_FIXTURE')
        h.cfg=SimpleNamespace(ws_base=url,tool='',os_name='linux')
        with patch.dict(os.environ,{'SH_DATAD_TOKEN':'DO_NOT_LOG'}),patch('builtins.print') as log:
            for _ in range(2):
                out=asyncio.run(h._make_project_context_handler()({'body':{'scope':'all'}}))
                self.assertEqual(out['status'],'ok')
            text=str(log.call_args_list)
            self.assertIn('SH_DATAD_TOKEN',text)
            self.assertNotIn('DO_NOT_LOG',text)
            self.assertEqual(log.call_count,1)
        self.assertEqual(len(seen),2)

    def test_legacy_context_block_precedes_backend(self):
        called=[]
        async def backend(*args):
            called.append(args)
            return {'legacy':True}
        result=asyncio.run(SessionHelper._block_legacy_context(backend)('GET','/v1/tasks?owner=me',None))
        self.assertEqual(result.get('error'),'upgrade_required','PC_LEGACY_DATAD_CONTEXT_FALLBACK')
        self.assertEqual(called,[])

    def test_old_shim_context_urls_refused_without_datad_dependency(self):
        h=SessionHelper.__new__(SessionHelper)
        h.cfg=SimpleNamespace(datad_url='http://must-not-use.invalid',datad_token='PRIVATE_FIXTURE')
        handler=h._make_vsapi_handler()
        for path in ('/v1/tasks?owner=me','/v1/sessions','/v1/config'):
            result=asyncio.run(handler('GET',path,None))
            self.assertEqual(result['error'],'upgrade_required','PC_LEGACY_DATAD_CONTEXT_FALLBACK')

    def test_old_helper_is_explicit_upgrade_error(self):
        with patch.object(mcp_shim,'_ipc_request',side_effect=mcp_shim._IPCFailure('helper_rejected')):
            with self.assertRaisesRegex(mcp_shim._IPCFailure,'project_context_unsupported'):
                mcp_shim._project_context({'scope':'all'})

    def test_production_ipc_factory_is_wired(self):
        code=inspect.getsource(SessionHelper._ipc_server)
        self.assertIn('project_context_handler=self._make_project_context_handler()',code,'PC_PRODUCTION_HANDLER_UNWIRED')
        caps=inspect.getsource(SessionHelper.session_capabilities_envelope)
        self.assertIn('project_context_v1',caps,'PC_CAPABILITY_UNWIRED')

    def test_real_ipc_helper_http_and_shim_with_restricted_mode_rejection(self):
        url,seen=self.server()
        h=SessionHelper.__new__(SessionHelper)
        h.enroll_credential=Credential('fixture','PRIVATE_FIXTURE')
        h.cfg=SimpleNamespace(ws_base=url,tool='',os_name='linux')
        async def run():
            with tempfile.TemporaryDirectory(prefix='pc-ipc-') as root:
                sock=str(Path(root)/'ipc.sock')
                async def send(*a):raise AssertionError('context invoked send')
                for restricted in ('normal','upload_only','send_body_only'):
                    bound=asyncio.Event()
                    task=asyncio.create_task(serve_ipc(sock,send,bound=bound,project_context_handler=h._make_project_context_handler(),
                           upload_only=restricted=='upload_only',send_body_only=restricted=='send_body_only'))
                    try:
                        await asyncio.wait_for(bound.wait(),2)
                        with patch.object(mcp_shim,'_IPC_SOCK',sock):
                            if restricted=='normal':
                                result=await asyncio.to_thread(mcp_shim._project_context,{'scope':'all'})
                                self.assertEqual(result['meta']['schema'],SCHEMA,'PC_IPC_RESULT_LOST')
                            else:
                                with self.assertRaises(mcp_shim._IPCFailure,msg='PC_RESTRICTED_MODE_ESCAPED'):
                                    await asyncio.to_thread(mcp_shim._project_context,{'scope':'all'})
                    finally:
                        task.cancel()
                        try:await task
                        except asyncio.CancelledError:pass
        asyncio.run(run())
        self.assertEqual(len(seen),1,'PC_RESTRICTED_MODE_ESCAPED')
