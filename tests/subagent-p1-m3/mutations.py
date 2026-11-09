"""AA010 one guard removed per criterion; copies/Go overlays only, no tree edits."""
import argparse,datetime,json,os,subprocess,tempfile,shutil
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
GO=[
 ('legacy-text-root-only','internal/m8/newplat_directory.go','strings.Count(item.Endpoint.FullSessionName, ".") <= 1','true','./internal/m8','TestSubAgentViewTreeLegacyAndFeatureOff','AA010_LEGACY_ROOT_ONLY'),
 ('input-sidecar','internal/m8/terminal.go','keyID, sessionName = h.subViewObservation(keyID, sessionName, deliveryID, role == "copy")','// removed sidecar routing','./internal/m8','TestSubAgentViewSidecarObservation','AA010_INPUT_SIDECAR_SCOPE'),
 ('new-new-negotiation','internal/m8/sub_agent.go','capable, ready := h.subAgentCapable(c)','_, ready := h.subAgentCapable(c); capable:=false','./internal/m8','TestSubAgentViewNewHubMixedMatrix/true','AA010_NEW_NEW_DELIVERY'),
 ('root-authority','internal/m8/sub_agent_view.go','if !ok {\n\t\t\tcontinue\n\t\t}','if !ok { root,_=m.h.projectionViewAgent(ctx,viewRoot(name)) }','./internal/m8','TestSubAgentViewAuthorizationBudgetAndRevocation','AA010_UNAUTHORIZED_CHILD_ENUMERATION'),
 ('root-mention-only','internal/m8/newplat_mention.go','out := make([]candidate, 0, len(cands))','out := make([]candidate, 0, len(cands));out=append(out,candidate{FullName:"aa."+viewRoot(r.PathValue("sessionName"))})','./internal/m8','TestSubAgentViewMentionRootsOnly','AA010_MENTION_ROOT_ONLY'),
 ('scope-generation','internal/m8/sub_agent_view.go','strconv.FormatInt(generation, 10)','"1"','./internal/m8','TestSubAgentViewScopeAndEvents','AA010_SCOPE_IDENTITY'),
 ('event-generation','internal/m8/sub_agent_view.go','scope.Generation != subAgentInt64(env.Meta, "from_generation")','false','./internal/m8','TestSubAgentViewScopeAndEvents','AA010_FORGED_EVENT_REACHED_PROJECTION'),
 ('scope-budget','internal/m8/console_mux.go','len(req.Subscriptions) > 8','len(req.Subscriptions) > 80','./internal/m8','TestSubAgentViewAuthorizationBudgetAndRevocation','AA010_REAL_SCOPE_BUDGET'),
 ('root-revoke','internal/m8/console_mux.go','s.Agent.ID == agentID || s.Agent.RootID == agentID','s.Agent.ID == agentID','./internal/m8','TestSubAgentViewAuthorizationBudgetAndRevocation','AA010_ROOT_REVOKE_WHOLE_TREE'),
 ('ticket-node','internal/m8/console_mux_subject.go','subject["node_id"] = a.NodeID','subject["node_id"] = "forged"','./internal/m8','TestSubAgentViewSignedSubject','AA010_SIGNED_SCOPE_BINDING'),
 ('view-revoke','internal/m8/sub_agent_view.go','if e != nil || user.UserIDLC() != a.Owner {\n\t\t\treturn false','if false && (e != nil || user.UserIDLC() != a.Owner) {\n\t\t\treturn false','./internal/m8','TestSubAgentViewSignedSubject','AA010_VIEW_EXPIRED_REVOKED_SUBTREE'),
 ('tree-ended-online','internal/m8/sub_agent_view.go','c != nil && capable && ready && n.State == "active"','c != nil && capable && ready','./internal/m8','TestSubAgentViewTreeLegacyAndFeatureOff','AA010_TREE_TOMBSTONE'),
 ('feature-off','internal/m8/sub_agent.go','if c == nil || !subAgentFeatureEnabled() {','if c == nil {','./internal/m8','TestSubAgentViewTreeLegacyAndFeatureOff','AA010_FEATURE_OFF_CHILD_REJECT'),
 ('legacy-rollback','internal/store/sub_agent_rollback.go','UPDATE session_delivery SET session_name=','UPDATE session_delivery SET session_name=', './internal/store','TestSubAgentOfflineRollbackLegacyClaimer','AA010_LEGACY_REPLAY_PARENT'),
 ('per-mail-return','internal/store/sub_agent_rollback.go','sent, e := enqueueDeliveryFailureNoticeTx(ctx, tx, *d, "sub_agent_ended", s.now())','sent, e := false, error(nil)','./internal/store','TestSubAgentOfflineRollbackLegacyClaimer','AA010_ROLLBACK_CLASSIFICATION'),
 ('unknown-preserve','internal/store/sub_agent_rollback.go','} else {\n\t\t\t\tout.Unknown++','} else {\n _,_ = tx.ExecContext(ctx,`UPDATE session_delivery SET state=\'processed\' WHERE id=?`,id)\n\t\t\t\tout.Unknown++','./internal/store','TestSubAgentOfflineRollbackLegacyClaimer','AA010_UNKNOWN_NOT_FAKE_ACK'),
 ('rollback-reopen','internal/store/sub_agent.go','if barred != 0 {','if false && barred != 0 {','./internal/store','TestSubAgentOfflineRollbackLegacyClaimer','AA010_ROLLBACK_NO_REOPEN'),
]
PYCASES=[
 ('page-cache','view-service/renderer-canary-sse.html','${VS_SCOPE_KEY || stats.sessionName}','${stats.sessionName}','cache','SubAgentScopeProjectionTest.test_aa010_real_signature_tamper_and_page_cache_generation','AA010_PAGE_CACHE_SCOPE'),
 ('sibling-cost','view-service/view_service.py',"return (root, node, generation)","return (root, 'same', 1)",'view','SubAgentScopeProjectionTest.test_aa010_sibling_cost_cursor_and_legacy_survive_restart','AA010_SIBLING_CURSOR'),
 ('unsigned-child','view-service/view_service.py',"if self._session().count('.') > 1: raise ValueError('scope ticket required')","if False: raise ValueError('scope ticket required')",'view','SubAgentScopeProjectionTest.test_aa010_scope_ticket_required_and_bound','AA010_UNSIGNED_CHILD_REJECT'),
 ('ticket-generation','view-service/view_service.py',"if subject.get('agent_id') != identity or subject.get('full_name', '').count('.') != 2:","if False:",'view','SubAgentScopeProjectionTest.test_aa010_scope_ticket_required_and_bound','AA010_TICKET_GENERATION_BINDING'),
 ('invalid-signature','view-service/view_service.py','if not ok or subject.get("full_name") != self._session() or not subject.get("agent_id"):','if False:','view','SubAgentScopeProjectionTest.test_aa010_real_signature_tamper_and_page_cache_generation','AA010_INVALID_SIGNATURE_REJECT'),
 ('helper-return-barrier','tools/sessionhelper/sessionhelper/sub_agent_store.py','downgrade_ready=pending==0','downgrade_ready=True','helper','StoreTests.test_aa010_offline_rollback_blocks_until_individual_returns_settle','AA010_ROLLBACK_PENDING_RETURN_BARRIER'),
 ('feature-off-returns','tools/sessionhelper/sessionhelper/sub_agent_connection.py','return await self.sub_agent_feature_off_returns(ws)','await asyncio.Event().wait()','helper','StoreTests.test_aa010_feature_off_drains_without_native_or_capability','AA010_FEATURE_OFF_PER_MAIL_RETURN'),
 ('legacy-json-reset','tools/sessionhelper/sessionhelper/app.py',"value = dict(version=1, trees=meta.get('trees', []) if meta.get('tree_version') == 1 else [])","\n        if meta.get('tree_version') != 1: return\n        value = dict(version=1, trees=meta.get('trees', []))",'helper','StoreTests.test_aa010_online_tree_json_never_enters_legacy_text','AA010_OLD_HUB_CLEARS_STALE_CHILDREN'),
]
def main():
 p=argparse.ArgumentParser();p.add_argument('--output',required=True);a=p.parse_args();out=Path(a.output).resolve();out.mkdir(parents=True,exist_ok=False)
 env={k:v for k,v in os.environ.items() if not k.startswith(('SH_','DINGWEI_','DW_','DATAD_')) and k not in ('CODEX_HOME','CLAUDE_CONFIG_DIR')};env.setdefault('GOCACHE',str(Path.home()/'.cache/go-build'));env.setdefault('GOMODCACHE',str(Path.home()/'go/pkg/mod'))
 results=[]
 def run(name,cmd,tmp,marker,env):
  start=datetime.datetime.now(datetime.timezone.utc).isoformat();r=subprocess.run(cmd,cwd=ROOT,env=env,capture_output=True,text=True,timeout=180);log=r.stdout+r.stderr;(out/(name+'.stdout')).write_text(log);row=dict(name=name,rc=r.returncode,target_red=r.returncode!=0 and marker in log,marker=marker,start=start,end=datetime.datetime.now(datetime.timezone.utc).isoformat());results.append(row);print(json.dumps(row),flush=True)
 for name,relative,needle,replacement,pkg,test,marker in GO:
  with tempfile.TemporaryDirectory(prefix='m3mut-') as directory:
   tmp=Path(directory);e={**env,'HOME':str(tmp/'home'),'TMPDIR':str(tmp),'TZ':'UTC'};(tmp/'home').mkdir();src=ROOT/relative;original=src.read_text();assert needle in original,name
   if name=='legacy-rollback':
    # Mutate both independent quarantine statements; no other behavior changes.
    changed=original.replace('d.SessionName+"\\x1fsub-quarantine:"+root','d.SessionName').replace('"\\x1fsub-quarantine:"+root','""')
    assert changed!=original
   else:changed=original.replace(needle,replacement,1)
   candidate=tmp/src.name;candidate.write_text(changed);overlay=tmp/'overlay.json';overlay.write_text(json.dumps({'Replace':{str(src):str(candidate)}}));run(name,['go','test','-overlay='+str(overlay),pkg,'-run','^'+test+'$','-count=1','-v'],tmp,marker,e)
 for name,relative,needle,replacement,kind,test,marker in PYCASES:
  with tempfile.TemporaryDirectory(prefix='m3mut-') as directory:
   tmp=Path(directory);e={**env,'HOME':str(tmp/'home'),'TMPDIR':str(tmp),'TZ':'UTC'};(tmp/'home').mkdir()
   if kind=='cache':
    shutil.copy(ROOT/'view-service/test_mux_projection.py',tmp/'test_mux_projection.py');shutil.copy(ROOT/'view-service/view_service.py',tmp/'view_service.py');candidate=tmp/'renderer-canary-sse.html';source=ROOT/relative;module='test_mux_projection.'+test;e['PYTHONPATH']=str(tmp)
   elif kind=='view':
    candidate=tmp/'view_service.py';source=ROOT/relative;module='test_mux_projection.'+test;e['PYTHONPATH']=str(tmp)+os.pathsep+str(ROOT/'view-service')
   else:
    shutil.copytree(ROOT/'tools/sessionhelper/sessionhelper',tmp/'sessionhelper',ignore=shutil.ignore_patterns('__pycache__'));candidate=tmp/'sessionhelper'/Path(relative).name;source=ROOT/relative;module='test_sub_agent_helper.'+test;e['PYTHONPATH']=str(tmp)+os.pathsep+str(ROOT/'tools/sessionhelper/tests')
   original=source.read_text();assert needle in original,name;candidate.write_text(original.replace(needle,replacement,1));run(name,['python3','-m','unittest',module],tmp,marker,e)
 # Browser mutation exercises production source through the normal browser harness.
 with tempfile.TemporaryDirectory(prefix='m3browser-') as directory:
  tmp=Path(directory);e={**env,'HOME':str(tmp/'home'),'TMPDIR':str(tmp),'TZ':'UTC'};(tmp/'home').mkdir()
  run('browser-tree',['node','view-service/sub-agent-tree-browser-regression.js','--mutate-tree'],tmp,'AA010_BROWSER_TREE_ONLINE',e)
  run('browser-stale-tree',['node','view-service/sub-agent-tree-browser-regression.js','--mutate-stale'],tmp,'AA010_BROWSER_STALE_TREE_REVOKE',e)
  run('browser-root-mask',['node','view-service/sub-agent-tree-browser-regression.js','--mutate-root-mask'],tmp,'AA010_BROWSER_ROOT_MASKS_SUBTREE',e)
 (out/'RESULT.json').write_text(json.dumps(results,ensure_ascii=False,indent=2)+'\n');return 0 if all(x['target_red'] for x in results) else 1
if __name__=='__main__':raise SystemExit(main())
