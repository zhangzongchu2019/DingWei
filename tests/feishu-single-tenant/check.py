"""Local Go regression/withdrawal proof. No live Hub, Feishu or seats.

Usage: python3 check.py /new/private/output-directory
Uses an isolated detached worktree. Every mutation must fail at its named
semantic assertion, then pass after exact byte restoration; compile errors
cannot satisfy the gate. Go modules/toolchain must already be locally cached.
"""
import datetime
import json
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
HUB = 'internal/m8/spec_clarification_reply.go'
NOTICE = 'internal/store/spec_clarification_notice.go'
INBOUND = 'internal/store/spec_clarification_inbound.go'
MUTATIONS = [
 ('native-enumeration', 'cmd/workpulse/spec_clarification.go', 'range native.BotChannelIDs() {', 'range native.BotChannelIDs()[:0] {', './cmd/workpulse', '^TestSingleTenantStartupUsesConfiguredNativeApplicationsWithoutMapping$', 'SINGLE_TENANT_NATIVE_STARTUP_UNAVAILABLE'),
 ('sdk-sender-proof', 'internal/feishu/feishu.go', '\n\t\t\treply.SenderTenant = value(ev.Event.Sender.TenantKey)', '', './internal/feishu', '^TestSingleTenantSDKPreservesSenderTenantForAudit$', 'SINGLE_TENANT_SDK_SENDER_EVIDENCE_LOST'),
 ('empty-tenant', HUB, 'if r.Tenant == "" || r.SenderTenant != r.Tenant {', 'if false || r.SenderTenant != r.Tenant {', './internal/m8', '^TestSingleTenantAuthorityGates$/^tenant-empty$', 'SINGLE_TENANT_AUTH_tenant-empty'),
 ('tenant-trim', HUB, ' || strings.TrimSpace(r.Tenant) != r.Tenant', '', './internal/m8', '^TestSingleTenantAuthorityGates$/^tenant-whitespace$', 'SINGLE_TENANT_AUTH_tenant-whitespace'),
 ('tenant-length', HUB, ' || len(r.Tenant) > 256', '', './internal/m8', '^TestSingleTenantAuthorityGates$/^tenant-long$', 'SINGLE_TENANT_AUTH_tenant-long'),
 ('tenant-controls', HUB, ' || strings.ContainsAny(r.Tenant, "\\x00\\r\\n")', '', './internal/m8', '^TestSingleTenantAuthorityGates$/^tenant-control$', 'SINGLE_TENANT_AUTH_tenant-control'),
 ('sender-tenant-equality', HUB, 'if r.Tenant == "" || r.SenderTenant != r.Tenant {', 'if r.Tenant == "" {', './internal/m8', '^TestSingleTenantAuthorityGates$/^sender-mismatch$', 'SINGLE_TENANT_AUTH_sender-mismatch'),
 ('current-app', HUB, ' || r.AppID != authority.AppID', '', './internal/m8', '^TestSingleTenantAuthorityGates$/^app-mismatch$', 'SINGLE_TENANT_AUTH_app-mismatch'),
 ('mapped-pin', HUB, 'if r.Tenant != authority.Tenant {', 'if false {', './internal/m8', '^TestSingleTenantAuthorityGates$/^pin-mismatch$', 'SINGLE_TENANT_AUTH_pin-mismatch'),
 ('native-authentication', HUB, ' || !r.Authenticated', '', './internal/m8', '^TestSingleTenantAuthorityGates$/^unverified$', 'SINGLE_TENANT_AUTH_unverified'),
 ('native-provenance', HUB, ' || msg.IngressProvenance != model.IngressFeishuWSAuthenticated', '', './internal/m8', '^TestSingleTenantAuthorityGates$/^provenance$', 'SINGLE_TENANT_AUTH_provenance'),
 ('unbound-notice-prepare', NOTICE, 'b.Issuer == "" || b.AppID == ""', 'b.Issuer == "" || b.Tenant == "" || b.AppID == ""', './internal/m8', '^TestSingleTenantUnmappedNoticeReplyAuditAndReplay$', 'SINGLE_TENANT_UNMAPPED_NOTICE_BLOCKED'),
 ('unbound-exact-parent', NOTICE, "AND (tenant=? OR tenant='')", 'AND (tenant=?)', './internal/m8', '^TestSingleTenantUnmappedNoticeReplyAuditAndReplay$', 'SINGLE_TENANT_REPLY_NOT_RECORDED_ONCE'),
 ('ambiguous-parent', NOTICE, 'if count != 1 {', 'if false {', './internal/store', '^TestSingleTenantAmbiguousParentFailsClosed$', 'SINGLE_TENANT_AMBIGUOUS_PARENT_ACCEPTED'),
 ('bind-observed-tenant', INBOUND, '\n\t\texpectedBinding.Tenant = r.Tenant', '', './internal/store', '^TestSingleTenantStorePersistsObservedTenantAndSender$', 'SINGLE_TENANT_STORE_BINDING_REJECTED'),
 ('store-sender-proof', INBOUND, 'if r.SenderTenant != r.Tenant {', 'if false {', './internal/store', '^TestSingleTenantStoreKeepsOtherBindingFieldsAndTenantProofStrict$/^sender-mismatch$', 'SINGLE_TENANT_STORE_BAD_BINDING_ACCEPTED_sender-mismatch'),
 ('store-other-binding-fields', INBOUND, ' || expectedBinding != b', '', './internal/store', '^TestSingleTenantStoreKeepsOtherBindingFieldsAndTenantProofStrict$/^binding-sha$', 'SINGLE_TENANT_STORE_BAD_BINDING_ACCEPTED_binding-sha'),
 ('persist-sender-audit', INBOUND, 'metadata, _ := json.Marshal(clarificationInboundMetadata{m.BotChannelID, m.SenderOpenID, m.ChatType, m.IngressProvenance, *r})', 'auditReply := *r\n\t\tauditReply.SenderTenant = ""\n\t\tmetadata, _ := json.Marshal(clarificationInboundMetadata{m.BotChannelID, m.SenderOpenID, m.ChatType, m.IngressProvenance, auditReply})', './internal/store', '^TestSingleTenantStorePersistsObservedTenantAndSender$', 'SINGLE_TENANT_AUDIT_NOT_DURABLE'),
]

