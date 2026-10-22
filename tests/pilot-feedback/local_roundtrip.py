"""Real production MCP/IPC/HTTP calls against a loopback Go fixture, no CLI."""
import asyncio
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import urllib.parse

async def main():
    fixture=json.loads(Path(sys.argv[1]).read_text())
    target=urllib.parse.urlsplit(fixture['origin'])
    if target.scheme!='http' or target.hostname!='127.0.0.1':raise ValueError('local fixture only')
    with tempfile.TemporaryDirectory(prefix='blocker-rt-',dir='/tmp') as temp:
        root=Path(temp);home=root/'home';home.mkdir(mode=0o700);state=root/'state';state.mkdir(mode=0o700);os.environ['HOME']=str(home)
        from sessionhelper.app import SessionHelper
        from sessionhelper.config import Config
        from sessionhelper.enroll import Credential
        cfg=Config('fixture','enrolled','SYNTHETIC',mode='driver',cli_cwd=str(root),state_dir=str(state),codex_home=str(home/'.codex'),inbox_db=str(state/'inbox.sqlite'),mcp_ipc_sock=str(root/'mcp.sock'),ws_base=fixture['origin'].replace('http:','ws:',1))
        helper=SessionHelper(cfg);helper.enroll_credential=Credential(fixture['agent_id'],fixture['credential'])
        ipc=asyncio.create_task(helper._ipc_server(None))
        async def call(name,args):
            env=dict(os.environ);env['DINGWEI_MCP_IPC_SOCK']=cfg.mcp_ipc_sock
            requests=[{'jsonrpc':'2.0','id':1,'method':'initialize','params':{}},{'jsonrpc':'2.0','id':2,'method':'tools/call','params':{'name':name,'arguments':args}}]
            p=await asyncio.to_thread(subprocess.run,[sys.executable,'-B','-m','sessionhelper','--mcp'],input=''.join(json.dumps(r)+'\n' for r in requests),env=env,cwd=root,text=True,capture_output=True,timeout=20)
            assert p.returncode==0 and not p.stderr,'QUESTION_REAL_MCP_PROCESS_FAILED'
            replies={r['id']:r for r in map(json.loads,p.stdout.splitlines()) if 'id' in r}
            result=replies[2]['result'];assert not result.get('isError'),'QUESTION_REAL_IPC_TOOL_ERROR'
            data=json.loads(result['content'][0]['text']);assert data['execution_authorized'] is False,'QUESTION_MCP_GRANTED_EXECUTION'
            return data
        try:
            await asyncio.wait_for(helper.ipc_bound.wait(),5)
            out=await call('dw_task_question_create',{'task_id':fixture['task_id'],'request_id':'mcp-create','expected_version':fixture['task_version'],'kind':'technical','title':'待核技术事实','observed_fact':'原任务技术缺口','question':'请提供核验材料','expected_answer':'材料与证据','entry_refs':['A1']})
            q=out['question'];qid=q['id'];assert q['state']=='needs_owner','QUESTION_REAL_CREATE_LOST'
            listing=await call('dw_task_question_list',{'task_id':fixture['task_id']})
            assert len(listing['questions'])==1 and listing['questions'][0]['id']==qid,'QUESTION_REAL_LIST_LOST'
            detail=await call('dw_task_question_list',{'question_id':qid})
            assert detail['answers']==[],'QUESTION_REAL_DETAIL_LOST'
            out=await call('dw_spec_clarifications',{'action':'technical','question_id':qid,'request_id':'mcp-input','entry_refs':['A1'],'body':'真实技术材料，仅供重核','evidence_ref':'local:immutable-fixture'})
            assert out['post']['kind']=='technical' and out['post']['entry_refs']==['A1'],'QUESTION_TECHNICAL_CLARIFICATION_NOT_RECORDED'
            detail=await call('dw_task_question_list',{'question_id':qid})
            assert detail['question']['state']=='answered_pending_recheck' and detail['question']['recheck_state']=='ready_to_recheck' and detail['question']['task_status']=='planned','QUESTION_TECHNICAL_INPUT_DID_NOT_WAIT_FOR_RECHECK'
            print(json.dumps({'result':'LOCAL_QUESTION_ROUNDTRIP_PASS','transport':'real MCP stdio -> actual helper IPC -> Go HTTP','mode':cfg.mode,'operations':4,'business_execution':False}))
        finally:
            ipc.cancel()
            try:await ipc
            except asyncio.CancelledError:pass

if __name__=='__main__':asyncio.run(main())
