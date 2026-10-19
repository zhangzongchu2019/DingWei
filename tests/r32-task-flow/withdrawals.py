#!/usr/bin/env python3
"""Actual isolated source withdrawals; require the designated assertion to fail."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile

ROOT=Path(__file__).resolve().parents[2]
GO=['go','test']
def go(package,test):return GO+[package,'-run','^'+test+'$','-count=1','-timeout=2m']
def change(text,old,new):
    if old not in text:raise RuntimeError('withdrawal anchor missing: '+old[:100])
    return text.replace(old,new,1)
def main():
    parser=argparse.ArgumentParser();parser.add_argument('--output',required=True);parser.add_argument('--anchor',default='HEAD');args=parser.parse_args()
    output=Path(args.output).resolve();output.mkdir(parents=True,exist_ok=True)
    anchor=subprocess.check_output(['git','rev-parse',args.anchor],cwd=ROOT,text=True).strip()
    work=Path(tempfile.mkdtemp(prefix='r32-withdraw-'))
    subprocess.run(['git','worktree','add','--detach',str(work),anchor],cwd=ROOT,check=True,stdout=subprocess.DEVNULL)
    env=dict(os.environ);env['PYTHONPATH']=str(work/'tools/sessionhelper');env.setdefault('NODE_PATH','/tmp/coder-a3-playwright/node_modules');env.setdefault('PW_CHANNEL','chrome');env['PATH']='/tmp/coder-0232-locked-venv/bin:'+env['PATH']
    flow='internal/store/console_task_flow.go';cases='internal/store/console_task_case_result.go';ident='internal/identity/task_authorization.go';hub='internal/m8/console_task_flow.go';inbox='tools/sessionhelper/sessionhelper/inbox.py'
    state=go('./internal/store','TestR32TaskStateCasesAndIndependentAgentSameOwner')
    specs=[]
    def add(name,file,mutate,command,marker):specs.append((name,file,mutate,command,marker))
    add('distinct_agents',flow,lambda s:change(s,'|| dev == review {','{'),state,'SAME_AGENT_PAIR_ACCEPTED')
    def roles(s):
        s=change(s,'(actor != t.Developer && actor != t.Reviewer)','false')
        return change(s,'if actor != t.Developer {','if false {')
    add('own_task_writer',flow,roles,state,'OTHER_TASK_WRITER_ACCEPTED')
    add('dependency_done',flow,lambda s:change(s,'if err = taskDependenciesDone(ctx, tx, deps); err != nil {','if err = taskDependenciesDone(ctx, tx, deps); err != nil && false {'),go('./internal/store','TestR32TaskDependencyAndIllegalTransitions'),'UNFINISHED_DEPENDENCY_STARTED')
    add('review_before_done',flow,lambda s:change(s,'if n == 0 || p != n {','if n < 0 || p < 0 {'),state,'UNCONFIRMED_TASK_DONE')
    add('developer_no_self_confirm',cases,lambda s:change(s,'&& !allowAnyTaskCaseConfirmer {','&& false {'),state,'DEVELOPER_SELF_CONFIRMED')
    add('remark_resets_confirmation',cases,lambda s:change(s,'confirmed_by=NULL,confirmed_at=NULL`','confirmed_by=console_spec_task_case_result.confirmed_by,confirmed_at=console_spec_task_case_result.confirmed_at`'),state,'REMARK_KEPT_CONFIRMATION')
    def recheck(s):
        begin=s.index('func (s *SQLite) UpdateConsoleTaskFlow');end=s.index('func taskDependenciesDone',begin)
        block=s[begin:end];old='if err = recheck(); err != nil {'
        if old not in block:raise RuntimeError('recheck anchor missing')
        return s[:begin]+block.replace(old,'if err = recheck(); err != nil && false {')+s[end:]
    add('precommit_pg_recheck',flow,recheck,go('./internal/store','TestR32TaskAtomicAuditRecheckAndIdempotency'),'PG_RECHECK_IGNORED')
    add('outbox_atomicity',flow,lambda s:change(s,'if err = flowOutbox(ctx, tx, t, v.id, v.role, v.event, at); err != nil {','if err = flowOutbox(ctx, tx, t, v.id, v.role, v.event, at); err != nil && false {'),go('./internal/store','TestR32OutboxFailureRollsBackAssignment'),'OUTBOX_FAILURE_ACCEPTED')
    add('current_group_membership',ident,lambda s:change(s,'if n != 1 {','if n < 0 {'),go('./internal/identity','TestR32TaskAuthorizationPGMembershipLocksAndOwners'),'LEFT_GROUP_TASK_ACCESS')
    def unlocked(s):
        s=change(s,'g, err := lockGroup(ctx, tx, group, true)','g, err := s.GetGroup(ctx, group)')
        return s.replace('FOR SHARE OF a','').replace('FOR SHARE','')
    add('readonly_pg_no_fallback',ident,unlocked,go('./internal/identity','TestR32TaskAuthorizationPGMembershipLocksAndOwners'),'READONLY_PG_AUTH_FELL_BACK')
    add('background_reconciliation',hub,lambda s:change(s,'h.reconcileTaskFlow(ctx)','// withdrawn background reconciliation'),go('./internal/m8','TestR32TaskRecoveryAfterPGCommitBeforeSQLiteMark'),'RECOVERY_REQUIRED_USER_REQUEST')
    add('old_helper_capability_gate',hub,lambda s:change(s,'if !capable {','if !capable && false {'),go('./internal/m8','TestR32TaskNotificationOfflineQueueOldHelperAndReliableFrame'),'OLD_HELPER_TASK_PAYLOAD_SENT')
    def shared(s):
        start=s.index('func (h *Hub) filterTaskFlowProjection');return s[:start]+change(s[start:],'if !ok {','if !ok || true {')
    add('shared_spec_task_privacy',hub,shared,go('./internal/m8','TestR32PublicSpecCannotBypassTaskReadRevocation'),'PUBLIC_SPEC_LEAKED_R32_TASK')
    add('notification_not_executable',inbox,lambda s:change(s,"VALUES(?,?,?,'processed',?,?)","VALUES(?,?,?,'received',?,?)"),['python3','-m','unittest','tools/sessionhelper/tests/test_task_flow_r32.py'],'TASK_NOTIFICATION_INJECTED')
    def consumed(s):return change(s,'            db.execute("INSERT OR IGNORE INTO delivery_inbox_read', '            db.execute("UPDATE delivery_inbox SET state=\'processed\' WHERE session_name=? AND delivery_id=?",(self.session_name,key))\n            db.execute("INSERT OR IGNORE INTO delivery_inbox_read')
    add('driver_ack_local_only',inbox,consumed,['python3','-m','unittest','tools/sessionhelper/tests/test_task_flow_r32.py'],'DRIVER_READ_ACK_CONSUMED_DELIVERY')
    add('board_five_second_refresh','internal/m8/console_task_board_page.go',lambda s:change(s,'loadBoard()},5000)','loadBoard()},60000)'),['node','view-service/r32-task-flow-browser-regression.js'],'R32_BOARD_REFRESH_STALLED')
    def early(s):
        start=s.index('func (s *SQLite) CompleteConsoleSpecPresplit');chunk=s[start:]
        injection='''for _,t:=range tasks{if _,e:=tx.ExecContext(ctx,`INSERT INTO console_spec_task(id,spec_id,revision,content_sha256,title,task_content,status,estimated_hours,depends_on_json,acceptance_json,input_anchors_json,created_by,created_at) VALUES(?,?,?,?,?,?,'planned',0,'[]','[]','[]',?,?)`,t.ID,req.SpecID,req.Revision,hash,t.Title,t.Content,req.OwnerPrincipal,now.UTC().Format(time.RFC3339Nano));e!=nil{return e}}
        raw, err := json.Marshal(tasks)'''
        return s[:start]+change(chunk,'raw, err := json.Marshal(tasks)',injection)
    add('owner_confirmation_before_tasks','internal/store/console_spec_presplit.go',early,go('./internal/m8','TestConsoleSpecPresplitAgentIdentityTasksAndFirstResult'),'UNCONFIRMED_PRESPLIT_TASK_CREATED')
    add('candidate_precommit_pg_recheck','internal/store/console_spec_presplit.go',lambda s:change(s,'if err = recheck[0](); err != nil {','if err = recheck[0](); err != nil && false {'),go('./internal/store','TestR32CandidateStageRechecksPGBeforeCommit'),'CANDIDATE_PG_RECHECK_IGNORED')
    add('pg_release_outcome_unknown',hub,lambda s:change(s,'if status >= 500 {','if status >= 999 {'),go('./internal/m8','TestR32PGReleaseFailureDoesNotClaimSQLiteRolledBack'),'PG_RELEASE_FAILURE_CLAIMED_REJECTION')
    add('http_503_outcome_unknown','tools/sessionhelper/sessionhelper/task_flow.py',lambda s:change(s,"if action == 'update' and exc.code >= 500:","if action == 'update' and exc.code >= 999:"),['python3','-m','unittest','tools/sessionhelper/tests/test_task_flow_r32.py'],'HTTP_503_CLAIMED_REJECTION')
    add('owner_explicit_done_reopen',flow,lambda s:change(s,'if status == "done" && !allowReopen {','if status == "done" && !allowReopen && false {'),go('./internal/store','TestR32OwnerExplicitlyReopensCompletedInvalidAssignment'),'DONE_REASSIGNED_WITHOUT_CONFIRMATION')
    add('waiting_page_does_not_starve_delivery',hub,lambda s:change(s,'after = items[len(items)-1].ID','return // withdrawn stable-page advance'),go('./internal/m8','TestR32TaskNotificationWaitingPageDoesNotBlockDelivery'),'WAITING_PAGE_BLOCKED_DELIVERABLE_NOTICE')
    driver='internal/m8/console_task_driver.go'
    def legacy_roles(s):
        s=change(s,'if actor.ID != state.Developer && actor.ID != state.Reviewer {','if false {')
        return change(s,' || (actor.ID != current.Developer && actor.ID != current.Reviewer)','')
    add('legacy_driver_state_fail_closed',driver,lambda s:change(s,'if e != nil {','if e != nil && false {'),go('./internal/m8','TestR32LegacyTaskDriverStateReadFailureCloses'),'LEGACY_DRIVER_STATE_READ_FAILED_OPEN')
    add('legacy_driver_current_actor',driver,legacy_roles,go('./internal/m8','TestR32LegacyTaskDriverReassignmentBetweenReads'),'LEGACY_DRIVER_REASSIGNMENT_LEAKED')
    add('legacy_driver_locked_snapshot',driver,lambda s:change(s,'current, err := flow.ConsoleTaskFlowState(r.Context(), task.ID)','current, err := state, error(nil)'),go('./internal/m8','TestR32LegacyTaskDriverReassignmentBetweenReads'),'LEGACY_DRIVER_REASSIGNMENT_LEAKED')
    add('main_list_current_actor',hub,lambda s:change(s,' || (actor != current.Developer && actor != current.Reviewer)',''),go('./internal/m8','TestR32MainTaskListRechecksCurrentActorAfterReassignment'),'MAIN_LIST_CURRENT_ACTOR_NOT_RECHECKED')
    add('store_list_current_actor',flow,lambda s:change(s,' || (agent != state.Developer && agent != state.Reviewer)',''),go('./internal/store','TestR32TaskListReassignmentBetweenSelectionAndProjection'),'STORE_TASK_LIST_REASSIGNED_ACTOR_LEAKED')
    def store_list_state_error(s):
        begin=s.index('func (s *SQLite) consoleTaskFlowItems');end=s.index('func (s *SQLite) ConsoleTaskFlowStates',begin)
        return s[:begin]+change(s[begin:end],'if e != nil {','if e != nil && false {')+s[end:]
    add('store_list_state_fail_closed',flow,store_list_state_error,go('./internal/store','TestR32TaskListStateReadFailurePropagates'),'STORE_TASK_LIST_STATE_ERROR_NOT_PROPAGATED')
    add('main_list_state_fail_closed',hub,lambda s:change(s,'list, err := repo.ListConsoleTaskFlow(r.Context(), actor)\n\t\tif err != nil {','list, err := repo.ListConsoleTaskFlow(r.Context(), actor)\n\t\tif err != nil && false {'),go('./internal/m8','TestR32MainTaskListStateReadFailureCloses'),'MAIN_LIST_STATE_READ_FAILED_OPEN')
    results=[]
    def execute(name,phase,command):
        p=subprocess.run(command,cwd=work,env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=180)
        (output/(name+'.'+phase+'.stdout')).write_text(p.stdout);(output/(name+'.'+phase+'.stderr')).write_text(p.stderr);(output/(name+'.'+phase+'.rc')).write_text(str(p.returncode)+'\n')
        return p
    for name,file,mutate,command,marker in specs:
        original=(work/file).read_text();mutant=mutate(original)
        positive=execute(name,'positive',command)
        if positive.returncode:raise RuntimeError('positive failed: '+name)
        try:
            (work/file).write_text(mutant);negative=execute(name,'withdrawn',command)
        finally:(work/file).write_text(original)
        restored=execute(name,'restored',command)
        text=negative.stdout+negative.stderr
        ok=negative.returncode!=0 and marker in text and '[build failed]' not in text and restored.returncode==0
        result={'name':name,'file':file,'source_sha256':hashlib.sha256(original.encode()).hexdigest(),'command':command,'expected_marker':marker,'positive_rc':positive.returncode,'withdrawn_rc':negative.returncode,'restored_rc':restored.returncode,'designated_red':ok};results.append(result)
        (output/'WITHDRAWALS.json').write_text(json.dumps({'anchor':anchor,'worktree':str(work),'results':results},indent=2)+'\n')
        print(name+': '+('PASS' if ok else 'FAIL'),flush=True)
        if not ok:raise RuntimeError('withdrawal did not hit designated assertion: '+name)
    print('R32_WITHDRAWALS_OK count='+str(len(results)))
if __name__=='__main__':main()
