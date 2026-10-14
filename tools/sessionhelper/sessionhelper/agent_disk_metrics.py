"""Helper-local bounded disk sampler. No View Service filesystem fallback.

Caller must supply the adapter's actual verified absolute launch directory.
No generic helper cwd or View Service disk fallback is permitted.
"""
from __future__ import annotations
import hashlib
import json
import os
import secrets
import stat
import time

SAFE_INT = 2**53 - 1
MAX_WIRE_BYTES = 2048
SCHEMA = "agent-disk/v1"


class DiskSampler:
    def __init__(self, *, instance=None, utc=time.time, monotonic=time.monotonic, syscalls=os):
        self.instance = instance or secrets.token_hex(16)
        if len(self.instance) != 32 or any(c not in '0123456789abcdef' for c in self.instance):
            raise ValueError('invalid_instance')
        self.utc, self.monotonic, self.os = utc, monotonic, syscalls
        self.sequence = 0

    def collect(self, verified_workdir: str | None):
        """Returns private timestamp + fixed-key record; no logs/raw path export.

        Syscalls may block in the kernel. The publisher holds one
        in-flight bounded worker, use a separate elapsed-time admission limit,
        and report unavailable if its result misses that limit. No hard kernel
        I/O cancellation is claimed by this synchronous collector.
        """
        self.sequence += 1
        if self.sequence > 2**64 - 1:
            raise ValueError('sequence_exhausted')
        begun = self.monotonic()
        record = {'schema': SCHEMA, 'status': 'unavailable', 'producer': 'helper',
                  'sample_instance': self.instance, 'sample_seq': str(self.sequence),
                  'sampled_at_ms': int(self.utc() * 1000),
                  'path_basis': 'cli_workdir_filesystem'}
        if not isinstance(verified_workdir, str) or not os.path.isabs(verified_workdir):
            record['reason'] = 'workdir_unbound'
            return begun, record
        fd = None
        try:
            fd = self.os.open(verified_workdir, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
            before = self.os.fstat(fd)
            if not stat.S_ISDIR(before.st_mode):
                raise ValueError('not_directory')
            fs = self.os.fstatvfs(fd)
            after = self.os.fstat(fd)
            named = self.os.stat(verified_workdir, follow_symlinks=False)
            identity = (before.st_dev, before.st_ino)
            if identity != (after.st_dev, after.st_ino) or identity != (named.st_dev, named.st_ino) or not stat.S_ISDIR(named.st_mode):
                raise ValueError('directory_changed')
            numbers = (fs.f_blocks, fs.f_bfree, fs.f_bavail, fs.f_frsize)
            if any(type(n) is not int or n < 0 for n in numbers) or fs.f_frsize == 0 or not (fs.f_bavail <= fs.f_bfree <= fs.f_blocks):
                raise ValueError('invalid_statvfs')
            total = fs.f_blocks * fs.f_frsize
            used = (fs.f_blocks - fs.f_bfree) * fs.f_frsize
            available = fs.f_bavail * fs.f_frsize
            if not 0 <= used <= total <= SAFE_INT or total == 0 or not 0 <= available <= total:
                raise ValueError('invalid_bytes')
            # Process-local opaque identity does not expose a path/mount inventory.
            opaque = hashlib.sha256((self.instance + ':' + str(identity)).encode()).hexdigest()[:32]
            record.update(status='ok', filesystem_instance=opaque,
                          used_bytes=used, total_bytes=total, available_bytes=available)
        except (OSError, ValueError, TypeError, OverflowError):
            record['reason'] = 'sample_failed'
        finally:
            if fd is not None:
                try:
                    self.os.close(fd)
                except OSError:
                    record = {k: v for k, v in record.items() if k not in ('used_bytes', 'total_bytes', 'available_bytes', 'filesystem_instance')}
                    record.update(status='unavailable', reason='sample_failed')
        return begun, record

    def wire(self, sample):
        begun, record = sample
        age_ms = int((self.monotonic() - begun) * 1000)
        if not 0 <= age_ms <= 5000:
            # No refreshed timestamp or fabricated fresh zero when sampling/send stalls.
            raise ValueError('sample_too_old')
        payload = dict(record, elapsed_send_age_ms=age_ms)
        raw = json.dumps(payload, ensure_ascii=False, separators=(',', ':')).encode()
        if len(raw) > MAX_WIRE_BYTES:
            raise ValueError('metric_size')
        return raw.decode()


def process_workdir(pid, alive, *, syscalls=os):
    """Bind the actual locally launched process cwd and mount namespace.

    No proc argv/environment or file contents are read, and no path leaves the
    helper. Remote/external drivers without an owned live process return None.
    """
    try:
        if type(pid) is not int or pid <= 0 or not alive():
            return None
        namespace = syscalls.readlink('/proc/self/ns/mnt')
        if syscalls.readlink(f'/proc/{pid}/ns/mnt') != namespace:
            return None
        workdir = syscalls.readlink(f'/proc/{pid}/cwd')
        if not os.path.isabs(workdir) or workdir.endswith(' (deleted)') or not alive():
            return None
        if syscalls.readlink(f'/proc/{pid}/ns/mnt') != namespace:
            return None
        return workdir
    except (OSError, ValueError, TypeError):
        return None


class BoundedDiskPublisher:
    """At most one daemon sampling thread across WS generations.

    Cancelling the awaiter cannot stop kernel I/O; an outstanding worker is
    never replaced or multiplied on reconnect. Late results are discarded.
    """
    def __init__(self, sampler=None):
        import threading
        self.sampler = sampler or DiskSampler()
        self.pending = None
        self.lock = threading.Lock()

    async def sample(self, adapter, timeout=2.0):
        import asyncio
        import concurrent.futures
        import threading
        with self.lock:
            if self.pending is not None and not self.pending.done():
                return None
            future = concurrent.futures.Future()
            self.pending = future
        def run():
            try:
                getter = getattr(adapter, 'agent_metrics_workdir', None)
                workdir = getter() if callable(getter) else None
                sample = self.sampler.collect(workdir)
                if workdir is not None and getter() != workdir:
                    started, record = sample
                    record = {k:v for k,v in record.items() if k not in ('used_bytes','total_bytes','available_bytes','filesystem_instance')}
                    record.update(status='unavailable',reason='namespace_unbound')
                    sample = started, record
                future.set_result(sample)
            except Exception:
                # Fixed failure only; never export raw exceptions or paths.
                future.set_result(None)
        try:
            threading.Thread(target=run, daemon=True, name='dw-agent-disk').start()
        except RuntimeError:
            future.set_result(None)
            return None
        try:
            sample = await asyncio.wait_for(asyncio.shield(asyncio.wrap_future(future)),timeout)
        except asyncio.TimeoutError:
            return None
        if sample is None:
            return None
        try:
            return self.sampler.wire(sample)
        except ValueError:
            return None
