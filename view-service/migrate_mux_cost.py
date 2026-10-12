"""Explicit offline cost migration. No HTTP route invokes this tool.
Only costs are read from the private legacy state. No credentials are output.
"""
import argparse, hashlib, hmac, http.client, json, math, re, secrets, sys, time
from pathlib import Path
from urllib.parse import urlparse
from view_service import CostProjection, _control_signature

MAX_INPUT=4<<20

def read_checked(path,sha):
    with open(path,'rb') as f:raw=f.read(MAX_INPUT+1)
    if len(raw)>MAX_INPUT or hashlib.sha256(raw).hexdigest()!=sha:
        raise ValueError('input hash/size mismatch')
    return json.loads(raw)

def validate_seed(legacy,mapping,authority,target,already_seeded=False,restart_proof=None):
    if mapping.get('version')!='cost-seed-v1' or mapping.get('target')!=target or authority.get('target')!=target:
        raise ValueError('migration scope mismatch')
    if restart_proof is not None:
        validate_restart_proof(restart_proof,mapping,target)
    registry={a['full_name']:a for a in authority['agents']}
    if len(registry)!=len(authority['agents']):raise ValueError('duplicate authority identity')
    seen_names=set();seen_agents=set();rows=[]
    for binding in mapping['bindings']:
        alias=binding['legacy_name'];name=binding['full_name'];actual=registry.get(name)
        keys=('agent_id','owner_lc','full_name') if already_seeded or restart_proof is not None else ('agent_id','owner_lc','full_name','epoch','H')
        if not actual or any(binding.get(k)!=actual.get(k) for k in keys):
            raise ValueError('identity/cut conflict')
        if restart_proof is not None and not already_seeded:
            if actual['epoch']==binding['epoch'] or actual['H']!='0':
                raise ValueError('restart not cold or epoch unchanged')
        if binding.get('coverage')!='all_completed_through_H':
            raise ValueError('unproven historical coverage')
        if alias in seen_names or actual['agent_id'] in seen_agents:
            raise ValueError('alias/merge conflict')
        seen_names.add(alias);seen_agents.add(actual['agent_id'])
        if alias not in legacy:raise ValueError('missing legacy account')
        old=legacy[alias]
        values={k:old.get(k,0) for k in ('cost_total','cost_last','turns')}
        for k in ('cost_total','cost_last'):
            n=values[k]
            if isinstance(n,bool) or not isinstance(n,(int,float)) or not math.isfinite(n) or n<0:
                raise ValueError('invalid historical cost')
        n=values['turns']
        if isinstance(n,bool) or not isinstance(n,int) or not 0<=n<2**63:
            raise ValueError('invalid historical turns')
        if values['cost_last']>values['cost_total']:raise ValueError('invalid historical delta')
        if not re.fullmatch('[0-9a-f]{1,32}',binding['epoch']) or not re.fullmatch('0|[1-9][0-9]{0,19}',binding['H']) or int(binding['H'])>2**64-1:
            raise ValueError('invalid authoritative cut')
        rows.append({**actual,**values,'epoch':binding['epoch'],'H':binding['H']})
    for alias,old in legacy.items():
        if isinstance(old,dict) and any(old.get(k,0)!=0 for k in ('cost_total','cost_last','turns')) and alias not in seen_names:
            raise ValueError('unmapped historical account')
    return rows

def validate_restart_proof(proof,mapping,target):
    # A hash-approved operator receipt, NOT an automatically inferred drain.
    # Tail observation alone cannot prove legacy costs; require distinct reviewed
    # reconciliation and publication-fence evidence. Never relabel old floors.
    if proof.get('version')!='cost-restart-v1' or proof.get('target')!=target:
        raise ValueError('restart proof scope mismatch')
    for key in ('cut_capture_sha256','drain_reconciliation_sha256',
                'publication_fence_sha256','old_hub_stop_sha256'):
        if not re.fullmatch('[0-9a-f]{64}',proof.get(key,'')):
            raise ValueError('missing reviewed phase evidence')
    if not proof.get('approval_ref') or any(proof.get(k) is not True for k in
            ('publishers_frozen','consumers_drained_stopped','old_hub_stopped',
             'no_completed_republication')):
        raise ValueError('restart phase not approved')
    expected=[{k:b[k] for k in ('agent_id','owner_lc','full_name','epoch','H')}
              for b in mapping['bindings']]
    if proof.get('old_cuts')!=expected:
        raise ValueError('immutable old cut mismatch')


