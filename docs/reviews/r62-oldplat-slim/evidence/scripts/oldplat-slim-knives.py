import subprocess, shutil, os, datetime
os.chdir('/tmp/wt-r62-mobile')
E='/tmp/oldplat-slim-evidence/knives'
os.makedirs(E, exist_ok=True)
src='internal/m8/console_platform.go'
backup='/tmp/oldplat-slim-evidence/console_platform.go.orig'
shutil.copy(src, backup)
orig=open(src).read()
K=[
 ('K1-admin-restore',
  '\t\t\tHref: "#platform-admin", Status: "approved", Active: true,\n\t\t})\n\t}',
  '\t\t\tHref: "#platform-admin", Status: "approved", Active: true,\n\t\t}, model.ConsolePlatformItem{\n\t\t\tID: "admin", Kind: "admin", Title: "旧版后台",\n\t\t\tHref: "/console/admin", Target: "_blank", Status: "approved", Active: true,\n\t\t})\n\t}',
  'TestConsolePlatformAdminEntrySlimMenuNeverExposesEntry|TestLegacyConsoleMenuSlim|TestSGroupEntryManagementKeepsWorkspaceEntriesNotLegacyAdmin'),
 ('K2-change-evidence-restore',
  '\titems = builtins',
  '\tif h.ChangeEvidence != nil && h.ChangeEvidence.Store != nil && h.ChangeEvidence.Provider != nil {\n\t\tbuiltins = append(builtins, model.ConsolePlatformItem{ID: "change-evidence", Kind: "change-evidence", Title: "变更风险与验收证据", Href: "/console/change-evidence", Target: "_blank", Status: "approved", Active: true})\n\t}\n\titems = builtins',
  'TestLegacyConsoleMenuSlim'),
 ('K3-openspec-read-restore',
  '\titems = builtins',
  '\tif h.SpecReader != nil {\n\t\tbuiltins = append(builtins, model.ConsolePlatformItem{ID: "openspec-read", Kind: "openspec-read", Title: "OpenSpec 只读文档", Href: "/console/openspec", Target: "_blank", Status: "approved", Active: true})\n\t}\n\titems = builtins',
  'TestLegacyConsoleMenuSlim'),
 ('K4-custom-items-merge-restore',
  '\titems = builtins',
  '\titems = append(builtins, items...)',
  'TestLegacyConsoleMenuSlimCustomItemsNeverEmitted|TestConsolePlatformHandlersExposeOnlyActionableBuiltinsAndEnforceWorkflow'),
]
summary=[]
for name, find, repl, tests in K:
    text=open(src).read()
    n=text.count(find)
    assert n==1, f'{name}: anchor count {n} != 1'
    open(src,'w').write(text.replace(find, repl))
    r=subprocess.run(['go','test','./internal/m8/','-run',tests,'-count=1'],capture_output=True,text=True)
    out=r.stdout+r.stderr
    open(f'{E}/{name}.log','w').write(out)
    fails=[l.strip() for l in out.splitlines() if 'FAIL' in l or '🔴' in l or '菜单' in l][:6]
    summary.append(f'{name} rc={r.returncode} | ' + ' / '.join(fails)[:400])
    shutil.copy(backup, src)
    assert open(src).read()==orig
r=subprocess.run(['go','test','./internal/m8/','-run','TestConsolePlatform|TestLegacyConsoleMenuSlim|TestNewPlatformConsoleMenuSlim|TestSGroupEntry','-count=1'],capture_output=True,text=True)
open(f'{E}/baseline-after-knives.log','w').write(r.stdout+r.stderr)
summary.append(f'baseline-after-knives rc={r.returncode}')
open(f'{E}/summary.txt','w').write('\n'.join(summary)+'\n')
print('\n'.join(summary))
