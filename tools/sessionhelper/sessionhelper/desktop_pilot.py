"""Local desktop MCP pilot. Synthetic input only; never connects to a Hub.

Use a newly configured desktop profile without credentials or pending enrollment.
Keep the terminal open, ask the existing desktop chat to read the fixture with
DW-MCP and reply exactly DW_DESKTOP_OK, then stop with Ctrl+C. This verifies
explicit tools only; it does not claim automatic model wake or real routing.
"""
from __future__ import annotations

import argparse
import asyncio
import json
import os
from pathlib import Path
import secrets
import signal
from types import SimpleNamespace

from .desktop import DesktopAdapter
from .enroll import write_private_file
from .ipc_listener import serve_ipc


class Pilot:
    def __init__(self, profile_path):
        profile_path = Path(profile_path)
        if profile_path.name != "setup-profile.json" or profile_path.is_symlink() or not profile_path.is_file() or profile_path.stat().st_mode & 0o077:
            raise ValueError("pilot requires a private setup-profile.json")
        values = json.loads(profile_path.read_text())
        if values.get("SH_MODE") != "desktop":
            raise ValueError("pilot requires desktop mode")
        self.state = Path(values["SH_STATE_DIR"]).resolve()
        if self.state != profile_path.resolve().parent or self.state.stat().st_mode & 0o077:
            raise ValueError("pilot state must be the private profile directory")
        self.socket = str(self.state / "dingwei-mcp.sock")
        if values.get("DINGWEI_MCP_IPC_SOCK") != self.socket:
            raise ValueError("pilot socket must match the owned state directory")
        for name in ("credential.json", "claim-pending.json", "enroll-pending.json", "helper-process.json", "dingwei-mcp.sock", "desktop-pilot.lock"):
            if os.path.lexists(self.state / name):
                raise ValueError("existing runtime/enrollment resource; preserve it and use a fresh pilot profile")
        self.nonce = secrets.token_hex(8)
        self.message_id = "desktop-pilot-" + self.nonce
        self.target = "desktop-pilot#LOCAL"
        self.db_base = self.state / ("desktop-pilot-" + self.nonce)
        self.report_path = self.state / ("desktop-pilot-" + self.nonce + "-report.json")
        self.report = {"mode": "LOCAL_SYNTHETIC_PILOT", "state": "PREPARED", "message_id": self.message_id,
                       "read_count": 0, "reply_count": 0, "hub_connections": 0,
                       "native_auto_notify": "NOT_VERIFIED", "native_auto_notify_implemented": False, "desktop_UI_owner_confirmation": "PENDING"}
        self.adapter = None
        self.socket_identity = None

    async def inbox(self, args):
        result = self.adapter.request(args)
        if args.get("operation") == "read" and args.get("message_id") == self.message_id and result.get("ok"):
            self.report["read_count"] += 1
            write_private_file(str(self.report_path), json.dumps(self.report))
        return result

    async def send(self, to, body, meta):
        if to != self.target or body != "DW_DESKTOP_OK" or meta.get("in_reply_to") != self.message_id:
            raise ValueError("pilot accepts only the exact synthetic reply and message correlation; nothing was sent")
        if not self.report["read_count"]:
            raise ValueError("read the fixture before replying")
        self.report["reply_count"] += 1
        write_private_file(str(self.report_path), json.dumps(self.report))
        return {"ok": True, "mode": "LOCAL_SYNTHETIC_PILOT", "sent_to_hub": False, "reply_count": self.report["reply_count"]}

    async def run(self, stop=None, ready=None):
        lock = self.state / "desktop-pilot.lock"
        fd = os.open(lock, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
        lock_stat = os.fstat(fd)
        lock_identity = (lock_stat.st_dev, lock_stat.st_ino)
        with os.fdopen(fd, "w") as output:
            json.dump({"pid": os.getpid(), "nonce": self.nonce}, output)
        task = None
        try:
            if os.path.lexists(self.socket):
                raise ValueError("socket appeared concurrently; do not replace it")
            self.adapter = DesktopAdapter(SimpleNamespace(inbox_db=str(self.db_base)))
            self.adapter.handle_delivery({"id": self.message_id, "from": self.target, "body": "Local synthetic desktop test. Reply only DW_DESKTOP_OK to desktop-pilot#LOCAL and use this message ID as in_reply_to."})
            write_private_file(str(self.report_path), json.dumps(self.report))
            bound = asyncio.Event()
            task = asyncio.create_task(serve_ipc(self.socket, self.send, bound=bound, inbox_handler=self.inbox, replace_existing=False))
            await asyncio.wait_for(bound.wait(), 5)
            # The fixture server is local/private; no account is registered or authenticated.
            socket_stat = os.lstat(self.socket)
            self.socket_identity = (socket_stat.st_dev, socket_stat.st_ino)
            os.chmod(self.socket, 0o600)
            self.report["state"] = "IPC_READY"
            write_private_file(str(self.report_path), json.dumps(self.report))
            print("LOCAL SYNTHETIC PILOT; automatic wake NOT_VERIFIED. Message ID:", self.message_id, flush=True)
            print("Reply destination:", self.target, "Report:", self.report_path, flush=True)
            if ready is not None:
                ready.set()
            if stop is None:
                stop = asyncio.Event()
                for sig in (signal.SIGINT, signal.SIGTERM):
                    asyncio.get_running_loop().add_signal_handler(sig, stop.set)
            await stop.wait()
        finally:
            if task is not None:
                task.cancel()
                await asyncio.gather(task, return_exceptions=True)
                if self.socket_identity and os.path.lexists(self.socket):
                    current = os.lstat(self.socket)
                    if (current.st_dev, current.st_ino) == self.socket_identity:
                        os.unlink(self.socket)
            if os.path.lexists(lock):
                current = os.lstat(lock)
                if (current.st_dev, current.st_ino) == lock_identity:
                    lock.unlink()
            # Retain the private synthetic report/inbox, never delete real state.


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--profile", type=Path, required=True)
    args = parser.parse_args(argv)
    asyncio.run(Pilot(args.profile).run())


if __name__ == "__main__":
    main()
