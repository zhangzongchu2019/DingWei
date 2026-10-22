"""Trusted Hub source-release activation, using the immutable external launcher."""
from __future__ import annotations

from datetime import datetime, timezone
from contextlib import contextmanager
import hashlib
import fcntl
import importlib.metadata
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import stat
import tempfile
import time
import uuid
import urllib.error
import urllib.request
from urllib.parse import urlsplit

from . import __version__
from .config import env_get

FIELDS = ('job_id', 'release_id', 'agent_id', 'owner_id', 'version', 'sha256')
MAX_BYTES = 32 * 1024 * 1024
HEALTH_RETRY_SECONDS = 5.0


class UpdateRejected(RuntimeError):
    pass


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise UpdateRejected('redirect_denied')


def load_stable(path, name):
    if path.is_symlink() or not path.is_file():
        raise UpdateRejected('manual_required')
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def origin(url, allow_loopback=False, hub=False):
    try:
        parsed = urlsplit(url)
        scheme = {'wss':'https', 'ws':'http'}.get(parsed.scheme, parsed.scheme) if hub else parsed.scheme
        host = (parsed.hostname or '').lower()
        if (not host or parsed.username is not None or parsed.password is not None or parsed.fragment or parsed.query or
                (scheme != 'https' and not (allow_loopback and scheme == 'http' and host in ('localhost', '127.0.0.1', '::1')))):
            raise ValueError()
        return scheme, host, parsed.port or (443 if scheme == 'https' else 80)
    except (ValueError, TypeError):
        raise UpdateRejected('origin_invalid') from None


