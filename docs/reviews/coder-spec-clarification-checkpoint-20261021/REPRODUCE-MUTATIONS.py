import subprocess,os,json,tempfile,datetime,sys
from pathlib import Path
repo=Path(sys.argv[1]).resolve()
root=Path(sys.argv[2]).resolve(); root.mkdir(parents=True,exist_ok=True)
wt=Path(tempfile.mkdtemp(prefix='clar-store-mutate-',dir='/tmp'))
source='2ec504c6cd7d8a2d012a2fc8ce14a906bff9638e'
subprocess.run(['git','worktree','add','--detach',str(wt),source],cwd=repo,check=True,stdout=subprocess.DEVNULL)
env=dict(os.environ,GOTOOLCHAIN='local',GOPROXY='off',GOSUMDB='off')
mutations=[
 ('M1','internal/store/spec_clarification.go','if role == "technical" && (u.Kind != "technical" || u.Corrects != 0 || u.Confirms != 0) {','if false && role == "technical" && (u.Kind != "technical" || u.Corrects != 0 || u.Confirms != 0) {','TestClarificationAuthorityCurrentPolicyAndDeputy','CLARIFICATION_NON_PM_AUTHORITY'),
 ('M2','internal/store/spec_clarification.go','answerHash := blockerHash([]any{hash, event})','answerHash := blockerHash([]any{q.Generation, a.Key(), u.Kind, u.Body, u.Evidence})','TestClarificationAppendReplayAndEqualTextSupplement','CLARIFICATION_EQUAL_TEXT_SUPPLEMENT_LOST'),
 ('M3','internal/store/spec_clarification.go','if err = check(); err != nil {\n\t\treturn out, err\n\t}\n\treturn out, tx.Commit()','if false { return out, err }\n\treturn out, tx.Commit()','TestClarificationFinalCheckAndAuditFailuresRollBackAll/identity','CLARIFICATION_PARTIAL_COMMIT'),
 ('M4','internal/store/pilot_question.go','if threadPolicy.Version != 0 {','if false && threadPolicy.Version != 0 {','TestClarificationLegacyAnswerPathsCannotBypassEnrollment','CLARIFICATION_LEGACY_WRITE_BYPASS'),
 ('M5','internal/store/spec_clarification.go','if u.Kind == "scope_change" {','if false && u.Kind == "scope_change" {','TestClarificationAnchorCorrectionScopeAndWriteFreeze','CLARIFICATION_SCOPE_CHANGE_MISSING_GATE'),
 ('M6','internal/store/spec_clarification_read.go','a.UserID == p.PMUser || a.UserID == p.DeputyUser','a.UserID == p.PMUser || a.UserID == q.PMUser || a.UserID == p.DeputyUser','TestClarificationReadPaginationCurrentGrantAndPrivateHistory','CLARIFICATION_OLD_PM_READ_GRANT_SURVIVED'),
 ('M7','internal/store/spec_clarification.go','if total >= blockerReplyTotal || recent >= blockerReplyPerMinute {','if false && (total >= blockerReplyTotal || recent >= blockerReplyPerMinute) {','TestClarificationReplyBudgetAndReplayOrder','CLARIFICATION_RATE_LIMIT_BYPASS')
]
results=[]
for mid,path,old,new,test,marker in mutations:
 p=wt/path;original=p.read_text();assert original.count(old)==1,(mid,original.count(old))
 cmd=['go','test','./internal/store','-run','^'+test+'$','-count=1','-timeout=2m']
 row={'id':mid,'path':path,'test':test,'marker':marker,'old':old,'new':new,'command':cmd,'start':datetime.datetime.now(datetime.timezone.utc).isoformat()}
 for phase in ['before','withdrawn','restored']:
  p.write_text(original.replace(old,new) if phase=='withdrawn' else original)
  result=subprocess.run(cmd,cwd=wt,env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
  (root/f'{mid}-{phase}.stdout').write_bytes(result.stdout);(root/f'{mid}-{phase}.stderr').write_bytes(result.stderr)
  row[phase]={'rc':result.returncode,'marker_present':marker.encode() in result.stdout+result.stderr}
  if phase=='withdrawn': assert result.returncode!=0 and marker.encode() in result.stdout+result.stderr,(mid,phase,result.stdout.decode(),result.stderr.decode())
  else: assert result.returncode==0,(mid,phase,result.stdout.decode(),result.stderr.decode())
 row['end']=datetime.datetime.now(datetime.timezone.utc).isoformat();results.append(row)
 (root/'MUTATIONS.json').write_text(json.dumps({'source':source,'rows':results},indent=2)+'\n')
 print(mid,'baseline0/designated-red/restored0',flush=True)
subprocess.run(['git','status','--porcelain'],cwd=wt,check=True)
subprocess.run(['git','worktree','remove',str(wt)],cwd=repo,check=True)
