"""Question tools use current Hub identity and cannot impersonate human PM decisions."""
import copy
import json
import threading
import unittest
import urllib.error
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest.mock import patch
from sessionhelper import pilot_question as api, mcp_shim


def create():
    return {'task_id':'task','request_id':'stable','expected_version':1,'kind':'technical','title':'问题','observed_fact':'实际现象','question':'需明确内容','expected_answer':'结论与证据'}

class PilotQuestionTest(unittest.TestCase):
    def test_mcp_create_only_current_task_no_human_identity(self):
        with patch.object(mcp_shim,'_ipc_request',return_value={'execution_authorized':False}) as call:
            result=mcp_shim._dw_task_question_create(create())
        self.assertFalse(result['execution_authorized'])
        req=call.call_args.args[0]
        self.assertEqual(req['type'],'pilot_question_v1');self.assertEqual(req['task_id'],'task')
        self.assertEqual(req['body']['request_id'],'stable')
        for field in ('pm_user','leader_user','responsible_principal','agent_id','credential','url','operation','scope_change'):
            with patch.object(mcp_shim,'_ipc_request',return_value={}),self.subTest(field=field),self.assertRaises(ValueError,msg='QUESTION_FORGED_MCP_IDENTITY'):
                mcp_shim._dw_task_question_create(dict(create(),**{field:'forged'}))
        for value in (True,False,0,'1',None):
            with patch.object(mcp_shim,'_ipc_request',return_value={}),self.assertRaises(ValueError,msg='QUESTION_VERSION_TYPE_WEAK'):mcp_shim._dw_task_question_create(dict(create(),expected_version=value))

    def test_input_is_technical_material_not_business_answer(self):
        args={'question_id':'question','request_id':'stable','expected_version':1,'event_id':'event','body':'技术材料','evidence_ref':'git:immutable'}
        with patch.object(mcp_shim,'_ipc_request',return_value={}) as call:mcp_shim._dw_task_question_input(args)
        self.assertEqual(call.call_args.args[0]['action'],'input')
        for field in ('answer_kind','scope_change','operation','pm_user','responsible_principal','task_id'):
            with patch.object(mcp_shim,'_ipc_request',return_value={}),self.assertRaises(ValueError,msg='QUESTION_AGENT_BUSINESS_DECISION'):
                mcp_shim._dw_task_question_input(dict(args,**{field:'forged'}))

    def test_list_detail_and_collection_are_disjoint(self):
        for args in ({},{'task_id':'task','limit':100},{'spec_id':'spec','cursor':'opaque'},{'question_id':'q','before':4,'change_before':8}):
            with patch.object(mcp_shim,'_ipc_request',return_value={}):mcp_shim._dw_task_question_list(args)
        for args in ({'question_id':'q','task_id':'task'},{'before':1},{'limit':101},{'limit':True},{'question_id':'q\nforged'},{'url':'https://outside.invalid'}):
            with patch.object(mcp_shim,'_ipc_request',return_value={}),self.assertRaises(ValueError,msg='QUESTION_QUERY_SCOPE_FORGED'):mcp_shim._dw_task_question_list(args)

    def test_old_helper_safe_failure_no_alternate_route(self):
        for method,args in ((mcp_shim._dw_task_question_create,create()),(mcp_shim._dw_task_question_list,{})):
            with patch.object(mcp_shim,'_ipc_request',side_effect=mcp_shim._IPCFailure('helper_rejected')) as call:
                with self.assertRaisesRegex(mcp_shim._IPCFailure,'question_unsupported_upgrade_helper'):method(args)
            self.assertEqual(call.call_count,1)

    def test_unknown_write_and_unsupported_hub_are_not_retried(self):
        args=create();req={'type':'pilot_question_v1','action':'create','task_id':args.pop('task_id'),'body':args}
        for exc in (OSError('private credential DO_NOT_RETURN'),urllib.error.HTTPError('https://hub.invalid',503,'secret',{},None)):
            with patch('urllib.request.OpenerDirector.open',side_effect=exc) as call:out=api.question_request('wss://hub.invalid/ws',{},req)
            self.assertEqual(out['outcome'],'unknown','QUESTION_UNKNOWN_WRITE_RETRIED');self.assertFalse(out['retryable']);self.assertEqual(call.call_count,1);self.assertNotIn('secret',json.dumps(out))
        with patch('urllib.request.OpenerDirector.open',side_effect=urllib.error.HTTPError('https://hub.invalid',404,'secret',{},None)):
            self.assertEqual(api.question_request('wss://hub.invalid',{},req)['error'],'question_unsupported_upgrade_hub')

    def test_real_http_redirect_never_forwards_credentials(self):
        seen=[]
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                seen.append((self.path,self.headers.get('Authorization')));self.send_response(302);self.send_header('Location','/leak');self.end_headers()
            def log_message(self,*args):pass
        server=ThreadingHTTPServer(('127.0.0.1',0),Handler);thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
        try:
            out=api.question_request('ws://127.0.0.1:'+str(server.server_port),{'Authorization':'Bearer SYNTHETIC'},{'action':'list','body':{'limit':1}})
            self.assertEqual(out['http_status'],302);self.assertEqual(len(seen),1,'QUESTION_REDIRECT_LEAK');self.assertEqual(seen[0][1],'Bearer SYNTHETIC')
        finally:server.shutdown();server.server_close();thread.join()

    def test_success_requires_explicit_no_execution(self):
        from io import BytesIO
        class Response(BytesIO):
            status=200
            def __enter__(self):return self
            def __exit__(self,*args):self.close()
        for payload in ({},{'execution_authorized':True},{'execution_authorized':0}):
            with patch('urllib.request.OpenerDirector.open',return_value=Response(json.dumps(payload).encode())):
                result=api.question_request('wss://hub.invalid',{}, {'action':'list','body':{}})
            self.assertEqual(result.get('error'),'question_read_failed','QUESTION_RESULT_EXECUTION_FLAG_WEAK')
        with patch('urllib.request.OpenerDirector.open',return_value=Response(b'{"execution_authorized":false,"questions":[]}')):
            self.assertEqual(api.question_request('wss://hub.invalid',{}, {'action':'list','body':{}})['status'],'ok')

    def test_three_tools_visible_and_no_pm_write_tool(self):
        from sessionhelper.mcp_shim import Request
        response=mcp_shim._dispatch(Request(id=1,jsonrpc='2.0',method='tools/list',params={}))
        names={x['name'] for x in response.result['tools']}
        self.assertEqual(len(names),20)
        self.assertTrue({'dw_task_question_create','dw_task_question_list','dw_task_question_input'}<=names,'QUESTION_MCP_REGISTRATION_MISSING')
        self.assertFalse(any('question_answer' in n or 'question_assign' in n for n in names))

if __name__=='__main__':unittest.main()