class SelfUpdater:
    def __init__(self, cfg, source_validator, agent_name, *, launcher=None, allow_loopback=False):
        # Enrollment preserves display case; Hub session/offer names are canonical
        # ASCII lower-case for the same credential-bound Agent identity.
        self.cfg, self.validate_source, self.agent_name = cfg, source_validator, agent_name.lower()
        self.allow_loopback = allow_loopback
        self.health_nonce = None
        self.health_lock_sha256 = None
        configured = launcher or env_get('SH_UPDATE_LAUNCHER', '')
        if not configured or not Path(configured).is_absolute():
            raise UpdateRejected('manual_required')
        path = Path(configured)
        self.mount = path.parent.resolve()
        if path.resolve() != path:
            raise UpdateRejected('manual_required')
        self.guard = load_stable(path, '_dw_stable_guard')
        self.package = load_stable(self.mount / 'helper_update_package.py', '_dw_stable_package')
        state_root = Path(os.path.expanduser(cfg.state_dir or '~/.dingwei')) / 'update'
        bound = env_get('SH_UPDATE_ROOT', '')
        if bound and (not Path(bound).is_absolute() or Path(bound).resolve() != state_root.resolve()):
            raise UpdateRejected('manual_required')
        self.state = self.guard.State(state_root, self.mount / 'factory')

    def runtime_lock_sha256(self):
        # The stable mounted runtime lock is shared by every accepted slot.
        # lock_changed is refused before activation; never read candidate state.
        lock = self.mount / 'requirements-runtime.lock'
        if lock.is_symlink() or not lock.is_file():
            raise UpdateRejected('lock_changed')
        try:
            return hashlib.sha256(lock.read_bytes()).hexdigest()
        except OSError:
            raise UpdateRejected('lock_changed') from None

    def pending_runtime_lock_sha256(self, pending):
        digest = self.runtime_lock_sha256()
        try:
            marker = self.state.slot(pending) / '.helper-update-binding.json'
            fd = os.open(marker, os.O_RDONLY | os.O_NOFOLLOW)
            with os.fdopen(fd, 'rb') as stream:
                raw = stream.read(4097)
            if len(raw) > 4096:
                raise ValueError()
            binding = json.loads(raw)
            if (not isinstance(binding, dict) or
                    any(binding.get(k) != pending.get(k) for k in FIELDS) or
                    binding.get('lock_sha256') != digest):
                raise ValueError()
        except (OSError, ValueError, TypeError, self.guard.UpdateError):
            raise UpdateRejected('lock_changed') from None
        return digest

    def validate_offer(self, env):
        self.validate_source(env)  # existing local-Hub system-envelope check, before URL/journal work
        meta = env.get('meta') or {}
        if meta.get('action') != 'update_self' or meta.get('agent_name') != self.agent_name:
            raise UpdateRejected('target_mismatch')
        for key in (*FIELDS, 'lock_sha256', 'url', 'expires_at', 'platform'):
            if not isinstance(meta.get(key), str) or not meta[key] or len(meta[key]) > 2048:
                raise UpdateRejected('offer_invalid')
        for key in ('sha256', 'lock_sha256'):
            if not re.fullmatch('[0-9a-f]{64}', meta[key]):
                raise UpdateRejected('offer_invalid')
        if meta['platform'] != 'linux' or not os.sys.platform.startswith('linux'):
            raise UpdateRejected('platform_unsupported')
        try:
            expires = datetime.fromisoformat(meta['expires_at'].replace('Z', '+00:00'))
            if expires.tzinfo is None:
                raise ValueError()
            seconds = expires.timestamp() - time.time()
        except (ValueError, TypeError, OverflowError):
            raise UpdateRejected('offer_invalid') from None
        if not 0 < seconds <= 1805:
            raise UpdateRejected('offer_expired')
        allowed = {origin(self.cfg.ws_base, self.allow_loopback, hub=True)}
        # This field is trusted only because source validation preceded it.
        if meta.get('file_service_origin'):
            configured = urlsplit(meta['file_service_origin'])
            if configured.path not in ('', '/'):
                raise UpdateRejected('origin_invalid')
            allowed.add(origin(meta['file_service_origin'], self.allow_loopback))
        if origin(meta['url'], self.allow_loopback) not in allowed:
            raise UpdateRejected('origin_denied')
        lock = self.mount / 'requirements-runtime.lock'
        if self.runtime_lock_sha256() != meta['lock_sha256']:
            raise UpdateRejected('lock_changed')
        pins = re.findall(r'^([A-Za-z0-9_-]+)==([^\s\\]+)', lock.read_text(), re.M)
        try:
            if not pins or any(importlib.metadata.version(k) != v for k, v in pins):
                raise UpdateRejected('lock_changed')
        except importlib.metadata.PackageNotFoundError:
            raise UpdateRejected('lock_changed') from None
        return dict(meta)

    def download(self, meta, destination):
        # No ambient proxy, auth handler, cookie jar, Agent headers or redirect forwarding.
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
        request = urllib.request.Request(meta['url'], headers={'User-Agent':'DingWei-helper-update/1'})
        count, deadline = 0, time.monotonic() + 60
        hasher = hashlib.sha256()
        try:
            with opener.open(request, timeout=15) as response, Path(destination).open('xb') as out:
                if response.status != 200:
                    raise UpdateRejected('download_failed')
                while True:
                    chunk = response.read(65536)
                    if not chunk:
                        break
                    count += len(chunk)
                    if count > MAX_BYTES or time.monotonic() > deadline:
                        raise UpdateRejected('download_limit')
                    hasher.update(chunk); out.write(chunk)
                out.flush(); os.fsync(out.fileno())
        except UpdateRejected:
            raise
        except (OSError, urllib.error.URLError):
            raise UpdateRejected('download_failed') from None
        if hasher.hexdigest() != meta['sha256']:
            raise UpdateRejected('sha256_mismatch')

    def prepare(self, env):
        # Serialize concurrent deliveries across helper processes without blocking guard state CAS.
        fd = os.open(self.state.root / 'prepare.lock', os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
        try:
            fcntl.flock(fd, fcntl.LOCK_EX)
            return self._prepare(env)
        finally:
            os.close(fd)

    @contextmanager
    def retention_transaction(self):
        if getattr(self.guard, 'RETENTION_API', None) != 1:
            retained = self.state.root / 'retain'
            if os.path.lexists(retained):
                directory = None
                try:
                    directory = os.open(retained, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
                    info = os.fstat(directory)
                    if info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode) != 0o700:
                        raise UpdateRejected('manual_required')
                    if os.listdir(directory):
                        raise UpdateRejected('manual_required')
                except OSError:
                    raise UpdateRejected('manual_required') from None
                finally:
                    if directory is not None: os.close(directory)
            yield None, None
            return
        retention = self.guard.Retention(self.state.root)
        try:
            with retention.transaction(if_present=True) as fd:
                yield retention, fd
        except self.guard.UpdateError as exc:
            raise UpdateRejected(str(exc)) from None

    def _prepare(self, env):
        meta = self.validate_offer(env)
        data = self.state.read()
        if meta['job_id'] in data['terminal']:
            known = data['terminal'][meta['job_id']]
            if not all(known.get(k) == meta[k] for k in FIELDS):
                raise UpdateRejected('offer_mismatch')
            meta['from_version'] = known.get('from_version') or __version__
            return meta, known['stage'], False
        if data['pending']:
            if all(data['pending'].get(k) == meta[k] for k in FIELDS):
                meta['from_version'] = data['pending'].get('from_version') or __version__
                needs_restart = not data['pending'].get('restart_requested', False) and data['pending']['starts'] == 0
                return meta, 'installed', needs_restart
            raise UpdateRejected('update_busy')
        meta['from_version'] = __version__
        if not re.fullmatch(r'\d+(?:\.\d+){2,3}', meta['version']):
            raise UpdateRejected('version_invalid')
        def numbers(v):
            return tuple((list(map(int, v.split('.'))) + [0] * 4)[:4])
        if numbers(meta['version']) <= numbers(__version__):
            raise UpdateRejected('version_not_new')
        # Download stays outside retain/state locks; prepare.lock still serializes offers.
        work = Path(tempfile.mkdtemp(prefix='.incoming-', dir=self.state.root))
        try:
            artifact = work / 'source.tgz'
            self.download(meta, artifact)
            try:
                manifest, files = self.package.verify(artifact, meta['sha256'])
            except (ValueError, OSError):
                raise UpdateRejected('package_invalid') from None
            if manifest['version'] != meta['version'] or manifest['platform'] != meta['platform']:
                raise UpdateRejected('release_mismatch')
            if manifest['lock_sha256'] != meta['lock_sha256']:
                raise UpdateRejected('lock_changed')
            slot = work / 'payload'; slot.mkdir(mode=0o700)
            for name, content in files.items():
                if name.startswith('factory/'):
                    target = slot / name[len('factory/'):]
                    target.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
                    with target.open('xb') as out:
                        out.write(content); out.flush(); os.fsync(out.fileno())
            # Preserve the already-verified job/lock binding beside candidate
            # code; the stable guard's six-field journal stays unchanged.
            with (slot / '.helper-update-binding.json').open('xb') as out:
                out.write(json.dumps({k: meta[k] for k in (*FIELDS, 'lock_sha256')}, sort_keys=True).encode())
                out.flush(); os.fsync(out.fileno())
            for directory in sorted((p for p in slot.rglob('*') if p.is_dir()), reverse=True):
                fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY)
                try: os.fsync(fd)
                finally: os.close(fd)
            with self.retention_transaction() as (retention, retain_fd):
                with self.state.locked():
                    data = self.state.read()
                    if data['pending']:
                        raise UpdateRejected('update_busy')
                    if retention is not None:
                        retention.check_locked(retain_fd, slot)
                    slots = self.state.root / 'slots'; slots.mkdir(mode=0o700, exist_ok=True)
                    if slots.is_symlink() or slots.resolve() != slots:
                        raise UpdateRejected('state_invalid')
                    destination = slots / meta['sha256']
                    if destination.exists():
                        # Recover a crash after rename but before arming, never remove confirmed code.
                        if retention is not None:
                            retention.check_locked(retain_fd, replacing=destination)
                        retained = [data.get('current'), data.get('previous')]
                        if destination.is_symlink() or any(v and v['sha256'] == meta['sha256'] for v in retained):
                            raise UpdateRejected('slot_exists')
                        shutil.rmtree(destination)
                    os.replace(slot, destination)
                    directory = os.open(slots, os.O_RDONLY | os.O_DIRECTORY)
                    try:
                        os.fsync(directory)
                    finally:
                        os.close(directory)
        finally:
            shutil.rmtree(work, ignore_errors=True)
        # Download may outlast a nearly expired offer. Recheck all gates before arming.
        self.validate_offer(env)
        # Arm after full verified slot creation; guard's transaction rechecks pending/terminal.
        try:
            self.state.arm(meta)
        except self.guard.UpdateError as exc:
            raise UpdateRejected(str(exc)) from None
        return meta, 'installed', True

    def mark_restart_requested(self, meta):
        with self.state.locked():
            data = self.state.read()
            pending = data['pending']
            if not pending or not all(pending.get(k) == meta[k] for k in FIELDS):
                raise UpdateRejected('offer_mismatch')
            pending['restart_requested'] = True
            self.guard.atomic_json(self.state.path, data)

    def health(self):
        pending = self.state.read()['pending']
        if not pending or pending['version'] != __version__:
            return None  # manual factory bootstrap has no update health transaction
        self.health_lock_sha256 = self.pending_runtime_lock_sha256(pending)
        self.health_nonce = uuid.uuid4().hex
        return {'type':'helper_update_health', 'health_nonce':self.health_nonce, 'lock_sha256':self.health_lock_sha256, **{k: pending[k] for k in FIELDS},
                'action':'update_self', 'stage':'restarted', 'helper_version':__version__,
                'from_version':pending.get('from_version') or __version__}

    def confirm(self, env):
        self.validate_source(env)
        meta = env.get('meta') or {}
        if meta.get('type') != 'helper_update_confirm':
            raise UpdateRejected('confirmation_invalid')
        if not self.health_nonce or meta.get('health_nonce') != self.health_nonce or meta.get('version') != __version__:
            raise UpdateRejected('confirmation_mismatch')
        if (not self.health_lock_sha256 or meta.get('lock_sha256') != self.health_lock_sha256 or
                self.pending_runtime_lock_sha256(self.state.read()['pending'] or {}) != self.health_lock_sha256):
            raise UpdateRejected('confirmation_mismatch')
        self.state.confirm(meta)
        self.health_nonce = None
        self.health_lock_sha256 = None
        return {**meta, 'from_version':self.state.read()['last_result'].get('from_version') or __version__}


