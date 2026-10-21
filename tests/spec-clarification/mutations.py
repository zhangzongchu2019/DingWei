#!/usr/bin/env python3
"""Git-frozen isolated withdrawals: green, designated behavioral red, restored green."""
import argparse,datetime,json,os,subprocess,tempfile
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
ROWS=[]
def go(mid,file,old,new,test,marker,pkg='store',count=1):
 ROWS.append(dict(id=mid,file='internal/'+pkg+'/'+file,old=old,new=new,count=count,test=test,marker=marker,kind='go',pkg=pkg))
go('authority','spec_clarification.go','if role == "technical" && (u.Kind != "technical" || u.Corrects != 0 || u.Confirms != 0) {','if false && role == "technical" && (u.Kind != "technical" || u.Corrects != 0 || u.Confirms != 0) {','TestClarificationAuthorityCurrentPolicyAndDeputy','CLARIFICATION_NON_PM_AUTHORITY')
go('equal-text','spec_clarification.go','answerHash := blockerHash([]any{hash, event})','answerHash := blockerHash([]any{q.Generation, a.Key(), u.Kind, u.Body, u.Evidence})','TestClarificationAppendReplayAndEqualTextSupplement','CLARIFICATION_EQUAL_TEXT_SUPPLEMENT_LOST')
go('last-identity-check','spec_clarification.go','if err = check(); err != nil {\n\t\treturn out, err\n\t}\n\treturn out, nil','if false { return out, err }; return out, nil','TestClarificationFinalCheckAndAuditFailuresRollBackAll/identity','CLARIFICATION_PARTIAL_COMMIT')
go('legacy-refusal-contract','pilot_question.go','return QuestionResult{}, ErrClarificationUpgradeRequired','return QuestionResult{}, nil','TestClarificationLegacyAnswerPathsCannotBypassEnrollment','CLARIFICATION_LEGACY_WRITE_BYPASS')
go('scope-change','spec_clarification.go','if u.Kind == "scope_change" {','if false && u.Kind == "scope_change" {','TestClarificationAnchorCorrectionScopeAndWriteFreeze','CLARIFICATION_SCOPE_CHANGE_MISSING_GATE')
go('historic-pm-reader','spec_clarification_read.go','a.UserID == p.PMUser || a.UserID == p.DeputyUser','a.UserID == p.PMUser || a.UserID == q.PMUser || a.UserID == p.DeputyUser','TestClarificationReadPaginationCurrentGrantAndPrivateHistory','CLARIFICATION_OLD_PM_READ_GRANT_SURVIVED')
go('reply-budget','spec_clarification.go','if total >= blockerReplyTotal || recent >= blockerReplyPerMinute {','if false && (total >= blockerReplyTotal || recent >= blockerReplyPerMinute) {','TestClarificationReplyBudgetAndReplayOrder','CLARIFICATION_RATE_LIMIT_BYPASS')
for name,old in [('parent','n.ProviderMessageID != r.ParentID'),('binding','n.Binding != b'),('policy','p.Version != n.PolicyVersion'),('generation','q.Generation != n.Generation')]:
 go('inbound-'+name,'spec_clarification_inbound.go',old,'false','TestClarificationDirectInboundGuardFields/'+name,'CLARIFICATION_DIRECT_'+name.upper()+'_ACCEPTED')
