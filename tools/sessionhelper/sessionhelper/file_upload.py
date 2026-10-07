"""Agent file upload policy (R-V2-AGENT-FILE-UPLOAD, plan runbooks/PLAN-R-V2-AGENT-FILE-UPLOAD-opus50tc2k.md §7/§9/§13).

The uploaded link is public for 7 days without authentication, so "the Agent may read it" is not
enough. Two layers:

* the file descriptor is opened by the Agent's own process (kernel permissions) and handed over with
  SCM_RIGHTS — helper never accepts or resolves a client-supplied path;
* policy is decided on what that descriptor really points at: real path (allow-list workspace root,
  no dot segments; deny-list protected locations and names), (st_dev, st_ino) against protected files
  (hard links), and a content scan for current credential / auth.json token strings (copies).

Nothing here logs file contents, tokens, tickets or full paths.
"""

from __future__ import annotations

import fnmatch
import hashlib
import json
import os
import stat
import sys
import threading
import time
from collections import deque
from dataclasses import dataclass, field

# == internal/filepolicy.MaxUploadBytes (a test pins the Go constant).
UPLOAD_MAX_BYTES = 50 * 1024 * 1024
READ_CHUNK = 1024 * 1024
MIN_SECRET_LEN = 20
SECRET_SOURCE_MAX_BYTES = 1024 * 1024
SYSTEM_PREFIXES = ("/proc", "/sys", "/dev", "/etc", "/run")
PROTECTED_NAME_PATTERNS = ("inbox.sqlite3", "inbox.sqlite3-wal", "inbox.sqlite3-shm", "*.env", ".env*",
                           "*.pem", "*.key", "id_rsa*", "id_ed25519*")


class UploadRejected(Exception):
    """Policy refusal. ``code`` is a short reason code safe for logs and IPC."""

    def __init__(self, code: str):
        super().__init__(code)
        self.code = code


def fd_real_path(fd: int) -> str:
    """Real path of the file behind *fd*, as seen from this process's mount namespace."""
    try:
        if os.fstat(fd).st_nlink == 0:
            raise UploadRejected("deleted")
        if sys.platform.startswith("linux"):
            path = os.readlink(f"/proc/self/fd/{fd}")
        elif sys.platform == "darwin":
            import fcntl

            raw = fcntl.fcntl(fd, 50, b"\0" * 1024)  # F_GETPATH
            path = raw.split(b"\0", 1)[0].decode("utf-8", "surrogateescape")
        else:
            raise UploadRejected("unresolved_path")
    except UploadRejected:
        raise
    except OSError:
        raise UploadRejected("unresolved_path") from None
    if path.endswith(" (deleted)"):
        raise UploadRejected("deleted")
    if not path.startswith("/"):
        raise UploadRejected("unresolved_path")
    return os.path.normpath(path)


def _under(path: str, root: str) -> bool:
    root = root.rstrip("/") or "/"
    return path == root or path.startswith(root + "/")


@dataclass
class UploadPolicy:
    workspace_root: str
    protected_dirs: tuple[str, ...] = ()
    protected_files: tuple[str, ...] = ()

    @classmethod
    def build(cls, workspace_root: str, *, codex_home: str = "", state_dir: str = "", cred_file: str = "",
              enroll_token_file: str = "", inbox_db: str = "") -> "UploadPolicy":
        root = os.path.realpath(workspace_root) if workspace_root else ""
        dirs = [d for d in (codex_home, state_dir) if d]
        files = [f for f in (cred_file, enroll_token_file) if f]
        if codex_home:
            files.append(os.path.join(codex_home, "auth.json"))
        if inbox_db:
            files += [inbox_db, inbox_db + "-wal", inbox_db + "-shm"]
        return cls(root, tuple(os.path.realpath(d) for d in dirs), tuple(os.path.realpath(f) for f in files))

    @property
    def enabled(self) -> bool:
        return bool(self.workspace_root) and self.workspace_root != "/"

    def check(self, real_path: str, st: os.stat_result) -> None:
        if not self.enabled:
            raise UploadRejected("upload_disabled")
        if not stat.S_ISREG(st.st_mode):
            raise UploadRejected("not_regular_file")
        if not _under(real_path, self.workspace_root) or real_path == self.workspace_root:
            raise UploadRejected("outside_workspace")
        rel = os.path.relpath(real_path, self.workspace_root)
        if any(part.startswith(".") for part in rel.split(os.sep)):
            raise UploadRejected("dot_segment")
        if any(_under(real_path, p) for p in SYSTEM_PREFIXES + self.protected_dirs):
            raise UploadRejected("protected_path")
        if real_path in self.protected_files:
            raise UploadRejected("protected_path")
        name = os.path.basename(real_path)
        if any(fnmatch.fnmatchcase(name, pattern) for pattern in PROTECTED_NAME_PATTERNS):
            raise UploadRejected("protected_name")
        for protected in self.protected_files:
            try:
                other = os.stat(protected)
            except OSError:
                continue
            if (other.st_dev, other.st_ino) == (st.st_dev, st.st_ino):
                raise UploadRejected("protected_inode")


