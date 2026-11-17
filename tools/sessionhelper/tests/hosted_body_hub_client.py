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
                 mcp_ipc_sock=seed['mcp_ipc_sock'], ipc_send_body_only=not seed.get('cli_repeat',False),
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
        if seed.get('cli_repeat'):
            STEP='installed_send_cli_repeat'
            helper.install_send_script()
            cli = home/'.dingwei/send.py'
            STEP='checking_installed_cli'
            assert cli.is_file(), "installed caller CLI missing"
            env = dict(os.environ, SH_MCP_IPC_SOCK=cfg.mcp_ipc_sock)
            async def call(*args):
                proc = await asyncio.create_subprocess_exec(sys.executable,'-B',str(cli),*args,env=env,stdout=asyncio.subprocess.PIPE,stderr=asyncio.subprocess.PIPE)
                out, err = await asyncio.wait_for(proc.communicate(),12)
                result = json.loads(out)
                assert proc.returncode == {'accepted':0,'unknown':2,'definitive_failure':1}[result['outcome']]
                return proc.returncode,result
            print('HOSTED_READY',flush=True)
            assert await asyncio.to_thread(sys.stdin.readline) == 'send\n'
            args=('peer#u_zzc','--request-id','r60-cli-stable','DW_HOSTED_SEND_MARKER')
            STEP='first_cli_acceptance'
            rc, first = await call(*args)
            print('CLI_CALL_RESULT '+json.dumps({'rc':rc,'outcome':first.get('outcome'),'reason':first.get('reason')}),file=sys.stderr,flush=True)
            assert rc == 0 and first['request_id']=='r60-cli-stable'
            async with asyncio.timeout(8):
                while first['phase'] != 'hub_durable_queued':
                    await asyncio.sleep(.05)
                    rc, first = await call('--status','r60-cli-stable')
                    assert first['outcome'] != 'definitive_failure'
            STEP='explicit_cli_status'
            rc, queried = await call('--status','r60-cli-stable')
            assert rc==0 and queried['delivery_id']==first['delivery_id'] and queried['source_message_id']==first['source_message_id']
            STEP='second_cli_same_request'
            rc, second = await call(*args)
            print('CLI_CALL_RESULT '+json.dumps({'rc':rc,'outcome':second.get('outcome'),'reason':second.get('reason')}),file=sys.stderr,flush=True)
            assert rc == 0 and second['phase']=='hub_durable_queued'
            assert second['source_message_id']==first['source_message_id']
            assert second['delivery_id']==first['delivery_id']
            print('CLI_REPEAT_RESULT '+json.dumps({'source_message_id':first['source_message_id'],'delivery_id':first['delivery_id'],'request_id':'r60-cli-stable','process_send_calls':2}),flush=True)
            print('CLI_REPEAT_RESOLVED',flush=True)
            assert await asyncio.to_thread(sys.stdin.readline)=='stop\n'
            return
        STEP="starting_mcp"
        env = dict(os.environ, SH_IPC_SEND_BODY_ONLY='1', DINGWEI_MCP_IPC_SOCK=cfg.mcp_ipc_sock)
        child = await asyncio.create_subprocess_exec(sys.executable, '-B', '-m', 'sessionhelper.mcp_shim',
            env=env, stdin=asyncio.subprocess.PIPE, stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.DEVNULL)
        if seed.get('joint_fault'):
            async def tool(ident, name, arguments):
                req=dict(jsonrpc='2.0',id=ident,method='tools/call',params=dict(name=name,arguments=arguments))
                child.stdin.write((json.dumps(req)+'\n').encode());await child.stdin.drain()
                response=json.loads(await asyncio.wait_for(child.stdout.readline(),4))
                assert response['id']==ident and 'error' not in response
                return response['result'],json.loads(response['result']['content'][0]['text'])
            print('HOSTED_READY',flush=True)
            STEP='joint_initial_send'
            assert await asyncio.to_thread(sys.stdin.readline)=='send\n'
            result, first=await tool(10,'send_message',dict(to='peer#u_zzc',body='DW_HOSTED_JOINT_MARKER',reply_policy='no_reply',request_id='r60-joint-stable'))
            assert first['outcome']=='accepted' and first['phase']=='local_queued'
            print('JOINT_RESULT '+json.dumps(first),flush=True);print('JOINT_SUBMITTED',flush=True)
            STEP='joint_unknown_status'
            assert await asyncio.to_thread(sys.stdin.readline)=='query_unknown\n'
            result, unknown=await tool(11,'dw_send_status',dict(request_id='r60-joint-stable'))
            assert result.get('isError') is True and unknown['outcome']=='unknown' and unknown['phase']=='unknown'
            assert unknown['source_message_id']==first['source_message_id']
            with helper.outbound_recovery.queue.db() as db:
                row=db.execute('SELECT body,source_id,state FROM outbound_request WHERE request_id=?',('r60-joint-stable',)).fetchone()
                assert row['body']=='DW_HOSTED_JOINT_MARKER' and row['source_id']==first['source_message_id'] and row['state']=='unknown'
            print('JOINT_RESULT '+json.dumps(unknown),flush=True);print('JOINT_UNKNOWN',flush=True)
            STEP='joint_status_recovery'
            assert await asyncio.to_thread(sys.stdin.readline)=='resume\n'
            async with asyncio.timeout(18):
                for ident in range(20,400):
                    result, recovered=await tool(ident,'dw_send_status',dict(request_id='r60-joint-stable'))
                    assert recovered['outcome']!='definitive_failure'
                    if recovered['phase']=='hub_durable_queued':break
                    await asyncio.sleep(.05)
                else:raise AssertionError('status did not recover')
            assert recovered['request_id']=='r60-joint-stable' and recovered['source_message_id']==first['source_message_id'] and recovered['delivery_id'].startswith('dw-dlv_')
            with helper.outbound_recovery.queue.db() as db:
                row=db.execute('SELECT attempts,body FROM outbound_request WHERE request_id=?',('r60-joint-stable',)).fetchone()
                assert row['attempts']>=2 and row['body']==''
            print('JOINT_RESULT '+json.dumps(recovered),flush=True);print('JOINT_RECOVERED',flush=True)
            assert await asyncio.to_thread(sys.stdin.readline)=='stop\n'
            return
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
        if seed.get('cli_repeat') or seed.get('joint_fault'): print('HOSTED_STOPPED', flush=True)
    print('HOSTED_STOPPED', flush=True)


if __name__ == '__main__':
    try: asyncio.run(main())
    except Exception as exc:
        print("HOSTED_DIAGNOSTIC phase="+STEP+" class="+type(exc).__name__,file=sys.stderr,flush=True)
        print('HOSTED_ERROR', flush=True)
        raise SystemExit(1)
