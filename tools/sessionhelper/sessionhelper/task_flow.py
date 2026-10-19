"""Bounded task HTTP calls. Credentials remain in the owning helper."""
from __future__ import annotations
import json
import urllib.error
import urllib.parse
import urllib.request

class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def task_request(ws_url, headers, args):
    action = args.get('action')
    u = urllib.parse.urlsplit(ws_url)
    if u.scheme not in ('ws', 'wss') or not u.netloc or u.username or u.password:
        raise ValueError('invalid configured Hub')
    base = urllib.parse.urlunsplit(('https' if u.scheme == 'wss' else 'http', u.netloc, '', '', ''))
    body = dict(args.get('body') or {})
    if action == 'list':
        if set(body) - {'limit', 'cursor', 'spec_id', 'status'}:
            raise ValueError('invalid task list fields')
        limit = body.get('limit', 50)
        if type(limit) is not int or not 1 <= limit <= 100:
            raise ValueError('invalid task limit')
        path = '/api/agent/console/tasks?' + urllib.parse.urlencode(body)
        method, data = 'GET', None
    elif action == 'update':
        task_id = args.get('task_id')
        if not isinstance(task_id, str) or not 1 <= len(task_id) <= 256:
            raise ValueError('invalid task id')
        allowed = {'request_id', 'expected_version', 'operation', 'blocker', 'acceptance_id', 'case_name'}
        if set(body) - allowed or not isinstance(body.get('request_id'), str) or not 1 <= len(body['request_id']) <= 128:
            raise ValueError('invalid task request id')
        if type(body.get('expected_version')) is not int or body['expected_version'] < 1:
            raise ValueError('invalid task version')
        if body.get('operation') not in ('start','resume','block','complete','mark_case','confirm_case'):
            raise ValueError('invalid task operation')
        path = '/api/agent/console/tasks/' + urllib.parse.quote(task_id, safe='')
        method, data = 'PATCH', json.dumps(body).encode('utf-8')
        if len(data) > 16 << 10:
            raise ValueError('task body too large')
    else:
        raise ValueError('invalid task action')
    req = urllib.request.Request(base + path, data=data, method=method,
                                 headers={**headers, 'Content-Type': 'application/json'})
    try:
        # Never follow a redirect with the dedicated Agent credential.
        with urllib.request.build_opener(NoRedirect()).open(req, timeout=15) as response:
            raw = response.read((2 << 20) + 1)
            if len(raw) > 2 << 20:
                raise ValueError('task response too large')
            payload = json.loads(raw)
            if not isinstance(payload, dict):
                raise ValueError('invalid task response')
            return {'status':'ok', 'http_status':response.status, **payload}
    except urllib.error.HTTPError as exc:
        if action == 'update' and exc.code >= 500:
            return {'status':'error','http_status':exc.code,'error':'task_write_outcome_unknown',
                    'outcome':'unknown','retryable':False}
        return {'status':'error', 'http_status':exc.code,
                'error':'task_unsupported' if exc.code == 404 else 'task_request_rejected', 'retryable':False}
    except (OSError, ValueError):
        return {'status':'error', 'error':'task_write_outcome_unknown' if action=='update' else 'task_read_failed',
                'outcome':'unknown' if action=='update' else 'failed', 'retryable':False}