go('inbound-expiry','spec_clarification_inbound.go','if now.Before(created) || !now.Before(expires) {','if false && (now.Before(created) || !now.Before(expires)) {','TestClarificationDirectInboundGuardFields/expiry','CLARIFICATION_DIRECT_EXPIRY_ACCEPTED')
go('inbound-switch','spec_clarification_inbound.go','if !enabled || check == nil','if false || check == nil','TestClarificationDirectInboundGuardFields/disabled','CLARIFICATION_DIRECT_DISABLED_ACCEPTED')
go('event-replay','spec_clarification_inbound.go','if err == nil && (oldMessage != r.MessageID || oldHash != hash) {','if false && err == nil && (oldMessage != r.MessageID || oldHash != hash) {','TestClarificationDurableInboundDoubleIDConflict/event','CLARIFICATION_STAGE_EVENT_CONFLICT_ACCEPTED')
go('message-replay','spec_clarification_inbound.go','if out.PayloadSHA != hash {','if false && out.PayloadSHA != hash {','TestClarificationDurableInboundDoubleIDConflict/message','CLARIFICATION_STAGE_MESSAGE_CONFLICT_ACCEPTED')
go('image-upload-owner','spec_clarification.go','if owner != a.Key() || question != qid || linked {','if false && owner != a.Key() || question != qid || linked {','TestClarificationPrivateImageActorQuestionAndImmutableLink','CLARIFICATION_OTHER_ACTOR_STAGED_IMAGE_ATTACHED')
go('image-private-read','spec_clarification_images.go','if !linked && owner != a.Key() {','if false && !linked && owner != a.Key() {','TestClarificationPrivateImageActorQuestionAndImmutableLink','CLARIFICATION_UNLINKED_IMAGE_DISCLOSED')
go('revision-isolation','spec_clarification_projection.go','WHERE spec_id=? AND revision=? AND content_sha256=? AND','WHERE spec_id=? AND ?>0 AND content_sha256=? AND','TestClarificationPublicRevisionDoesNotInheritOldThread','CLARIFICATION_PUBLIC_REVISION_CROSS_BIND')
go('cursor-post-generation','project_context.go','[]any{lastFloor, c.State.ID','[]any{lastFloor-lastFloor, c.State.ID','TestClarificationContextProjectionChangesCursorWithoutReopeningTask','CLARIFICATION_CONTEXT_CURSOR_NOT_INVALIDATED')
go('task-theme-isolation','spec_clarification_projection.go','WHERE q.task_id=? AND p.spec_id=?','WHERE ? IS NOT NULL AND p.spec_id=?','TestClarificationContextProjectionChangesCursorWithoutReopeningTask','CLARIFICATION_CONTEXT_OTHER_THEME_LEAK')
go('correction-notification','spec_clarification.go','if (u.Kind == "correction" || u.Kind == "scope_change") &&','if false && (u.Kind == "correction" || u.Kind == "scope_change") &&','TestClarificationCorrectionNotifiesCurrentReadersWithoutReopening','CLARIFICATION_CORRECTION_NOTIFICATION_MISSING')
go('login-single-use','spec_clarification_login.go',' AND consumed_at IS NULL','','TestClarificationLoginContinuationCookieExpiryAndSingleUse','CLARIFICATION_RETURN_REPLAYED',count=2)
go('login-cookie-binding','spec_clarification_login.go','cookie_sha=?','? IS NOT NULL','TestClarificationLoginContinuationCookieExpiryAndSingleUse','CLARIFICATION_RETURN_COOKIE_UNBOUND',count=2)
go('login-expiry','spec_clarification_login.go','expires_at>?','? IS NOT NULL','TestClarificationLoginContinuationCookieExpiryAndSingleUse','CLARIFICATION_RETURN_EXPIRED_ACCEPTED')
go('provider-app','feishu.go','ev.EventV2Base.Header.AppID != "" && ev.EventV2Base.Header.AppID != g.appID','false','TestClarificationSDKRejectsApplicationAndTenantDrift','CLARIFICATION_SDK_WRONG_APP_ACCEPTED',pkg='feishu')
go('provider-tenant','feishu.go','tenant != "" && reply.Tenant != "" && tenant != reply.Tenant','false && tenant != "" && reply.Tenant != "" && tenant != reply.Tenant','TestClarificationSDKRejectsApplicationAndTenantDrift','CLARIFICATION_SDK_WRONG_TENANT_ACCEPTED',pkg='feishu')
for name,old,new,test,marker in [
 ('helper-wire-role',"kind='technical'","kind='explanation'",'test_wire_route_is_technical_no_auto_retry_no_redirect','CLARIFICATION_AGENT_WIRE_AUTHORITY'),
 ('helper-redirect','ProxyHandler({}),NoRedirect()','ProxyHandler({})','test_wire_route_is_technical_no_auto_retry_no_redirect','CLARIFICATION_REDIRECT_CREDENTIAL_LEAK'),
 ('helper-execution-bool',"payload.get('execution_authorized') is not False","payload.get('execution_authorized') != False",'test_response_is_data_without_execution','CLARIFICATION_HELPER_EXECUTION_FLAG_WEAK')]:
 ROWS.append(dict(id=name,file='tools/sessionhelper/sessionhelper/spec_clarification.py',old=old,new=new,count=1,kind='python',test=test,marker=marker))
for name,old,new,script,marker in [
 ('mobile-unknown-id','if(pending&&pending.logical!==logical)','pending=null;if(pending&&pending.logical!==logical)','spec-clarification-browser-regression.js','MOBILE_UNKNOWN_REKEY_DUPLICATED'),
 ('mobile-upload-boundary',"if($('images').files.length&&uploaded.length!==$('images').files.length)","if(false&&$('images').files.length&&uploaded.length!==$('images').files.length)",'spec-clarification-mobile-flow-regression.js','MOBILE_INCOMPLETE_UPLOAD_POSTED')]:
 ROWS.append(dict(id=name,file='internal/m8/spec_clarification_page.go',old=old,new=new,count=1,kind='browser',test=script,marker=marker))


# Append-only storage and trusted metadata boundaries are independently withdrawn.
for mid,trigger,op,table,marker in [
 ('post-update','spec_clarification_post_no_update','UPDATE','spec_clarification_post','CLARIFICATION_METADATA_MUTABLE'),
 ('post-delete','spec_clarification_post_no_delete','DELETE','spec_clarification_post','CLARIFICATION_POST_DELETABLE'),
 ('body-update','spec_clarification_body_no_update','UPDATE','pilot_question_answer','CLARIFICATION_BODY_MUTABLE'),
 ('body-delete','spec_clarification_body_no_delete','DELETE','pilot_question_answer','CLARIFICATION_BODY_DELETABLE')]:
 old='CREATE TRIGGER '+trigger+' BEFORE '+op+' ON '+table
 go(mid,'migrations/0061_spec_clarification_thread.sql',old,old+' WHEN 0' if table=='spec_clarification_post' else old+' WHEN 0 AND', 'TestClarificationAppendReplayAndEqualTextSupplement',marker)