def _json_strings(value, out: set[bytes]) -> None:
    if isinstance(value, str):
        if len(value) >= MIN_SECRET_LEN:
            out.add(value.encode("utf-8"))
    elif isinstance(value, dict):
        for item in value.values():
            _json_strings(item, out)
    elif isinstance(value, list):
        for item in value:
            _json_strings(item, out)


def secret_needles(content: bytes) -> set[bytes]:
    """Whole (stripped) content plus every JSON string value of length >= 20."""
    needles: set[bytes] = set()
    whole = content.strip()
    if len(whole) >= MIN_SECRET_LEN:
        needles.add(whole)
    try:
        _json_strings(json.loads(content.decode("utf-8")), needles)
    except (ValueError, UnicodeDecodeError):
        pass
    return needles


class SecretScanner:
    """Token set from the credential file and CODEX_HOME/auth.json, re-read when they change.

    Re-read on every upload (cheap: stat, then parse only if (mtime_ns, size, ino) changed), because
    Codex refreshes auth.json in place and a start-up snapshot would miss the renewed tokens.
    Values stay in memory only.
    """

    def __init__(self, sources: tuple[str, ...]):
        self.sources = tuple(s for s in sources if s)
        self._cache: dict[str, tuple[tuple[int, int, int], frozenset[bytes]]] = {}
        self._lock = threading.Lock()

    def needles(self) -> tuple[bytes, ...]:
        found: set[bytes] = set()
        with self._lock:
            for path in self.sources:
                try:
                    st = os.stat(path)
                except OSError:
                    self._cache.pop(path, None)
                    continue
                sig = (st.st_mtime_ns, st.st_size, st.st_ino)
                cached = self._cache.get(path)
                if cached is None or cached[0] != sig:
                    try:
                        with open(path, "rb") as fh:
                            content = fh.read(SECRET_SOURCE_MAX_BYTES)
                    except OSError:
                        self._cache.pop(path, None)
                        continue
                    cached = (sig, frozenset(secret_needles(content)))
                    self._cache[path] = cached
                found |= cached[1]
        return tuple(sorted(found, key=len, reverse=True))


def scan_and_digest(fd: int, size: int, needles: tuple[bytes, ...], chunk: int = READ_CHUNK) -> str:
    """Read exactly *size* bytes from *fd* (pread, offset 0), refuse on any needle, return sha256 hex.

    The tail of the previous chunk (longest needle - 1 bytes) is prepended to the next one so a token
    split across a chunk boundary is still found. Reading more or fewer bytes than *size* aborts.
    """
    digest = hashlib.sha256()
    keep = max((len(n) for n in needles), default=1) - 1
    tail = b""
    offset = 0
    while offset < size:
        data = os.pread(fd, min(chunk, size - offset), offset)
        if not data:
            raise UploadRejected("size_changed")
        offset += len(data)
        digest.update(data)
        window = tail + data
        if any(n in window for n in needles):
            raise UploadRejected("secret_content")
        tail = window[-keep:] if keep > 0 else b""
    if os.pread(fd, 1, size):
        raise UploadRejected("size_changed")
    return digest.hexdigest()


def iter_exact(fd: int, size: int, chunk: int = READ_CHUNK):
    """Yield exactly *size* bytes from *fd*; abort if the file shrank or grew since fstat."""
    offset = 0
    while offset < size:
        data = os.pread(fd, min(chunk, size - offset), offset)
        if not data:
            raise UploadRejected("size_changed")
        offset += len(data)
        yield data
    if os.pread(fd, 1, size):
        raise UploadRejected("size_changed")


@dataclass
class RateLimiter:
    """Per-process hourly limits (reset on helper restart; not a global quota)."""

    max_count: int = 20
    max_bytes: int = 1024 * 1024 * 1024
    window: float = 3600.0
    clock: object = time.monotonic
    _events: deque = field(default_factory=deque)
    _lock: threading.Lock = field(default_factory=threading.Lock)

    def admit(self, size: int) -> None:
        with self._lock:
            now = self.clock()
            while self._events and now - self._events[0][0] >= self.window:
                self._events.popleft()
            used = sum(n for _, n in self._events)
            if len(self._events) + 1 > self.max_count or used + size > self.max_bytes:
                raise UploadRejected("rate_limited")
            self._events.append((now, size))