def main(output):
 out = Path(output).resolve(); out.mkdir(mode=0o700,parents=True,exist_ok=False)
 source = subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip()
 wt = out/'worktree'
 subprocess.run(['git','worktree','add','--detach',str(wt),source],cwd=ROOT,check=True,stdout=subprocess.PIPE)
 home=out/'home';home.mkdir(mode=0o700);tmp=out/'tmp';tmp.mkdir(mode=0o700)
 env={'PATH':'/usr/bin:/bin','HOME':str(home),'TMPDIR':str(tmp),'GOTOOLCHAIN':'local','GOPROXY':'off','GOSUMDB':'off','HUB_TEST_PG_STRICT':'1','HUB_TEST_PG_SKIP_OK':'0'}
 for key in ['GOPATH','GOCACHE','GOMODCACHE']:
  env[key]=subprocess.check_output(['go','env',key],cwd=ROOT,text=True).strip()
 rows=[]
 def run(name,packages,selector,marker=''):
  cmd=['go','test',*packages,'-run',selector,'-count=1','-v','-timeout=5m']
  start=datetime.datetime.now(datetime.timezone.utc).isoformat()
  p=subprocess.run(cmd,cwd=wt,env=env,capture_output=True,text=True,timeout=360)
  (out/(name+'.stdout')).write_text(p.stdout);(out/(name+'.stderr')).write_text(p.stderr);(out/(name+'.rc')).write_text(str(p.returncode)+'\n')
  row={'name':name,'command':cmd,'start':start,'end':datetime.datetime.now(datetime.timezone.utc).isoformat(),'rc':p.returncode,'marker':marker,'assertion_red':bool(marker and p.returncode==1 and marker in p.stdout and re.search(r'^--- FAIL:',p.stdout,re.M))}
  rows.append(row);(out/'CHECKS.json').write_text(json.dumps({'source':source,'rows':rows},indent=2)+'\n');return row
 assert run('baseline',['./cmd/workpulse','./internal/feishu','./internal/store','./internal/m8'],'TestSingleTenant|TestClarification')['rc']==0
 for name,path,old,new,pkg,test,marker in MUTATIONS:
  p=wt/path;original=p.read_text();assert original.count(old)==1,(name,original.count(old))
  try:
   p.write_text(original.replace(old,new));assert run(name,[pkg],test,marker)['assertion_red'],name
  finally:p.write_text(original)
  assert run(name+'-restored',[pkg],test)['rc']==0,name+' restore'
 assert not subprocess.check_output(['git','status','--porcelain','--untracked-files=no'],cwd=wt)
 (out/'RESULT.json').write_text(json.dumps({'result':'LOCAL_INERT_PASS_NOT_TARGET_ACCEPTANCE','source':source,'mutations':len(MUTATIONS),'restored':True,'no_production_or_feishu_network':True},indent=2)+'\n')
 print('LOCAL_INERT_PASS_NOT_TARGET_ACCEPTANCE mutations='+str(len(MUTATIONS)))

if __name__=='__main__':main(*sys.argv[1:])
