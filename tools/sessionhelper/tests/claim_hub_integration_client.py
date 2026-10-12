"""Isolated actual HTTP enrollment/helper child; never print codes or tokens."""
import asyncio
import hashlib
import json
import os
from pathlib import Path
import sys

from sessionhelper.app import SessionHelper
from sessionhelper.claim_enroll import claim_enroll, cancel_pending_claim, PENDING
from sessionhelper.config import Config
from sessionhelper.enroll import CREDENTIAL_FILE, EnrollStopped, UrllibTransport


def main():
    if '--resume' in sys.argv:
        print('CLAIM_RESUME_READY fresh_interpreter', flush=True)
    config = json.loads(sys.stdin.readline())
    state = Path(config['state'])
    transport = UrllibTransport(config['base'])
    notifications = []

    def show(command):
        assert command.startswith('#pair ')
        # Code is already in the private pending file; no secret crosses stdout.
        print('CLAIM_PAIR_READY pending', flush=True)
        assert json.loads(sys.stdin.readline()) == {'operation': 'paired'}

    options = dict(state_dir=str(state), owner=config['requested'], short_name='np',
                   agent_name='desktop', mode='desktop', show=show,
                   notify=notifications.append, max_polls=3)
    if config['mode'] == 'cancel_then_new':
        def cancel_wait(command):
            assert command.startswith('#pair ')
            print('CLAIM_CANCEL_READY pending', flush=True)
            assert json.loads(sys.stdin.readline()) == {'operation': 'cancel'}
            raise KeyboardInterrupt()
        try:
            claim_enroll(transport=transport, **dict(options, show=cancel_wait))
            raise AssertionError('expected interrupted pairing')
        except KeyboardInterrupt:
            pass
        archive = Path(cancel_pending_claim(state_dir=str(state), owner=config['requested'], transport=transport))
        assert archive.stat().st_mode & 0o077 == 0
        assert not (state/PENDING).exists() and not (state/CREDENTIAL_FILE).exists()
        print('CLAIM_CANCELLED archived', flush=True)
        assert json.loads(sys.stdin.readline()) == {'operation': 'new'}
    if config['mode'] == 'lost_ack' and not config.get('resume'):
        class LostResponse(UrllibTransport):
            def post_authenticated(self, path, body, headers):
                result = super().post_authenticated(path, body, headers)
                if path.endswith('/ack'):
                    raise OSError('synthetic lost acknowledgement response')
                return result
        try:
            claim_enroll(transport=LostResponse(config['base']), **options)
            raise AssertionError('expected lost ACK response')
        except OSError:
            assert (state/PENDING).exists() and (state/CREDENTIAL_FILE).exists()
        # Run fresh interpreter: recovery must depend on durable files, not memory.
        print('CLAIM_RESTART_READY persisted', flush=True)
        assert json.loads(sys.stdin.readline()) == {'operation': 'restart'}
        os.execv(sys.executable, [sys.executable, __file__, '--resume'])
    try:
        credential = claim_enroll(transport=transport, **options)
    except EnrollStopped:
        assert config['mode'] == 'takeover'
        assert not (state/CREDENTIAL_FILE).exists() and (state/PENDING).exists()
        print('CLAIM_DENIED no_credential', flush=True)
        return
    assert not (state/PENDING).exists()
    private = json.loads((state/CREDENTIAL_FILE).read_text())
    assert private['user_id'] == config['actual']
    assert private['agent_full_name'] == 'desktop.'+config['actual_short']
    assert (state/CREDENTIAL_FILE).stat().st_mode & 0o077 == 0
    if config.get('resume'):
        assert hashlib.sha256((state/CREDENTIAL_FILE).read_bytes()).hexdigest() == config['saved_sha']
    assert len(notifications) == (0 if config.get('resume') else 2 + (config['mode']=='reuse') + (config['mode']=='cancel_then_new'))
    asyncio.run(connect(config, credential))


async def connect(config, credential):
    state = Path(config['state'])
    cfg = Config(session_name=credential.agent_name.lower(), key_id="", secret=credential.secret,
                 ws_base='ws'+config['base'][4:], mode='desktop', tool='CODEX',
                 cli_cwd=str(state), state_dir=str(state), inbox_db=str(state/'inbox.sqlite'),
                 mcp_ipc_sock=str(state/'mcp.sock'), collect=False,
                 provision_audit_db=str(state/'provision.sqlite'),
                 provision_versions_file=str(state/'versions.json'),
                 provision_update_state_file=str(state/'update.json'))
    helper = SessionHelper(cfg)
    helper.enroll_credential = credential
    connection = asyncio.create_task(helper._run_once())
    try:
        async with asyncio.timeout(8):
            while not helper.ipc_bound.is_set() or not helper.outbound_receipts.negotiated:
                if connection.done():
                    connection.result()
                    raise AssertionError('connection ended')
                await asyncio.sleep(.01)
        print('CLAIM_CONNECTED authenticated', flush=True)
        assert json.loads(await asyncio.to_thread(sys.stdin.readline)) == {'operation':'stop'}
    finally:
        connection.cancel()
        await asyncio.gather(connection, return_exceptions=True)
    assert not helper.outbound_receipts.pending
    print('CLAIM_CLEAN joined', flush=True)


if __name__ == '__main__':
    try:
        main()
    except BaseException as exc:
        print('CLAIM_ERROR '+type(exc).__name__, flush=True)
        raise SystemExit(1)
