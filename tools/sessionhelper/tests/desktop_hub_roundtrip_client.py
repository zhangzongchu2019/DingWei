"""Local test child; bootstrap comes over stdin, never argv or shared logs.

Uses actual helper connection loops and MCP subprocess. Scripted tool calls
stand in for a model: this does not validate a Mac GUI/model or Feishu onboarding.
"""
import asyncio
import json
import os
from pathlib import Path
import sys

from sessionhelper.app import SessionHelper
from sessionhelper.config import Config
from sessionhelper.enroll import Credential


async def main():
    bootstrap = json.loads(sys.stdin.readline())
    state = Path(bootstrap['state'])
    assert Path.home().resolve() == state.resolve(), "integration child requires its synthetic HOME"
    # Go t.TempDir owns this synthetic HOME but may create it as 0755. Match the
    # installer's private desktop profile contract before a hook can read it.
    state.chmod(0o700)
    cfg = Config(session_name='desktop', key_id=bootstrap['key_id'], secret=bootstrap['secret'],
                 ws_base=bootstrap['ws_base'], mode='desktop', tool='CODEX', cli_cwd=str(state),
                 state_dir=str(state), inbox_db=str(state/'inbox.sqlite'),
                 mcp_ipc_sock=str(state/'mcp.sock'), collect=False,
                 provision_audit_db=str(state/'provision.sqlite'),
                 provision_versions_file=str(state/'versions.json'),
                 provision_update_state_file=str(state/'update.json'))
    helper = SessionHelper(cfg)
    if bootstrap['mode'] != 'legacy':
        helper.enroll_credential = Credential('desktop', bootstrap['secret'])
    connection = asyncio.create_task(helper._run_once())
    child = None
    try:
        async with asyncio.timeout(8):
            while not helper.ipc_bound.is_set() or not helper.outbound_receipts.negotiated:
                if connection.done(): connection.result(); raise AssertionError('connection ended')
                await asyncio.sleep(.01)
        env = dict(os.environ, DINGWEI_MCP_IPC_SOCK=cfg.mcp_ipc_sock)
        child = await asyncio.create_subprocess_exec(sys.executable, '-m', 'sessionhelper.mcp_shim',
                    stdin=asyncio.subprocess.PIPE, stdout=asyncio.subprocess.PIPE,
                    stderr=asyncio.subprocess.DEVNULL, env=env)
        async def send(ident, name, args):
            child.stdin.write((json.dumps({'jsonrpc':'2.0','id':ident,'method':'tools/call',
                 'params':{'name':name,'arguments':args}})+'\n').encode())
            await child.stdin.drain()
        async def read_packet(ident):
            packet=json.loads(await asyncio.wait_for(child.stdout.readline(),12))
            assert packet['id']==ident
            return packet
        async def read(ident):
            packet=await read_packet(ident)
            assert 'error' not in packet
            return json.loads(packet['result']['content'][0]['text'])
        hook_command = None
        async def hook_request():
            process = await asyncio.create_subprocess_exec(*hook_command,
                        stdin=asyncio.subprocess.PIPE, stdout=asyncio.subprocess.PIPE,
                        stderr=asyncio.subprocess.DEVNULL, env=env)
            event = dict(hook_event_name='Stop', session_id='fixture-owner-thread',
                         turn_id='fixture-turn', stop_hook_active=False)
            try:
                stdout, _ = await asyncio.wait_for(process.communicate(json.dumps(event).encode()), 5)
                assert process.returncode == 0
                return json.loads(stdout)
            finally:
                if process.returncode is None:
                    process.kill()
                    await process.wait()
        if bootstrap['mode'] == 'enrolled_stop_hook':
            binding = state / 'stop-binding.json'
            binding.write_text(json.dumps(dict(enabled=True, session_id='fixture-owner-thread',
                                               inbox_path=helper.adapter.path)))
            binding.chmod(0o600)
            hook_command = [sys.executable, '-B', '-m', 'sessionhelper',
                            '--desktop-stop-hook', '--binding', str(binding)]
            assert await hook_request() == {}  # Empty inbox cannot ask for a turn.
            print('BRIDGE_WAIT_READY hook-armed-no-model',flush=True)
            async with asyncio.timeout(5):
                while helper.adapter.request({'operation':'status'})['unread'] != 1:
                    if connection.done(): connection.result()
                    await asyncio.sleep(.01)
            requested = await hook_request()
            assert requested['decision'] == 'block'
            assert 'DW_SYNTHETIC_QUESTION' not in json.dumps(requested)
            assert await hook_request() == {}
            assert helper.adapter.request({'operation':'status'})['unread'] == 1
            # Scripted call stands in for Codex, NOT a real model continuation.
            await send(10, 'dw_wait_message', {'wait_seconds':1})
        else:
            await send(10, 'dw_wait_message', {'wait_seconds':5})
            async with asyncio.timeout(5):
                while not helper.adapter._wait_guard.locked():
                    if child.returncode is not None: raise AssertionError('MCP exited')
                    await asyncio.sleep(.01)
            print('BRIDGE_WAIT_READY pending',flush=True)
        incoming=await read(10)
        assert incoming['state']=='message' and incoming['model_execution_verified'] is False
        message=incoming['message']
        assert message['id']=='desktop-question' and 'DW_SYNTHETIC_QUESTION' in message['body']
        assert message['reply_to']=='peer#'+bootstrap['key_id']
        assert helper.adapter.request({'operation':'status'})['unread']==1
        print('BRIDGE_INPUT_READY received',flush=True)
        command=json.loads(await asyncio.to_thread(sys.stdin.readline))
        assert command=={'operation':'reply'}
        args={'message_id':message['id'],'body':'DW_SYNTHETIC_ANSWER'}
        if bootstrap['mode']=='enrolled_group_removed':
            await send(11,'dw_reply_message',args); assert 'error' in await read_packet(11)
            await send(12,'dw_reply_message',args); assert 'error' in await read_packet(12)
            rejection=helper.adapter.wait_message({'wait_seconds':1})
            unread=helper.adapter.request({'operation':'status'})['unread']
            assert rejection['state']=='reply_rejected' and unread==1
            print('BRIDGE_REJECTED '+json.dumps({'state':rejection['state'],'unread':unread,'repeat_refused':True}),flush=True)
        else:
            await send(11,'dw_reply_message',args); outcome=await read(11)
            assert outcome['ok'] is True and outcome['stage']=='hub_durable_queued'
            await send(12,'dw_reply_message',args); duplicate=await read(12)
            assert duplicate==outcome
            unread=helper.adapter.request({'operation':'status'})['unread']
            assert unread==0
            if hook_command is not None:
                assert await hook_request() == {}
            print('BRIDGE_RESULT '+json.dumps({**outcome,'inbound_delivery_id':message['delivery_id'],
                  'unread_after':unread,'duplicate_cached':True}),flush=True)
        command=json.loads(await asyncio.to_thread(sys.stdin.readline))
        assert command=={'operation':'stop'}
    finally:
        if child is not None:
            child.stdin.close()
            try: await asyncio.wait_for(child.wait(),3)
            except asyncio.TimeoutError: child.kill();await child.wait()
        connection.cancel()
        await asyncio.gather(connection,return_exceptions=True)
    assert not helper.adapter._wait_guard.locked()
    assert not helper.outbound_receipts.pending
    print('BRIDGE_CLEAN joined',flush=True)


if __name__=='__main__':
    try: asyncio.run(main())
    except BaseException as exc:
        print('BRIDGE_ERROR '+type(exc).__name__,flush=True)
        raise SystemExit(1)
