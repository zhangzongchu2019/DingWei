#!/usr/bin/env python3
"""Withdraw one named safety boundary in a private Git export; require its assertion."""
import argparse, io, json, os, subprocess, tarfile, tempfile, time
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
PY='/data/ai-dev-data/helper-deps-lock-venv/bin/python'
CASES=[]
def case(name,file,old,new,package,test,marker):
    cmd=([PY,'-B','-m','unittest','discover','-s','tools/sessionhelper/tests','-p','test_task_blocker.py','-v'] if package=='python' else ['go','test',package,'-count=1','-run','^'+test+'$'])
    CASES.append(dict(name=name,edits=[(file,old,new)],argv=cmd,marker=marker))
S='internal/store/task_blocker.go'; M='internal/m8/task_blocker.go'; P='tools/sessionhelper/sessionhelper/task_blocker.py'; J='tools/sessionhelper/sessionhelper/blocker_journal.py'; D='tools/sessionhelper/sessionhelper/driver_adapter.py'
def store(name,old,new,test,marker):case(name,S,old,new,'./internal/store',test,marker)
def helper(name,file,old,new,marker):case(name,file,old,new,'python','',marker)
store('initial-current-state','!reflect.DeepEqual(t, expected)','!reflect.DeepEqual(t, t)','TestBlockerRequestReplayAndRollbackAreAtomic','BLOCKER_INITIAL_STATE_NOT_RECHECKED')
store('card-cas','b.Version != u.ExpectedVersion','b.Version != b.Version','TestBlockerRecheckRequiresCurrentDeveloperVersionAndLease','BLOCKER_CARD_CAS_BYPASS')
store('developer-only-lease','if !actor.developer(t) || b.InputVersion','if false && !actor.developer(t) || b.InputVersion','TestBlockerRecheckRequiresCurrentDeveloperVersionAndLease','BLOCKER_REVIEWER_OPENED_DEVELOPER_LEASE')
store('addressed-reply','if u.Operation == "input" && !(actor.owner(t) || b.WaitingPrincipal == key)','if false && u.Operation == "input" && !(actor.owner(t) || b.WaitingPrincipal == key)','TestBlockerWaiterRightsAndFullMode','BLOCKER_UNADDRESSED_REPLY_ACCEPTED')
store('private-receipt','if !canRead {\n\t\tout.Blocker =','if false && !canRead {\n\t\tout.Blocker =','TestBlockerWaiterRightsAndFullMode','BLOCKER_WAITER_GOT_TASK_CONTEXT')
store('request-payload-binding','if oldHash != payload {','if false && oldHash != payload {','TestBlockerRequestReplayAndRollbackAreAtomic','BLOCKER_REQUEST_HASH_NOT_BOUND')
store('immutable-event','if previous != inputHash {','if false && previous != inputHash {','TestBlockerLifecycleNoImplicitResumeAndNoRepeatedWake','BLOCKER_EVENT_REPLACED')
store('unknown-is-not-pass','if c.Result != "pass" {','if false && c.Result != "pass" {','TestBlockerLifecycleNoImplicitResumeAndNoRepeatedWake','BLOCKER_UNKNOWN_RESOLVED')
store('full-approval','|| (b.Mode == "full" && b.FullApprovalRef == "")','|| (false && b.Mode == "full" && b.FullApprovalRef == "")','TestBlockerWaiterRightsAndFullMode','BLOCKER_FULL_MODE_APPROVAL_BYPASS')
store('lease-identity','|| u.LeaseID != b.LeaseID ||','|| false && u.LeaseID != b.LeaseID ||','TestBlockerRecheckRequiresCurrentDeveloperVersionAndLease','BLOCKER_FORGED_LEASE_ACCEPTED')
store('lease-expiry','|| blockerLeaseExpired(b.LeaseUntil, now) {','|| false && blockerLeaseExpired(b.LeaseUntil, now) {','TestBlockerRecheckRequiresCurrentDeveloperVersionAndLease','BLOCKER_EXPIRED_LEASE_ACCEPTED')
store('concurrent-new-input','if u.InputVersion != b.InputVersion {','if false && u.InputVersion != b.InputVersion {','TestBlockerNewInputDuringRecheckAndAssignmentInvalidate','BLOCKER_NEW_INPUT_SWALLOWED')
store('shared-resume-gate','if n != 0 {','if n < 0 {','TestBlockerLifecycleNoImplicitResumeAndNoRepeatedWake','BLOCKER_LEGACY_RESUME_BYPASS')
store('final-authority-check','if err = recheck(); err != nil {\n\t\treturn out, err\n\t}\n\tif err = tx.Commit();','if err = error(nil); err != nil {\n\t\treturn out, err\n\t}\n\tif err = tx.Commit();','TestBlockerRequestReplayAndRollbackAreAtomic','BLOCKER_FINAL_RECHECK_COMMITTED')
store('legacy-text-preserved','if status == "blocked" && strings.TrimSpace(old) != ""','if false && status == "blocked" && strings.TrimSpace(old) != ""','TestBlockerMultipleCardsLegacyGateAndPreservation','BLOCKER_LEGACY_TEXT_DROPPED')
store('condition-transition','changed := !was && satisfied','changed := false && !was && satisfied','TestBlockerDependencyConditionTransitionCreatesOneInput','BLOCKER_VERIFIED_CONDITION_NOT_WAKING')
store('outbox-repair','if b.State == "ready_to_recheck" || b.State == "rechecking" {\n\t\t\tif err = blockerWake','if false && (b.State == "ready_to_recheck" || b.State == "rechecking") {\n\t\t\tif err = blockerWake','TestBlockerOutboxReconciliationRepairsMissingSignalWithoutNewInput','BLOCKER_OUTBOX_NOT_REPAIRED_IDEMPOTENTLY')
case('owner-cannot-resume','internal/store/console_specs.go','if enrolled > 0 {','if false && enrolled > 0 {','./internal/store','TestBlockerOwnerCannotImpersonateResumeAfterResolution','BLOCKER_OWNER_IMPERSONATED_RESUME')
case('old-driver-gate','internal/store/console_task_driver.go','if unresolved != 0 {','if false && unresolved != 0 {','./internal/store','TestBlockerLegacyDriverStartCannotBypassUnresolvedCards','BLOCKER_LEGACY_DRIVER_START_BYPASS <nil>')
case('current-group-pair',M,'if !a.Allows(claim) {','if false && !a.Allows(claim) {','./internal/m8','TestBlockerCurrentGroupPairRecheckedBeforePrivateRead','BLOCKER_CURRENT_PAIR_GROUP_AUTH_BYPASS')
case('waiter-no-private-get',S,'if !actor.taskReader(t) {','if false && !actor.taskReader(t) {','./internal/m8','TestBlockerHTTPCurrentAgentAndExplicitOutsideWaiter','BLOCKER_WAITER_READ_WHOLE_TASK')
case('pg-human-lock','internal/identity/project_context.go','SELECT state,short_name FROM np_user WHERE user_id_lc=$1 FOR SHARE','SELECT state,short_name FROM np_user WHERE user_id_lc=$1','./internal/m8','TestBlockerPGHumanReplyCommitBeforeDisableAndDisableBeforeReply','BLOCKER_PG_HUMAN_LOCK_NOT_HELD_THROUGH_SQLITE_COMMIT')
case('control-origin','internal/m8/hub.go','&& ctx.Value(blockerDispatchKey{}) != true','&& false && ctx.Value(blockerDispatchKey{}) != true','./internal/m8','TestBlockerOfflineOldHelperAndReliableNonExecutableWake','BLOCKER_FORGED_CONTROL_ROUTED')
case('old-helper-capability',M,'capable := client.blockerNoticeV1 && client.deliveryACKV1','capable := client.deliveryACKV1','./internal/m8','TestBlockerOfflineOldHelperAndReliableNonExecutableWake','BLOCKER_UNKNOWN_CONTROL_SENT_TO_OLD_HELPER')
helper('fixed-prompt',P,"safe['body']=fixed","safe['body']=env['body']",'BLOCKER_REPLY_INJECTED_AS_COMMAND')
helper('metadata-whitelist',P,"safe['meta']={k:m[k] for k in (","safe['meta']={**m, **{k:m[k] for k in (",'BLOCKER_UNTRUSTED_METADATA_COPIED')
# Complete the intentionally expanded dictionary expression; never count a syntax failure.
CASES[-1]['edits'].append((P,"if k in m}\n    safe['meta'].update", "if k in m}}\n    safe['meta'].update"))
helper('strict-version-type',P,'type(meta.get(field)) is not int','not isinstance(meta.get(field),int)','BLOCKER_BAD_SIGNAL_TYPE_ACCEPTED')
helper('current-recipient',P,"or b.get('developer_agent_id')!=agent_id", "or False and b.get('developer_agent_id')!=agent_id",'BLOCKER_STALE_WAKE_ENQUEUED')
helper('helper-full-approval',P,"if b.get('mode')=='full' and not b.get('full_approval_ref'):","if False and b.get('mode')=='full' and not b.get('full_approval_ref'):",'BLOCKER_FULL_GATE_BYPASS')
helper('pending-restart',J,"state IN('pending','dispatching','admitted')","state IN('dispatching','admitted')",'BLOCKER_PENDING_WAKE_LOST_ON_RESTART')
helper('dispatch-journal',J,"SET state='dispatching',updated_at", "SET state='pending',updated_at",'BLOCKER_UNKNOWN_DISPATCH_REPLAYED')
helper('ambiguous-no-rpc-replay',D,'receipt=self.driver.poll_receipt(command.delivery_id)','receipt=self.driver.deliver(command.delivery_id,command.text)','BLOCKER_UNKNOWN_WINDOW_OPENED_NEW_TURN')
helper('transient-defer-same-command',D,'self._commands.defer_current(command.delivery_id);self._bridge_sleep(0.5);return','self._bridge_sleep(0.5);return','BLOCKER_TRANSIENT_FAILURE_DROPPED_WAKE')

