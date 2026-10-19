#!/usr/bin/env python3
"""Real isolated R8 source withdrawals; each designated red must restore green."""
import argparse,hashlib,json,os,subprocess,tempfile
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
def change(s,a,b):
    if a not in s:raise RuntimeError('withdrawal anchor missing: '+a[:100])
    return s.replace(a,b,1)
def section(s,start,end,fn):
    a=s.index(start);b=s.index(end,a);return s[:a]+fn(s[a:b])+s[b:]
def go(pkg,test):return ['go','test',pkg,'-run','^'+test+'$','-count=1','-timeout=2m']
def main():
    parser=argparse.ArgumentParser();parser.add_argument('--anchor',default='HEAD');parser.add_argument('--output',required=True);args=parser.parse_args()
    anchor=subprocess.check_output(['git','rev-parse',args.anchor],cwd=ROOT,text=True).strip();out=Path(args.output).resolve();out.mkdir(parents=True,exist_ok=True)
    work=Path(tempfile.mkdtemp(prefix='r8-withdraw-'));subprocess.run(['git','worktree','add','--detach',str(work),anchor],cwd=ROOT,check=True,stdout=subprocess.DEVNULL)
    env=dict(os.environ);env['PYTHONPATH']=str(work/'tools/sessionhelper');env.setdefault('NODE_PATH','/tmp/coder-a3-playwright/node_modules');env.setdefault('PW_CHANNEL','chrome')
    store='internal/store/console_spec_agent_review.go';hub='internal/m8/console_spec_agent_review.go';sql='internal/store/migrations/0056_console_spec_agent_review_r32.sql';helper='tools/sessionhelper/sessionhelper/spec_review.py'
    tests=[]
    def add(name,file,fn,cmd,marker):tests.append((name,file,fn,cmd,marker))
    def sg(name):return go('./internal/store',name)
    def hg(name):return go('./internal/m8',name)
    py=['python3','-m','unittest','tools/sessionhelper/tests/test_spec_review_r32.py']
    complete=lambda s,fn:section(s,'func (s *SQLite) CompleteSpecAgentRequest','func (s *SQLite) FailSpecAgentRequest',fn)
    read=lambda s,fn:section(s,'func (h *Hub) readSpecAgentAuthorized','func (h *Hub) HandleAgentSpecReview',fn)
    def unbind(s):
        return complete(s,lambda t:change(change(t,'if !sameSpecAgent(q, a) {','if !sameSpecAgent(q, a) && false {'),'if n != 1 {','if n != 1 && false {'))
    add('callback_identity_pair',store,unbind,sg('TestR8ActualReviewerPendingHumanGateAndReplay'),'R8_FORGED_CALLBACK_ACCEPTED')
    add('pending_outcome',store,lambda s:change(s,'Outcome: "pending"','Outcome: "false_positive"'),sg('TestR8ActualReviewerPendingHumanGateAndReplay'),'R8_PENDING_PROVENANCE_LOST')
    add('human_issue_text_immutable',store,lambda s:change(s,'if a.ID != o.ID || a.Text != o.Text {','if (a.ID != o.ID || a.Text != o.Text) && false {'),sg('TestR8ActualReviewerPendingHumanGateAndReplay'),'R8_HUMAN_REPLACED_ISSUE')
    add('database_lease_write_family',sql,lambda s:s.replace('WHEN EXISTS(SELECT 1 FROM console_spec_agent_request','WHEN 0 AND EXISTS(SELECT 1 FROM console_spec_agent_request'),sg('TestR8PendingLockCoversAllStoreWrites'),'R8_LOCK_BYPASSED')
    add('one_active_request',sql,lambda s:change(s,'CREATE UNIQUE INDEX console_spec_agent_active','CREATE INDEX console_spec_agent_active'),sg('TestR8PendingLockCoversAllStoreWrites'),'R8_PARALLEL_REQUEST_ACCEPTED')
    add('deadline',store,lambda s:change(s,'AND deadline<=?`','AND deadline<=? AND 0`'),sg('TestR8DeadlineRestartAndIdentityPrecommitFailClosed'),'R8_TIMEOUT_REPLY_ACCEPTED')
    add('restart',store,lambda s:change(s,'AND boot<>?`','AND boot<>? AND 0`'),sg('TestR8DeadlineRestartAndIdentityPrecommitFailClosed'),'R8_RESTART_REPLY_ACCEPTED')
    add('pg_precommit_recheck',store,lambda s:complete(s,lambda t:change(t,'if check != nil {\n\t\tif e = check();','if check != nil && false {\n\t\tif e = check();')),sg('TestR8DeadlineRestartAndIdentityPrecommitFailClosed'),'R8_PRECOMMIT_ERROR_IGNORED')
    add('cancelled_reply',store,lambda s:complete(s,lambda t:change(t,'if q.Status != "pending" {','if q.Status != "pending" && false {')),sg('TestR8CancelledSameEditorLateReply'),'R8_CANCELLED_LATE_RESULT_ACCEPTED')
    add('criteria_awaits_confirmation',store,lambda s:change(s,'q.Status = "awaiting_confirmation"','q.Status = "completed"'),sg('TestR8CriteriaCandidateRequiresConfirmationStatus'),'R8_CANDIDATE_NOT_PENDING_CONFIRMATION')
    add('owner_quote',store,lambda s:change(s,'!strings.Contains(snap.Content, c.OwnerRef)','(!strings.Contains(snap.Content, c.OwnerRef) && false)'),sg('TestR8CriteriaRejectsQuotesAndSpoofedConfirmation'),'R8_INVALID_CRITERIA_ACCEPTED')
    add('checked_by_not_agent_supplied',store,lambda s:change(s,'c.CheckedBy != ""','(c.CheckedBy != "" && false)'),sg('TestR8CriteriaRejectsQuotesAndSpoofedConfirmation'),'R8_INVALID_CRITERIA_ACCEPTED')
    add('result_revision_binding',store,lambda s:change(s,'r.Revision != q.Revision || r.SHA != q.SHA','(r.Revision != q.Revision && false) || r.SHA != q.SHA'),sg('TestR8CriteriaRejectsQuotesAndSpoofedConfirmation'),'R8_INVALID_CRITERIA_ACCEPTED')
    add('result_hash_binding',store,lambda s:change(s,'r.Revision != q.Revision || r.SHA != q.SHA','r.Revision != q.Revision || (r.SHA != q.SHA && false)'),sg('TestR8CriteriaRejectsQuotesAndSpoofedConfirmation'),'R8_INVALID_CRITERIA_ACCEPTED')
    add('criteria_result_kind',store,lambda s:change(s,'if r.Issues != nil || len(r.Criteria)','if (r.Issues != nil && false) || len(r.Criteria)'),sg('TestR8CriteriaRejectsQuotesAndSpoofedConfirmation'),'R8_INVALID_CRITERIA_ACCEPTED')
    add('fresh_gate_required',sql,lambda s:change(s,"WHEN NEW.state='final'","WHEN 0 AND NEW.state='final'"),sg('TestR8CriteriaConfirmationFreshRevisionAndOldGateHistory'),'R8_FRESH_REVIEW_BYPASSED')
    add('fresh_gate_descendant',sql,lambda s:change(s,'WHEN EXISTS(SELECT 1 FROM console_spec_agent_fresh f WHERE f.spec_id=NEW.spec_id','WHEN 0 AND EXISTS(SELECT 1 FROM console_spec_agent_fresh f WHERE f.spec_id=NEW.spec_id'),sg('TestR8FreshReviewRequirementFollowsHumanRevision'),'R8_FRESH_REVIEW_LOST_ON_EDIT')
    add('human_confirmation_even_legacy_acceptance',sql,lambda s:change(s,"AND g.human_confirmed=1 AND q.status='completed'","AND q.status='completed'"),sg('TestR8ActualReviewerPendingHumanGateAndReplay'),'R8_PENDING_FINALIZED')
    add('owned_agent_pg', 'internal/identity/spec_review_authorization.go',lambda s:change(s,'a.OwnerLC != Lower(owner)','(a.OwnerLC != Lower(owner) && false)'),go('./internal/identity','TestR8OwnedAgentPGAuthorizationAndRevocationLock'),'R8_CROSS_OWNER_PG_AUTHORIZED')
    add('http_callback_current_actor',hub,lambda s:change(s,'if q.AgentID != actor {','if q.AgentID != actor && false {'),hg('TestR8HTTPRealReliableReviewAndHumanOnlyFinalize'),'R8_FORGED_HTTP_CALLBACK_ACCEPTED')
    add('ws_callback_endpoint',hub,lambda s:change(s,'q.KeyID != c.keyID || q.SessionName != c.sessionName || ',''),hg('TestR8WSCallbackUsesActualAuthenticatedEndpoint'),'R8_FORGED_WS_CALLBACK_ACCEPTED')
    add('read_cancel_current_state',hub,lambda s:read(s,lambda t:change(t,'current.Status == "cancelled" || ','')),hg('TestR8HTTPReadErrorsAndCancellationBetweenSelectionAndProjection'),'R8_READ_RACE_LEAKED_REQUEST')
    add('read_state_error_fail_closed',hub,lambda s:read(s,lambda t:change(t,'if e != nil {','if e != nil && false {')),hg('TestR8HTTPReadErrorsAndCancellationBetweenSelectionAndProjection'),'R8_READ_RACE_LEAKED_REQUEST')
    add('helper_system_origin',helper,lambda s:change(s," or env.get('from') != 'workpulse#' + key_id",''),py,'R8_FORGED_HELPER_REQUEST_ACCEPTED')
    def raw_prompt(s):
        a=s.index("    out['body'] = (");b=s.index('    return out',a)
        return s[:a]+"    out['body'] = env['body']\n"+s[b:]
    add('helper_no_raw_spec_prompt',helper,raw_prompt,py,'R8_RAW_SPEC_INJECTED')
    add('helper_no_redirect',helper,lambda s:change(s,'build_opener(NoRedirect())','build_opener(urllib.request.HTTPRedirectHandler())'),py,'R8_CREDENTIAL_REDIRECTED')
    add('helper_uncertain_reply',helper,lambda s:change(s,"'outcome':'unknown' if method=='POST' else 'failed'","'outcome':'rejected'"),py,'R8_UNCERTAIN_REPLY_FALSE_REJECTION')
    records=[]
    try:
        for name,file,mutate,command,marker in tests:
            case=out/name;case.mkdir(exist_ok=True);p=work/file;original=p.read_bytes();changed=mutate(original.decode())
            if changed==original.decode():raise RuntimeError('mutation made no change: '+name)
            phases={}
            for phase in ('positive','withdrawn','restored'):
                p.write_bytes(changed.encode() if phase=='withdrawn' else original)
                result=subprocess.run(command,cwd=work,env=env,capture_output=True,text=True,timeout=180)
                (case/(phase+'.stdout')).write_text(result.stdout);(case/(phase+'.stderr')).write_text(result.stderr);(case/(phase+'.rc')).write_text(str(result.returncode)+'\n')
                text=result.stdout+'\n'+result.stderr
                valid=(result.returncode==0) if phase!='withdrawn' else (result.returncode!=0 and marker in text and not any(bad in text for bad in ('[build failed]','undefined:','SyntaxError','ImportError')))
                phases[phase]={'rc':result.returncode,'valid':valid,'marker_present':marker in text}
                if not valid:
                    p.write_bytes(original);raise RuntimeError(name+' phase '+phase+' did not satisfy designated gate; see '+str(case))
            assert p.read_bytes()==original
            records.append({'name':name,'source_file':file,'source_sha256':hashlib.sha256(original).hexdigest(),'mutation_sha256':hashlib.sha256(changed.encode()).hexdigest(),'command':command,'marker':marker,'phases':phases})
            (out/'WITHDRAWALS.json').write_text(json.dumps({'anchor':anchor,'count':len(records),'expected':len(tests),'records':records},indent=2)+'\n')
            print(name+' GREEN_RED_RESTORED',flush=True)
    finally:
        # Keep worktree and raw failures for audit; no source exchange or cleanup.
        (out/'WORKTREE.txt').write_text(str(work)+'\n')
    print('R8_WITHDRAWALS_OK count='+str(len(records)),flush=True)
if __name__=='__main__':main()
