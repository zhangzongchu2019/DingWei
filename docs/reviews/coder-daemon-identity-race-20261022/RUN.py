"""Four concurrent isolated module runs, plus a deterministic delayed notification."""
import concurrent.futures,datetime,hashlib,json,os,pathlib,subprocess,sys,tempfile,threading,time
ROOT=pathlib.Path(__file__).resolve().parents[3]
TEST='tools/sessionhelper/tests/test_daemon_identity.py'
SUITE='tools.sessionhelper.tests.test_daemon_identity'
WAIT="""        deadline = time.monotonic() + 5
        while 'initialized' not in self.peer.methods and time.monotonic() < deadline:
            time.sleep(.01)
"""
def main(output):
 out=pathlib.Path(output).resolve();out.mkdir(mode=0o700,parents=True,exist_ok=False)
 source=subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip()
 base='ce58c1bfd0a49e3d12161df6b1dffcea38cfb945'
 changed=subprocess.check_output(['git','diff','--name-only',base,source,'--',':(exclude)docs'],cwd=ROOT,text=True).splitlines();assert changed==[TEST]
 test_bytes=(ROOT/TEST).read_bytes(); rows=[]
 def now():return datetime.datetime.now(datetime.timezone.utc).isoformat()
 def run(name,cmd,barrier=None,extra=None):
  with tempfile.TemporaryDirectory(prefix='did-race-h-') as home,tempfile.TemporaryDirectory(prefix='did-race-t-') as tmp:
   env=dict(HOME=home,TMPDIR=tmp,PATH=str(pathlib.Path(sys.executable).parent)+':/usr/bin:/bin',PYTHONPATH=str(ROOT/'tools/sessionhelper')+':'+str(ROOT/'tools/sessionhelper/tests'),PYTHONDONTWRITEBYTECODE='1')
   if extra:env.update(extra)
   if barrier:barrier.wait(timeout=10)
   start=now();monotonic_start=time.monotonic_ns()
   p=subprocess.run(cmd,cwd=ROOT,env=env,capture_output=True,timeout=35)
   end=now();monotonic_end=time.monotonic_ns()
   for ext,data in [('stdout',p.stdout),('stderr',p.stderr),('rc',str(p.returncode).encode()+b'\n')]: (out/(name+'.'+ext)).write_bytes(data)
   return dict(name=name,source=source,command=cmd,start=start,end=end,start_ns=monotonic_start,end_ns=monotonic_end,rc=p.returncode,isolated_HOME=True,isolated_TMPDIR=True,interpreter=sys.executable)
 for wave in range(5):
  barrier=threading.Barrier(4)
  with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
   fs=[pool.submit(run,f'wave-{wave+1}-worker-{worker+1}',[sys.executable,'-B','-m','unittest','-v',SUITE],barrier) for worker in range(4)]
   batch=[f.result() for f in fs]
  assert all(x['rc']==0 for x in batch),batch
  assert max(x['start_ns'] for x in batch)<min(x['end_ns'] for x in batch),'FOUR_PROCESSES_DID_NOT_OVERLAP'
  rows.extend(batch)
  (out/'STRESS.json').write_text(json.dumps(dict(source=source,waves=wave+1,workers=4,rows=rows),indent=2)+'\n')
 # This copy changes only the test's wait during the red run. Runtime source is
 # read from ROOT, and LocalPeer deliberately records initialized 200ms late.
 probe='''import importlib.util,os,pathlib,time,unittest
p=pathlib.Path(os.environ['DID_TEST_COPY'])
spec=importlib.util.spec_from_file_location('delayed_identity_fixture',p)
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
original=m.DaemonWiring.setUp
class Delayed(list):
 def append(self,value):
  if value=='initialized':
   if os.environ['DID_DROP']=='1':return
   time.sleep(.2)
  super().append(value)
def setup(self):
 original(self);self.peer.methods=Delayed()
m.DaemonWiring.setUp=setup
suite=unittest.TestSuite([m.DaemonWiring('test_default_observe_wrong_exe_warns_and_connects')])
result=unittest.TextTestRunner(verbosity=2).run(suite)
raise SystemExit(0 if result.wasSuccessful() else 1)
'''
 (out/'delay-probe.py').write_text(probe)
 with tempfile.TemporaryDirectory(prefix='did-race-copy-') as tmp:
  copy=pathlib.Path(tmp)/'test_daemon_identity.py';assert test_bytes.decode().count(WAIT)==1
  proofs=[]
  for name,withdraw,drop in [('delay-positive',False,False),('delay-withdrawal',True,False),('delay-restored',False,False),('missing-notification',False,True)]:
   copy.write_bytes(test_bytes.decode().replace(WAIT,'').encode() if withdraw else test_bytes)
   row=run(name,[sys.executable,'-B',str(out/'delay-probe.py')],extra=dict(DID_TEST_COPY=str(copy),DID_DROP='1' if drop else '0'))
   stderr=(out/(name+'.stderr')).read_text()
   if withdraw or drop:
    assert row['rc']==1 and 'OBSERVE_REJECTED_CONNECTION' in stderr and 'AssertionError' in stderr and 'FAIL:' in stderr and 'ERROR:' not in stderr,row
   else:assert row['rc']==0,row
   if drop:assert 4.5<(row['end_ns']-row['start_ns'])/1e9<8,'WAIT_NOT_BOUNDED'
   row.update(test_copy_sha256=hashlib.sha256(copy.read_bytes()).hexdigest(),withdraw_wait=withdraw,drop_notification=drop);proofs.append(row)
 (out/'PROOFS.json').write_text(json.dumps(dict(source=source,rows=proofs),indent=2)+'\n')
 for x in rows:
  stderr=(out/(x['name']+'.stderr')).read_text();assert 'Ran 18 tests' in stderr and stderr.rstrip().endswith('OK')
 (out/'RESULT.json').write_text(json.dumps(dict(source=source,base=base,changed_non_docs=changed,test_sha256=hashlib.sha256(test_bytes).hexdigest(),workers=4,waves=5,module_runs=20,tests_per_run=18,total_test_executions=360,failures=0,all_waves_four_overlapping=True,delay_ms=200,proof_runs=4,product_changes=False,online_changes=False,uid=os.getuid()),indent=2)+'\n')
 print('IDENTITY_RACE_PASS workers=4 waves=5 modules=20 tests=360; delay=green/red/restored; missing-notification=bounded-red')
if __name__=='__main__':main(sys.argv[1])
