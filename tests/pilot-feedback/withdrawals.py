#!/usr/bin/env python3
"""Each local withdrawal must hit its designated behavioral assertion, then restore."""
import argparse,io,json,subprocess,tarfile,tempfile,time
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2];PY='/data/ai-dev-data/helper-deps-lock-venv/bin/python';CASES=[]
Q='internal/store/pilot_question.go';R='internal/store/pilot_question_read.go';B='internal/store/pilot_question_blocker.go';T='internal/store/pilot_question_types.go';M='internal/m8/pilot_question.go';P='tools/sessionhelper/sessionhelper/pilot_question.py'
def case(name,file,old,new,pkg,test,marker):
 cmd=[PY,'-B','-m','unittest','discover','-s','tools/sessionhelper/tests','-p','test_pilot_question.py','-v'] if pkg=='python' else ['go','test',pkg,'-count=1','-run','^'+test+'$','-timeout=2m']
 CASES.append(dict(name=name,edits=[(file,old,new)],argv=cmd,marker=marker))
def store(name,file,old,new,test,marker):case(name,file,old,new,'./internal/store',test,marker)
store('card-cas',Q,'q.Version != u.ExpectedVersion','q.Version != q.Version','TestPilotQuestionCASAndClockRollbackFailClosed','QUESTION_STALE_VERSION_ACCEPTED')
store('clock-rollback',Q,'e != nil || now.Before(start)','e != nil || false && now.Before(start)','TestPilotQuestionCASAndClockRollbackFailClosed','QUESTION_CLOCK_ROLLBACK_ANSWER')
store('immutable-event',Q,'if previous != hash {','if false && previous != hash {','TestPilotQuestionAnswerAtomicAndUsesOnlyD2Wake','QUESTION_EVENT_REPLACED')
store('logical-content-dedup',Q,'if e == nil {\n\t\t\tout.Outcome = "unchanged"','if false && e == nil {\n\t\t\tout.Outcome = "unchanged"','TestPilotQuestionSameContentNewEventDeduplicatesAndRoleRevokes','QUESTION_CONTENT_DUPLICATED_WAKE')
store('no-agent-pm',T,'return a.UserID != "" && (questionManager(a, c) || a.UserID == q.PMUser || q.Responsible == a.Key())','return true','TestPilotQuestionNoPartialCommitAndAgentNotPM','QUESTION_AGENT_IMPERSONATED_PM')
store('final-pg-check',Q,'if err = check(); err != nil {\n\t\treturn out, err\n\t}\n\tif err = tx.Commit(); err != nil {\n\t\treturn out, err\n\t}\n\treturn out, nil\n}\nfunc questionAcknowledgment','if err = error(nil); err != nil {\n\t\treturn out, err\n\t}\n\tif err = tx.Commit(); err != nil {\n\t\treturn out, err\n\t}\n\treturn out, nil\n}\nfunc questionAcknowledgment','TestPilotQuestionNoPartialCommitAndAgentNotPM','QUESTION_FINAL_CHECK_IGNORED')
store('sixty-minute-lower-bound',R,'if now.Before(due) {','if false && now.Before(due) {','TestPilotQuestionReminderExactlySixtyMinutesAndOncePerGeneration','QUESTION_SLA_EARLY_REMINDER')
store('one-sla-per-generation','internal/store/migrations/0059_pilot_questions.sql',"CREATE UNIQUE INDEX idx_pilot_question_reminder_once ON pilot_question_outbox(question_id,generation) WHERE kind='remind_60m';",'-- SLA partial uniqueness withdrawn','TestPilotQuestionReminderExactlySixtyMinutesAndOncePerGeneration','QUESTION_ASSIGN_SECOND_SLA')
store('assign-preserves-clock',Q,'// Explicit reassignment is a new policy event, not a reset of the SLA clock.','q.DueAt=now.Add(time.Hour).UTC().Format(time.RFC3339Nano)\n // clock reset withdrawn','TestPilotQuestionReminderExactlySixtyMinutesAndOncePerGeneration','QUESTION_ASSIGN_RESET_SLA')
store('unknown-no-resend',R,'if state == "dispatching" {','if false && state == "dispatching" {','TestPilotQuestionReplyFirstCancelsNoticeAndUnknownNeverRetries','QUESTION_UNKNOWN_NOTICE_RETRIED')
store('answer-first-cancels-reminder',R,'if q.FirstSubstantiveAt != "" {','if false && q.FirstSubstantiveAt != "" {','TestPilotQuestionReplyFirstCancelsNoticeAndUnknownNeverRetries','QUESTION_REPLY_FIRST_REMINDER_SENT')
store('unconfigured-not-delivered',R,'(!configured || recipient == "")','(false && !configured || recipient == "")','TestPilotQuestionUnconfiguredNeverClaimsSent','QUESTION_UNCONFIGURED_SENT')
store('question-only-reader',T,'a.UserID == q.PMUser','true','TestPilotQuestionReadGrantIsOnlyQuestionAndHistoryBounded','QUESTION_UNGRANTED_READ')
store('history-bounded',R,'ORDER BY rowid DESC LIMIT 101','ORDER BY rowid DESC LIMIT 10000','TestPilotQuestionReadGrantIsOnlyQuestionAndHistoryBounded','QUESTION_MINIMUM_GRANT_OR_HISTORY_BOUND')
store('pending-change-blocks-old-scope',T,'if n != 0 {','if n < 0 {','TestPilotQuestionBusinessChangeBlocksAllOldScopeAndNeedsHumanDecision','QUESTION_PENDING_CHANGE_EXECUTION_BYPASS')
store('owner-only-change-decision',Q,'a.UserID != c.Owner || u.ChangeID == ""','false && a.UserID != c.Owner || u.ChangeID == ""','TestPilotQuestionBusinessChangeBlocksAllOldScopeAndNeedsHumanDecision','QUESTION_PM_OWNER_DECISION')
store('d2-old-waiter-revoked',Q,'if err = questionReassignBlocker(ctx, tx, c, a, q, now); err != nil {','if err = error(nil); err != nil {','TestPilotQuestionReassignRevokesSharedD2WaiterGrant','QUESTION_REASSIGN_RETAINED_D2_WAITER')
store('production-full',Q,'if u.ProductionOrSecurity {','if false && u.ProductionOrSecurity {','TestPilotQuestionProductionFullAndAckDoesNotStopSLA','QUESTION_PRODUCTION_NOT_FULL')
store('ack-is-not-substantive',Q,'kind == "substantive" && questionAcknowledgment(u.Body)','false && kind == "substantive" && questionAcknowledgment(u.Body)','TestPilotQuestionProductionFullAndAckDoesNotStopSLA','QUESTION_ACK_TREATED_AS_SUBSTANTIVE')
store('completed-feedback-no-wake',B,'if status == "done" {','if false && status == "done" {','TestPilotQuestionSpecOnlyAndCompletedTaskNeverWake','QUESTION_POST_COMPLETION_CREATED_BLOCKER')
store('pm-cannot-grant-outsiders',Q,'(!owner && !delegated)','(false && !owner && !delegated)','TestPilotQuestionPMDelegationLimitedToExistingQuestionReaders','QUESTION_PM_GRANTED_NEW_RIGHTS')
case('pg-send-reply-serialized','internal/identity/project_context.go','if questionID != "" {','if false && questionID != "" {','./internal/m8','TestPilotQuestionPGActualSendReplyOrderingAndCurrentBinding','QUESTION_ANSWER_COMMITTED_DURING_SEND')
case('safe-public-origin','internal/m8/pilot_question_worker.go','u.Scheme != "https"','false && u.Scheme != "https"','./internal/m8','TestPilotQuestionMessageNoPrivatePayloadAndOriginCannotBeClientSelected','QUESTION_UNTRUSTED_ORIGIN')
case('no-private-feishu-body','internal/m8/pilot_question_worker.go','return lead + "\\n类型："','return q.ObservedFact + lead + "\\n类型："','./internal/m8','TestPilotQuestionMessageNoPrivatePayloadAndOriginCannotBeClientSelected','QUESTION_FEISHU_PRIVATE_DATA_OR_CALLBACK_CONFUSION')
case('provider-native-uuid','internal/feishu/feishu.go','if question {\n\t\tbody.Uuid(out.RequestID)','if false && question {\n\t\tbody.Uuid(out.RequestID)','./internal/feishu','TestPilotQuestionNativeSDKPrivateRecipientAndUUID','QUESTION_NATIVE_PRIVATE_UUID_LOST')
case('helper-strict-version',P,"type(body['expected_version']) is not int","not isinstance(body['expected_version'],int)",'python','','QUESTION_VERSION_TYPE_WEAK')
case('helper-result-execution-flag',P,"payload.get('execution_authorized') is not False","payload.get('execution_authorized') == True",'python','','QUESTION_RESULT_EXECUTION_FLAG_WEAK')
case('helper-reject-query-mix',P,"if set(body)-allowed or task:","if False:",'python','','QUESTION_QUERY_SCOPE_FORGED')

