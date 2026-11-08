from pathlib import Path
import subprocess,json,hashlib
root=Path('/tmp/s-gates-green-criterion-mutations');out=Path('/tmp/s-gates-green-20261108');results=[]
cases=[
 ('mut-extseat-pep','internal/extseat/service.go','s.auditTenantDeny(ctx, p)','_, _, _ = s.Identity.ResolveFullName(ctx, target)\n\t\ts.auditTenantDeny(ctx, p)','./internal/extseat','TestTenantDenyPEPBeforeResolve','resource query spy='),
 ('mut-extseat-deny','internal/extseat/service.go','return "", seat, "", extshared.ErrTenantRejected','return "", seat, "", nil','./internal/extseat','TestTenantDenyIsErrorNotSuccess','deny response changed'),
 ('mut-extseat-existence','internal/extseat/service.go','s.auditTenantDeny(ctx, p)','p.Subject = target\n\t\ts.auditTenantDeny(ctx, p)','./internal/extseat','TestTenantDenyExistingAndMissingSameShape','target existence affected'),
 ('mut-store-pep','internal/store/spec_clarification_inbound.go','s.auditClarificationStageDeny(ctx, m, now)','_ = s.db.QueryRowContext(ctx, "SELECT name FROM sqlite_master LIMIT 1").Scan(new(string))\n\t\ts.auditClarificationStageDeny(ctx, m, now)','./internal/store','TestStageDenyBeforeResourceSQL','resource spy='),
 ('mut-store-deny','internal/store/spec_clarification_inbound.go','return out, ErrTaskFlowPermission','return out, nil','./internal/store','TestStageDenyNotSuccessfulEmptyResult','original deny changed'),
 ('mut-store-existence','internal/store/spec_clarification_inbound.go','s.auditClarificationStageDeny(ctx, m, now)','m.SenderOpenID = m.FeishuReply.ParentID\n\t\ts.auditClarificationStageDeny(ctx, m, now)','./internal/store','TestStageDenyExistingMissingSameResponseAndAudit','existence influenced minimal audit'),
]
for name,path,old,new,package,test,cause in cases:
 p=root/path;source=p.read_text();assert old in source
 try:
  p.write_text(source.replace(old,new,1))
  r=subprocess.run(['python3','/tmp/s-gates-green-run.py',name,'--root',str(root),'go','test',package,'-run','^'+test+'$','-count=1','-timeout=15m'])
  log=(out/(name+'.log')).read_text();results.append({'name':name,'test':test,'rc':r.returncode,'expected':1,'cause_found':cause in log,'mutated_source_sha256':hashlib.sha256(p.read_bytes()).hexdigest()});(out/'CRITERION-MUTATIONS.json').write_text(json.dumps(results,indent=2))
  assert r.returncode==1 and cause in log,(name,r.returncode,log[-1500:])
 finally:p.write_text(source)
print('CRITERION_MUTATIONS_OK restored=true')
