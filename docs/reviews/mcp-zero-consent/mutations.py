"""One cut at a time in an isolated source snapshot; require exact target reds."""
import argparse,json,os,shutil,subprocess,tempfile,time
from pathlib import Path
ROOT=Path(__file__).resolve().parents[3]
p=argparse.ArgumentParser();p.add_argument('--output',type=Path,required=True);a=p.parse_args();out=a.output.resolve();out.mkdir(parents=True,exist_ok=False)
# file, original, replacement, package, test, own assertion
cuts=[
('outer-attempt-once','internal/mcpgw/oauth.go','hash("started:"+attempt)','hash("started:"+location.Query().Get("state"))','mcpgw','TestConsentOneStartPerOAuthAttempt','ZC_OUTER_ATTEMPT_STARTED_TWICE'),
('external-length','internal/extshared/extshared.go','{1,20}','{1,21}','extshared','TestShortNameRules','want invalid'),
('external-long-parse','internal/extshared/extshared.go','{1,20}','{1,8}','extseat','TestConsentLongNameMigrationAndConcurrencyPG','ZC_LONG_ISSUE'),
('name-lower','internal/feishulogin/defaults.go','range strings.ToLower(s)','range s','feishulogin','TestConsentNameGeneration/english','ZC_NAME_english'),
('name-symbols','internal/feishulogin/defaults.go',"c >= 'a' && c <= 'z'","c >= ' ' && c <= 'z'",'feishulogin','TestConsentNameGeneration/symbols','ZC_NAME_symbols'),
('name-initials','internal/feishulogin/defaults.go','a.Style = pinyin.FirstLetter','a.Style = pinyin.Normal','feishulogin','TestConsentNameGeneration/polyphonic','ZC_NAME_polyphonic'),
('name-fallback','internal/feishulogin/defaults.go','name = "pm"','name = "bot"','feishulogin','TestConsentNameGeneration/empty','ZC_NAME_empty'),
('name-eight','internal/feishulogin/defaults.go','name = name[:8]','name = name[:9]','feishulogin','TestConsentNameGeneration/long','ZC_NAME_long'),
('name-UTC','internal/feishulogin/defaults.go','now.UTC().Format','now.Format','feishulogin','TestConsentNameGeneration/english','ZC_NAME_english'),
('disabled-form','internal/feishulogin/connect.go','if h.defaults != nil && len(h.defaults.Allowed(h.teamTenantKey)) > 0 {','if h.defaults != nil {','feishulogin','TestConsentSingleAndDisabled/empty','CSRF input not found'),
('single-input','internal/feishulogin/connect.go','{{if .Automatic}}','{{if false}}','feishulogin','TestConsentSingleAndDisabled/single','ZC_FORM_MODE_single'),
('manual-length','internal/feishulogin/handler.go',' || len(req.ShortName) > 8','', 'feishulogin','TestConnectPageRejectsInvalidShortName/toolongname','short "toolongname"'),
('default-overrides','internal/feishulogin/connect.go','if r.PostForm.Get("short_name") != "" || r.PostForm.Get("target_agent") != "" {','if false {','feishulogin','TestConsentOverridesAndRevocation/short-override','ZC_OVERRIDE_short-override'),
('target-membership','internal/feishulogin/defaults.go','if name == target {','if name != "" {','feishulogin','TestConsentSelectionRejectsChangedPolicy/outside','ZC_SELECTION_outside'),
('target-valid','internal/feishulogin/defaults.go','return h.defaults.Valid(ctx, target)','return true','feishulogin','TestConsentSingleAndDisabled/invalid','ZC_INVALID_TARGET'),
('option-filter','internal/feishulogin/defaults.go','if h.allowedTarget(ctx, name) {','if true {','feishulogin','TestConsentMultipleSelectionBoundOnce','ZC_MULTI_FILTER'),
('recent-scope','internal/extwire/defaults.go','p.subject=$4','p.subject<>$4','extwire','TestConsentRecentTrustedPrincipalPG','ZC_RECENT_LATEST'),
('recent-order','internal/extwire/defaults.go','ORDER BY g.seq DESC','ORDER BY g.seq ASC','extwire','TestConsentRecentTrustedPrincipalPG','ZC_RECENT_LATEST'),
('recent-filter','internal/feishulogin/defaults.go','if strings.EqualFold(recent, target) {','if recent != "" {','feishulogin','TestConsentSelectionRejectsChangedPolicy/recent-outside','ZC_RECENT_OUTSIDE'),
('config-tenant','internal/extwire/defaults.go','d.allowed[tenant]','d.allowed["t"]','extwire','TestConsentConfigIsolationAndValidation','ZC_CONFIG_TENANT_LEAK'),
('subject-active','internal/extwire/defaults.go','u.State == identity.UserActive','(u.State == identity.UserActive || u.State != identity.UserActive)', 'extwire','TestConsentAuthoritativeValidity/owner-inactive','ZC_VALIDITY_owner-inactive'),
('name-once','internal/feishulogin/defaults.go','a.Request.TargetAgent = req.Target','a.Request.ShortName = AutomaticShortName(UserInfo{EnName:"changed"},h.now())\n a.Request.TargetAgent = req.Target','feishulogin','TestConsentMultipleSelectionBoundOnce','ZC_SELECTION_NAME_ONCE'),
('selection-purpose','internal/feishulogin/defaults.go','a.Purpose != StatePurposeSelection || ','','feishulogin','TestConsentSelectionRejectsChangedPolicy/wrong-purpose','ZC_SELECTION_wrong-purpose'),
('selection-browser','internal/mcpgw/oauth.go','a, e := g.readAttempt(r, attempt, false)','a, e := g.readAttempt(r, attempt, false)\n if req.Selection != "" { e = nil }','mcpgw','TestConsentGatewaySingleAndMultiBrowserGuards/no-cookie','ZC_GW_SELECTION_no-cookie'),
('issue-expiry','internal/feishulogin/handler.go','if !h.now().Before(attempt.ExpiresAt) {','if false {','feishulogin','TestConsentIssueExpiry','ZC_ISSUE_EXPIRED'),
('migration-upper','internal/store/migrate/migrations/0108_extseat_short_name.sql','{1,20}','{1,21}','store/migrate','TestConsentMigrationLedgerPG','ZC_LEDGER_OVER20_ACCEPTED'),
('migration-old','internal/store/migrate/migrations/0108_extseat_short_name.sql',"{1,20}","{9,20}",'store/migrate','TestConsentMigrationLedgerPG','violated by some row'),
('rollback-close','docs/reviews/mcp-zero-consent/RELEASE-NOTES.md','封口全部长名外部席位','处理席位','feishulogin','TestConsentRollbackRunbook','ZC_ROLLBACK_RUNBOOK_MISSING'),
('rollback-pending','docs/reviews/mcp-zero-consent/RELEASE-NOTES.md','处理完这些席位确定未执行的投递','处理相关记录','feishulogin','TestConsentRollbackRunbook','ZC_ROLLBACK_RUNBOOK_MISSING'),
('rollback-check','docs/reviews/mcp-zero-consent/RELEASE-NOTES.md','存在长名行时不得恢复旧CHECK，只能前向修复','允许恢复','feishulogin','TestConsentRollbackRunbook','ZC_ROLLBACK_RUNBOOK_MISSING'),
]
with tempfile.TemporaryDirectory(prefix='zcm') as tmp:
 snap=Path(tmp)/'src';shutil.copytree(ROOT,snap,ignore=shutil.ignore_patterns('.git','raw','__pycache__','node_modules','dist'))
 env={'PATH':'/usr/bin:/bin','HOME':tmp,'TMPDIR':tmp,'GOCACHE':'/home/dev/.cache/go-build','GOMODCACHE':'/home/dev/go/pkg/mod','GOPROXY':'off','HUB_TEST_PG_STRICT':'1'}
 cache=Path('/home/dev/.embedded-postgres-go')
 if cache.exists():(Path(tmp)/'.embedded-postgres-go').symlink_to(cache,target_is_directory=True)
 def run(name,package,test):
  cmd=['go','test','-count=1','-v','-timeout=5m','./internal/'+package,'-run',test]
  r=subprocess.run(cmd,cwd=snap,env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT);(out/(name+'.stdout')).write_bytes(r.stdout);return r,cmd
 b,cmd=run('baseline','feishulogin','Consent');assert b.returncode==0,b.stdout.decode()
 results=[]
 for name,file,old,new,pkg,test,target in cuts:
  f=snap/file;s=f.read_text();assert old in s,(name,'needle missing');f.write_text(s.replace(old,new,1))
  try:r,cmd=run(name,pkg,test)
  finally:f.write_text(s)
  txt=r.stdout.decode(errors='replace');valid=r.returncode!=0 and target in txt and '[build failed]' not in txt and 'build failed' not in txt
  item={'name':name,'file':file,'command':cmd,'rc':r.returncode,'target_assertion':target,'target_red':valid};results.append(item);print(json.dumps(item),flush=True)
 (out/'RESULT.json').write_text(json.dumps(results,indent=2)+'\n')
 raise SystemExit(int(not all(r['target_red'] for r in results)))