case('owner-old-plan-paired','internal/store/console_specs.go','if err = taskBlockersClear(ctx, tx, proposalID); err != nil {','if err = error(nil); err != nil {','./internal/store','TestBlockerMultipleCardsLegacyGateAndPreservation','BLOCKER_OWNER_PLAN_BYPASS <nil>')
CASES[-1]['edits'].append(('internal/store/console_specs.go','if enrolled > 0 {','if false && enrolled > 0 {'))
store('resolved-does-not-resume','if pass {\n\t\t\t\tb.State = "resolved"','if pass {\n\t\t\t\t_, _ = tx.ExecContext(ctx, `UPDATE console_spec_task SET status=\'in_progress\' WHERE id=?`, t.ID)\n\t\t\t\tb.State = "resolved"','TestBlockerLifecycleNoImplicitResumeAndNoRepeatedWake','BLOCKER_RECHECK_AUTO_RESUMED')

store('get-no-business-write','if err = tx.Commit(); err != nil {\n\t\treturn nil, nil, err\n\t}\n\treturn cards, inputs, nil','if _, err = tx.ExecContext(ctx, `UPDATE console_task_blocker SET version=version+1 WHERE task_id=?`, t.ID); err != nil {return nil,nil,err}\n\tif err = tx.Commit(); err != nil {\n\t\treturn nil, nil, err\n\t}\n\treturn cards, inputs, nil','TestBlockerReadOnlySnapshotAndBoundedInputHistory','BLOCKER_GET_WROTE_STATE')
CASES[-1]['edits'].append((S,'&sql.TxOptions{ReadOnly: true}','&sql.TxOptions{ReadOnly: false}'))
helper('notice-mode-fence','tools/sessionhelper/sessionhelper/app.py','if self.cfg.mode!="driver" or not m["automatic_recheck"] or not getattr(self,"enroll_credential",None) or getattr(self.cfg,"ipc_upload_only",False) or getattr(self.cfg,"ipc_send_body_only",False):','if False:','BLOCKER_NOTICE_INJECTED_TO_LEGACY_OR_RESTRICTED_MODE')

