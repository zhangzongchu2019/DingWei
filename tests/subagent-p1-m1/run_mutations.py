"""Existing-file Go overlays; one behavioral mutation per case, no product edits."""
import argparse,json,os,pathlib,subprocess,tempfile,datetime
ROOT=pathlib.Path(__file__).resolve().parents[2]
CASES=[
 ('depth','internal/subagent/route.go','if len(parts) > 5 {','if len(parts) > 500 {','./internal/subagent','^TestRouteParser$','AA002_DEPTH_EXCEEDED'),
 ('segments','internal/subagent/route.go','if !validName(parts[i]) {','if false {','./internal/subagent','^TestRouteParser$','AA001_MALFORMED'),
 ('parent-fallback','internal/store/sub_agent.go','return nil, subagent.Fail("not_found", "子 Agent 不存在")','return &subagent.Scope{RootID:root,NodeID:"",Generation:1,Path:path,Revision:revision,ConnSeq:seq},nil','./internal/m8','^TestSubAgentHubNegotiationRoutesAndSiblingIdentity$','AA003_NOT_FOUND'),
 ('ended','internal/store/sub_agent.go','if state == "ended" || state == "draining" {','if false {','./internal/store','^TestSubAgentSnapshotRecoveryAndTombstone$','AA003_ENDED_DISTINCT'),
 ('reconnect-ready','internal/m8/sub_agent.go','if !ready {\n\t\t\treturn subagent.Fail("registry_unavailable", "tree not confirmed")','if false && !ready {\n\t\t\treturn subagent.Fail("registry_unavailable", "tree not confirmed")','./internal/m8','^TestSubAgentHubReconnectRequiresNegotiationAndSnapshot$','AA003_RECONNECT_TREE_NOT_INHERITED'),
 ('codex-capability','internal/m8/sub_agent.go','if !strings.EqualFold(c.tool, "codex") || !metaBool(env.Meta, "delivery_ack_v1") && !metaBool(env.Meta, "delivery_ack_v2") {','if false {','./internal/m8','^TestSubAgentHubCapabilityAndEpochNegatives/old-helper$','AA003_CAPABILITY_BOUNDARY'),
 ('snapshot-epoch','internal/store/sub_agent.go','if seq != snap.ConnSeq {','if false {','./internal/store','^TestSubAgentSnapshotRecoveryAndTombstone$','AA009_STALE_CONNECTION'),
 ('snapshot-version','internal/store/sub_agent.go','if snap.Revision <= revision || snap.BaseRevision != revision {','if false {','./internal/store','^TestSubAgentSnapshotRecoveryAndTombstone$','AA009_SAME_REV_DIFFERENT_DIGEST'),
 ('revive','internal/store/sub_agent.go','if (p.state == "ended" && n.State != "ended") || (p.state == "draining" && n.State != "draining" && n.State != "ended") || (p.state == "active" && n.State == "allocating") {','if false {','./internal/store','^TestSubAgentSnapshotRecoveryAndTombstone$','AA009_ENDED_CANNOT_REVIVE'),
 ('alias-rebind','internal/store/sub_agent.go','if err == nil && owner != n.NodeID {','if false {','./internal/store','^TestSubAgentSnapshotRecoveryAndTombstone$','AA003_ALIAS_REBIND'),
 ('source-identity','internal/m8/sub_agent.go','env.From = sessionAddress(delivery.From.FullName, from.KeyID)','env.From = sessionAddress(from.SessionName, from.KeyID)','./internal/m8','^TestSubAgentHubTrustedSourceReplyAndSpoof$','AA004_CHILD_ORIGIN'),
 ('authorization','internal/m8/sub_agent.go','if err = h.enforceAgentGrouping(ctx, from, env, target.keyID, target.sessionName); err != nil {','if false {','./internal/m8','^TestSubAgentHubAuthBeforeAddressAndTree$','AA004_AUTH_BEFORE_ADDRESS'),
 ('payload','internal/m8/sub_agent.go','ctx = context.WithValue(ctx, subAgentDeliveryKey{}, &delivery)','env.Body="mutation"\nctx = context.WithValue(ctx, subAgentDeliveryKey{}, &delivery)','./internal/m8','^TestSubAgentHubNegotiationRoutesAndSiblingIdentity$','AA003_BODY_AND_TARGET'),
 ('dedup-scope','internal/store/sqlite.go','d.SourceType = d.SubRoute.SourceType(d.SourceType)','d.SourceType = d.SourceType','./internal/m8','^TestSubAgentHubNegotiationRoutesAndSiblingIdentity$','AA009_SIBLINGS_ROUTE_DELIVERIES'),
 ('claim-scope','internal/m8/hub.go','claimKey := subAgentClaimKey(env)','claimKey := env.ID','./internal/m8','^TestSubAgentHubNegotiationRoutesAndSiblingIdentity$','AA009_SIBLINGS_ROUTE_DELIVERIES'),
 ('node-id','internal/subagent/route.go','s.RootID, s.NodeID, s.Generation','s.RootID, "", s.Generation','./internal/m8','^TestSubAgentIdentityIncludesNodeAndGeneration$','AA009_ID_INCLUDES_NODE'),
 ('generation-id','internal/subagent/route.go','s.RootID, s.NodeID, s.Generation','s.RootID, s.NodeID, int64(1)','./internal/m8','^TestSubAgentIdentityIncludesNodeAndGeneration$','AA009_ID_INCLUDES_GENERATION'),
 ('generation-check','internal/store/sub_agent.go','if got.NodeID != scope.NodeID || got.Generation != scope.Generation {','if got.NodeID != scope.NodeID {','./internal/store','^TestSubAgentDeliverySidecarAtomicAndRestart$','AA009_GENERATION_CHECK'),
 ('sidecar-atomic','internal/store/sqlite.go','if err != nil {\n\t\treturn err\n\t}\n\treturn tx.Commit()\n}\n\nfunc scanSessionDelivery','if err != nil {\n _ = tx.Commit()\n\t\treturn err\n\t}\n\treturn tx.Commit()\n}\n\nfunc scanSessionDelivery','./internal/store','^TestSubAgentDeliverySidecarAtomicAndRestart$','AA009_SIDECAR_ATOMIC'),
 ('replay-capability','internal/m8/sub_agent.go','if !capable {\n\t\t\treturn subagent.Fail("unsupported", "connection not negotiated")','if false && !capable {\n\t\t\treturn subagent.Fail("unsupported", "connection not negotiated")','./internal/m8','^TestSubAgentHubDrainAndACKFailClosed$','AA009_UNNEGOTIATED_REPLAY'),
 ('ack-scope','internal/m8/sub_agent.go','if metaString(env.Meta, "target_node_id") != route.To.NodeID || subAgentInt64(env.Meta, "lane_generation") != route.To.Generation {','if false {','./internal/m8','^TestSubAgentHubDrainAndACKFailClosed$','AA009_CROSS_LANE_ACK'),
 ('failure-return','internal/store/failure_notice.go','returnRoute = &subagent.DeliveryRoute{To: scope}','returnRoute = nil','./internal/store','^TestSubAgentFailureNoticeRetainsSourceNode$','AA004_FAILURE_RETURN_TO_NODE'),
 ('reply-policy','internal/m8/reply_to_origin.go','if parsed, e := subagent.Parse(addr.SessionName, ""); e == nil && len(parsed.Path) > 0 {','if parsed, e := subagent.Parse(addr.SessionName, ""); false && e == nil && len(parsed.Path) > 0 {','./internal/m8','^TestSubAgentHubAutomaticReplyUsesPersistedSource$','AA004_CHILD_DEFAULT_REPLY_POLICY'),
 ('reply-generation','internal/m8/sub_agent.go','if delivery.To == nil || delivery.To.RootID != pinned.RootID || delivery.To.NodeID != pinned.NodeID || delivery.To.Generation != pinned.Generation {','if false {','./internal/m8','^TestSubAgentHubAutomaticReplyUsesPersistedSource$','AA009_REPLY_GENERATION_PIN'),
 ('feature-off','internal/m8/sub_agent.go','if c == nil || !subAgentFeatureEnabled() {','if c == nil {','./internal/m8','^TestSubAgentHubFeatureOffPreservesRoot$','AA009_FEATURE_OFF'),
 ('migration-old-data','internal/store/sqlite.go','// Reuse the exact-DDL preflight: a retained aggregate table after','if name=="0075_sub_agent_registry.sql" { _,_ = s.db.ExecContext(ctx,`UPDATE session_delivery SET envelope_json=char(123)||char(125)`) }\n// Reuse the exact-DDL preflight: a retained aggregate table after','./internal/store','^TestSubAgentMigrationPreservesOldData$','AA009_MIGRATION_OLD_DATA'),
]
def main():
 p=argparse.ArgumentParser();p.add_argument('--output',required=True);args=p.parse_args();out=pathlib.Path(args.output).resolve();out.mkdir(parents=True,exist_ok=False)
 env={k:v for k,v in os.environ.items() if not k.startswith(('SH_','DINGWEI_','DW_','DATAD_')) and k not in ('CODEX_HOME','CLAUDE_CONFIG_DIR')}
 original_home=os.environ.get('HOME','/home/dev');env.setdefault('GOCACHE',original_home+'/.cache/go-build');env.setdefault('GOMODCACHE',original_home+'/go/pkg/mod')
 results=[]
 for name,relative,needle,replacement,pkg,test,marker in CASES:
  with tempfile.TemporaryDirectory(prefix='sam1-',dir='/tmp') as tmp:
   tmp=pathlib.Path(tmp);env.update(HOME=str(tmp/'home'),TMPDIR=str(tmp/'tmp'),TZ='UTC');(tmp/'home').mkdir();(tmp/'tmp').mkdir()
   src=ROOT/relative;original=src.read_text();count=original.count(needle)
   if count<1:raise RuntimeError(f'{name}: needle absent')
   # replay guard occurs in routing and draining; only mutate the drain function.
   if name=='replay-capability':
    offset=original.index('func (h *Hub) subAgentDrainCheck');mutated=original[:offset]+original[offset:].replace(needle,replacement,1)
   else:mutated=original.replace(needle,replacement,1)
   candidate=tmp/src.name;candidate.write_text(mutated);overlay=tmp/'overlay.json';overlay.write_text(json.dumps({'Replace':{str(src):str(candidate)}}))
   cmd=['go','test','-overlay='+str(overlay),pkg,'-run',test,'-count=1','-v'];start=datetime.datetime.now(datetime.timezone.utc).isoformat()
   r=subprocess.run(cmd,cwd=ROOT,env=env,capture_output=True,text=True,timeout=120);log=r.stdout+r.stderr;(out/(name+'.stdout')).write_text(log)
   row={'name':name,'file':relative,'command':cmd[:2]+['-overlay=<isolated>']+cmd[3:],'expected_marker':marker,'rc':r.returncode,'target_red':r.returncode!=0 and marker in log,'start':start,'end':datetime.datetime.now(datetime.timezone.utc).isoformat()};results.append(row);print(json.dumps(row),flush=True)
 (out/'RESULT.json').write_text(json.dumps(results,ensure_ascii=False,indent=2)+'\n');return 0 if all(r['target_red'] for r in results) else 1
if __name__=='__main__':raise SystemExit(main())
