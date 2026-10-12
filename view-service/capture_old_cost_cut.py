"""Read-only pre-upgrade cut observer for the OLD Hub's existing internal SSE.
No new authority route, no VS cost application, no payload output. The result
proves a frozen replay tail only; it deliberately does NOT assert cost coverage.
"""
import argparse, hashlib, http.client, json, os, re, sys, time
from urllib.parse import urlparse, quote

MAX_LINE=16<<20
MAX_BYTES=256<<20

def read_cut(response,full_name):
    epoch=None;last=None;first=None;count=0;total=0;pending=None;digest=hashlib.sha256()
    deadline=time.monotonic()+35
    while time.monotonic()<deadline:
        line=response.readline(MAX_LINE+1)
        total+=len(line)
        if not line or len(line)>MAX_LINE or total>MAX_BYTES:
            raise ValueError('cut EOF/size limit')
        if line.startswith(b'event:'):
            raise ValueError('control/gap in old stream')
        if line.startswith(b'id: '):
            m=re.fullmatch(rb'([0-9a-f]{1,32})-(0|[1-9][0-9]{0,19})\r?\n',line[4:])
            if not m or int(m[2])>2**64-1 or pending is not None:
                raise ValueError('invalid old cursor')
            ep=m[1].decode();seq=int(m[2])
            if (epoch is not None and epoch!=ep) or (last is not None and seq!=last+1):
                raise ValueError('non-contiguous frozen suffix')
            epoch=ep;pending=seq
        if line.startswith(b'data: '):
            if pending is None:raise ValueError('unsequenced old data')
            # Hash exact wire payload privately; never interpret/apply costs.
            digest.update(line);last=pending;pending=None;count+=1
            if first is None:first=last
        if line.strip()==b': ping':
            if last is None or pending is not None:
                raise ValueError('empty old ring cannot prove old epoch/H')
            return dict(full_name=full_name,epoch=epoch,H=str(last),floor=str(first),
                        replay_count=count,replay_sha256=digest.hexdigest(),
                        coverage='TAIL_ONLY_NOT_COST_COVERAGE')
    raise ValueError('old cut deadline')

def capture(base,name):
    p=urlparse(base)
    if p.scheme!='http' or p.username or p.query or p.fragment or p.path not in ('','/'):
        raise ValueError('trusted internal origin required')
    c=http.client.HTTPConnection(p.hostname,p.port or 80,timeout=35)
    try:
        c.request('GET','/internal/view/'+quote(name,safe='')+'/events')
        r=c.getresponse()
        if r.status!=200 or 'text/event-stream' not in r.getheader('Content-Type',''):
            raise ValueError('old SSE unavailable')
        return read_cut(r,name)
    finally:c.close()

def main():
    p=argparse.ArgumentParser()
    p.add_argument('--old-hub-url',required=True);p.add_argument('--full-name',required=True)
    p.add_argument('--output',required=True)
    p.add_argument('--publishers-frozen',action='store_true',required=True)
    a=p.parse_args();cut=capture(a.old_hub_url,a.full_name)
    # Operator-owned private file; refusing replacement protects an old cut.
    fd=os.open(a.output,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
    with os.fdopen(fd,'w') as f:json.dump(cut,f,sort_keys=True);f.write('\n');f.flush();os.fsync(f.fileno())
    print('OLD_CUT_CAPTURED: tail only; separate cost drain reconciliation required')

if __name__=='__main__':
    try:main()
    except Exception:
        print('OLD_CUT_REFUSED: frozen old SSE/evidence unavailable',file=sys.stderr);sys.exit(1)