def receipt(meta, stage, reason=''):
    return {'type':'provision_ack', 'action':'update_self', 'target':'self',
            'lock_sha256':meta.get('lock_sha256', ''),
            **{k: meta.get(k, '') for k in FIELDS}, 'from_version':meta.get('from_version') or __version__,
            'to_version':meta.get('version', ''), 'stage':stage, 'reason':reason,
            'ok':stage not in ('failed','rolled_back'), 'message':reason,
            'system':True, 'no_mirror':True}


def is_confirmation(env):
    return isinstance(env.get('meta'), dict) and env['meta'].get('type') == 'helper_update_confirm'


async def status_loop(helper, ws):
    """Only update-aware authenticated Hubs receive new control-plane messages."""
    import asyncio
    from .protocol import envelope, session_addr
    response = getattr(ws, 'response', None)
    headers = getattr(response, 'headers', {})
    if headers.get('X-DW-Helper-Update') != 'helper-update/v1' or not getattr(helper, 'enroll_credential', None):
        await asyncio.Future()
    try:
        updater = helper._get_self_updater()
    except UpdateRejected:
        await asyncio.Future()
    while True:
        pending = updater.state.read()['pending']
        if not pending:
            result = updater.state.read().get('last_result')
            if result and result.get('stage') == 'rolled_back':
                meta = receipt({**result, 'lock_sha256':updater.runtime_lock_sha256()},
                               'rolled_back', result.get('reason', 'candidate_failed'))
                await ws.send(json.dumps(envelope(session_addr('workpulse', helper.cfg.key_id), '', helper.book.self_addr, meta)))
            await asyncio.Future()
        ready = not helper.cfg.mcp_ipc_sock or helper.ipc_bound.is_set()
        if helper.cfg.mode == 'driver':
            ready = ready and bool(getattr(helper.adapter, '_started', False))
        elif helper.cfg.mode == 'cli' and hasattr(helper.adapter, 'ready'):
            ready = ready and helper.adapter.ready.is_set()
        if ready:
            health = updater.health()
            if health:
                await ws.send(json.dumps(envelope(session_addr('workpulse', helper.cfg.key_id), '', helper.book.self_addr, health)))
            # A lost confirmation must not strand a healthy pending trial.
            # Each retry supersedes its nonce; reconnect creates a new status loop.
            await asyncio.sleep(HEALTH_RETRY_SECONDS)
            continue
        await asyncio.sleep(0.1)
