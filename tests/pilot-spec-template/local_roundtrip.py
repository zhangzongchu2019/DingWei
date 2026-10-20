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
            listing=await call('dw_spec_template',{'action':'list'})
            assert len(listing['drafts'])==1 and listing['drafts'][0]['draft_id']==fixture['draft_id'],'TEMPLATE_REAL_LIST_LOST'
            detail=await call('dw_spec_template',{'action':'get','draft_id':fixture['draft_id']})
            v=detail['draft'];assert v['can_confirm'] is False,'TEMPLATE_AGENT_GOT_CONFIRMATION'
            common=dict(action='responsibility',draft_id=v['draft_id'],expected_version=v['version'],plan_sha256=v['plan_sha256'],entry_ids=['R1'],role='executor')
            out=await call('dw_spec_template',dict(common,request_id='mcp-accept',decision='accepted'))
            assert any(x['role']=='executor' and x['accepted'] for x in out['draft']['roles']),'TEMPLATE_REAL_RECEIPT_LOST'
            out=await call('dw_spec_template',dict(common,request_id='mcp-withdraw',decision='withdrawn',reason='fixture responsibility withdrawn'))
            assert not any(x['role']=='executor' and x['accepted'] for x in out['draft']['roles']),'TEMPLATE_REAL_WITHDRAWAL_LOST'
            print(json.dumps({'result':'LOCAL_TEMPLATE_ROUNDTRIP_PASS','transport':'real MCP stdio -> actual helper IPC -> Go HTTP','mode':cfg.mode,'operations':4,'business_execution':False}))

        finally:
            ipc.cancel()
            try:await ipc
            except asyncio.CancelledError:pass

if __name__=='__main__':asyncio.run(main())
