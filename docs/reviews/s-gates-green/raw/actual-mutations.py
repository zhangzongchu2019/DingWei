from pathlib import Path
import subprocess,json,hashlib,shutil,sys
root=Path('/tmp/s-gates-green-mutations');out=Path('/tmp/s-gates-green-20261108');runner='/tmp/s-gates-green-run.py';results=[]
def run(name,args,want):
 r=subprocess.run(['python3',runner,name,'--root',str(root),*args]);results.append({'name':name,'rc':r.returncode,'expected':want});(out/'MUTATIONS.json').write_text(json.dumps(results,indent=2));assert r.returncode==want,(name,r.returncode,want)
def equal():
 manifest=json.loads((out/'FROZEN-SOURCES.json').read_text());bad=[p for p,h in manifest.items() if hashlib.sha256((root/p).read_bytes()).hexdigest()!=h];assert not bad,bad
# Each mutation starts with exactly the frozen main-tree source files.
equal()
p=root/'internal/extseat/service.go';original=p.read_text()
try:
 assert original.count('s.auditTenantDeny(ctx, p)')==1
 p.write_text(original.replace('s.auditTenantDeny(ctx, p)','// mutation: omitted audit'))
 run('mut-extseat-audit',['go','test','./internal/extseat','-run','^TestTenantDenyMinimalAudit$','-count=1'],1)
 assert 'minimal deny audit missing' in (out/'mut-extseat-audit.log').read_text()
finally:p.write_text(original)
equal()
p=root/'internal/store/spec_clarification_inbound.go';original=p.read_text()
try:
 assert original.count('s.auditClarificationStageDeny(ctx, m, now)')==2
 p.write_text(original.replace('s.auditClarificationStageDeny(ctx, m, now)','// mutation: omitted audit'))
 run('mut-store-audit',['go','test','./internal/store','-run','^TestStageDenyMinimalAudit$','-count=1'],1)
 assert 'deny audit missing' in (out/'mut-store-audit.log').read_text()
finally:p.write_text(original)
equal()
p=root/'internal/gatesmutant';p.mkdir()
try:
 (p/'mutant.go').write_text('package gatesmutant\nimport "database/sql"\nfunc extraOpen(){ db,_:=sql.Open("sqlite",":memory:");if db!=nil{db.Close()} }\n')
 run('mut-release-boundary',['sh','scripts/run-build-tests.sh'],1)
 log=(out/'mut-release-boundary.log').read_text();assert '6 处（基线 5' in log;assert 'HELPER_GATE_START' not in log
finally:shutil.rmtree(p)
equal()
run('mutation-reverted-canonical',['sh','scripts/run-build-tests.sh'],0)
equal();print('MUTATIONS_OK source_restored=true')