# For body triggers the following existing WHEN is replaced as a whole.
for row in ROWS:
 if row['id'].startswith('body-'):
  row['old']+='\n WHEN EXISTS';row['new']=row['old'].replace('WHEN EXISTS','WHEN 0 AND EXISTS')
go('metadata-json-authority','../model/model.go','FeishuReply *FeishuReplyMetadata `json:"-"`','FeishuReply *FeishuReplyMetadata `json:"feishu_reply"`','TestClarificationSDKPreservesAuthenticatedReplyMetadata','CLARIFICATION_REPLY_METADATA_JSON_FORGERY',pkg='feishu')
go('audit-atomic','spec_clarification.go','if err = questionAudit(ctx, tx, qid, a.Key(), "clarification_append", q.Version, map[string]any{"floor": floor, "role": role, "kind": u.Kind, "policy_version": p.Version}, at); err != nil {','if err = error(nil); err != nil {','TestClarificationFinalCheckAndAuditFailuresRollBackAll/audit','CLARIFICATION_PARTIAL_COMMIT')
go('blocker-atomic','spec_clarification.go','if err = questionBlockerInput(ctx, tx, c, a, q, event, u.Body, u.Evidence, now); err != nil {','if err = error(nil); err != nil {','TestClarificationFinalCheckAndAuditFailuresRollBackAll/blocker','CLARIFICATION_PARTIAL_COMMIT')

def main():
 p=argparse.ArgumentParser();p.add_argument('--commit',required=True);p.add_argument('--output',required=True);p.add_argument('--only');a=p.parse_args()
 out=Path(a.output).resolve();out.mkdir(parents=True,exist_ok=True)
 source=subprocess.check_output(['git','rev-parse',a.commit+'^{commit}'],cwd=ROOT,text=True).strip()
 wt=Path(tempfile.mkdtemp(prefix='clar-withdraw-'));subprocess.run(['git','worktree','add','--detach',str(wt),source],cwd=ROOT,check=True,stdout=subprocess.DEVNULL)
 env=dict(os.environ,GOTOOLCHAIN='local',GOPROXY='off',GOSUMDB='off',NODE_PATH='/tmp/coder-a3-playwright/node_modules',PW_CHANNEL='chrome',GOPATH='/home/dev/go',GOMODCACHE='/home/dev/go/pkg/mod',GOCACHE='/home/dev/.cache/go-build')
 report={'source':source,'rows':[],'started_at':datetime.datetime.now(datetime.timezone.utc).isoformat()}
 def save(): (out/'MUTATIONS.json').write_text(json.dumps(report,indent=2)+'\n')
 try:
  for row in ROWS:
   if a.only and row['id'] not in a.only.split(','):continue
   path=wt/row['file'];original=path.read_text();assert original.count(row['old'])==row['count'],(row['id'],original.count(row['old']))
   result={**row,'phases':{}};report['rows'].append(result);save()
   if row['kind']=='go':cmd=['go','test','./internal/'+row['pkg'],'-run','^'+row['test']+'$','-count=1','-timeout=3m']
   elif row['kind']=='python':cmd=['/tmp/coder-0232-locked-venv/bin/python','-m','unittest','test_spec_clarification.ClarificationToolTest.'+row['test']];env['PYTHONPATH']=str(wt/'tools/sessionhelper')+':'+str(wt/'tools/sessionhelper/tests')
   else:cmd=['node','view-service/'+row['test']]
   result['command']=cmd
   try:
    for phase in ['before','withdrawn','restored']:
     path.write_text(original.replace(row['old'],row['new']) if phase=='withdrawn' else original)
     with tempfile.TemporaryDirectory(prefix='clar-home-') as home:
      phase_env=dict(env,HOME=home)
      r=subprocess.run(cmd,cwd=wt,env=phase_env,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=240)
     (out/(row['id']+'-'+phase+'.stdout')).write_bytes(r.stdout);(out/(row['id']+'-'+phase+'.stderr')).write_bytes(r.stderr)
     has=row['marker'].encode() in r.stdout+r.stderr
     result['phases'][phase]={'rc':r.returncode,'marker_present':has};save()
     if phase=='withdrawn':assert r.returncode!=0 and has,(row['id'],phase,r.stdout[-2500:],r.stderr[-2500:])
     else:assert r.returncode==0,(row['id'],phase,r.stdout[-2500:],r.stderr[-2500:])
   finally:path.write_text(original)
   print(row['id']+' green/designated-red/restored-green',flush=True)
  report['finished_at']=datetime.datetime.now(datetime.timezone.utc).isoformat();save()
 finally:subprocess.run(['git','worktree','remove',str(wt)],cwd=ROOT,check=True)
if __name__=='__main__':main()
