"""Local-only real helper/WS/Unix-IPC/MCP bridge; credentials enter over stdin.

Go owns the temporary Hub, database and human approval requests. No model,
production endpoint or external account is used. Only prefixed protocol output
is consumed by Go; bootstrap and capabilities are never printed.
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
    try:
        async with asyncio.timeout(10):
            while not helper.ipc_bound.is_set():
                if connection.done():
                    connection.result()
                    raise RuntimeError('connection ended')
                await asyncio.sleep(.01)
        child = await asyncio.create_subprocess_exec(
            sys.executable, '-B', '-m', 'sessionhelper.mcp_shim',
            stdin=asyncio.subprocess.PIPE, stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
            env=dict(os.environ, DINGWEI_MCP_IPC_SOCK=cfg.mcp_ipc_sock))
        diagnostic = bytearray()
        async def drain_diagnostics():
            while True:
                chunk = await child.stderr.read(4096)
                if not chunk:
                    return
                diagnostic.extend(chunk)
        diagnostic_task = asyncio.create_task(drain_diagnostics())
        ident = 0
        async def rpc(method, params):
            nonlocal ident
            ident += 1
            child.stdin.write((json.dumps(dict(jsonrpc='2.0', id=ident,
                                              method=method, params=params))+'\n').encode())
            await child.stdin.drain()
            packet = json.loads(await asyncio.wait_for(child.stdout.readline(), 38))
            if packet.get('id') != ident:
                raise RuntimeError('RPC id mismatch')
            return packet
        await rpc('initialize', {})
        listing = await rpc('tools/list', {})
        names = {v['name'] for v in listing['result']['tools']}
        if not {'dw_spec_create', 'dw_spec_update'} <= names:
            raise RuntimeError('missing spec tools')
        # Intentional test-only mutation: Go captures this private diagnostic
        # and must reject it without emitting its contents into test logs.
        if os.environ.get('A3_E2E_TEST_DIAGNOSTIC_LEAK') == '1':
            print(b['secret'], file=sys.stderr, flush=True)
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
            await diagnostic_task
            # Do not forward child diagnostics.  This is the complete MCP
            # stderr stream, privately scanned before teardown.
            if b['secret'].encode() in diagnostic or b'"capability"' in diagnostic:
                raise RuntimeError('private MCP diagnostic')
        connection.cancel()
        await asyncio.gather(connection, return_exceptions=True)
    print('A3_E2E_CLEAN', flush=True)


if __name__ == '__main__':
    try:
        asyncio.run(main())
    except BaseException as exc:
        print('A3_E2E_ERROR '+type(exc).__name__, flush=True)
        raise SystemExit(1)
