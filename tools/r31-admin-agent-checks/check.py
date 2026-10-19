#!/usr/bin/env python3
"""Local admin inventory checks and exact-witness withdrawals; never deploys."""
import argparse
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

ROOT=Path(__file__).resolve().parents[2]

def run(argv,env):
 p=subprocess.run(argv,cwd=ROOT,env=env,text=True,capture_output=True,timeout=240)
 return dict(argv=argv,rc=p.returncode,stdout=p.stdout,stderr=p.stderr)

def main():
 parser=argparse.ArgumentParser();parser.add_argument('--out',type=Path,required=True);parser.add_argument('--jobs',type=int,default=2);parser.add_argument('--browser',action='store_true');args=parser.parse_args()
 args.out.mkdir(parents=True,exist_ok=False)
 env=dict(os.environ,HUB_TEST_PG_STRICT='1',HUB_TEST_PG_EMBEDDED='1',HUB_TEST_PG_SKIP_OK='0');env.pop('HUB_TEST_PG_DSN',None);env.pop('MUTATION',None)
 store='internal/identity/admin_agents.go';api='internal/identity/httpapi/admin_agents.go';ordinary='internal/identity/memstore.go';projection='internal/identity/httpapi/httpapi.go'
 node_read='internal/hosted/admin_agent_node.go';wiring='cmd/workpulse/newplat.go'
 sources={p:(ROOT/p).read_text()for p in (store,api,ordinary,projection,node_read,wiring)}
 watched=[*sources,'cmd/workpulse/r31_admin_agent_keyless_test.go','internal/m8/console_auth.go','internal/m8/newplat_console.go','internal/identity/identitytest/admin_agents.go','internal/identity/admin_agents_test.go','internal/identity/admin_agents_pg_test.go','internal/identity/httpapi/r31_admin_agents_test.go','internal/m8/r31_admin_agent_capability_test.go','view-service/r31-admin-agents-browser-regression.js','tools/r31-admin-agent-checks/check.py']
 source_hashes={p:hashlib.sha256((ROOT/p).read_bytes()).hexdigest()for p in watched}
 cases=[
 ('store-upper-bound',store,'limit > AdminAgentPageMax','limit > AdminAgentPageMax+1','./internal/identity','^TestR31AdminAgentMemoryContract/all_owners_private_source_and_stable_pages$','ADMIN_BOUND_MISSING'),
 ('mem-role',store,"u == nil || u.State != UserActive || u.Type != UserPlatformAdmin","u == nil",'./internal/identity','^TestR31AdminAgentMemoryContract/nonadmin_and_inactive_rejected_on_every_read$','ADMIN_ROLE_GATE_MISSING'),
 ('mem-active',store,"u == nil || u.State != UserActive || u.Type != UserPlatformAdmin","u == nil || u.Type != UserPlatformAdmin",'./internal/identity','^TestR31AdminAgentMemoryContract/nonadmin_and_inactive_rejected_on_every_read$','ADMIN_ROLE_GATE_MISSING'),
 ('pg-role',store,'state != string(UserActive) || kind != string(UserPlatformAdmin)','state != string(UserActive)','./internal/identity','^TestR31AdminAgentPGContract/nonadmin_and_inactive_rejected_on_every_read$','ADMIN_ROLE_GATE_MISSING'),
 ('pg-active',store,'state != string(UserActive) || kind != string(UserPlatformAdmin)','kind != string(UserPlatformAdmin)','./internal/identity','^TestR31AdminAgentPGContract/nonadmin_and_inactive_rejected_on_every_read$','ADMIN_ROLE_GATE_MISSING'),
 ('http-role',api,'err != nil || u == nil || u.State != identity.UserActive || u.Type != identity.UserPlatformAdmin','err != nil || u == nil','./internal/identity/httpapi','^TestR31AdminAgentHTTPRoleGateBeforeProjection$','ADMIN_HTTP_ROLE_FENCE_MISSING'),
 ('bearer-cookie',api,'len(r.Header.Values("Authorization")) != 0','false','./internal/identity/httpapi','^TestR31AdminAgentBearerCannotInheritHumanCookie$','ADMIN_BEARER_INHERITED_HUMAN_ROLE'),
 ('source-response',api,'item["host"] = row.SourceIP','item["host"] = ""','./internal/identity/httpapi','^TestR31AdminAgentHTTPProjectionAndAudit$','ADMIN_RESPONSE_SOURCE_MISSING'),
 ('node-response',api,'item["host"] = name','item["host"] = name[:0]','./internal/identity/httpapi','^TestR31AdminAgentHTTPProjectionAndAudit$','ADMIN_NODE_HOST_MISSING'),
 ('mem-read-audit',store,'m.audit = append(m.audit, adminAgentAudit(Lower(actor), limit, len(page.Agents), at))','_ = adminAgentAudit(Lower(actor), limit, len(page.Agents), at)','./internal/identity/httpapi','^TestR31AdminAgentHTTPProjectionAndAudit$','ADMIN_READ_AUDIT_OR_BOUND_MISSING'),
 ('pg-read-audit',store,"INSERT INTO audit_log(actor,action,target,after_json,ts) VALUES($1,$2,$3,jsonb_build_object('detail',$4::text),$5)",'SELECT $1::text,$2::text,$3::text,$4::text,$5::timestamptz','./internal/identity','^TestR31AdminAgentPGAuditAtomicAndFailClosed$','ADMIN_PG_READ_AUDIT_MISSING'),
 ('audit-fail-open',store,'if err != nil {\n\t\treturn AdminAgentPage{}, err\n\t}\n\treturn page, nil','if err != nil {\n\t\treturn page, nil\n\t}\n\treturn page, nil','./internal/identity','^TestR31AdminAgentPGAuditAtomicAndFailClosed$','ADMIN_PG_AUDIT_FAIL_OPEN'),
 ('ordinary-owner-fence',ordinary,'ag.OwnerLC == owner && ag.Current()','(ag.OwnerLC == owner || true) && ag.Current()','./internal/identity/httpapi','^TestR31AdminAgentOrdinaryListRemainsPrivate$','ORDINARY_LIST_OWNER_FENCE_MISSING'),
 ('ordinary-bearer-source',projection,'ag.Origin == identity.OriginSelfhost && len(r.Header.Values("Authorization")) == 0','ag.Origin == identity.OriginSelfhost','./internal/identity/httpapi','^TestR31AdminAgentOrdinaryListRemainsPrivate$','ORDINARY_BEARER_SOURCE_GATE_MISSING'),
 ('keyless-production-wiring',wiring,'api.AdminHostedNodeName = hosted.NewStore(pool).AgentNodeName','_ = hosted.NewStore(pool)','./cmd/workpulse','^TestR31AdminAgentNodeWithoutMasterKeyProductionWiring/existing$','ADMIN_NODE_KEYLESS_WIRING_MISSING'),
 ('keyless-read-query',node_read,'return name, nil','return name[:0], nil','./cmd/workpulse','^TestR31AdminAgentNodeWithoutMasterKeyProductionWiring/existing$','ADMIN_NODE_KEYLESS_WIRING_MISSING'),
 ('missing-hosted-tables',node_read,'state.SQLState() == "42P01"','state.SQLState() == "42P99"','./cmd/workpulse','^TestR31AdminAgentNodeWithoutMasterKeyProductionWiring/no-hosted-tables$','ADMIN_NODE_TABLE_ABSENCE_BROKE_LIST'),
 ('hide-unrelated-node-error',node_read,'state.SQLState() == "42P01"','state.SQLState() != ""','./cmd/workpulse','^TestR31AdminAgentNodeWithoutMasterKeyProductionWiring/unrelated-query-error$','ADMIN_NODE_UNRELATED_ERROR_HIDDEN'),
 ]
 positive=run(['go','test','./internal/identity','./internal/identity/httpapi','./internal/m8','./cmd/workpulse','-run','TestR31AdminAgent','-count=1'],env)
 assert positive['rc']==0,positive
 (args.out/'positive.json').write_text(json.dumps(positive,indent=2)+'\n')
 def mutation(case):
  name,file,old,new,package,test,marker=case;source=sources[file]
  if name=='ordinary-owner-fence':
   start=source.index('func (m *MemStore) ListAgents(');end=source.index('\nfunc ',start)
   part=source[start:end];assert part.count(old)==1
   mutant=source[:start]+part.replace(old,new)+source[end:]
  else:
   assert source.count(old)==1,(name,source.count(old));mutant=source.replace(old,new)
  with tempfile.TemporaryDirectory(prefix='r31-admin-inert-overlay-')as td:
   td=Path(td);replacement=td/'mutant.go';replacement.write_text(mutant);overlay=td/'overlay.json';overlay.write_text(json.dumps({'Replace':{str(ROOT/file):str(replacement)}}))
   result=run(['go','test','-overlay',str(overlay),package,'-run',test,'-count=1'],env)
  result.update(name=name,expected_marker=marker)
  assert result['rc']==1 and marker in result['stdout']+result['stderr'] and '--- FAIL: TestR31AdminAgent' in result['stdout'],result
  assert '[build failed]' not in result['stdout']+result['stderr'] and 'panic:' not in result['stdout']+result['stderr'],result
  (args.out/(name+'.json')).write_text(json.dumps(result,indent=2)+'\n');return result
 with ThreadPoolExecutor(max_workers=args.jobs)as executor:results=list(executor.map(mutation,cases))
 browser=[]
 if args.browser:
  for name,marker in [('', 'R31 admin-agent browser: passed=6 failed=0'),('admin-menu-for-manager','ADMIN_LIST_MENU_ROLE_GATE_MISSING'),('drop-ip-render','ADMIN_LIST_IP_OR_HOST_RENDER_MISSING'),('drop-page-cursor','ADMIN_LIST_CURSOR_MISSING'),('', 'R31 admin-agent browser: passed=6 failed=0')]:
   result=run(['node','view-service/r31-admin-agents-browser-regression.js'],dict(env,MUTATION=name))
   assert result['rc']==(1 if name else 0) and marker in result['stdout']+result['stderr'],result
   assert 'MUTATION_ANCHOR_STALE' not in result['stdout']+result['stderr'],result
   result['name']=name or ('restored' if browser else 'positive');browser.append(result)
 restored=run(['go','test','./internal/identity','./internal/identity/httpapi','./internal/m8','./cmd/workpulse','-run','TestR31AdminAgent','-count=1'],env);assert restored['rc']==0,restored
 for p,sha in source_hashes.items():assert hashlib.sha256((ROOT/p).read_bytes()).hexdigest()==sha,('source changed during run',p)
 report=dict(utc=datetime.now(timezone.utc).isoformat(),environment={'host':os.uname().nodename,'python':sys.version,'scope':'local HTTP fixtures and isolated embedded PG; no production access'},source_sha256=source_hashes,positive=positive,withdrawals=results,browser=browser,restored=restored,result='LOCAL_PASS_PENDING_INDEPENDENT_REVIEW')
 (args.out/'CHECKS.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
 print(json.dumps({'result':report['result'],'go_withdrawals':len(results),'browser_runs':len(browser),'out':str(args.out)}))

if __name__=='__main__':main()
