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
            assert p.returncode==0 and not p.stderr,'BLOCKER_REAL_MCP_PROCESS_FAILED'
            replies={r['id']:r for r in map(json.loads,p.stdout.splitlines()) if 'id' in r}
            result=replies[2]['result'];assert not result.get('isError'),'BLOCKER_REAL_IPC_TOOL_ERROR'
            data=json.loads(result['content'][0]['text']);assert data['execution_authorized'] is False,'BLOCKER_MCP_GRANTED_EXECUTION'
            return data
        try:
            await asyncio.wait_for(helper.ipc_bound.wait(),5)
            out=await call('dw_task_blocker_set',{'task_id':fixture['task_id'],'request_id':'mcp-create','expected_version':fixture['task_version'],'kind':'technical','where':'原任务等待材料','waiting_principal':'agent:'+fixture['agent_id'],'expected_input':'当前原任务真实材料'})
            b=out['blocker'];bid=b['id'];assert b['state']=='waiting','BLOCKER_REAL_CREATE_LOST'
            listing=await call('dw_task_blocker_list',{'task_id':fixture['task_id'],'blocker_id':bid})
            assert len(listing['blockers'])==1 and listing['inputs_complete'],'BLOCKER_REAL_LIST_LOST'
            out=await call('dw_task_blocker_input',{'task_id':fixture['task_id'],'blocker_id':bid,'request_id':'mcp-input','expected_version':b['version'],'event_id':'local-evidence','body':'材料到件，仅供核对','evidence_ref':'local:immutable-fixture'})
            b=out['blocker'];assert b['state']=='ready_to_recheck' and b['input_version']==1,'BLOCKER_REAL_INPUT_LOST'
            out=await call('dw_task_blocker_recheck',{'task_id':fixture['task_id'],'blocker_id':bid,'request_id':'mcp-begin','expected_version':b['version'],'operation':'begin_recheck','input_version':1})
            b=out['blocker'];assert b['state']=='rechecking' and b['lease_id'],'BLOCKER_REAL_LEASE_LOST'
            out=await call('dw_task_blocker_recheck',{'task_id':fixture['task_id'],'blocker_id':bid,'request_id':'mcp-finish','expected_version':b['version'],'operation':'finish_recheck','input_version':1,'lease_id':b['lease_id'],'checks':[{'id':i,'result':'pass','evidence_ref':'local:fixture-'+i} for i in ('materials','environment','authorization')]})
            assert out['blocker']['state']=='resolved' and out['outcome']=='rechecked_not_resumed','BLOCKER_REAL_FINISH_BYPASS'
            print(json.dumps({'result':'LOCAL_BLOCKER_ROUNDTRIP_PASS','transport':'real MCP stdio -> actual helper IPC -> Go HTTP','mode':cfg.mode,'operations':5,'business_execution':False}))
        finally:
            ipc.cancel()
            try:await ipc
            except asyncio.CancelledError:pass

if __name__=='__main__':asyncio.run(main())