def run(args):
    args.commit=subprocess.check_output(['git','-C',str(ROOT),'rev-parse',args.commit+'^{commit}'],text=True).strip()
    chosen=[c for c in CASES if not args.only or c['name'] in args.only]
    if args.only and set(args.only)-{c['name'] for c in CASES}:raise ValueError('unknown withdrawal')
    out=Path(args.output).resolve();out.mkdir(parents=True,exist_ok=True)
    archive=subprocess.check_output(['git','-C',str(ROOT),'archive',args.commit,'internal','cmd','view-service','tools/sessionhelper','tools/pgtest','tests/pilot-project-context','tests/pilot-blocker-wake','go.mod','go.sum','VERSION'])
    results=[]
    with tempfile.TemporaryDirectory(prefix='blocker-withdraw-') as temp:
        root=Path(temp)/'source';root.mkdir();home=Path(temp)/'home';home.mkdir(mode=0o700)
        (home/'.embedded-postgres-go').symlink_to('/home/dev/.embedded-postgres-go',target_is_directory=True)
        with tarfile.open(fileobj=io.BytesIO(archive)) as tar:tar.extractall(root,filter='data')
        env={'HOME':str(home),'PATH':str(Path(PY).parent)+':/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin','PYTHONPATH':str(root/'tools/sessionhelper'),'PYTHONDONTWRITEBYTECODE':'1','LANG':'C.UTF-8','TZ':'UTC','GOCACHE':'/home/dev/.cache/go-build','GOMODCACHE':'/home/dev/go/pkg/mod','GOPATH':'/home/dev/go','GOPROXY':'off','GOSUMDB':'off','DW_DESKTOP_INTEGRATION_PYTHON':PY}
        baseline=[('go',['go','test','./internal/store','./internal/m8','-count=1','-timeout=5m','-run','^TestBlocker']),('helper',CASES[-1]['argv'])]
        def execute(label,cmd):
            start=time.time();p=subprocess.run(cmd,cwd=root,env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=360)
            (out/(label+'.log')).write_text(p.stdout);return p,round(time.time()-start,3)
        for label,cmd in baseline:
            p,_=execute('baseline-'+label,cmd)
            if p.returncode:raise AssertionError('baseline failed '+label)
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
            ok=p.returncode!=0 and c['marker'] in p.stdout and not any(s in p.stdout for s in ('build failed','SyntaxError','undefined:'))
            results.append({**c,'rc':p.returncode,'designated_red':ok,'seconds':seconds})
            (out/'RESULTS.json').write_text(json.dumps({'commit':args.commit,'cases':results},indent=2)+'\n')
            print(c['name'],p.returncode,ok,flush=True)
            if not ok:raise AssertionError('not designated red: '+c['name'])
        for label,cmd in baseline:
            p,_=execute('restored-'+label,cmd)
            if p.returncode:raise AssertionError('restore failed '+label)
if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('--commit',required=True);p.add_argument('--output',required=True);p.add_argument('--only',nargs='*');run(p.parse_args())
