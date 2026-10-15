"""Real filesystem sockets, isolated under a short /tmp root; no live helper."""
import asyncio
import contextlib
import io
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock

from sessionhelper.ipc_listener import (
    IPC_SOCKET_PATH_MAX_BYTES, IPCSocketPathError, serve_ipc,
)


class IPCSocketPathTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix="ipc-len-", dir="/tmp")
        self.addCleanup(self.tmp.cleanup)

    def path(self, byte_count, prefix=""):
        root = self.tmp.name + "/"
        return root + prefix + "x" * (byte_count - len(os.fsencode(root + prefix)))

    async def assert_rejected(self, path):
        bound = asyncio.Event()
        bound.set()  # A stale ready flag must not survive a failed startup.
        error_log = io.StringIO()
        with contextlib.redirect_stderr(error_log):
            with mock.patch("sessionhelper.ipc_listener.socket.socket") as factory:
                with self.assertRaises(IPCSocketPathError) as raised:
                    await serve_ipc(path, None, bound=bound)
                factory.assert_not_called()
        expected = (f"IPC socket startup failed: path too long "
                    f"({len(os.fsencode(path))} bytes; maximum {IPC_SOCKET_PATH_MAX_BYTES} bytes)")
        self.assertEqual(str(raised.exception), expected)
        self.assertEqual(error_log.getvalue(), f"[ipc_listener] ERROR {expected}\n")
        self.assertNotIn(path, error_log.getvalue())
        self.assertFalse(bound.is_set())

    async def test_oversized_path_fails_explicitly_before_unlink(self):
        path = self.path(IPC_SOCKET_PATH_MAX_BYTES + 1, "synthetic-private-")
        Path(path).write_bytes(b"keep-existing-file")
        await self.assert_rejected(path)
        self.assertEqual(Path(path).read_bytes(), b"keep-existing-file")

    async def test_multibyte_path_counts_encoded_bytes_not_characters(self):
        path = self.path(IPC_SOCKET_PATH_MAX_BYTES + 1, "目录-")
        self.assertLess(len(path), IPC_SOCKET_PATH_MAX_BYTES)
        await self.assert_rejected(path)

    async def test_maximum_length_binds_and_serves_then_clears_ready(self):
        path = self.path(IPC_SOCKET_PATH_MAX_BYTES)
        self.assertEqual(len(os.fsencode(path)), IPC_SOCKET_PATH_MAX_BYTES)
        bound = asyncio.Event()
        task = asyncio.create_task(serve_ipc(path, None, bound=bound))
        try:
            await asyncio.wait_for(bound.wait(), 2)
            self.assertFalse(task.done())
            reader, writer = await asyncio.open_unix_connection(path)
            writer.write(b'{"type":"unsupported-test"}\n')
            await writer.drain()
            response = await asyncio.wait_for(reader.readline(), 2)
            self.assertIn(b'"ok": false', response)
            writer.close()
            await writer.wait_closed()
        finally:
            task.cancel()
            with contextlib.suppress(asyncio.CancelledError):
                await task
        self.assertFalse(bound.is_set())


if __name__ == "__main__":
    unittest.main()
