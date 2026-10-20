"""Explicit template invitations; no spec confirmation or business signoff."""
from __future__ import annotations
import json
import urllib.error
import urllib.parse
import urllib.request
from .task_flow import NoRedirect
from .task_blocker import ID

def validate_request(args):
    if not isinstance(args,dict) or set(args)-{'type','request_id','action','draft_id','body'}:raise ValueError('invalid template request')
    action=args.get('action');draft=args.get('draft_id','');body=args.get('body',{})
    if action not in ('list','get','responsibility') or not isinstance(body,dict):raise ValueError('invalid template operation')
    if not isinstance(draft,str) or len(draft)>256 or draft and not ID.fullmatch(draft):raise ValueError('invalid template identity')
    if action=='list':
        if draft or set(body)-{'after'} or not isinstance(body.get('after',''),str) or len(body.get('after',''))>256:raise ValueError('invalid template page')
    elif action=='get':
        if not draft or body:raise ValueError('invalid template query')
    else:
        required={'request_id','expected_version','plan_sha256','entry_ids','role','decision'}
        if not draft or set(body)-required-{'reason'} or not required<=set(body):raise ValueError('invalid responsibility fields')
        if not isinstance(body['request_id'],str) or not 1<=len(body['request_id'])<=128 or type(body['expected_version']) is not int or body['expected_version']<1:raise ValueError('invalid responsibility version')
        sha=body['plan_sha256']
        if not isinstance(sha,str) or len(sha)!=64 or any(c not in '0123456789abcdef' for c in sha):raise ValueError('invalid plan hash')
        ids=body['entry_ids']
        if not isinstance(ids,list) or not 1<=len(ids)<=100 or any(not isinstance(x,str) or not ID.fullmatch(x) or len(x)>128 for x in ids) or len(set(ids))!=len(ids):raise ValueError('invalid template entries')
        if body['role'] not in ('executor','reviewer') or body['decision'] not in ('accepted','withdrawn'):raise ValueError('human-only template role/operation')
        if not isinstance(body.get('reason',''),str) or len(body.get('reason','').encode())>2048 or body['decision']=='withdrawn' and not body.get('reason','').strip():raise ValueError('invalid receipt reason')
    return action,draft,dict(body)

def template_request(ws_url,headers,args):
    action,draft,body=validate_request(args)
    u=urllib.parse.urlsplit(ws_url)
    if u.scheme not in ('ws','wss') or not u.netloc or u.username or u.password or u.fragment:raise ValueError('invalid configured Hub')
    base=urllib.parse.urlunsplit(('https' if u.scheme=='wss' else 'http',u.netloc,'','',''))
    path='/api/agent/console/spec-templates'+('/'+urllib.parse.quote(draft,safe='') if draft else '')
    method='POST' if action=='responsibility' else 'GET'
    if method=='POST':data=json.dumps(dict(body,operation='responsibility')).encode()
    else:path+='?'+urllib.parse.urlencode(body);data=None
    req=urllib.request.Request(base+path,data=data,method=method,headers={**headers,'Content-Type':'application/json'})
    try:
        with urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect()).open(req,timeout=15) as response:
            raw=response.read((2<<20)+1)
            if len(raw)>2<<20:raise ValueError('template response too large')
            payload=json.loads(raw)
            if not isinstance(payload,dict) or payload.get('execution_authorized') is not False:raise ValueError('invalid template result')
            return {'status':'ok','http_status':response.status,**payload}
    except urllib.error.HTTPError as exc:
        unknown=method=='POST' and exc.code>=500
        return {'status':'error','http_status':exc.code,'error':'template_outcome_unknown' if unknown else 'template_unsupported_upgrade_hub' if exc.code in (404,426) else 'template_request_rejected','outcome':'unknown' if unknown else 'rejected','retryable':False}
    except (OSError,ValueError):
        return {'status':'error','error':'template_outcome_unknown' if method=='POST' else 'template_read_failed','outcome':'unknown' if method=='POST' else 'failed','retryable':False}
