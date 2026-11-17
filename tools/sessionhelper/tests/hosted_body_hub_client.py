"""Synthetic local Hub child; never connects to a deployed Hub or model."""
import asyncio
import json
import os
from pathlib import Path
import sys
from sessionhelper.app import SessionHelper
from sessionhelper.config import Config
from sessionhelper.enroll import Credential


STEP="initializing"

async def main():
    global STEP
    seed = json.loads(sys.stdin.readline())
    home = Path(seed['home'])
    assert Path.home().resolve() == home.resolve(), "integration child requires its synthetic HOME"
    home.chmod(0o700)
    cfg = Config('desktop', 'u_zzc', seed['secret'], mode='echo', ws_base=seed['base'], collect=False,
                 cli_cwd=str(home), state_dir=str(home/'.dingwei'), inbox_db=str(home/'inbox.sqlite3'),
                 mcp_ipc_sock=seed['mcp_ipc_sock'], ipc_send_body_only=True,
                 provision_audit_db=str(home/'audit'), provision_versions_file=str(home/'versions'),
                 provision_update_state_file=str(home/'provision'))
    helper = SessionHelper(cfg); helper.enroll_credential = Credential('desktop', seed['secret'])
    connection = asyncio.create_task(helper.run_forever())
    child = None
    try:
        STEP="waiting_for_responsibility_listener"
        async with asyncio.timeout(8):
            while not helper.ipc_bound.is_set() or not helper.outbound_receipts.recovery or not helper.outbound_receipts.recovery.get('targets_available',True) or not helper.outbound_recovery.queue.sender():
                if connection.done(): connection.result(); raise AssertionError('connection ended')
                await asyncio.sleep(.01)
        STEP="starting_mcp"
        env = dict(os.environ, SH_IPC_SEND_BODY_ONLY='1', DINGWEI_MCP_IPC_SOCK=cfg.mcp_ipc_sock)
        child = await asyncio.create_subprocess_exec(sys.executable, '-B', '-m', 'sessionhelper.mcp_shim',
            env=env, stdin=asyncio.subprocess.PIPE, stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.DEVNULL)
        print('HOSTED_READY', flush=True)
        for ident in (1, 2):
            STEP="mcp_send_"+str(ident)
            assert await asyncio.to_thread(sys.stdin.readline) == 'send\n'
            req = dict(jsonrpc='2.0', id=ident, method='tools/call', params=dict(name='send_message',
                arguments=dict(to='peer#u_zzc', body='DW_HOSTED_SEND_MARKER', reply_policy='no_reply')))
            child.stdin.write((json.dumps(req)+'\n').encode()); await child.stdin.drain()
            result = json.loads(await asyncio.wait_for(child.stdout.readline(), 8))
            assert result['id'] == ident and not result['result'].get('isError', False)
            outcome=json.loads(result['result']['content'][0]['text'])
            assert outcome['outcome']=='accepted' and outcome['accepted_by'] in ('local_helper','hub')
            request_id=outcome['request_id']
            async with asyncio.timeout(8):
                while outcome['phase'] not in ('hub_durable_queued','rejected'):
                    await asyncio.sleep(.05)
                    query=dict(jsonrpc='2.0',id=ident+100,method='tools/call',params=dict(name='dw_send_status',arguments=dict(request_id=request_id)))
                    child.stdin.write((json.dumps(query)+'\n').encode());await child.stdin.drain()
                    reply=json.loads(await child.stdout.readline())
                    outcome=json.loads(reply['result']['content'][0]['text'])
            if ident==1:
                assert outcome['phase']=='hub_durable_queued' and outcome['delivery_id'].startswith('dw-dlv_')
            else:
                assert outcome['outcome']=='definitive_failure' and outcome['reason']=='permission_denied'
            print('HOSTED_RESOLVED', flush=True)
        assert await asyncio.to_thread(sys.stdin.readline) == 'stop\n'
    finally:
        if STEP=="waiting_for_responsibility_listener":
            print("HOSTED_READINESS "+json.dumps(dict(ipc=helper.ipc_bound.is_set(),negotiated=helper.outbound_receipts.negotiated,capability=bool(helper.outbound_receipts.recovery),recovery=hasattr(helper,"outbound_recovery"))),file=sys.stderr,flush=True)
        if child:
            child.stdin.close()
            try: await asyncio.wait_for(child.wait(), 3)
            except asyncio.TimeoutError: child.kill(); await child.wait()
        connection.cancel(); await asyncio.gather(connection, return_exceptions=True)
    print('HOSTED_STOPPED', flush=True)


if __name__ == '__main__':
    try: asyncio.run(main())
    except Exception as exc:
        print("HOSTED_DIAGNOSTIC phase="+STEP+" class="+type(exc).__name__,file=sys.stderr,flush=True)
        print('HOSTED_ERROR', flush=True)
        raise SystemExit(1)
