"""Real SessionHelper/DriverAdapter/CLI, controlled model endpoint and faults.

The two installer overrides prevent touching shared ~/.dingwei launchers. All
inbox/dispatch/ACK/WS paths remain production methods. Credentials use stdin;
no production credentials, daemon, helper or database is opened or restarted.
"""
import asyncio
import json
import os
from pathlib import Path
import re
import shutil
import sys
import threading
import time
import tomllib

from ack_v2_real_cli_endpoint import ControlledModel


def atomic_json(root, name, value):
    temporary=root/(name+'.tmp');temporary.write_text(json.dumps(value));temporary.replace(root/name)


async def main():
    boot=json.loads(sys.stdin.readline());root=Path(boot['state']);root.mkdir(parents=True,exist_ok=True)
    model=ControlledModel(root)
    kind=boot['driver']
    os.environ.update(SH_DRIVER=kind,SH_EMIT_SESSION_EVENT='1',SH_ONLINE_LIST_PATH=str(root/'online.list'),SH_CLI_CWD=str(root))
    from sessionhelper.app import SessionHelper
    from sessionhelper.config import Config
    from sessionhelper.enroll import Credential
    from sessionhelper.driver_adapter import DriverAdapter
    if kind=='claude':
        from sessionhelper.claudeDriver import ClaudeProtocolDriver,DEFAULT_CMD
        native_env={'CLAUDE_CONFIG_DIR':str(root/'claude-config'),'ANTHROPIC_BASE_URL':f'http://127.0.0.1:{model.server.server_port}',
            'ANTHROPIC_API_KEY':'r40-controlled-synthetic-only','ANTHROPIC_AUTH_TOKEN':'','CLAUDE_CODE_OAUTH_TOKEN':'',
            'CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC':'1','DISABLE_TELEMETRY':'1','DISABLE_ERROR_REPORTING':'1','NO_PROXY':'127.0.0.1,localhost'}
        base_command=['env','IS_SANDBOX=1',shutil.which('claude'),'--model','opus','--dangerously-skip-permissions'] if boot.get('explicit_claude') else list(DEFAULT_CMD)
        cmd=[*base_command,'--setting-sources','','--strict-mcp-config','--mcp-config','{"mcpServers":{}}','--settings','{"disableAllHooks":true}','--no-session-persistence','--tools','']
        driver=ClaudeProtocolDriver(store_path=str(root/'native-ledger.db'),cwd=str(root),cmd=cmd,env=native_env)
        atomic_json(root,'native-launch.json',{'argv':driver._cmd})
    else:
        import sessionhelper.codexDriver as module
        os.environ['R40_CONTROLLED_API_KEY']='r40-controlled-synthetic-only'
        os.environ['NO_PROXY']='127.0.0.1,localhost'
        original_init=module.StdioJsonRpc.__init__
        overrides=[f'model_providers.r40controlled.name="r40controlled"',f'model_providers.r40controlled.base_url="http://127.0.0.1:{model.server.server_port}/v1"',
            'model_providers.r40controlled.wire_api="responses"','model_providers.r40controlled.env_key="R40_CONTROLLED_API_KEY"',
            'model_providers.r40controlled.requires_openai_auth=false','model_providers.r40controlled.supports_websockets=false','model_provider="r40controlled"']
        user_config=Path.home()/'.codex/config.toml'
        if user_config.exists():
            for name in tomllib.loads(user_config.read_text()).get('mcp_servers',{}):
                if not re.fullmatch(r'[A-Za-z0-9_-]+',name):raise RuntimeError('INCONCLUSIVE: cannot isolate configured MCP name')
                overrides.append('mcp_servers.'+name+'.enabled=false')
        def native_init(self,cmd,*,cwd,env=None):
            cmd=list(cmd)
            for value in overrides:cmd.extend(['-c',value])
            original_init(self,cmd,cwd=cwd,env=env)
        module.StdioJsonRpc.__init__=native_init
        driver=module.CodexProtocolDriver(store_path=str(root/'native-ledger.db'),workdir=str(root),transport_mode='direct',manage_daemon=False,
            ephemeral=not boot.get('persistent_thread',False),model='gpt-5.1-codex-mini',model_provider='r40controlled')
    crash_hit=threading.Event()
    def crash_window(phase):
        if boot.get('fault_window') != phase or crash_hit.is_set():return
        crash_hit.set()
        atomic_json(root,'crash-window.json',{'phase':phase,'at':time.time()})
        threading.Event().wait(150)  # Parent kills this isolated process group.
    if boot.get('fault_window') == 'dispatch_sent_receipt_unread':
        if kind == 'claude':
            native_start=driver.start
            def start_with_window():
                native_start()
                if getattr(driver,'_input_window_installed',False):return
                original_input=driver._proc.stdin
                class NativeInput:
                    def __getattr__(self,name):return getattr(original_input,name)
                    def write(self,text):return original_input.write(text)
                    def flush(self):
                        result=original_input.flush();crash_window('dispatch_sent_receipt_unread');return result
                driver._proc.stdin=NativeInput();driver._input_window_installed=True
            driver.start=start_with_window
        else:
            original_request=module.StdioJsonRpc.request
            def request_with_window(self,method,*args,**kwargs):
                result=original_request(self,method,*args,**kwargs)
                if method=='turn/start':crash_window('dispatch_sent_receipt_unread')
                return result
            module.StdioJsonRpc.request=request_with_window
    original_build=DriverAdapter._build_driver
    DriverAdapter._build_driver=lambda self,which:driver
    class IsolatedHelper(SessionHelper):
        def install_send_script(self):pass
        def install_upload_command(self):pass
    cfg=Config(session_name='opus50ts',key_id='enrolled',secret=boot['secret'],ws_base=boot['ws_base'],mode='driver',tool=kind,
        proxy='off',cli_cwd=str(root),state_dir=str(root),collect=False,inbox_db=str(root/'inbox.sqlite'),mcp_ipc_sock='',
        turn_terminal_timeout_seconds=120,provision_audit_db=str(root/'provision.sqlite'),provision_versions_file=str(root/'versions.json'),
        provision_update_state_file=str(root/'update.json'))
    helper=IsolatedHelper(cfg);DriverAdapter._build_driver=original_build
    if boot.get('late_terminal_test'):
        helper.adapter._terminal_timeout=.3
        # Fault: delay terminal beyond transport watchdog without issuing an
        # interrupt. The actual CLI/model stream remains live for late evidence.
        helper.adapter._best_effort_timeout_interrupt=lambda command:None
    helper.enroll_credential=Credential('Opus50TS',boot['secret'])
    if boot.get('fault_window') == 'receipt_before_admitted_persist':
        native_admitted=helper.delivery_inbox.record_admitted
        def admitted_with_window(*args,**kwargs):
            crash_window('receipt_before_admitted_persist')
            return native_admitted(*args,**kwargs)
        helper.delivery_inbox.record_admitted=admitted_with_window
    original_poll=driver.poll_receipt
    queries=[];atomic_json(root,'recovery-queries.json',queries)
    def poll(identifier):
        queries.append(identifier);atomic_json(root,'recovery-queries.json',queries)
        return original_poll(identifier)
    driver.poll_receipt=poll
    original_deliver=driver.deliver
    def deliver(identifier,body):
        receipt=original_deliver(identifier,body)
        if receipt.state in ('accepted','processing','done') and (kind!='claude' or receipt.state!='processing' or str(receipt.detail).startswith('accepted,')):
            turn=helper.adapter._receipt_turn_id(receipt)
            if turn:model.observe_admission(identifier,turn)
        return receipt
    driver.deliver=deliver
    emit_name='_emit' if kind=='claude' else '_emit_event'
    original_emit=getattr(driver,emit_name)
    def event(ev):
        if ev.kind=='state_change' and ev.data.get('change')=='turn_completed' and ev.delivery_id and ev.turn_id:
            model.observe_terminal(ev.delivery_id,ev.turn_id)
        return original_emit(ev)
    setattr(driver,emit_name,event)
    faults={'drop_injected':False,'drop_injected_confirm':False,'drop_legacy_acks':bool(boot.get('drop_legacy_acks')),'pause_claim':False,'pause_dispatch':False}
    dispatch_gate=threading.Event();dispatch_gate.set()
    if hasattr(helper.delivery_inbox,'confirm_delivery_ack'):
        confirms=[]
        original_confirm=helper.delivery_inbox.confirm_delivery_ack
        def confirm(meta):
            confirms.append(dict(meta));atomic_json(root,'native-confirms.json',confirms)
            if faults['drop_injected_confirm'] and meta.get('state')=='injected':return False
            return original_confirm(meta)
        helper.delivery_inbox.confirm_delivery_ack=confirm
    original_claim=helper.delivery_inbox.claim_next
    def claim():
        if faults['pause_claim']:return None
        return original_claim()
    helper.delivery_inbox.claim_next=claim
    if hasattr(helper.delivery_inbox,'begin_dispatch'):
        original_dispatch=helper.delivery_inbox.begin_dispatch
        def dispatch(did):
            won=original_dispatch(did)
            if won and faults['pause_dispatch']:
                atomic_json(root,'dispatch-window.json',{'delivery_id':did})
                dispatch_gate.wait(150)
            return won
        helper.delivery_inbox.begin_dispatch=dispatch
    import websockets
    native_connect=websockets.connect
    captured=[]
    active_socket=None
    class FaultWS:
        def __init__(self,ws):self.ws=ws
        def __getattr__(self,name):return getattr(self.ws,name)
        def __aiter__(self):return self.ws.__aiter__()
        async def send(self,raw):
            packet=json.loads(raw);meta=packet.get('meta') or {}
            if meta.get('type') in ('delivery_ack','delivery_cancel_ack'):
                captured.append(packet);atomic_json(root,'native-acks.json',captured)
            if meta.get('type')=='delivery_ack':
                if faults['drop_injected'] and meta.get('state')=='injected':return
                if faults['drop_legacy_acks'] and meta.get('state') in ('injected','processed'):return
            return await self.ws.send(raw)
    class FaultConnection:
        def __init__(self,connection):self.connection=connection
        async def __aenter__(self):
            nonlocal active_socket
            active_socket=FaultWS(await self.connection.__aenter__());return active_socket
        async def __aexit__(self,*args):return await self.connection.__aexit__(*args)
    websockets.connect=lambda *args,**kwargs:FaultConnection(native_connect(*args,**kwargs))
    connection=None
    async def connect():
        nonlocal connection
        connection=asyncio.create_task(helper._run_once())
        async with asyncio.timeout(20):
            while not getattr(helper,'_connection_agent_id',''):
                if connection.done():connection.result();raise RuntimeError('real helper connection ended')
                await asyncio.sleep(.01)
    async def disconnect():
        nonlocal connection
        if connection is not None:
            connection.cancel();await asyncio.gather(connection,return_exceptions=True);connection=None
    try:
        await connect();atomic_json(root,'ready.json',{'ready':True,'driver':kind})
        while True:
            raw=await asyncio.to_thread(sys.stdin.readline)
            if not raw:break
            command=json.loads(raw);operation=command['operation']
            if operation=='release':model.release(command['label'])
            elif operation=='disconnect':await disconnect()
            elif operation=='reconnect':await connect()
            elif operation=='fault':
                faults[command['name']]=command['value']
                if command['name']=='pause_dispatch':
                    if command['value']:dispatch_gate.clear()
                    else:dispatch_gate.set()
            elif operation=='debug':
                import faulthandler
                faulthandler.dump_traceback(file=sys.stderr,all_threads=True)
                command_current=helper.adapter._commands.current
                debug={'current':command_current.delivery_id if command_current else '',
                    'states':getattr(driver,'_envelope_states',{}),'drained':list(getattr(driver,'_events_drained',set()))}
                atomic_json(root,'debug-state.json',debug)
            elif operation=='replay_queued':
                packet=next(p for p in captured if (p.get('meta') or {}).get('delivery_id')==command['delivery_id'] and p['meta'].get('state')=='queued')
                await active_socket.send(json.dumps(packet))
            elif operation=='recovery_status':
                current=helper.adapter._commands.current
                atomic_json(root,'recovery-status.json',{'started':helper.adapter._started,'restored':getattr(helper.adapter,'_lifecycles_restored',False),'current':current.delivery_id if current else ''})
            elif operation=='snapshot':
                rows={}
                try:
                    from sessionhelper.inbox_ack import utc_stamp
                except ImportError:
                    from datetime import datetime, timezone
                    def utc_stamp(value):return datetime.fromtimestamp(value,timezone.utc).isoformat().replace('+00:00','Z')
                for did in command['identifiers']:
                    if hasattr(helper.delivery_inbox,'lifecycle'):
                        row=helper.delivery_inbox.lifecycle(did)
                    else:
                        import sqlite3
                        with sqlite3.connect(str(root/'inbox.sqlite')) as db:
                            db.row_factory=sqlite3.Row
                            old=db.execute('SELECT state FROM delivery_inbox WHERE delivery_id=?',(did,)).fetchone()
                        row=dict(state=old['state'],turn_id='',ack_protocol=None,processing_mode='legacy',ack_generation=None,turn_outcome=None,admitted_at=None,processed_at=None) if old else None
                    if row is not None:
                        rows[did]={key:row[key] for key in ('state','turn_id','ack_protocol','processing_mode','ack_generation','turn_outcome')}
                        for src,dst in (('admitted_at','injected_stamp'),('processed_at','processed_stamp')):
                            rows[did][dst]=utc_stamp(row[src]) if row[src] is not None else ''
                atomic_json(root,'local-snapshot.json',rows)
            elif operation=='stop':break
            else:raise ValueError('unsupported isolated probe operation')
            atomic_json(root,'control-'+str(command['id'])+'.json',{'ok':True})
    finally:
        dispatch_gate.set();await disconnect();driver.stop();model.close()


if __name__=='__main__':
    # Keep inherited seat credentials/settings out of child CLI environments.
    allowed={k:v for k,v in os.environ.items() if k in ('PATH','HOME','LANG','USER','LOGNAME','TMPDIR','PYTHONPATH')}
    os.environ.clear();os.environ.update(allowed)
    try:asyncio.run(main())
    except BaseException as exc:
        print('REAL_ACK_CLI_CLIENT_ERROR '+type(exc).__name__,file=sys.stderr,flush=True)
        raise
