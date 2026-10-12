"""Bounded read-only probe of an explicitly selected existing local endpoint.

Socket ownership + loaded ID are NOT original GUI provenance or permission to
start turns. Never start a daemon, guess a path, read history, or approve actions.
"""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import stat
import time

from . import __version__


class EndpointProbeError(Exception):
    pass


def _identity(path):
    p = Path(path)
    if not p.is_absolute() or len(os.fsencode(str(p))) > 103:
        raise EndpointProbeError('invalid_socket_path')
    # Reject symlink traversal, not merely a symlink at the final component.
    for parent in [p, *p.parents]:
        if parent.is_symlink():
            raise EndpointProbeError('symlink_path')
    try:
        meta = p.lstat()
        directory = p.parent.stat()
    except OSError:
        raise EndpointProbeError('socket_unavailable') from None
    if not stat.S_ISSOCK(meta.st_mode) or meta.st_uid != os.getuid() or meta.st_mode & 0o077:
        raise EndpointProbeError('socket_not_private_owned')
    if directory.st_uid != os.getuid() or directory.st_mode & 0o077:
        raise EndpointProbeError('socket_parent_not_private_owned')
    return meta.st_dev, meta.st_ino


def probe_endpoint(path, thread_id, *, timeout=5):
    if not isinstance(thread_id, str) or not 1 <= len(thread_id) <= 256:
        raise EndpointProbeError('explicit_thread_required')
    if not isinstance(timeout, (int, float)) or not 0 < timeout <= 15:
        raise EndpointProbeError('invalid_timeout')
    identity = _identity(path)
    deadline = time.monotonic() + timeout
    from websockets.sync.client import unix_connect

    def remaining():
        value = deadline - time.monotonic()
        if value <= 0:
            raise EndpointProbeError('probe_timeout')
        return value

    def request(ws, number, method, params):
        ws.send(json.dumps({'id': number, 'method': method, 'params': params}))
        for _ in range(32):
            packet = ws.recv(timeout=remaining())
            if not isinstance(packet, str):
                raise EndpointProbeError('invalid_protocol')
            msg = json.loads(packet)
            if not isinstance(msg, dict):
                raise EndpointProbeError('invalid_protocol')
            if 'id' in msg and 'method' in msg:
                # Server requests (approval/elicitation/etc) are never answered.
                raise EndpointProbeError('server_request_requires_owner')
            if type(msg.get('id')) is int and msg['id'] == number:
                if 'error' in msg or not isinstance(msg.get('result'), dict):
                    raise EndpointProbeError('request_rejected')
                return msg['result']
            # Notifications may contain unrelated chat text: discard, no log.
            if 'id' in msg or not isinstance(msg.get('method'), str):
                raise EndpointProbeError('unexpected_response')
        raise EndpointProbeError('notification_budget_exceeded')

    try:
        with unix_connect(path, uri='ws://localhost/', open_timeout=remaining(),
                          close_timeout=0.2, max_size=65536, max_queue=4,
                          compression=None) as ws:
            if _identity(path) != identity:
                raise EndpointProbeError('socket_replaced')
            request(ws, 1, 'initialize', {'clientInfo': {
                'name': 'dingwei-desktop-endpoint-probe', 'version': __version__}})
            ws.send(json.dumps({'method': 'initialized', 'params': {}}))
            result = request(ws, 2, 'thread/loaded/list', {})
            data = result.get('data')
            if (not isinstance(data, list) or len(data) > 4096 or
                    any(not isinstance(item, str) or not 1 <= len(item) <= 256 for item in data)
                    or result.get('nextCursor') is not None):
                raise EndpointProbeError('loaded_list_incomplete_or_invalid')
            if _identity(path) != identity:
                raise EndpointProbeError('socket_replaced')
            return {'status': 'READONLY_ENDPOINT_PROBE_PASS',
                    'selected_thread_loaded': thread_id in data,
                    'loaded_thread_count': len(data),
                    'original_desktop_binding_verified': False,
                    'native_auto_notify_implemented': False,
                    'history_read': False, 'turns_started': 0}
    except EndpointProbeError:
        raise
    except Exception:
        # Never include raw remote errors, frames, credentials or chat text.
        raise EndpointProbeError('transport_or_protocol_failure') from None


def main(argv=None):
    parser = argparse.ArgumentParser(description='只读检查明确指定的已有 Unix 端点，不启动会话')
    parser.add_argument('--socket', required=True)
    parser.add_argument('--thread-id', required=True)
    args = parser.parse_args(argv)
    try:
        result = probe_endpoint(args.socket, args.thread_id)
    except EndpointProbeError as exc:
        print(json.dumps({'status': 'ENDPOINT_PROBE_REFUSED', 'reason': str(exc)}))
        raise SystemExit(2) from None
    print(json.dumps(result, ensure_ascii=False, indent=2))
