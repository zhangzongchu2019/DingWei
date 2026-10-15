"""Local-only real helper/WS/Unix-IPC/MCP bridge; credentials enter over stdin.

Go owns the temporary Hub, database and authenticated direct-write requests. No model,
production endpoint or external account is used. Only prefixed protocol output
is consumed by Go; bootstrap credentials are never printed.
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
    b = json.loads(sys.stdin.readline())
    state = Path(b['state'])
    state.chmod(0o700)
    if Path.home().resolve() != state.resolve():
        raise RuntimeError('private HOME required')
    cfg = Config(session_name=b['name'].lower(), key_id=b['key_id'], secret=b['secret'],
                 ws_base=b['ws_base'], mode='desktop', tool='CODEX', proxy='off',
                 cli_cwd=str(state), state_dir=str(state), collect=False,
                 inbox_db=str(state/'inbox.sqlite'), mcp_ipc_sock=str(state/'mcp.sock'),
                 provision_audit_db=str(state/'provision.sqlite'),
                 provision_versions_file=str(state/'versions.json'),
                 provision_update_state_file=str(state/'update.json'))
    helper = SessionHelper(cfg)
    helper.enroll_credential = Credential(b['name'], b['secret'])
    connection = asyncio.create_task(helper._run_once())
    child = None
    mcp_stdout = mcp_stderr = None
    try:
        async with asyncio.timeout(10):
            while not helper.ipc_bound.is_set():
                if connection.done():
                    connection.result()
                    raise RuntimeError('connection ended')
                await asyncio.sleep(.01)
        # Both MCP streams are private artifacts, never DEVNULL. Go scans these
        # together with the helper's complete stdout/stderr after process join.
        mcp_stdout = os.fdopen(os.open(state/'mcp.stdout', os.O_WRONLY|os.O_CREAT|os.O_EXCL, 0o600), 'wb', buffering=0)
        mcp_stderr = os.fdopen(os.open(state/'mcp.stderr', os.O_WRONLY|os.O_CREAT|os.O_EXCL, 0o600), 'wb', buffering=0)
        child = await asyncio.create_subprocess_exec(
            sys.executable, '-B', '-m', 'sessionhelper.mcp_shim',
            stdin=asyncio.subprocess.PIPE, stdout=asyncio.subprocess.PIPE,
            stderr=mcp_stderr,
            env=dict(os.environ, DINGWEI_MCP_IPC_SOCK=cfg.mcp_ipc_sock))
        ident = 0
        async def rpc(method, params):
            nonlocal ident
            ident += 1
            child.stdin.write((json.dumps(dict(jsonrpc='2.0', id=ident,
                                              method=method, params=params))+'\n').encode())
            await child.stdin.drain()
            raw_packet = await asyncio.wait_for(child.stdout.readline(), 38)
            mcp_stdout.write(raw_packet)
            packet = json.loads(raw_packet)
            if packet.get('id') != ident:
                raise RuntimeError('RPC id mismatch')
            return packet
        await rpc('initialize', {})
        listing = await rpc('tools/list', {})
        names = {v['name'] for v in listing['result']['tools']}
        if not {'dw_spec_create', 'dw_spec_update'} <= names:
            raise RuntimeError('missing spec tools')
        print('A3_E2E_READY', flush=True)
        while True:
            raw = await asyncio.to_thread(sys.stdin.readline)
            if not raw:
                raise RuntimeError('controller EOF')
            command = json.loads(raw)
            if command.get('stop'):
                break
            packet = await rpc('tools/call', dict(name=command['name'], arguments=command['args']))
            # Assert the actual model-facing RPC never exposes either credential.
            encoded = json.dumps(packet)
            if b['secret'] in encoded or '"capability"' in encoded:
                raise RuntimeError('private material crossed model boundary')
            print('A3_E2E_RESULT '+encoded, flush=True)
    finally:
        if child is not None:
            child.stdin.close()
            try:
                await asyncio.wait_for(child.wait(), 3)
            except asyncio.TimeoutError:
                child.kill()
                await child.wait()
            # Capture any non-protocol tail before declaring the bridge clean.
            mcp_stdout.write(await child.stdout.read())
        for stream in (mcp_stdout, mcp_stderr):
            if stream is not None:
                stream.close()
        connection.cancel()
        await asyncio.gather(connection, return_exceptions=True)
    print('A3_E2E_CLEAN', flush=True)


if __name__ == '__main__':
    try:
        asyncio.run(main())
    except BaseException as exc:
        print('A3_E2E_ERROR '+type(exc).__name__, flush=True)
        raise SystemExit(1)
