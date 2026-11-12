import sys,time,json,tempfile
from pathlib import Path
sys.path.insert(0,'/tmp/hosted-test-time-c782')
import run
out=Path('/tmp/hosted-test-time-c782')
while not (out/'repeat3.json').exists():time.sleep(2)
assert json.loads((out/'repeat3.json').read_text())['rc']==0
cache=Path(tempfile.mkdtemp(prefix='htt782-cold-',dir='/tmp'));run.env['XDG_CACHE_HOME']=str(cache)
cmd=['go','test','-json','-count=1','-parallel=6','-timeout=3m','./internal/hosted','-run','^Test(SchemaApplyRollbackRepeatable|SchemaDropLeavesNoG4Tables|AppliedLedgerRejectsMissingHostedTableWithoutRepair|FrozenR30SchemaModelColumnMigration|NodeSeatPoolQuotaFlow|NodeCredentialLifecycle)$']
r=run.run('cold-tools',cmd)
bins=list((cache/'dingwei-pgtest').glob('pgtest-*'))
summary={'cache_was_empty':True,'cache_dir':str(cache),'tool_binaries':len(bins),'rc':r['rc'],'top':len(r['hosted_top'])}
(out/'cold-cache.json').write_text(json.dumps(summary,indent=2)+'\n');print(summary,flush=True)
assert r['rc']==0 and len(r['hosted_top'])==6 and len(bins)==1
