"""R60-③ dw_inbox: a lookup miss is an answer, not a transport failure.

Reading (or locally marking) an id that is not in this seat's inbox used to
reach the model as helper_rejected / outcome unknown / "do not automatically
resend", which is the same shape the shim uses for a dead socket. These cases
pin the deterministic negative, its no-side-effect property, and that only an
explicitly marked, allowlisted code is ever surfaced.
"""
import asyncio
import json
import os
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

from sessionhelper import mcp_shim
from sessionhelper.desktop import DesktopAdapter
from sessionhelper.inbox import DeliveryInbox

MISSING = 'dw-dlv_' + '0' * 32
PRESENT = 'dw-dlv_' + 'a' * 32


def encode(payload):
    return json.dumps(payload).encode('utf-8') + b'\n'


class DriverInboxLookupMissTest(unittest.TestCase):
    def inbox(self, root):
        inbox = DeliveryInbox(os.path.join(root, 'inbox.db'), 8, session_name='seat')
        inbox.accept(PRESENT, {'body': 'real delivery', 'meta': {'type': 'delivery', 'delivery_id': PRESENT}})
        return inbox

    def test_read_and_ack_of_an_unknown_id_are_definite_and_write_nothing(self):
        with tempfile.TemporaryDirectory() as root:
            inbox = self.inbox(root)
            for operation in ('read', 'ack'):
                with self.subTest(operation=operation):
                    self.assertEqual(inbox.request({'operation': operation, 'message_id': MISSING}),
                                     {'ok': False, 'error': 'not_found', 'outcome': 'none'})
            # No row was created and the real item is still unread.
            self.assertEqual(inbox.request({'operation': 'status'})['unread'], 1)
            self.assertEqual([m['id'] for m in inbox.request({'operation': 'list'})['messages']], [PRESENT])

    def test_request_id_is_the_delivery_id_so_reading_by_it_works(self):
        with tempfile.TemporaryDirectory() as root:
            inbox = self.inbox(root)
            self.assertEqual(inbox.claim_next()[0], PRESENT)
            self.assertTrue(inbox.begin_dispatch(PRESENT))
            request_id = inbox.lifecycle(PRESENT)['request_id']
            self.assertEqual(request_id, PRESENT)
            got = inbox.request({'operation': 'read', 'message_id': request_id})
            self.assertTrue(got['ok'])
            self.assertEqual(got['message']['body'], 'real delivery')

    def test_desktop_lookup_miss_is_marked_the_same_way(self):
        with tempfile.TemporaryDirectory() as root:
            adapter = DesktopAdapter(SimpleNamespace(inbox_db=os.path.join(root, 'desktop')))
            for operation in ('read', 'ack'):
                with self.subTest(operation=operation):
                    self.assertEqual(adapter.request({'operation': operation, 'message_id': MISSING}),
                                     {'ok': False, 'error': 'not_found', 'outcome': 'none'})
            self.assertEqual(adapter.request({'operation': 'status'})['unread'], 0)


class ShimLookupMissTest(unittest.TestCase):
    def test_marked_miss_is_surfaced_with_only_fixed_scalar_keys(self):
        got = mcp_shim._decode_ipc_response(encode(
            {'ok': False, 'error': 'not_found', 'outcome': 'none', 'mode': 'driver',
             'request_id': 'abc', 'detail': '/home/seat/.dingwei/secret', 'sql': 'SELECT 1',
             'rows': [{'body': 'private'}]}))
        self.assertEqual(got, {'ok': False, 'result': {'error': 'not_found', 'outcome': 'none', 'mode': 'driver'}})

    def test_unmarked_or_unlisted_negatives_stay_helper_rejected(self):
        for payload in ({'ok': False, 'error': 'not_found'},
                        {'ok': False, 'error': 'not_found', 'outcome': 'rejected'},
                        {'ok': False, 'error': 'send_failed', 'outcome': 'none'}):
            with self.subTest(payload=payload):
                with self.assertRaises(mcp_shim._IPCFailure) as caught:
                    mcp_shim._decode_ipc_response(encode(payload))
                self.assertEqual(caught.exception.category, 'helper_rejected')

    def test_request_too_large_passthrough_is_unchanged(self):
        got = mcp_shim._decode_ipc_response(encode(
            {'ok': False, 'error': 'request_too_large', 'outcome': 'rejected', 'limit': 65536}))
        self.assertEqual(got, {'ok': False, 'result': {'outcome': 'rejected', 'limit': 65536}})


class InboxIpcRoundTripTest(unittest.IsolatedAsyncioTestCase):
    async def test_read_unknown_id_returns_not_found_instead_of_helper_rejected(self):
        from sessionhelper.ipc_listener import serve_ipc
        with tempfile.TemporaryDirectory() as root:
            inbox = DeliveryInbox(os.path.join(root, 'inbox.db'), 8, session_name='seat')
            inbox.accept(PRESENT, {'body': 'real delivery', 'meta': {'type': 'delivery', 'delivery_id': PRESENT}})

            async def inbox_handler(args):
                return await asyncio.to_thread(inbox.request, args)

            async def send(*args):
                raise AssertionError('a read must not send')
            sock = str(Path(root) / 'ipc.sock')
            bound = asyncio.Event()
            task = asyncio.create_task(serve_ipc(sock, send, bound=bound, inbox_handler=inbox_handler))
            try:
                await asyncio.wait_for(bound.wait(), 2)
                with patch.object(mcp_shim, '_IPC_SOCK', sock):
                    missed = await asyncio.to_thread(mcp_shim._desktop_inbox,
                                                     {'operation': 'read', 'message_id': MISSING})
                    self.assertEqual(missed, {'ok': False, 'result': {'error': 'not_found', 'outcome': 'none'}})
                    found = await asyncio.to_thread(mcp_shim._desktop_inbox,
                                                    {'operation': 'read', 'message_id': PRESENT})
                    self.assertEqual(found['message']['body'], 'real delivery')
            finally:
                task.cancel()
                try:
                    await task
                except asyncio.CancelledError:
                    pass


if __name__ == '__main__':
    unittest.main()
