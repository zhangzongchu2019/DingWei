"""Current-Hub question tools; no Feishu callback, PM identity or URL overrides."""
from __future__ import annotations
import json
import urllib.error
import urllib.parse
import urllib.request
from .task_flow import NoRedirect
from .task_blocker import ID

CREATE={'request_id','expected_version','kind','title','observed_fact','question','expected_answer','input_anchors','production_or_security','entry_refs'}
INPUT={'request_id','expected_version','event_id','body','evidence_ref'}
COLLECTION={'spec_id','task_id','limit','cursor'}
DETAIL={'before','change_before'}
def validate_request(args):
    if not isinstance(args,dict) or set(args)-{'type','request_id','action','task_id','question_id','body'}:raise ValueError('invalid question request')
    action=args.get('action');task=args.get('task_id','');qid=args.get('question_id','');body=args.get('body',{})
    if action not in ('list','create','input') or not isinstance(body,dict):raise ValueError('invalid question operation')
    for value in (task,qid):
        if not isinstance(value,str) or value and not ID.fullmatch(value):raise ValueError('invalid question identity')
    if action=='list':
        allowed=DETAIL if qid else COLLECTION
        if set(body)-allowed or task:raise ValueError('invalid question query')
        for k,v in body.items():
            if k in ('limit','before','change_before'):
                if type(v) is not int or v<1 or k=='limit' and v>100:raise ValueError('invalid question limit')
            elif not isinstance(v,str) or len(v)>4096 or (k in ('spec_id','task_id') and not ID.fullmatch(v)):raise ValueError('invalid question selector')
    else:
        allowed=CREATE if action=='create' else INPUT
        required={'request_id','expected_version','kind','title','observed_fact','question','expected_answer','entry_refs'} if action=='create' else INPUT
        if set(body)-allowed or not required<=set(body):raise ValueError('invalid question fields')
        if not isinstance(body['request_id'],str) or not 1<=len(body['request_id'])<=128 or type(body['expected_version']) is not int or body['expected_version']<1:raise ValueError('invalid question request/version')
        if action=='create':
            if not task or qid or body['kind'] not in ('requirement_unclear','change','technical','resource'):raise ValueError('invalid question creation')
            if 'entry_refs' in body:
                refs=body['entry_refs']
                if not isinstance(refs,list) or not 1<=len(refs)<=32 or any(not isinstance(v,str) or not ID.fullmatch(v) for v in refs) or len(set(refs))!=len(refs):raise ValueError('invalid question entry references')
            if 'production_or_security' in body and type(body['production_or_security']) is not bool:raise ValueError('invalid question risk')
            for k,n in [('title',512),('observed_fact',4096),('question',4096),('expected_answer',4096)]:
                if not isinstance(body[k],str) or not body[k].strip() or len(body[k].encode())>n:raise ValueError('invalid question text')
            if not isinstance(body.get('input_anchors',''),str) or len(body.get('input_anchors','').encode())>4096:raise ValueError('invalid evidence references')
        else:
            if task or not qid or not isinstance(body['event_id'],str) or not 1<=len(body['event_id'])<=128:raise ValueError('invalid question input identity')
            for k,n in [('body',8192),('evidence_ref',2048)]:
                if not isinstance(body[k],str) or not body[k].strip() or len(body[k].encode())>n:raise ValueError('invalid technical material')
    return action,task,qid,dict(body)

def question_request(ws_url,headers,args):
    action,task,qid,body=validate_request(args)
    u=urllib.parse.urlsplit(ws_url)
    if u.scheme not in ('ws','wss') or not u.netloc or u.username or u.password or u.fragment:raise ValueError('invalid configured Hub')
    base=urllib.parse.urlunsplit(('https' if u.scheme=='wss' else 'http',u.netloc,'','',''))
    method='GET' if action=='list' else 'POST'
    if action=='create':path='/api/agent/console/tasks/'+urllib.parse.quote(task,safe='')+'/questions'
    else:
        path='/api/agent/console/questions'+('/'+urllib.parse.quote(qid,safe='') if qid else '')
        if action=='input':path+='/inputs'
    if method=='GET':path+='?'+urllib.parse.urlencode(body);data=None
    else:
        data=json.dumps(body).encode()
        if len(data)>32<<10:raise ValueError('question body too large')
    req=urllib.request.Request(base+path,data=data,method=method,headers={**headers,'Content-Type':'application/json'})
    try:
        with urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect()).open(req,timeout=15) as response:
            raw=response.read((2<<20)+1)
            if len(raw)>2<<20:raise ValueError('question response too large')
            payload=json.loads(raw)
            if not isinstance(payload,dict) or payload.get('execution_authorized') is not False:raise ValueError('invalid question result')
            return {'status':'ok','http_status':response.status,**payload}
    except urllib.error.HTTPError as exc:
        unknown=method!='GET' and exc.code>=500
        return {'status':'error','http_status':exc.code,'error':'question_outcome_unknown' if unknown else 'clarification_requires_anchored_tools_upgrade_helper' if exc.code==426 else 'question_unsupported_upgrade_hub' if exc.code==404 else 'question_request_rejected','outcome':'unknown' if unknown else 'rejected','retryable':False}
    except (OSError,ValueError):
        return {'status':'error','error':'question_outcome_unknown' if method!='GET' else 'question_read_failed','outcome':'unknown' if method!='GET' else 'failed','retryable':False}
