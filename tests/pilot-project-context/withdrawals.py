#!/usr/bin/env python3
"""Execute targeted withdrawals in a private Git export, never a running seat."""
import argparse
import io
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import time

ROOT=Path(__file__).resolve().parents[2]
PYTHON='/data/ai-dev-data/helper-deps-lock-venv/bin/python'
GOM8=['go','test','./internal/m8','-count=1','-run']
GOSTORE=['go','test','./internal/store','-count=1','-run']
GOIDENTITY=['go','test','./internal/identity','-count=1','-run']
PYCMD=[PYTHON,'-B','-m','unittest','discover','-s','tools/sessionhelper/tests','-p','test_project_context.py','-v']
# Each case names the intended assertion, not merely a nonzero exit status.
CASES=[
 ('duplicate-auth','internal/m8/project_context.go','len(auths) != 1','len(auths) < 1',GOM8+['^TestProjectContextAuthenticationAndQueryMatrix$'],'PC_AUTH_OR_QUERY_BYPASS'),
 ('cookie-auth','internal/m8/project_context.go','len(r.Header.Values("Cookie")) != 0','false',GOM8+['^TestProjectContextAuthenticationAndQueryMatrix$'],'PC_AUTH_OR_QUERY_BYPASS'),
 ('credential-recheck','internal/identity/project_context.go','subtle.ConstantTimeCompare([]byte(a.Actor.CredentialHash), []byte(hash))','subtle.ConstantTimeCompare([]byte(hash), []byte(hash))',GOIDENTITY+['^TestProjectContextPGAuthorizationLocksAndCurrentState$'],'PC_CREDENTIAL_RECHECK_MISSING'),
 ('pg-agent-lock','internal/identity/project_context.go','FROM np_agent WHERE id=$1 FOR SHARE','FROM np_agent WHERE id=$1',GOIDENTITY+['^TestProjectContextPGAuthorizationLocksAndCurrentState$'],'PC_PG_AUTH_LOCK_NOT_HELD'),
 ('current-role','internal/store/project_context.go','!reflect.DeepEqual(now, old.State)','!reflect.DeepEqual(now, now)',GOSTORE+['^TestProjectContextSQLiteCandidateMetadataAndRoleRace$'],'PC_SQLITE_STALE_CURRENT_ROLE'),
 ('current-revision','internal/store/project_context.go','specID != old.SpecID || revision != old.Revision || sha != old.SHA','false && (specID != old.SpecID || revision != old.Revision || sha != old.SHA)',GOSTORE+['^TestProjectContextSQLiteCandidateMetadataAndRoleRace$'],'PC_SQLITE_STALE_REVISION_PROJECTED'),
 ('directory-privacy','internal/store/project_context.go','active && !hidden','active || hidden',GOM8+['^TestProjectContextNoDirectoryAndCrossOwnerMembership$'],'PC_NODIRECTORY_BYPASS'),
 ('group-pair-authorization','internal/m8/project_context.go','&& a.Allows(identity.ContextClaim{','&& (true || a.Allows(identity.ContextClaim{',GOM8+['^TestProjectContextMovedAgentEmptyAndNoReconciliationWrites$'],'PC_MOVED_AGENT_STILL_READS_TASK'),
 ('online-agent-binding','internal/m8/project_context.go','online[AgentKeyID(peer.OwnerLC)+"\\x00"+peer.NameLC()] != id','false',GOM8+['^TestProjectContextNoDirectoryAndCrossOwnerMembership$'],'PC_ONLINE_IDENTITY_NOT_BOUND'),
 ('needs-reassignment','internal/m8/project_context.go','!state.NeedsReassignment','(true || !state.NeedsReassignment)',GOM8+['^TestProjectContextRevokedAndUnassignedNoData$'],'PC_NEEDS_REASSIGNMENT_LEAK'),
 ('cursor-authority-drift','internal/m8/project_context.go','cursor.Auth != currentDigest','cursor.Auth != cursor.Auth',GOM8+['^TestProjectContextPaginationBindingAndDrift$'],'PC_CURSOR_AUTH_DRIFT_IGNORED'),
 ('cursor-actor-binding','internal/m8/project_context.go','c.Actor != actor','false && c.Actor != actor',GOM8+['^TestProjectContextPaginationBindingAndDrift$'],'PC_CURSOR_ACTOR_UNBOUND'),
 ('cursor-expiry','internal/m8/project_context.go','c.Expires <= h.now().Unix()','false && c.Expires <= h.now().Unix()',GOM8+['^TestProjectContextCursorExpiry$'],'PC_CURSOR_NO_EXPIRY'),
 ('query-upper-limit','internal/m8/project_context.go','n < 1 || n > 100','n < 1 || false',GOM8+['^TestProjectContextParserLimitAndIdentityInputs$'],'PC_QUERY_LIMIT_OR_SELECTOR_GATE'),
 ('body-size-limit','internal/m8/project_context.go','len(output) <= contextMaxBytes','len(output) <= contextMaxBytes*10',GOM8+['^TestProjectContextResponseBudgetAndOversizedItem$'],'PC_BODY_LIMIT_OR_CONTINUATION_MISSING'),
 ('single-item-no-truncation','internal/m8/project_context.go','len(tasks)+len(sessions) <= 1','len(tasks)+len(sessions) <= 0',GOM8+['^TestProjectContextResponseBudgetAndOversizedItem$'],'PC_SINGLE_ENTRY_TRUNCATED_OR_RETURNED'),
 ('pg-final-recheck','internal/m8/project_context.go','return check()','return nil',GOM8+['^TestProjectContextFinalAuthorizationRoundtripFailureClosed$'],'PC_PG_FINAL_RECHECK_IGNORED'),
 ('linked-criteria-only','internal/store/project_context.go','if linked[id.ID] {','if true || linked[id.ID] {',GOSTORE+['^TestProjectContextSQLiteProjectionReadOnlyAndExactRevision$'],'PC_SQLITE_PRIVATE_CRITERION_LEAK'),
 ('http-schema','tools/sessionhelper/sessionhelper/project_context.py',"set(payload)!=expected|{'meta'}", "False and set(payload)!=expected|{'meta'}",PYCMD,'PC_BAD_SCHEMA_ACCEPTED'),
 ('http-no-redirect','tools/sessionhelper/sessionhelper/project_context.py','        return None','        return super().redirect_request(req, fp, code, msg, headers, newurl)',PYCMD,'PC_CREDENTIAL_REDIRECTED'),
 ('dedicated-helper-identity','tools/sessionhelper/sessionhelper/app.py','if not getattr(self, "enroll_credential", None):','if False and not getattr(self, "enroll_credential", None):',PYCMD,'PC_SHARED_CREDENTIAL_FALLBACK'),
 ('legacy-datad-refusal','tools/sessionhelper/sessionhelper/app.py','if urlsplit(path).path in (','if False and urlsplit(path).path in (',PYCMD,'PC_LEGACY_DATAD_CONTEXT_FALLBACK'),
 ('upload-only-isolation','tools/sessionhelper/sessionhelper/ipc_listener.py','if upload_only:','if upload_only and msg_type != "project_context_v1":',PYCMD,'PC_RESTRICTED_MODE_ESCAPED'),
 ('send-only-isolation','tools/sessionhelper/sessionhelper/ipc_listener.py','if send_body_only:','if send_body_only and msg_type != "project_context_v1":',PYCMD,'PC_RESTRICTED_MODE_ESCAPED'),
 ('production-ipc-wiring','tools/sessionhelper/sessionhelper/app.py','project_context_handler=self._make_project_context_handler()','project_context_handler=None',PYCMD,'PC_PRODUCTION_HANDLER_UNWIRED'),
]


