"""Actual frozen helper/MCP/Stop local Hub test; synthetic identity, no model."""
import asyncio
import json
import os
from pathlib import Path
import signal
import sys

from sessionhelper.enroll import Credential, save_credential


async def main():
    bootstrap = json.loads(sys.stdin.readline())
    state = Path(bootstrap['state'])
    state.chmod(0o700)
    binary = bootstrap['binary']
    save_credential(str(state), Credential('desktop', bootstrap['secret']))
    env = {key: value for key, value in os.environ.items()
           if not key.startswith(('SH_', 'DW_', 'DINGWEI_', 'CODEX_', 'CLAUDE_', 'OPENCODE_'))}
    env.update(SH_ENROLL='1', SH_MODE='desktop', SH_SESSION_NAME='desktop', SH_TOOL='CODEX',
               SH_STATE_DIR=str(state), SH_WS_BASE=bootstrap['ws_base'],
               SH_INBOX_DB=str(state/'inbox.sqlite'), SH_CLI_CWD=str(state), SH_COLLECT='0',
               DINGWEI_MCP_IPC_SOCK=str(state/'mcp.sock'))
    helper = await asyncio.create_subprocess_exec(binary, env=env, cwd=state,
                stdin=asyncio.subprocess.DEVNULL, stdout=asyncio.subprocess.DEVNULL,
                stderr=asyncio.subprocess.DEVNULL)
    mcp = None
    try:
        async with asyncio.timeout(8):
            while not (state/'mcp.sock').exists():
                assert helper.returncode is None
                await asyncio.sleep(.01)
        mcp = await asyncio.create_subprocess_exec(binary, '--mcp', env=env,
              stdin=asyncio.subprocess.PIPE, stdout=asyncio.subprocess.PIPE,
              stderr=asyncio.subprocess.DEVNULL)
        async def call(ident, tool, args):
            packet={'jsonrpc':'2.0','id':ident,'method':'tools/call',
                    'params':{'name':tool,'arguments':args}}
            mcp.stdin.write((json.dumps(packet)+'\n').encode())
            await mcp.stdin.drain()
            result=json.loads(await asyncio.wait_for(mcp.stdout.readline(),12))
            assert result['id']==ident and 'error' not in result
            return json.loads(result['result']['content'][0]['text'])
        binding=state/'stop-binding.json'
        binding.write_text(json.dumps(dict(enabled=True,session_id='fixture-owner-thread',
                                           inbox_path=str(state/'inbox.sqlite.desktop.sqlite'))))
        binding.chmod(0o600)
        async def hook():
            process=await asyncio.create_subprocess_exec(binary,'--desktop-stop-hook','--binding',str(binding),
                       env=env,stdin=asyncio.subprocess.PIPE,stdout=asyncio.subprocess.PIPE,
                       stderr=asyncio.subprocess.DEVNULL)
            try:
                event=dict(hook_event_name='Stop',session_id='fixture-owner-thread',
                           turn_id='fixture-turn',stop_hook_active=False)
                stdout,_=await asyncio.wait_for(process.communicate(json.dumps(event).encode()),5)
                assert process.returncode==0
                return json.loads(stdout)
            finally:
                if process.returncode is None:
                    process.kill();await process.wait()
        assert await hook()=={}
        print('BRIDGE_WAIT_READY frozen-hook-no-model',flush=True)
        # Wait only for metadata; the hook must request continuation before a
        # scripted stand-in invokes the model's message-reading tool.
        async with asyncio.timeout(5):
            ident=100
            while (await call(ident,'dw_inbox',{'operation':'status'}))['unread'] != 1:
                ident+=1
                assert helper.returncode is None
                await asyncio.sleep(.02)
        assert (await hook())['decision']=='block'
        assert await hook()=={}
        incoming=await call(10,'dw_wait_message',{'wait_seconds':5})
        assert incoming['state']=='message' and incoming['model_execution_verified'] is False
        message=incoming['message']
        assert message['id']=='desktop-question' and message['reply_to']=='peer#'+bootstrap['key_id']
        assert (await call(13,'dw_inbox',{'operation':'status'}))['unread']==1
        print('BRIDGE_INPUT_READY frozen-hook-requested',flush=True)
        assert json.loads(await asyncio.to_thread(sys.stdin.readline))=={'operation':'reply'}
        args={'message_id':message['id'],'body':'DW_SYNTHETIC_ANSWER'}
        outcome=await call(11,'dw_reply_message',args)
        assert outcome['ok'] is True and outcome['stage']=='hub_durable_queued'
        assert await call(12,'dw_reply_message',args)==outcome
        assert (await call(14,'dw_inbox',{'operation':'status'}))['unread']==0
        assert await hook()=={}
        print('BRIDGE_RESULT '+json.dumps({**outcome,'inbound_delivery_id':message['delivery_id'],
                                           'unread_after':0,'duplicate_cached':True}),flush=True)
        assert json.loads(await asyncio.to_thread(sys.stdin.readline))=={'operation':'stop'}
    finally:
        if mcp is not None:
            mcp.stdin.close()
            try: await asyncio.wait_for(mcp.wait(),3)
            except asyncio.TimeoutError: mcp.kill();await mcp.wait()
        if helper.returncode is None: helper.send_signal(signal.SIGTERM)
        try: await asyncio.wait_for(helper.wait(),5)
        except asyncio.TimeoutError: helper.kill();await helper.wait();raise AssertionError('helper did not stop')
    print('BRIDGE_CLEAN frozen-joined',flush=True)


if __name__=='__main__':
    try: asyncio.run(main())
    except BaseException as exc:
        print('BRIDGE_ERROR '+type(exc).__name__,flush=True)
        raise SystemExit(1)
