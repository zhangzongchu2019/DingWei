"""Local-only HTTP -> production helper IPC -> actual MCP stdio process."""
import asyncio
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import urllib.parse

async def main():
    config=json.loads(Path(sys.argv[1]).read_text())
    origin=urllib.parse.urlsplit(config['origin'])
    if origin.scheme!='http' or origin.hostname!='127.0.0.1':
        raise ValueError('local fixture only')
    with tempfile.TemporaryDirectory(prefix='pc-rt-',dir='/tmp') as scratch:
        root=Path(scratch);home=root/'home';home.mkdir(mode=0o700)
        state=root/'state';state.mkdir(mode=0o700)
        os.environ['HOME']=str(home)
        from sessionhelper.config import Config
        from sessionhelper.app import SessionHelper
        from sessionhelper.enroll import Credential
        cfg=Config('fixture','enrolled','SYNTHETIC',mode='driver',cli_cwd=str(root),state_dir=str(state),
                   codex_home=str(home/'.codex'),inbox_db=str(state/'inbox.sqlite'),mcp_ipc_sock=str(root/'mcp.sock'),
                   ws_base=config['origin'].replace('http:','ws:',1))
        helper=SessionHelper(cfg)
        helper.enroll_credential=Credential('fixture',config['credential'])
        task=asyncio.create_task(helper._ipc_server(None))
        try:
            await asyncio.wait_for(helper.ipc_bound.wait(),5)
            env=dict(os.environ);env['DINGWEI_MCP_IPC_SOCK']=cfg.mcp_ipc_sock
            requests=[{'jsonrpc':'2.0','id':1,'method':'initialize','params':{}},
                      {'jsonrpc':'2.0','id':2,'method':'tools/list','params':{}},
                      {'jsonrpc':'2.0','id':3,'method':'tools/call','params':{'name':'project_context','arguments':{'scope':'all','limit':1}}}]
            p=await asyncio.to_thread(subprocess.run,[sys.executable,'-B','-m','sessionhelper','--mcp'],
                     input=''.join(json.dumps(r)+'\n' for r in requests),env=env,cwd=root,text=True,capture_output=True,timeout=15)
            assert p.returncode==0 and not p.stderr, 'PC_REAL_MCP_PROCESS_FAILED'
            responses={r['id']:r for r in map(json.loads,p.stdout.splitlines()) if 'id' in r}
            assert responses[1]['result']['serverInfo']['version']=='0.2.3.10','PC_REAL_MCP_VERSION_DRIFT'
            assert any(t['name']=='project_context' for t in responses[2]['result']['tools']),'PC_REAL_MCP_TOOL_UNWIRED'
            result=responses[3]['result']
            assert not result.get('isError'), 'PC_REAL_MCP_TOOL_ERROR'
            body=json.loads(result['content'][0]['text'])
            assert body['meta']['source']=='current_hub' and body['meta']['schema']=='hub-project-context-v1','PC_REAL_HUB_SCHEMA_LOST'
            assert [r['task_id'] for r in body['tasks']['items']]==['r32-task'],'PC_REAL_HTTP_IPC_MCP_PROJECTION_LOST'
            print(json.dumps({'result':'LOCAL_CONTEXT_ROUNDTRIP_PASS','mode':cfg.mode,'source':body['meta']['source'],
                              'schema':body['meta']['schema'],'task_count':len(body['tasks']['items'])}))
        finally:
            task.cancel()
            try:await task
            except asyncio.CancelledError:pass

if __name__=='__main__':asyncio.run(main())
