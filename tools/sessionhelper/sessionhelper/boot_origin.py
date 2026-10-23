"""Local, best-effort provenance for the actual helper process (not authentication)."""
from __future__ import annotations

import json
import os
from pathlib import Path
import secrets
import sys
import threading

RECORD_NAME = "helper-boot-origin.json"
_ATTEMPTED_PID = None
_LOCK = threading.Lock()


def _starttime(pid: int) -> int:
    raw = Path("/proc/self/stat").read_text(encoding="ascii")
    head, separator, tail = raw.rpartition(")")
    if not separator or head.split("(", 1)[0].strip() != str(pid):
        raise ValueError("self_stat_invalid")
    ticks = int(tail.split()[19])  # proc(5) field 22, clock ticks since boot.
    if ticks <= 0:
        raise ValueError("self_starttime_invalid")
    return ticks


def _record() -> dict:
    import sessionhelper
    pid = os.getpid()
    return {
        "type": "helper_boot_origin",
        "pid": pid,
        "starttime": _starttime(pid),
        "sessionhelper_file": str(Path(sessionhelper.__file__).resolve(strict=True)),
        "isolated": int(sys.flags.isolated),
        "boot_marker": secrets.token_hex(16),
    }


def _stderr(line: str) -> None:
    try:
        sys.stderr.write(line + "\n")
        sys.stderr.flush()
    except Exception:
        pass  # Even a broken stderr must not prevent helper startup.


def _replace(state_dir: str, data: bytes) -> None:
    directory = Path(state_dir)
    directory.mkdir(mode=0o700, parents=True, exist_ok=True)
    directory_fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    temporary = ".helper-boot-origin-" + secrets.token_hex(16)
    created = False
    try:
        info = os.fstat(directory_fd)
        if info.st_uid != os.geteuid() or info.st_mode & 0o022:
            raise PermissionError("state_directory_unprotected")
        fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                     0o600, dir_fd=directory_fd)
        created = True
        with os.fdopen(fd, "wb") as output:
            os.fchmod(output.fileno(), 0o600)
            output.write(data)
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, RECORD_NAME, src_dir_fd=directory_fd, dst_dir_fd=directory_fd)
        created = False
        os.fsync(directory_fd)
    finally:
        try:
            if created:
                os.unlink(temporary, dir_fd=directory_fd)
        finally:
            os.close(directory_fd)


def record_boot_origin(state_dir: str) -> None:
    """Attempt once per PID; no credentials, environment values or exception text."""
    global _ATTEMPTED_PID
    pid = os.getpid()
    with _LOCK:
        if _ATTEMPTED_PID == pid:
            return
        _ATTEMPTED_PID = pid
    try:
        line = json.dumps(_record(), ensure_ascii=True, sort_keys=True, separators=(",", ":"))
    except Exception:
        _stderr("HELPER_BOOT_ORIGIN_WARNING origin_unavailable")
        return
    try:
        _replace(state_dir, (line + "\n").encode("ascii"))
    except Exception:
        _stderr("HELPER_BOOT_ORIGIN_WARNING record_write_failed")
    _stderr(line)
