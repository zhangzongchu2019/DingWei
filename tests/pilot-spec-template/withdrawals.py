#!/usr/bin/env python3
"""Withdraw one real guard at a time; require designated behavioral red, restore."""
import argparse,io,json,subprocess,tarfile,tempfile,time
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2];PY='/data/ai-dev-data/helper-deps-lock-venv/bin/python';CASES=[]
S='internal/store/pilot_template_source.go';D='internal/store/pilot_template_draft.go';V='internal/store/pilot_template_validation.go';R='internal/store/pilot_template_receipt.go';C='internal/store/pilot_template_confirm.go';G='internal/store/pilot_template_gate.go';M='internal/m8/pilot_template.go';P='tools/sessionhelper/sessionhelper/pilot_template.py'
def case(name,file,old,new,test,marker,pkg='./internal/store',last=False):
 cmd=[PY,'-B','-m','unittest','discover','-s','tools/sessionhelper/tests','-p','test_pilot_template.py','-v'] if pkg=='python' else ['go','test',pkg,'-count=1','-run','^'+test+'$','-timeout=2m']
 CASES.append(dict(name=name,file=file,old=old,new=new,argv=cmd,marker=marker,last=last))
case('source-request-immutable',S,'if hash != payload {','if false && hash != payload {','TestPilotTemplateSourceProvenanceDedupeAndSSRF','TEMPLATE_SOURCE_REQUEST_REBOUND')
case('source-owner-only',S,'id, owner))','id, "owner-a"))','TestPilotTemplateSourceProvenanceDedupeAndSSRF','TEMPLATE_SOURCE_CROSS_OWNER_READ')
case('source-https-only','internal/store/pilot_template_types.go','u.Scheme != "https"','false && u.Scheme != "https"','TestPilotTemplateSourceProvenanceDedupeAndSSRF','TEMPLATE_SSRF_SOURCE_ACCEPTED')
case('source-tenant-suffix','internal/store/pilot_template_types.go','!(strings.HasSuffix(host, ".feishu.cn") || strings.HasSuffix(host, ".larksuite.com"))','false','TestPilotTemplateSourceProvenanceDedupeAndSSRF','TEMPLATE_SSRF_SOURCE_ACCEPTED')
case('six-fields-not-unknown',V,'if templateUnknown(field.v) || len(field.v) > 8192 {','if false {','TestPilotTemplateMissingSixFieldsAndIdentity/method','TEMPLATE_MISSING_NOT_EXPLICIT')
case('no-agent-business-responsibility',V,'if !strings.HasPrefix(f.Signoff, "user:") || !templatePrincipal(f.Signoff) {','if false {','TestPilotTemplateMissingReviewOrAgentSignoffRejected','TEMPLATE_INDEPENDENT_REVIEW_OR_HUMAN_SIGNOFF_BYPASSED')
case('independent-review-required',V,'if !templatePrincipal(f.Reviewer) || d.TeamMode == "single" && !strings.HasPrefix(f.Reviewer, "agent:") {','if false {','TestPilotTemplateMissingReviewOrAgentSignoffRejected','TEMPLATE_INDEPENDENT_REVIEW_OR_HUMAN_SIGNOFF_BYPASSED')
case('self-review-rejected',V,'if f.Executor == f.Reviewer {','if false {','TestPilotTemplateMissingReviewOrAgentSignoffRejected','TEMPLATE_INDEPENDENT_REVIEW_OR_HUMAN_SIGNOFF_BYPASSED')
case('stock-budget-preserved',V,'e.Kind != "case" || e.BusinessWriteLimit != want.Writes','e.Kind != "case"','TestPilotTemplateRiskUpgradeUnknownAndStockBudget','TEMPLATE_STOCK_TAMPERED_BUDGET_ACCEPTED')
case('no-owner-proxy-receipt',R,'role.Principal == a.Key()','true','TestPilotTemplateReceiptCannotProxyAndWithdrawal','TEMPLATE_OWNER_PROXY_ACCEPTED')
case('incomplete-contract-no-consent',R,'if u.Decision == "accepted" {','if false {','TestPilotTemplateConsentCannotAttachIncompleteContract','TEMPLATE_INCOMPLETE_CONTRACT_ACCEPTED')
case('receipt-plan-cas',R,'u.ExpectedVersion != r.Version || u.PlanSHA != view.PlanSHA','false','TestPilotTemplateRevisionInvalidatesReceiptsAndKeepsOldBody','TEMPLATE_LATE_PLAN_RECEIPT_ACCEPTED')
case('full-human-review-receipt',R,'a.UserID == "" || !u.ConfirmHuman || templateUnknown(u.Reason)','a.UserID == "" || templateUnknown(u.Reason)','TestPilotTemplateFullRequiresScopedHumanReceipt','TEMPLATE_FULL_HUMAN_CHECKBOX_BYPASSED')
case('preview-current-role-only',D,'if !templateActorAllowed(a, ref) {','if false {','TestPilotTemplateHTTPPrivateInvitationsAndHumanOnlyConfirmation','TEMPLATE_FOREIGN_INVITATION_LEAK','./internal/m8')
case('parent-final-only',D,'parentState != "final" ||','false ||','TestPilotTemplateParentMustBeFinalAndCurrent','TEMPLATE_MUTABLE_PARENT_ACCEPTED')
case('parent-sha-pinned',D,'sha != d.Parent.SHA ||','false ||','TestPilotTemplateParentMustBeFinalAndCurrent','TEMPLATE_PARENT_SHA_DRIFT_ACCEPTED')
case('draft-final-pg-check',D,'if err = check(); err != nil {','if err = error(nil); err != nil {','TestPilotTemplateCheckFailureRollsBack','TEMPLATE_REVOKE_CHECK_IGNORED',last=True)
case('confirm-final-pg-check',C,'if err = check(); err != nil {','if err = error(nil); err != nil {','TestPilotTemplateConfirmRollbackAndIdempotence','TEMPLATE_CONFIRM_REVOKE_IGNORED',last=True)
case('no-receipt-transfer-between-versions',D,'id, ref.Version, role.EntryID, role.Role, role.Principal','id, 1, role.EntryID, role.Role, role.Principal','TestPilotTemplateRevisionInvalidatesReceiptsAndKeepsOldBody','TEMPLATE_OLD_RECEIPT_REUSED')
case('sticky-template-enrollment',G,'if errors.Is(err, sql.ErrNoRows) {\n\t\treturn ErrConsoleSpecGate','if errors.Is(err, sql.ErrNoRows) {\n\t\treturn nil','TestPilotTemplateRealFinalSuccessorAndStickyEnrollment','TEMPLATE_ENROLLMENT_DROPPED_ON_LEGACY_REVISION')
case('actual-executor-cannot-review',G,'if entry.Fields.Executor == "agent:"+agent {','if false {','TestPilotTemplateActualExecutorCannotPerformIndependentReview','TEMPLATE_ACTUAL_EXECUTOR_ACCEPTED_AS_INDEPENDENT_REVIEW')
case('impact-blocks-old-execution',G,'if impacts > 0 {','if false {','TestPilotTemplateTaskAssignmentAndImpactGuard','TEMPLATE_OLD_SCOPE_EXECUTION_NOT_BLOCKED')
case('group-required-for-template-start',G,'if len(pair) != 2 && group == "" {','if false {','TestPilotTemplateLegacyTaskPathsCannotSkipCurrentContract','TEMPLATE_LEGACY_STATUS_BYPASSED_GROUP_AUTH')
case('task-pair-matches-accepted-roles',G,'if !matches(entry.Fields.Executor, dev) || !matches(entry.Fields.Reviewer, review) {','if false && (!matches(entry.Fields.Executor, dev) || !matches(entry.Fields.Reviewer, review)) {','TestPilotTemplateTaskAssignmentAndImpactGuard','TEMPLATE_TASK_PAIR_DIFFERS_FROM_ACCEPTED_ROLES')
case('create-cannot-implicitly-start','internal/store/console_specs.go','templateEnrolled > 0 && proposal.Status != "planned" && proposal.Status != "proposed"','false && templateEnrolled > 0 && proposal.Status != "planned" && proposal.Status != "proposed"','TestPilotTemplateLegacyTaskPathsCannotSkipCurrentContract','TEMPLATE_CREATE_IMPLICITLY_STARTED_TASK')
case('driver-rechecks-template','internal/store/console_task_driver.go','for _, id := range ids {\n\t\tif e = templateTaskGate(ctx, tx, id, start.AnnouncedAt); e != nil {','for _, id := range ids {\n _ = id\n\t\tif e = error(nil); e != nil {','TestPilotTemplateLegacyTaskPathsCannotSkipCurrentContract','TEMPLATE_LEGACY_DRIVER_BYPASSED_GROUP_AUTH')
case('d3-unchanged-copy-not-new-contract','internal/store/pilot_question.go','u.NewRevision < 2','u.NewRevision < 1','TestPilotTemplateD3ConfirmsActualDerivedSuccessor','TEMPLATE_D3_UNCHANGED_DERIVED_COPY_ACCEPTED')
case('helper-strict-integer-version',P,"type(body['expected_version']) is not int","not isinstance(body['expected_version'],int)",'','TEMPLATE_MCP_PLAN_CAS_WEAK','python')
case('helper-no-human-operation',P,"body['role'] not in ('executor','reviewer')","body['role'] not in ('executor','reviewer','signoff','full_review')",'','TEMPLATE_MCP_HUMAN_BOUNDARY','python')
case('helper-query-disjoint',P,"if not draft or body:raise ValueError('invalid template query')","if not draft:raise ValueError('invalid template query')",'','TEMPLATE_MCP_READ_SCOPE_WEAK','python')
# A real PG lock schedule, rather than sleep-and-assume concurrency evidence.
case('pg-current-actor-row-lock','internal/identity/project_context.go','FROM np_agent WHERE id=$1 FOR SHARE','FROM np_agent WHERE id=$1','TestPilotTemplatePGCurrentPrincipalAndRoleLocks','TEMPLATE_PG_AUTH_LOCK_NOT_HELD','./internal/identity')

