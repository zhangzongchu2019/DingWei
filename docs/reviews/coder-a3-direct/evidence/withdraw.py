from pathlib import Path
import subprocess,tempfile,json,os
root=Path.cwd()
cases=[
 ('auth','internal/m8/agent_spec_direct.go','h.Identity.AuthenticateAgent(r.Context(), authorization[1])','h.Identity.ResolveFullName(r.Context(), "Kimi.zzc")','./internal/m8','^TestAgentSpecDirectAuthenticationAndLegacyFailClosed$'),
 ('ownership','internal/store/console_specs.go','if agentSelfOnly {\n\t\tif revision.CreatedByKind','if false && agentSelfOnly {\n\t\tif revision.CreatedByKind','./internal/store','^TestAgentDirectRevisionOneOwnershipAndAudit$'),
 ('audit','internal/store/console_specs.go','action = "agent_update"','action = "revise"','./internal/store','^TestAgentDirectRevisionOneOwnershipAndAudit$'),
 ('legacy','internal/m8/spec_grant.go','"status": "upgrade_required"','"status": "approval_required"','./internal/m8','^TestSpecGrantLegacyRequestAndPollFailFastWithoutWrites$'),
 ('old-http','internal/m8/agent_spec_direct.go','if r.Header.Get("X-DW-Spec-Write") != "direct-v1" {','if false && r.Header.Get("X-DW-Spec-Write") != "direct-v1" {','./internal/m8','^TestAgentSpecDirectAuthenticationAndLegacyFailClosed$'),
 ('batch-atomic','internal/store/console_specs.go','defer tx.Rollback()','defer tx.Commit()','./internal/store','^TestSpecArchiveBatchAllOrNothingPermissionAndRestore$'),
 ('archive-permission','internal/store/console_specs.go','if owner != actor && !mayDeleteOthers {','if false && owner != actor && !mayDeleteOthers {','./internal/store','^TestSpecArchiveBatchAllOrNothingPermissionAndRestore$'),
 ('restore','internal/store/console_specs.go','value, action := 0, "unarchive"','value, action := 1, "unarchive"','./internal/store','^TestSpecArchiveBatchAllOrNothingPermissionAndRestore$'),
 ('default-list','internal/m8/console_specs.go','if !item.Archived || includeArchived {','if true || !item.Archived || includeArchived {','./internal/m8','^TestSpecArchiveDefaultListBoardSharedReadAndRestore$'),
 ('board','internal/m8/console_task_board_page.go','if spec.Archived {','if false && spec.Archived {','./internal/m8','^TestSpecArchiveDefaultListBoardSharedReadAndRestore$'),
]
results=[]
for label,file,old,new,pkg,test in cases:
 with tempfile.TemporaryDirectory(prefix='a3-withdraw-') as td:
  p=root/file;source=p.read_text()
  if label=='batch-atomic':
   start=source.index('func (s *SQLite) SetConsoleSpecsArchived(');a=source[:start];b=source[start:];assert old in b;b=b.replace(old,new,1);changed=a+b
  else:
   assert old in source,(label,'anchor missing');changed=source.replace(old,new,1)
  replacement=Path(td)/p.name;replacement.write_text(changed)
  overlay=Path(td)/'overlay.json';overlay.write_text(json.dumps({'Replace':{str(p):str(replacement)}}))
  log='/tmp/coder-a3-withdraw-'+label+'.log'
  with open(log,'w') as out:r=subprocess.run(['go','test','-overlay='+str(overlay),pkg,'-run',test,'-count=1','-timeout','4m'],stdout=out,stderr=subprocess.STDOUT)
  result=dict(case=label,rc=r.returncode,log=log);results.append(result);print(label,r.returncode,flush=True)
Path('/tmp/coder-a3-withdraw.json').write_text(json.dumps(results,indent=2)+'\n')
assert all(r['rc']==1 for r in results),results