def run(args):
 commit=subprocess.check_output(['git','rev-parse',args.commit+'^{commit}'],cwd=ROOT,text=True).strip();out=Path(args.output).resolve();out.mkdir(parents=True,exist_ok=True)
 chosen=[c for c in CASES if not args.only or c['name'] in args.only]
 if args.only and set(args.only)-{c['name'] for c in CASES}:raise ValueError('unknown case')
 archive=subprocess.check_output(['git','archive',commit],cwd=ROOT)
 with tempfile.TemporaryDirectory(prefix='question-withdraw-') as temp:
  root=Path(temp)/'source';root.mkdir();home=Path(temp)/'home';home.mkdir(mode=0o700);(home/'.embedded-postgres-go').symlink_to('/home/dev/.embedded-postgres-go',target_is_directory=True)
  with tarfile.open(fileobj=io.BytesIO(archive)) as tar:tar.extractall(root,filter='data')
  env={'HOME':str(home),'PATH':str(Path(PY).parent)+':/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin','PYTHONPATH':str(root/'tools/sessionhelper'),'PYTHONDONTWRITEBYTECODE':'1','LANG':'C.UTF-8','TZ':'UTC','GOCACHE':'/home/dev/.cache/go-build','GOMODCACHE':'/home/dev/go/pkg/mod','GOPATH':'/home/dev/go','GOPROXY':'off','GOSUMDB':'off','DW_DESKTOP_INTEGRATION_PYTHON':PY}
  baseline=[('go',['go','test','./internal/store','./internal/m8','./internal/feishu','-count=1','-timeout=3m','-run','^TestPilotQuestion']),('helper',[PY,'-B','-m','unittest','discover','-s','tools/sessionhelper/tests','-p','test_pilot_question.py','-v'])]
  def execute(label,cmd):
   start=time.time();p=subprocess.run(cmd,cwd=root,env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=300);(out/(label+'.log')).write_text(p.stdout);return p,round(time.time()-start,3)
  for label,cmd in baseline:
   p,_=execute('baseline-'+label,cmd)
   if p.returncode:raise AssertionError('baseline failed '+label)
  results=[]
  for c in chosen:
   saved={}
   try:
    for file,old,new in c['edits']:
     path=root/file
     if path not in saved:saved[path]=path.read_text()
     source=path.read_text()
     if old not in source:raise AssertionError('missing mutation target '+c['name'])
     path.write_text(source.replace(old,new,1))
    p,seconds=execute(c['name'],c['argv'])
   finally:
    for path,source in saved.items():path.write_text(source)
   red=p.returncode!=0 and c['marker'] in p.stdout and not any(s in p.stdout for s in ('build failed','SyntaxError','undefined:'))
   results.append({**c,'rc':p.returncode,'designated_red':red,'seconds':seconds});(out/'RESULTS.json').write_text(json.dumps({'commit':commit,'cases':results},indent=2)+'\n');print(c['name'],p.returncode,red,flush=True)
   if not red:raise AssertionError('not designated red '+c['name'])
  for label,cmd in baseline:
   p,_=execute('restored-'+label,cmd)
   if p.returncode:raise AssertionError('restore failed '+label)
if __name__=='__main__':
 p=argparse.ArgumentParser();p.add_argument('--commit',required=True);p.add_argument('--output',required=True);p.add_argument('--only',nargs='*');run(p.parse_args())