def run(args):
 commit=subprocess.check_output(['git','rev-parse',args.commit+'^{commit}'],cwd=ROOT,text=True).strip();out=Path(args.output).resolve();out.mkdir(parents=True,exist_ok=True)
 chosen=[c for c in CASES if not args.only or c['name'] in args.only]
 if args.only and set(args.only)-{c['name'] for c in CASES}:raise ValueError('unknown case')
 archive=subprocess.check_output(['git','archive',commit],cwd=ROOT)
 with tempfile.TemporaryDirectory(prefix='template-withdraw-') as temp:
  root=Path(temp)/'source';root.mkdir();home=Path(temp)/'home';home.mkdir(mode=0o700);(home/'.embedded-postgres-go').symlink_to('/home/dev/.embedded-postgres-go',target_is_directory=True)
  with tarfile.open(fileobj=io.BytesIO(archive)) as tar:tar.extractall(root,filter='data')
  env={'HOME':str(home),'PATH':str(Path(PY).parent)+':/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin','PYTHONPATH':str(root/'tools/sessionhelper'),'PYTHONDONTWRITEBYTECODE':'1','LANG':'C.UTF-8','TZ':'UTC','GOCACHE':'/home/dev/.cache/go-build','GOMODCACHE':'/home/dev/go/pkg/mod','GOPATH':'/home/dev/go','GOPROXY':'off','GOSUMDB':'off','DW_DESKTOP_INTEGRATION_PYTHON':PY}
  baseline=[('go',['go','test','./internal/store','./internal/m8','./internal/identity','-count=1','-timeout=3m','-run','^TestPilotTemplate']),('helper',[PY,'-B','-m','unittest','discover','-s','tools/sessionhelper/tests','-p','test_pilot_template.py','-v'])]
  def execute(label,cmd):
   start=time.time();p=subprocess.run(cmd,cwd=root,env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=300);(out/(label+'.log')).write_text(p.stdout);return p,round(time.time()-start,3)
  for label,cmd in baseline:
   p,_=execute('baseline-'+label,cmd)
   if p.returncode:raise AssertionError('baseline failed '+label)
  results=[]
  for c in chosen:
   target=root/c['file'];saved=target.read_text()
   if c['old'] not in saved:raise AssertionError('missing target '+c['name'])
   mutated=c['new'].join(saved.rsplit(c['old'],1)) if c['last'] else saved.replace(c['old'],c['new'],1)
   try:
    target.write_text(mutated);p,seconds=execute(c['name'],c['argv'])
   finally:target.write_text(saved)
   red=p.returncode!=0 and c['marker'] in p.stdout and not any(x in p.stdout for x in ('build failed','SyntaxError','undefined:'))
   results.append({**c,'rc':p.returncode,'designated_red':red,'seconds':seconds});(out/'RESULTS.json').write_text(json.dumps({'commit':commit,'cases':results},indent=2)+'\n');print(c['name'],p.returncode,red,flush=True)
   if not red:raise AssertionError('not designated red '+c['name'])
  for label,cmd in baseline:
   p,_=execute('restored-'+label,cmd)
   if p.returncode:raise AssertionError('restoration failed '+label)
if __name__=='__main__':
 p=argparse.ArgumentParser();p.add_argument('--commit',required=True);p.add_argument('--output',required=True);p.add_argument('--only',nargs='*');run(p.parse_args())
