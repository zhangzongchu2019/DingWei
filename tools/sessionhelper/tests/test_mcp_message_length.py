"""AG UTF-8 boundaries through the actual listener; no live helper or credentials."""
import asyncio
import json
from pathlib import Path
import tempfile
import unittest

from sessionhelper.ipc_limits import MESSAGE_BODY_MAX_BYTES as L, IPC_REQUEST_MAX_BYTES
from sessionhelper.ipc_listener import _handle_connection


class Writer:
    def __init__(self):
        self.data = bytearray()
    def write(self, value):
        self.data.extend(value)
    async def drain(self):
        pass


class LengthTest(unittest.IsolatedAsyncioTestCase):
    async def dispatch(self, payload, *, restricted=False):
        sent = []
        async def sender(to, body, meta):
            sent.append((to, body))
            return {'sent': True}
        reader = asyncio.StreamReader(limit=IPC_REQUEST_MAX_BYTES + 2)
        reader.feed_data(json.dumps(payload, ensure_ascii=True).encode() + b'\n')
        reader.feed_eof()
        writer = Writer()
        await _handle_connection(reader, writer, sender, send_body_only=restricted)
        return json.loads(writer.data), sent

    async def test_boundary_matrix(self):
        for unit in ('a', '中', '😀', '\x01'):
            for delta in (-1, 0, 1):
                n = L + delta
                width = len(unit.encode())
                body = unit * (n // width) + 'x' * (n % width)
                for mode in ('ordinary', 'body-only', 'file'):
                    with self.subTest(unit=repr(unit), delta=delta, mode=mode), tempfile.TemporaryDirectory() as tmp:
                        payload = dict(type='mcp_send', to='target.zzc', body=body)
                        if mode == 'file':
                            path = Path(tmp) / 'body.txt'
                            path.write_bytes(body.encode())
                            payload.pop('body')
                            payload['file'] = str(path)
                        result, sent = await self.dispatch(payload, restricted=mode == 'body-only')
                        if delta <= 0:
                            self.assertTrue(result.get('ok'), 'MCP_LEN_EXACT_UTF8 ' + str(result))
                            self.assertEqual(sent, [('target.zzc', body)], 'MCP_LEN_EXACT_UTF8')
                        else:
                            self.assertFalse(result.get('ok'), 'MCP_LEN_OVERSIZE_ZERO_DELIVERY')
                            self.assertEqual(sent, [], 'MCP_LEN_OVERSIZE_ZERO_DELIVERY')

    async def test_ordinary_preserves_whitespace(self):
        body = ' \n中😀\t '
        result, sent = await self.dispatch(dict(type='mcp_send', to='target.zzc', body=body))
        self.assertTrue(result['ok'])
        self.assertEqual(sent[0][1], body, 'MCP_LEN_EXACT_UTF8')
