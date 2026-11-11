import json,hashlib,subprocess,datetime
from pathlib import Path
root=Path('/data/ai-dev-data/hosted-test-time-c782');once=Path('/data/ai-dev-data/hosted-test-time-once-c782');out=Path('/tmp/hosted-test-time-c782/review-fix')
env=json.loads(Path('/tmp/hosted-test-time-c782/env.json').read_text());env['TMPDIR']='/tmp/httfix';Path(env['TMPDIR']).mkdir(exist_ok=True)
source=root/'internal/hosted/reconcile_test.go';original=source.read_text();target=once/'internal/hosted/reconcile_test.go'
old='\tdelete(plans, "2c4g")\n';assert original.count(old)==1
instrument='''\tdelete(plans, "2c4g")
	close(hostedPlanDeleteWindow)
	select {
	case <-hostedPlanWitnessDone:
	case <-time.After(10 * time.Second):
	}
'''
fixed=original.replace(old,instrument);target.write_text(fixed)
witness=once/'internal/hosted/plan_parallel_witness_test.go';witness.write_text('''package hosted
import (
 "testing"
 "time"
)
var hostedPlanDeleteWindow = make(chan struct{})
var hostedPlanWitnessDone = make(chan struct{})
func TestHostedPlansParallelWitness(t *testing.T) {
 parallelHostedTest(t)
 defer close(hostedPlanWitnessDone)
 select {
 case <-hostedPlanDeleteWindow:
 case <-time.After(30*time.Second):t.Fatal("writer did not enter delete window")
 }
 if _, ok := PlanByID("2c4g"); !ok {
  t.Fatal("GLOBAL_PLANS_SERIALIZATION_WITNESS: parallel reader lost the approved plan")
 }
}
''')
cmd=['go','test','-json','-race','-count=1','-timeout=3m','./internal/hosted','-run','^(TestUpgradeUnknownPlanNotSwallowed|TestHostedPlansParallelWitness)$']
def run(name):
 r={'cmd':cmd,'cwd':str(once),'start':datetime.datetime.now(datetime.timezone.utc).isoformat(),'reconcile_sha256':hashlib.sha256(target.read_bytes()).hexdigest(),'reader_sha256':hashlib.sha256(witness.read_bytes()).hexdigest()}
 with (out/(name+'.log')).open('w') as f:p=subprocess.run(['env','-i',*[k+'='+v for k,v in env.items()],*cmd],cwd=once,stdout=f,stderr=subprocess.STDOUT)
 r.update(rc=p.returncode,end=datetime.datetime.now(datetime.timezone.utc).isoformat());events=[]
 for line in (out/(name+'.log')).read_text().splitlines():
  try:x=json.loads(line)
  except ValueError:continue
  if isinstance(x,dict):events.append(x)
 r['test_results']=[{k:x.get(k) for k in ['Test','Action','Elapsed']} for x in events if x.get('Test') and x.get('Action') in ['pass','fail','skip']]
 (out/(name+'.json')).write_text(json.dumps(r,ensure_ascii=False,indent=2)+'\n');print(name,json.dumps(r,ensure_ascii=False),flush=True)
 return r
control=run('global-plans-control');assert control['rc']==0
old='func TestUpgradeUnknownPlanNotSwallowed(t *testing.T) {\n';assert fixed.count(old)==1
target.write_text(fixed.replace(old,old+'\tparallelHostedTest(t)\n'))
mutant=run('global-plans-mutant');log=(out/'global-plans-mutant.log').read_text()
assert mutant['rc']!=0 and 'GLOBAL_PLANS_SERIALIZATION_WITNESS' in log and '[build failed]' not in log
assert any(x['Test']=='TestHostedPlansParallelWitness' and x['Action']=='fail' for x in mutant['test_results'])
assert any(x['Test']=='TestUpgradeUnknownPlanNotSwallowed' and x['Action']=='pass' for x in mutant['test_results'])
assert source.read_text()==original
print('MUTATION_G_PASS: serial control green; parallel reader red at required marker',flush=True)