def authority_request(base,target,key,names,cold_start=False):
    parsed=urlparse(base)
    if parsed.scheme!='http' or parsed.username or parsed.query or parsed.fragment or parsed.path not in ('','/'):
        raise ValueError('internal authority URL invalid')
    # Service key is target-scoped; the endpoint is private-network HTTP like
    # existing Hub/VS bridges. Operators must use that trusted network only.
    path='/internal/console/cost-seed-authority';body=json.dumps({'target':target,'full_names':names,'cold_start':cold_start},separators=(',',':')).encode()
    stamp=str(int(time.time()));nonce=secrets.token_hex(16)
    # Reverse Hub verifier uses the distinct service-to-Hub direction.
    signature=_control_signature(key,'POST',path,stamp,nonce,body,direction='view-service-to-hub/v1')
    conn=http.client.HTTPConnection(parsed.hostname,parsed.port or 80,timeout=1)
    try:
        conn.request('POST',path,body=body,headers={'X-DW-VS-Target':target,'X-DW-Timestamp':stamp,'X-DW-Nonce':nonce,'X-DW-Signature':signature,'Content-Type':'application/json'})
        response=conn.getresponse();data=response.read(MAX_INPUT+1)
        if response.status!=200 or len(data)>MAX_INPUT:raise ValueError('authority refused')
        return json.loads(data)
    finally:conn.close()

def main():
    parser=argparse.ArgumentParser()
    for name in ('legacy-state','legacy-sha256','mapping','mapping-sha256','projection','authority-url','target','service-key-file'):
        parser.add_argument('--'+name,required=True)
    parser.add_argument('--consumers-stopped',action='store_true',required=True)
    parser.add_argument('--restart-proof')
    parser.add_argument('--restart-proof-sha256')
    args=parser.parse_args()
    if bool(args.restart_proof)!=bool(args.restart_proof_sha256):
        raise ValueError('restart proof hash required')
    legacy=read_checked(args.legacy_state,args.legacy_sha256);mapping=read_checked(args.mapping,args.mapping_sha256)
    proof=read_checked(args.restart_proof,args.restart_proof_sha256) if args.restart_proof else None
    if proof is not None:
        if proof.get('legacy_sha256')!=args.legacy_sha256 or proof.get('mapping_sha256')!=args.mapping_sha256:
            raise ValueError('restart inputs mismatch')
        validate_restart_proof(proof,mapping,args.target)
    key=Path(args.service_key_file).read_bytes().strip()
    if len(key)<32:raise ValueError('service key unavailable')
    fingerprint=hashlib.sha256((args.legacy_sha256+':'+args.mapping_sha256+(':'+args.restart_proof_sha256 if proof is not None else '')).encode()).hexdigest()
    projection=CostProjection(args.projection)
    already_seeded=projection.has_seed(fingerprint)
    names=[b['full_name'] for b in mapping['bindings']]
    agents=[];authority_id=None
    for offset in range(0,len(names),8):
        part=authority_request(args.authority_url,args.target,key,names[offset:offset+8],cold_start=proof is not None and not already_seeded)
        if authority_id is not None and authority_id!=part.get('authority_id'):raise ValueError('multiple authority instances')
        authority_id=part['authority_id'];agents.extend(part['agents'])
    authority={'target':args.target,'agents':agents}
    rows=validate_seed(legacy,mapping,authority,args.target,already_seeded,restart_proof=proof)
    changed=projection.seed(rows,fingerprint)
    print(json.dumps({'migration':'cost-seed-v1','seeded':changed,'count':len(rows),'source_sha256':args.legacy_sha256,'mapping_sha256':args.mapping_sha256}))

if __name__=='__main__':
    try:main()
    except Exception:
        print('COST_SEED_REFUSED: inputs/authority/coverage/storage require controlled review',file=sys.stderr)
        sys.exit(1)
