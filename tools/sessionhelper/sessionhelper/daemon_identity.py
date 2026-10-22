"""Linux Unix-peer identity checks before sending any app-server bytes.

No process discovery, pidfile guessing, daemon startup or mutable state. An
optional owned binding must come from a trusted launcher holding that process.
"""
from __future__ import annotations

from dataclasses import dataclass
import os
from pathlib import Path
import shutil
import socket
import stat
import struct
import sys


class DaemonIdentityError(RuntimeError):
    """Only fixed categories may escape into transport/admission telemetry."""
    REASONS = frozenset({'unsupported', 'peer_unverifiable', 'uid_mismatch',
        'expected_exe_unverifiable', 'exe_unverifiable', 'exe_mismatch',
        'starttime_unverifiable', 'process_changed', 'owned_binding_invalid',
        'owned_pid_mismatch', 'owned_starttime_mismatch', 'invalid_switch'})

    def __init__(self, reason: str):
        self.reason = reason if reason in self.REASONS else 'peer_unverifiable'
        super().__init__('daemon_identity_' + self.reason)


@dataclass(frozen=True)
class OwnedDaemonIdentity:
    pid: int
    starttime: int


@dataclass(frozen=True)
class PeerIdentity:
    pid: int
    uid: int
    starttime: int
    scope: str

    def metadata(self) -> dict:
        return {'peer_pid': self.pid, 'peer_uid': self.uid,
                'peer_starttime': self.starttime, 'identity_scope': self.scope}


def process_starttime(pid: int) -> int:
    if type(pid) is not int or pid <= 0:
        raise DaemonIdentityError('starttime_unverifiable')
    try:
        raw = Path('/proc', str(pid), 'stat').read_text()
        # comm may contain whitespace and closing parentheses. Fields after the
        # final ')' start at field 3; starttime is field 22, not a naive split.
        head, separator, tail = raw.rpartition(')')
        if not separator or head.split('(', 1)[0].strip() != str(pid):
            raise ValueError()
        value = int(tail.split()[19])
        if value <= 0:
            raise ValueError()
        return value
    except (OSError, ValueError, IndexError, UnicodeError):
        raise DaemonIdentityError('starttime_unverifiable') from None


def _expected_executable(configured: str) -> os.stat_result:
    try:
        if not isinstance(configured, str) or not configured:
            raise ValueError()
        path = configured if os.path.isabs(configured) else shutil.which(configured)
        if not path:
            raise ValueError()
        resolved = Path(path).resolve(strict=True)
        info = resolved.stat()
        if not stat.S_ISREG(info.st_mode) or not os.access(resolved, os.X_OK):
            raise ValueError()
        return info
    except (OSError, ValueError, RuntimeError):
        raise DaemonIdentityError('expected_exe_unverifiable') from None


def verify_peer(sock: socket.socket, expected_exe: str,
                owned: OwnedDaemonIdentity | None = None) -> PeerIdentity:
    if sys.platform != 'linux' or not hasattr(socket, 'SO_PEERCRED'):
        raise DaemonIdentityError('unsupported')
    try:
        raw = sock.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, struct.calcsize('iII'))
        pid, uid, _gid = struct.unpack('iII', raw)
        if pid <= 0:
            raise ValueError()
    except (OSError, ValueError, TypeError, struct.error):
        raise DaemonIdentityError('peer_unverifiable') from None
    if uid != os.getuid():
        raise DaemonIdentityError('uid_mismatch')
    if owned is not None and (not isinstance(owned, OwnedDaemonIdentity)
            or type(owned.pid) is not int or owned.pid <= 0
            or type(owned.starttime) is not int or owned.starttime <= 0):
        raise DaemonIdentityError('owned_binding_invalid')
    if owned is not None and pid != owned.pid:
        raise DaemonIdentityError('owned_pid_mismatch')
    before = process_starttime(pid)
    if owned is not None and before != owned.starttime:
        raise DaemonIdentityError('owned_starttime_mismatch')
    expected = _expected_executable(expected_exe)
    try:
        exe = Path('/proc', str(pid), 'exe')
        target = os.readlink(exe)
        if not os.path.isabs(target) or target.endswith(' (deleted)'):
            raise ValueError()
        actual = exe.stat()
        if not stat.S_ISREG(actual.st_mode):
            raise ValueError()
    except (OSError, ValueError):
        raise DaemonIdentityError('exe_unverifiable') from None
    if (actual.st_dev, actual.st_ino) != (expected.st_dev, expected.st_ino):
        raise DaemonIdentityError('exe_mismatch')
    after = process_starttime(pid)
    if after != before:
        raise DaemonIdentityError('process_changed')
    return PeerIdentity(pid=pid, uid=uid, starttime=after,
                        scope='owned_pid_starttime' if owned is not None else 'uid_exe_only')