def run(commit,out):
    out=Path(out).resolve();out.mkdir(parents=True,exist_ok=True)
    exported=subprocess.check_output(['git','-C',str(ROOT),'archive',commit,'internal','cmd','view-service','tools/sessionhelper','go.mod','go.sum','VERSION'])
    results=[]
    with tempfile.TemporaryDirectory(prefix='pc-withdraw-') as temp:
        root=Path(temp)/'source';root.mkdir()
        with tarfile.open(fileobj=io.BytesIO(exported)) as tar:tar.extractall(root,filter='data')
        home=Path(temp)/'home';home.mkdir(mode=0o700)
        pyenv={'HOME':str(home),'PATH':str(Path(PYTHON).parent)+':/usr/bin:/bin','PYTHONPATH':str(root/'tools/sessionhelper'),'PYTHONDONTWRITEBYTECODE':'1','LANG':'C.UTF-8'}
        goenv=dict(os.environ);goenv.pop('HUB_TEST_PG_DSN',None)
        for name,filename,old,new,cmd,marker in CASES:
            path=root/filename;original=path.read_text()
            if old not in original:raise AssertionError('missing mutation target '+name)
            mutant=original.replace(old,new,1)
            # A complete authorization predicate, rather than a syntactically
            # invalid half-expression, is withdrawn here.
            if name=='group-pair-authorization':
                mutant=mutant.replace('Reviewer: state.Reviewer}) {','Reviewer: state.Reviewer})) {',1)
            if name=='dedicated-helper-identity':
                # The same phrase exists in other handlers; target only the context factory.
                start=original.index('    def _make_project_context_handler(self):')
                mutant=original[:start]+original[start:].replace(old,new,1)
            path.write_text(mutant)
            start=time.time()
            env=pyenv if cmd[0]==PYTHON else goenv
            try:
                p=subprocess.run(cmd,cwd=root,env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=180)
                log=p.stdout;rc=p.returncode
            finally:path.write_text(original)
            (out/(name+'.log')).write_text(log)
            ok=rc!=0 and marker in log and 'build failed' not in log and 'SyntaxError' not in log
            results.append({'name':name,'rc':rc,'marker':marker,'designated_red':ok,'seconds':round(time.time()-start,3)})
            (out/'RESULTS.json').write_text(json.dumps({'commit':commit,'cases':results},indent=2)+'\n')
            print(name,rc,ok,flush=True)
            if not ok:raise AssertionError('withdrawal did not reach designated assertion: '+name)
        for label,cmd,env in [('go-restored',GOM8+['^TestProjectContext'],goenv),('python-restored',PYCMD,pyenv)]:
            p=subprocess.run(cmd,cwd=root,env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=180)
            (out/(label+'.log')).write_text(p.stdout)
            if p.returncode:raise AssertionError('restore failed '+label)
    return results


if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('--commit',required=True);p.add_argument('--output',required=True);args=p.parse_args()
    run(args.commit,args.output)
