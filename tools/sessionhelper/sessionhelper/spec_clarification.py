"""Read anchored discussion or append non-authoritative technical material only.

No human authority, provider identity, URL override or automatic write retry.
An uncertain write is looked up by its original request ID before any retry.
"""
from __future__ import annotations
import json
import urllib.error
import urllib.parse
import urllib.request
from .task_flow import NoRedirect
from .task_blocker import ID


def validate_request(args):
    if not isinstance(args, dict) or set(args)-{'type','action','question_id','request_id','body'}:
        raise ValueError('invalid clarification request')
    action=args.get('action'); qid=args.get('question_id'); body=args.get('body',{})
    if action not in ('list','technical','status') or not isinstance(qid,str) or not ID.fullmatch(qid) or not isinstance(body,dict):
        raise ValueError('invalid clarification identity/action')
    if action=='list':
        if set(body)-{'before','limit'}: raise ValueError('invalid clarification query')
        for k,v in body.items():
            if type(v) is not int or v<1 or k=='limit' and v>100: raise ValueError('invalid clarification pagination')
    elif action=='status':
        if set(body)!={'request_id'} or not isinstance(body['request_id'],str) or not ID.fullmatch(body['request_id']) or len(body['request_id'])>128:
            raise ValueError('invalid clarification status identity')
    else:
        if set(body)-{'request_id','body','evidence_ref','entry_refs'} or not {'request_id','body','entry_refs'}<=set(body):
            raise ValueError('technical material requires typed entry references')
        for k,n in [('request_id',128),('body',8192),('evidence_ref',2048)]:
            v=body.get(k,'')
            if not isinstance(v,str) or len(v.encode())>n or k!='evidence_ref' and not v.strip(): raise ValueError('invalid clarification text')
        refs=body['entry_refs']
        if not isinstance(refs,list) or not 1<=len(refs)<=32 or any(not isinstance(v,str) or not ID.fullmatch(v) or len(v)>256 for v in refs) or len(set(refs))!=len(refs):
            raise ValueError('invalid clarification references')
    return action,qid,dict(body)


def clarification_request(ws_url,headers,args):
    action,qid,body=validate_request(args)
    u=urllib.parse.urlsplit(ws_url)
    if u.scheme not in ('ws','wss') or not u.netloc or u.username or u.password or u.fragment: raise ValueError('invalid configured Hub')
    base=urllib.parse.urlunsplit(('https' if u.scheme=='wss' else 'http',u.netloc,'','',''))
    path='/api/agent/console/questions/'+urllib.parse.quote(qid,safe='')+'/clarifications'
    method='POST' if action=='technical' else 'GET'
    if action=='status':path+='/requests/'+urllib.parse.quote(body['request_id'],safe='')
    elif action=='list':path+='?'+urllib.parse.urlencode(body)
    data=None
    if method=='POST':
        data=json.dumps(dict(body,kind='technical')).encode()
        if len(data)>32<<10:raise ValueError('clarification body too large')
    req=urllib.request.Request(base+path,data=data,method=method,headers={**headers,'Content-Type':'application/json'})
    try:
        with urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect()).open(req,timeout=15) as response:
            raw=response.read((2<<20)+1)
            if len(raw)>2<<20:raise ValueError('clarification response too large')
            payload=json.loads(raw)
            if not isinstance(payload,dict) or payload.get('execution_authorized') is not False:raise ValueError('invalid clarification response')
            return {**payload,'status':'ok','http_status':response.status,'data_only':True}
    except urllib.error.HTTPError as exc:
        unknown=method=='POST' and exc.code>=500
        return {'status':'error','http_status':exc.code,'error':'clarification_outcome_unknown' if unknown else 'clarification_request_not_recorded' if action=='status' and exc.code==404 else 'clarification_unsupported_upgrade_hub' if exc.code in (404,426) else 'clarification_rejected','outcome':'unknown' if unknown else 'rejected','retryable':False,'reuse_request_id':method=='POST','execution_authorized':False}
    except (OSError,ValueError):
        return {'status':'error','error':'clarification_outcome_unknown' if method=='POST' else 'clarification_read_failed','outcome':'unknown' if method=='POST' else 'failed','retryable':False,'reuse_request_id':method=='POST','execution_authorized':False}
