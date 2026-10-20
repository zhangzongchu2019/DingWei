"""Current Hub read-only context. Never falls back to the legacy datad service."""
from __future__ import annotations
import json
import urllib.error
import urllib.parse
import urllib.request

SCHEMA = 'hub-project-context-v1'
MAX_BYTES = 256 << 10

class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def validate_args(args):
    if not isinstance(args, dict) or set(args)-{'scope','limit','cursor'}:
        raise ValueError('invalid context fields')
    if args.get('scope') not in ('tasks','sessions','config','all'):
        raise ValueError('invalid context scope')
    if type(args.get('limit',50)) is not int or not 1 <= args.get('limit',50) <= 100:
        raise ValueError('invalid context limit')
    if not isinstance(args.get('cursor',''),str) or len(args.get('cursor',''))>4096:
        raise ValueError('invalid context cursor')
    return dict(args)


def context_request(ws_url,headers,args):
    args=validate_args(args)
    u=urllib.parse.urlsplit(ws_url)
    if u.scheme not in ('ws','wss') or not u.netloc or u.username or u.password or u.fragment:
        raise ValueError('invalid configured Hub')
    base=urllib.parse.urlunsplit(('https' if u.scheme=='wss' else 'http',u.netloc,'','',''))
    req=urllib.request.Request(base+'/api/agent/console/project-context?'+urllib.parse.urlencode(args),method='GET',headers=headers)
    try:
        # One request, no ambient proxies, cookies, URL overrides or redirects.
        opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect())
        with opener.open(req,timeout=10) as response:
            if response.status!=200:raise ValueError('unexpected response')
            raw=response.read(MAX_BYTES+1)
            if len(raw)>MAX_BYTES:raise ValueError('context response too large')
            payload=json.loads(raw)
            meta=payload.get('meta') if isinstance(payload,dict) else None
            expected={'tasks','sessions','config'} if args['scope']=='all' else {args['scope']}
            if not isinstance(meta,dict) or meta.get('schema')!=SCHEMA or meta.get('source')!='current_hub' or meta.get('scope')!=args['scope']:
                raise ValueError('invalid context schema')
            if set(payload)!=expected|{'meta'} or type(meta.get('complete')) is not bool or not isinstance(meta.get('next_cursor'),str):
                raise ValueError('invalid context envelope')
            if meta['complete'] != (meta['next_cursor']==''):
                raise ValueError('inconsistent context pagination')
            for key in expected:
                page=payload[key].get('specs') if key=='config' and isinstance(payload[key],dict) else payload[key]
                if not isinstance(page,dict) or not isinstance(page.get('items'),list) or not isinstance(page.get('next_cursor'),str) or len(page['items'])>args.get('limit',50):
                    raise ValueError('invalid context page')
            return {'status':'ok','http_status':200,**payload}
    except urllib.error.HTTPError as exc:
        # Use known public error codes only; never reflect arbitrary server text.
        codes = {400:'invalid_context_request',403:'context_access_denied',409:'context_changed',413:'context_entry_too_large',503:'project_context_unavailable'}
        result = {'status':'error','http_status':exc.code,'error':'project_context_unsupported' if exc.code in (404,426) else codes.get(exc.code,'project_context_rejected'),'retryable':exc.code==503}
        if exc.code in (400,409):
            result['discard_cursor']=True
        return result
    except (OSError,ValueError):
        return {'status':'error','error':'project_context_read_failed','retryable':False}
