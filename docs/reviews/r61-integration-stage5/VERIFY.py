#!/usr/bin/env python3
from pathlib import Path
import subprocess,json,hashlib,gzip,datetime
R=Path(__file__).resolve().parent;W=R.parents[2];E=R/'evidence'
def git(*args):return subprocess.check_output(['git',*args],cwd=W,text=True).strip()
for line in (R/'SHA256SUMS').read_text().splitlines():
 h,n=line.split('  ',1);assert hashlib.sha256((W/n).read_bytes()).hexdigest()==h,n
raw=json.loads((E/'raw-manifest.json').read_text())
for x in raw:
 p=R/x['archived'];b=gzip.decompress(p.read_bytes())if p.suffix=='.gz'else p.read_bytes();assert hashlib.sha256(b).hexdigest()==x['raw_sha256'],p
m=json.loads((E/'product-manifest.json').read_text());V=m['source']
for f,x in m['products'].items():
 if x.get('deleted'):assert not (W/f).exists();continue
 assert hashlib.sha256(subprocess.check_output(['git','show',V+':'+f],cwd=W)).hexdigest()==x['sha256'],f
 assert hashlib.sha256((W/f).read_bytes()).hexdigest()==x['sha256'],f
 assert git('rev-parse',V+':'+f)==x['git_object']
assert not [f for f in git('diff','--name-only',V,'HEAD').splitlines()if not f.startswith(('docs/','openspec/'))]
s=json.loads((E/'source-preservation.json').read_text());assert all(s['critical_ancestors'].values())and s['renderer_retainCanonicalTrustedProvenance'];assert git('show','-s','--format=%P',s['merge_source']).split()==s['merge_parents']
for ref in s['critical_ancestors']:subprocess.run(['git','merge-base','--is-ancestor',ref,V],cwd=W,check=True)
f=json.loads((E/'serial/full-acceptance.json').read_text());assert f['passed']and f['all_serial']and f['source']==s['source'];prev=None
for x in f['gates']:
 assert x['rc']==0 and x['source']==s['source'];a=datetime.datetime.fromisoformat(x['start_utc']);b=datetime.datetime.fromisoformat(x['end_utc']);assert b>=a
 if prev:assert a>=prev
 prev=b
v=json.loads((E/'postversion/version-source.json').read_text());assert v['source']==V and v['parent_full_gate_source']==s['source'];assert git('rev-parse',V+'^')==s['source'];assert sorted(git('diff','--name-only',V+'^',V).splitlines())==sorted(['VERSION','internal/version/version.go','tools/sessionhelper/sessionhelper/__init__.py'])
a=json.loads((E/'postversion/post-acceptance.json').read_text());assert a['passed']and a['all_serial']and a['source']==V
knife=json.loads((E/'serial/http-four-mutation/summary.json').read_text());assert knife['physical_knives']==1 and knife['business_red']and knife['build']['rc']==0 and knife['mutated']['rc']!=0 and knife['restored']['rc']==0 and knife['porcelain']==''
p=json.loads((E/'parsed-final-gates.json').read_text());assert all(not x['top_fail'] and not x['all_fail'] and not x['data_race']for x in p.values());assert all(not x['interrupted_lines']for k,x in p.items()if k.endswith(('m8-store-full3','m8-version-full1')))
receipt=(E/'serial/browser-receipt.txt').read_text();lines=[x for x in receipt.splitlines()if x.startswith('suite.')and'.rc='in x];assert len(lines)==48 and all(x.endswith('=0')for x in lines)and'aggregate_rc=0'in receipt
for n in ['r15_session_history_pagination','r15_session_history_mutations','r15_follow_release_after_replay']:assert 'suite.'+n+'.rc=0'in receipt
c=json.loads((E/'serial/compat-results.json').read_text());assert len(c)==3 and all(x['rc']==0 and x['actual_handshake']and x['actual_recv_accept']for x in c)
print('VERIFY_OK source',V,'products',len(m['products']),'raw_files',len(raw),'serial_gates',len(f['gates']),'browser48','integration_mutations1')
