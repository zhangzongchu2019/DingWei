"""Synthetic local Hub child; never connects to a deployed Hub or model."""
import asyncio
import json
import os
from pathlib import Path
import sys
from sessionhelper.app import SessionHelper
from sessionhelper.config import Config
from sessionhelper.enroll import Credential


async def main():
    seed = json.loads(sys.stdin.readline())
    home = Path(seed['home']); assert Path.home() == home
    cfg = Config('desktop', 'u_zzc', seed['secret'], mode='echo', ws_base=seed['base'], collect=False,
                 cli_cwd=str(home), state_dir=str(home/'.dingwei'), inbox_db=str(home/'inbox.sqlite3'),
                 mcp_ipc_sock=str(home/'s'), ipc_send_body_only=True,
                 provision_audit_db=str(home/'audit'), provision_versions_file=str(home/'versions'),
                 provision_update_state_file=str(home/'provision'))
    helper = SessionHelper(cfg); helper.enroll_credential = Credential('desktop', seed['secret'])
    connection = asyncio.create_task(helper._run_once())
    child = None
    try:
        async with asyncio.timeout(8):
            while not helper.ipc_bound.is_set():
                if connection.done(): connection.result(); raise AssertionError('connection ended')
                await asyncio.sleep(.01)
        env = dict(os.environ, SH_IPC_SEND_BODY_ONLY='1', DINGWEI_MCP_IPC_SOCK=cfg.mcp_ipc_sock)
        child = await asyncio.create_subprocess_exec(sys.executable, '-B', '-m', 'sessionhelper.mcp_shim',
            env=env, stdin=asyncio.subprocess.PIPE, stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.DEVNULL)
        print('HOSTED_READY', flush=True)
        for ident in (1, 2):
            assert await asyncio.to_thread(sys.stdin.readline) == 'send\n'
            req = dict(jsonrpc='2.0', id=ident, method='tools/call', params=dict(name='send_message',
                arguments=dict(to='peer#u_zzc', body='DW_HOSTED_SEND_MARKER', reply_policy='no_reply')))
            child.stdin.write((json.dumps(req)+'\n').encode()); await child.stdin.drain()
            result = json.loads(await asyncio.wait_for(child.stdout.readline(), 8))
            assert result['id'] == ident and not result['result'].get('isError', False)
            # This is deliberately only a socket-write receipt; Go observes actual Hub routing.
            assert json.loads(result['result']['content'][0]['text'])['sent'] is True
            print('HOSTED_SENT', flush=True)
        assert await asyncio.to_thread(sys.stdin.readline) == 'stop\n'
    finally:
        if child:
            child.stdin.close()
            try: await asyncio.wait_for(child.wait(), 3)
            except asyncio.TimeoutError: child.kill(); await child.wait()
        connection.cancel(); await asyncio.gather(connection, return_exceptions=True)
    print('HOSTED_STOPPED', flush=True)


if __name__ == '__main__':
    try: asyncio.run(main())
    except Exception:
        print('HOSTED_ERROR', flush=True)
        raise SystemExit(1)
