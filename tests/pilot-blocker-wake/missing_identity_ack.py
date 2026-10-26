"""Real helper durable worker -> local Hub WS ACK, with identity header absent."""
import asyncio
import contextlib
import json
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
from urllib.parse import urlsplit

import websockets

from sessionhelper.app import SessionHelper
from sessionhelper.config import Config
from sessionhelper.enroll import Credential
from sessionhelper.inbox import DeliveryInbox
from sessionhelper.protocol import AddressBook


async def main():
    fixture = json.loads(Path(sys.argv[1]).read_text())
    target = urlsplit(fixture['origin'])
    assert target.scheme == 'http' and target.hostname == '127.0.0.1', 'loopback fixture only'
    with tempfile.TemporaryDirectory(prefix='missing-id-ack-') as temp:
        h = SessionHelper.__new__(SessionHelper)
        h.cfg = Config('kimi', 'u_zzc', 'SYNTHETIC', mode='driver', inbox_db=str(Path(temp) / 'inbox.db'))
        h.book = AddressBook('kimi', 'u_zzc', '')
        h.enroll_credential = Credential('kimi', fixture['credential'])
        assert not hasattr(h.enroll_credential, 'agent_id')
        # Model an old Hub withholding the optional authenticated identity header.
        # The credential authenticates the WS itself; envelope identity is never used.
        h._bind_connection_agent_identity({})
        h.delivery_inbox = DeliveryInbox(h.cfg.inbox_db, session_name='kimi')
        h.delivery_busy_acked = set()
        h.adapter_handle_lock = asyncio.Lock()
        injected = []

        def inject(env, callback):
            injected.append(env)
            callback()
            return ''

        h.adapter = SimpleNamespace(handle_delivery=inject)
        async with websockets.connect(fixture['origin'].replace('http:', 'ws:', 1) + '/ws/agent',
                                      additional_headers={'Authorization': 'Bearer ' + fixture['credential']}, proxy=None) as ws:
            await ws.send(json.dumps({'to': 'workpulse#u_zzc', 'meta': {'type': 'session_capabilities',
                'delivery_ack_v1': True, 'blocker_notice_v1': True, 'blocker_wake_v1': True}}))
            while True:
                env = json.loads(await asyncio.wait_for(ws.recv(), 8))
                if env.get('meta', {}).get('kind') == 'task_blocker_ready':
                    break
            acknowledgements, done = [], asyncio.Event()

            async def send(text):
                ack = json.loads(text)
                acknowledgements.append(ack)
                await ws.send(text)
                if ack['meta']['state'] in ('processed', 'failed'):
                    done.set()

            transport = SimpleNamespace(send=send)
            await h.accept_delivery(transport, env)
            worker = asyncio.create_task(h.delivery_worker_loop(transport))
            try:
                await asyncio.wait_for(done.wait(), 3)
            finally:
                worker.cancel()
                with contextlib.suppress(asyncio.CancelledError):
                    await worker
            assert not injected, 'missing identity unexpectedly injected a model turn'
            assert [x['meta']['state'] for x in acknowledgements] == ['delivered', 'processed']
            ack = acknowledgements[-1]
            assert ack['meta']['reason'] == 'current_agent_identity_unavailable'
            print('ACK_RESULT ' + json.dumps(ack), flush=True)


if __name__ == '__main__':
    asyncio.run(main())
