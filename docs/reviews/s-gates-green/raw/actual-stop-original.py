from pathlib import Path
import os,json,time,signal,datetime,re,shutil
raw=Path('/tmp/s-gates-green-20261108')
report=json.loads((raw/'RACE-FINAL-REPORT.json').read_text())
assert report['list_diff_rc']==0 and report['list_diff_bytes']==0 and sum(report['totals'].values())==1137 and report['totals']['fail']==0
assert json.loads((raw/'race-extseat-full.json').read_text())['rc']==0
meta=json.loads((raw/'race-count3.json').read_text())
now=lambda:datetime.datetime.now(datetime.timezone.utc).isoformat()
if 'rc' in meta:
 assert meta['rc']==0,('original suite independently failed; investigate',meta)
 result={'finished_or_stopped':True,'mode':'finished','actual_rc':meta['rc'],'counted_as_rc0':True,'utc':now(),'signals':[]}
else:
 pids=json.loads((raw/'ORIGINAL-RACE-PIDS.json').read_text())['pids']
 case=next(x for x in pids if '/store.test ' in x['cmd'])
 pid=case['pid'];p=Path('/proc')/str(pid)
 def verify(pid,start=None):
  q=Path('/proc')/str(pid);parts=q.joinpath('stat').read_text().rsplit(')',1)[1].split()
  uid=int(next(x for x in q.joinpath('status').read_text().splitlines() if x.startswith('Uid:')).split()[1]);assert uid==os.getuid()
  if start:assert parts[19]==start,('PID reuse',pid)
  return parts
 verify(pid,case['start_ticks']);cmd=p.joinpath('cmdline').read_bytes().replace(b'\0',b' ').decode();assert '/store.test ' in cmd and '-test.count=3' in cmd
 # Do not interrupt a test while it owns a child process; wait for a plain SQLite fixture.
 children=p.joinpath('task',str(pid),'children').read_text().split();assert not children,('owned test has active children; retry later',children)
 fixtures=set()
 for f in p.joinpath('fd').iterdir():
  try:link=os.readlink(f)
  except OSError:continue
  m=re.match(r'^(/tmp/Test[^/]+)/',link)
  if m:fixtures.add(m.group(1))
 result={'finished_or_stopped':False,'mode':'owner_interrupted_after_all_authorized_shards_passed','counted_as_rc0':False,'utc':now(),'signals':[],'owned_fixture_dirs':sorted(fixtures),'verified_pid':pid,'verified_start_ticks':case['start_ticks']}
 os.kill(pid,signal.SIGINT);result['signals'].append({'pid':pid,'signal':'SIGINT','utc':now()})
 for _ in range(100):
  time.sleep(.1);meta=json.loads((raw/'race-count3.json').read_text())
  if 'rc' in meta:break
 else:raise RuntimeError('original runner did not publish actual exit; preserve lock and inspect')
 assert meta['rc']!=0,('interrupted run unexpectedly claimed zero',meta)
 result.update(finished_or_stopped=True,actual_rc=meta['rc'],actual_end_utc=meta.get('end_utc'))
 for f in fixtures:
  q=Path(f)
  if q.exists():
   assert q.parent==Path('/tmp') and q.name.startswith('Test') and not q.is_symlink() and q.stat().st_uid==os.getuid()
   shutil.rmtree(q)
 result['owned_fixture_cleanup']=True
log=(raw/'race-count3.log').read_text()
result['unexpected_failure_signatures']=[x for x in ('--- FAIL:','WARNING: DATA RACE') if x in log]
(raw/'ORIGINAL-RACE-DISPOSITION.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result))

assert not result['unexpected_failure_signatures'], ('original run contains an assertion/race failure; investigate before delivery', result)
