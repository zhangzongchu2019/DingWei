"""Local real helper + production driver; deterministic fake app-server CLI.

Only the external Codex process is a fixture. No driver event, admission, inbox,
WS sender or Hub AMR function is replaced. Credentials enter on private stdin.
"""
import asyncio
import json
import os
from pathlib import Path
import sys

async def main():
    boot=json.loads(sys.stdin.readline())
    state=Path(boot['state'])
    os.environ['SH_DRIVER']='codex'
    os.environ['SH_EMIT_SESSION_EVENT']='1'
    os.environ['SH_CODEX_BIN']=str(state/'fake-codex')
    os.environ['SH_CLI_CWD']=str(state)
    os.environ['FN_STATE']=str(state)
    (state/'outputs.json').write_text(json.dumps(boot['outputs']))
    # A real executable process speaks the documented stdio app-server wire.
    (state/'fake-codex').write_text('#!'+sys.executable+'\n'+FAKE_CODEX)
    (state/'fake-codex').chmod(0o700)
    from sessionhelper.app import SessionHelper
    from sessionhelper.config import Config
    from sessionhelper.enroll import Credential
    cfg=Config(session_name='kimi',key_id='enrolled',secret=boot['secret'],ws_base=boot['ws_base'],
               mode='driver',tool='CODEX',proxy='off',cli_cwd=str(state),state_dir=str(state),collect=False,
               inbox_db=str(state/'inbox.sqlite'),mcp_ipc_sock=str(state/'mcp.sock'),
               provision_audit_db=str(state/'provision.sqlite'),
               provision_versions_file=str(state/'versions.json'),provision_update_state_file=str(state/'update.json'))
    helper=SessionHelper(cfg)
    helper.enroll_credential=Credential('Kimi',boot['secret'])
    # Select the existing production stdio fallback, without a managed daemon.
    helper.adapter.driver._transport_mode='direct'
    helper.adapter.driver._manage_daemon=False
    connection=asyncio.create_task(helper._run_once())
    try:
        async with asyncio.timeout(15):
            while not helper.ipc_bound.is_set():
                if connection.done():connection.result();raise RuntimeError('Hub connection ended')
                await asyncio.sleep(.01)
        (state/'ready').write_text('ready')
        await asyncio.to_thread(sys.stdin.readline)
    finally:
        connection.cancel()
        await asyncio.gather(connection,return_exceptions=True)
        helper.adapter.driver.stop()

FAKE_CODEX=r'''
import json,os,sys,threading,time
from pathlib import Path
state=Path(os.environ['FN_STATE'])
lock=threading.Lock()
def send(value):
    with lock:
        print(json.dumps(value),flush=True)
def complete(identifier,turn,text):
    time.sleep(.1) # allow the production driver to register turn -> envelope
    send({'method':'item/started','params':{'threadId':'local-thread','turnId':turn,'item':{'type':'userMessage','id':'u-'+turn,'clientId':identifier}}})
    send({'method':'item/agentMessage/delta','params':{'threadId':'local-thread','turnId':turn,'itemId':'a-'+turn,'delta':text}})
    send({'method':'turn/completed','params':{'threadId':'local-thread','turn':{'id':turn,'status':'completed'}}})
for line in sys.stdin:
    message=json.loads(line)
    if 'id' not in message:continue
    method=message.get('method')
    if method=='initialize':result={'serverInfo':{'name':'local-fake-codex','version':'0'}}
    elif method in ('thread/start','thread/resume'):result={'thread':{'id':'local-thread'}}
    elif method=='turn/start':
        params=message['params'];identifier=params['clientUserMessageId'];turn='turn-'+identifier
        with (state/'injections.jsonl').open('a') as f:f.write(json.dumps({'delivery_id':identifier,'input':params['input']})+'\n')
        output=json.loads((state/'outputs.json').read_text()).get(identifier,'local control complete')
        result={'turn':{'id':turn,'status':'inProgress'}}
        send({'id':message['id'],'result':result})
        threading.Thread(target=complete,args=(identifier,turn,output),daemon=True).start()
        continue
    else:result={}
    send({'id':message['id'],'result':result})
'''

if __name__=='__main__':
    try:asyncio.run(main())
    except BaseException as exc:
        print('LOCAL_FAILURE_NOTICE_DRIVER_ERROR '+type(exc).__name__,file=sys.stderr,flush=True)
        raise SystemExit(1)
