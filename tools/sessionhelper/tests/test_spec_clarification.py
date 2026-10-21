"""Anchored Agent discussion remains data and can never claim human authority."""
import copy
import io
import json
import threading
import unittest
import urllib.error
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest.mock import patch
from sessionhelper import spec_clarification as api, mcp_shim
from sessionhelper import pilot_question

class ClarificationToolTest(unittest.TestCase):
    def technical(self):
        return {'action':'technical','question_id':'q','request_id':'stable','entry_refs':['A1'],'body':'技术证据','evidence_ref':'git:commit'}

    def test_technical_tool_has_no_human_authority_and_preserves_id(self):
        args=self.technical()
        with patch.object(mcp_shim,'_ipc_request',return_value={'execution_authorized':False}) as call:
            mcp_shim._dw_spec_clarifications(args)
        req=call.call_args.args[0]
        self.assertEqual(req['type'],'spec_clarification_v1')
        self.assertEqual(req['body']['request_id'],'stable')
        for field in ('kind','role','source','actor','pm_user','url','corrects','confirms','tenant','open_id','evidence'):
            with patch.object(mcp_shim,'_ipc_request') as call,self.assertRaises(ValueError,msg='CLARIFICATION_HELPER_AUTHORITY_SELECTOR'):
                mcp_shim._dw_spec_clarifications(dict(args,**{field:'forged'}))
            call.assert_not_called()
        for refs in (None,[],['A1','A1'],['A1\n'],[False],'A1'):
            with self.assertRaises(ValueError,msg='CLARIFICATION_HELPER_ANCHORS_WEAK'):
                mcp_shim._dw_spec_clarifications(dict(args,entry_refs=refs))
        for action in ('explanation','scope_change','confirmation','delete'):
            with self.assertRaises(ValueError):mcp_shim._dw_spec_clarifications(dict(args,action=action))

    def test_read_status_and_pagination_disjoint(self):
        for args in ({'action':'list','question_id':'q','limit':100,'before':4},{'action':'status','question_id':'q','request_id':'original'}):
            with patch.object(mcp_shim,'_ipc_request',return_value={}):mcp_shim._dw_spec_clarifications(args)
        for args in ({'action':'list','question_id':'q','request_id':'r'},{'action':'status','question_id':'q','request_id':'r','limit':1},{'action':'list','question_id':'q','limit':True}):
            with self.assertRaises(ValueError):mcp_shim._dw_spec_clarifications(args)

    def test_wire_route_is_technical_no_auto_retry_no_redirect(self):
        seen=[]
        class Handler(BaseHTTPRequestHandler):
            def do_POST(self):
                body=json.loads(self.rfile.read(int(self.headers['Content-Length'])))
                seen.append((self.path,body,self.headers.get('Authorization')))
                self.send_response(302);self.send_header('Location','/leak');self.end_headers()
            def log_message(self,*args):pass
        server=ThreadingHTTPServer(('127.0.0.1',0),Handler)
        thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
        try:
            a=self.technical();action=a.pop('action');qid=a.pop('question_id')
            req={'action':action,'question_id':qid,'body':a}
            out=api.clarification_request('ws://127.0.0.1:'+str(server.server_port),{'Authorization':'Bearer SYNTHETIC'},req)
            self.assertEqual(out['http_status'],302)
            self.assertEqual(len(seen),1,'CLARIFICATION_REDIRECT_CREDENTIAL_LEAK')
            self.assertEqual(seen[0][0],'/api/agent/console/questions/q/clarifications')
            self.assertEqual(seen[0][1]['kind'],'technical','CLARIFICATION_AGENT_WIRE_AUTHORITY')
        finally:server.shutdown();server.server_close();thread.join()

    def test_unknown_preserves_id_and_status_lookup_never_posts(self):
        args=self.technical();args['body']={'request_id':args['request_id'],'entry_refs':args['entry_refs'],'body':args['body']};args={k:v for k,v in args.items() if k in ('action','question_id','body')}
        for exc in (OSError('SENSITIVE'),urllib.error.HTTPError('https://hub.invalid',503,'SENSITIVE',{},None)):
            with patch('urllib.request.OpenerDirector.open',side_effect=exc) as call:
                out=api.clarification_request('wss://hub.invalid',{},args)
            self.assertEqual(out['outcome'],'unknown');self.assertTrue(out['reuse_request_id']);self.assertFalse(out['retryable']);self.assertEqual(call.call_count,1);self.assertNotIn('SENSITIVE',json.dumps(out))
        req={'action':'status','question_id':'q','body':{'request_id':'original/id'}}
        with patch('urllib.request.OpenerDirector.open',side_effect=urllib.error.HTTPError('https://hub.invalid',404,'missing',{},None)) as call:
            out=api.clarification_request('wss://hub.invalid',{},req)
        self.assertEqual(out['error'],'clarification_request_not_recorded')
        self.assertEqual(call.call_args.args[0].method,'GET')
        self.assertIn('/requests/original%2Fid',call.call_args.args[0].full_url)

    def test_old_helper_and_old_hub_fail_closed(self):
        with patch.object(mcp_shim,'_ipc_request',side_effect=mcp_shim._IPCFailure('helper_rejected')) as call:
            with self.assertRaisesRegex(mcp_shim._IPCFailure,'clarification_unsupported_upgrade_helper'):mcp_shim._dw_spec_clarifications(self.technical())
        self.assertEqual(call.call_count,1)
        with patch('urllib.request.OpenerDirector.open',side_effect=urllib.error.HTTPError('https://hub.invalid',426,'upgrade',{},None)):
            result=pilot_question.question_request('wss://hub.invalid',{}, {'action':'list','body':{}})
        self.assertEqual(result['error'],'clarification_requires_anchored_tools_upgrade_helper')

    def test_response_is_data_without_execution(self):
        class Response(io.BytesIO):
            status=200
            def __enter__(self):return self
            def __exit__(self,*_):self.close()
        for payload in ({},{'execution_authorized':True},{'execution_authorized':0}):
            with patch('urllib.request.OpenerDirector.open',return_value=Response(json.dumps(payload).encode())):
                out=api.clarification_request('wss://hub.invalid',{}, {'action':'list','question_id':'q','body':{}})
            self.assertEqual(out['status'],'error','CLARIFICATION_HELPER_EXECUTION_FLAG_WEAK')
        with patch('urllib.request.OpenerDirector.open',return_value=Response(b'{"posts":[],"execution_authorized":false}')):
            out=api.clarification_request('wss://hub.invalid',{}, {'action':'list','question_id':'q','body':{}})
        self.assertTrue(out['data_only']);self.assertFalse(out['execution_authorized'])

    def test_create_question_passes_typed_refs(self):
        from test_pilot_question import create
        with patch.object(mcp_shim,'_ipc_request',return_value={}) as call:
            mcp_shim._dw_task_question_create(dict(create(),entry_refs=['A1']))
        self.assertEqual(call.call_args.args[0]['body']['entry_refs'],['A1'])

if __name__=='__main__':unittest.main()
