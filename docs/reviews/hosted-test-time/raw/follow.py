import sys,time,json
from pathlib import Path
sys.path.insert(0,'/tmp/hosted-test-time-c782')
from run import run
out=Path('/tmp/hosted-test-time-c782')
while not (out/'after-all.json').exists():time.sleep(2)
a=json.loads((out/'after-all.json').read_text());h=next(v for k,v in a['packages'].items() if k.endswith('/internal/hosted'))
assert a['rc']==0,a['rc'];assert h['elapsed'][0]<=300,h;assert len(a['hosted_top'])==185
for n,c in [('repeat3',['go','test','-json','-count=3','-shuffle=782','-timeout=40m','./internal/hosted']),('build-tests',['sh','scripts/run-build-tests.sh']),('diff-check',['git','diff','--check'])]:
 r=run(n,c)
 if r['rc']:raise SystemExit(r['rc'])
print('VALIDATION_ALL_PASS',flush=True)
