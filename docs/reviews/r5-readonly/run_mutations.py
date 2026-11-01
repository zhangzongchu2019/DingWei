"""Withdraw guards using Go overlays and browser fixture injection; never edit sources.
All fixtures are synthetic. A mutation is killed only by a failing test assertion,
not an infrastructure or compiler error. Raw outputs and exit codes are retained.
"""
import json, os, subprocess, tempfile
from pathlib import Path
ROOT=Path(__file__).resolve().parents[3]
RAW=Path(__file__).resolve().parent/'raw'
S='internal/specread/service.go';G='internal/specread/github.go'
mutations=[
 ('feature-default-off','internal/m8/console_openspec_read.go',[('if os.Getenv("DW_OPENSPEC_READ_ENABLED") != "1" {','if false {')],'TestR5FeatureConfigurationAllGates/disabled'),
 ('credential-config','internal/m8/console_openspec_read.go',[('if !github.Ready() {','if false {')],'TestR5FeatureConfigurationAllGates/missing_file'),
('repository-config', 'internal/m8/console_openspec_read.go', [('if id == "" || strings.Trim(id, "0123456789") != "" || id[0] == \'0\' {', 'if false && (id == "" || strings.Trim(id, "0123456789") != "" || id[0] == \'0\') {')], 'TestR5FeatureConfigurationAllGates/missing_repositories'),
 ('subject-binding',S,[('b.Principal != actor','false')],'TestReadAuthorizationAndCoordinates/other_principal'),
 ('repository-allowlist',S,[('!s.Repositories[c.Provider+":"+c.Instance+":"+c.Repository]','false')],'TestReadAuthorizationAndCoordinates/foreign_repository'),
 ('repository-identity',S,[('repo.ID != c.Repository','false')],'TestReadAuthorizationAndCoordinates/repo_identity'),
 ('current-read-permission',S,[('if err = p.Authorize(ctx, repo, *b); err != nil {','if err = p.Authorize(ctx, repo, *b); false {'),('if err = p.Authorize(ctx, repo, *current); err != nil {','if err = p.Authorize(ctx, repo, *current); false {')],'TestReadAuthorizationAndCoordinates/no_read|TestReadRevocationAndCrossRepository'),
 ('final-permission-recheck',S,[('if err = p.Authorize(ctx, repo, *current); err != nil {','if err = p.Authorize(ctx, repo, *current); false {')],'TestReadAuthorizationAndCoordinates/revoked_during_read'),
 ('final-binding-recheck',S,[('current == nil || *current != *b','current != nil && false'),('p.Authorize(ctx, repo, *current)','p.Authorize(ctx, repo, *b)')],'TestReadAuthorizationAndCoordinates/unbound_during_read'),
 ('immutable-commit',S,[('(c.Commit != "" && commit != c.Commit)','false')],'TestReadAuthorizationAndCoordinates/bad_commit'),
 ('tree-uses-sha',S,[('p.Tree(ctx, repo, commit)','p.Tree(ctx, repo, requested)')],'TestReadImmutableRef'),
 ('path-scope',S,[('c.Path != "" && !ValidPath(c.Path)','false')],'TestReadPathAndTypes'),
 ('symlink-submodule',S,[('return e.Type == "blob" && (e.Mode == "100644" || e.Mode == "100755") && ValidPath(e.Path)','return ValidPath(e.Path)')],'TestReadPathAndTypes'),
 ('tree-size-precheck',S,[('if e.Size > MaxFile {','if false {')],'TestReadSizeEncodingAndCompleteness/2097153'),
 ('actual-size-limit',S,[('if len(data) > MaxFile {','if false {')],'TestReadSizeEncodingAndCompleteness/actual_overflow'),
 ('size-binding',S,[('if int64(len(data)) != e.Size {','if false {')],'TestReadSizeEncodingAndCompleteness/size_mismatch'),
 ('utf8',S,[('if !utf8.Valid(data) {','if false && !utf8.Valid(data) {')],'TestReadSizeEncodingAndCompleteness/invalid_utf8'),
 ('github-stable-id',G,[('string(v.ID) != id ||','false ||')],'TestGitHubRepositoryIdentityRedirect/wrong_id|TestGitHubRepositoryIdentityRedirect/same_host_wrong_id'),
 ('github-redirect-host',G,[('!safeAPI(r.URL) ||','false ||')],'TestGitHubRepositoryIdentityRedirect/cross_host'),
 ('github-name-reuse',G,[('string(identity.ID) != r.ID || identity.FullName != r.FullName','false')],'TestGitHubRepositoryNameReuseAndTimeout/name_reused'),
 ('github-account-id',G,[('string(v.User.ID) != b.AccountID ||','false ||')],'TestGitHubPermissionIdentityAndFailure/wrong_user'),
 ('github-recursive-truncation',G,[('if !all.Truncated {','if true {')],'TestGitHubTreeTruncationAndTypes/fallback'),
 ('github-incomplete-fallback',G,[('if t.Truncated {','if false {')],'TestGitHubTreeTruncationAndTypes/incomplete'),
 ('github-tree-coordinate',G,[('if v.SHA != sha {','if false {')],'TestGitHubTreeTruncationAndTypes/wrong_tree'),
 ('github-blob-coordinate',G,[('v.SHA != e.SHA ||','false ||')],'TestGitHubBlobBinding/sha'),
 ('github-blob-digest',G,[('hex.EncodeToString(sum.Sum(nil)) != e.SHA','false && hex.EncodeToString(sum.Sum(nil)) != e.SHA')],'TestGitHubBlobBinding/hash'),
 ('github-rate-header',G,[('if resp.Header.Get("X-RateLimit-Remaining") == "0" {','if false {')],'TestGitHubRateHeadersAndCredentialRotation'),
 ('github-response-bound',G,[('if len(data) > 8<<20 {','if false {')],'TestGitHubResourceBoundsAndRedirectRate/response_bytes'),
 ('github-tree-bound',G,[('if len(v.Entries) > 10000 {','if false {')],'TestGitHubResourceBoundsAndRedirectRate/tree_entries'),
 ('binding-inactive-principal','internal/store/openspec_read_binding.go',[(' AND p.active=1','')],'TestR5BindingPersistenceIsolationAuditAndRevocation'),
 ('admin-only','internal/m8/obs_hooks.go',[('return func(w http.ResponseWriter, r *http.Request) {','return func(w http.ResponseWriter, r *http.Request) { next(w,r); return;')],'TestR5AdminBindingMiddlewareCannotUseManagement'),
 ('binding-origin','internal/m8/console_openspec_read.go',[('if e != nil || origin.Host != r.Host','if false && (e != nil || origin.Host != r.Host'),('r.URL.RawQuery != "" {','r.URL.RawQuery != "") {')],'TestR5BindingJSONOriginAndReadPage'),
]
results=[]
import sys
selected=set(sys.argv[1:])
for name,source,replacements,test in mutations:
 if selected and name not in selected:continue
 data=(ROOT/source).read_text()
 for old,new in replacements:
  assert old in data,(name,old)
  data=data.replace(old,new)
 with tempfile.TemporaryDirectory(prefix='r5-mut-') as tmp:
  target=Path(tmp)/Path(source).name;target.write_text(data)
  overlay=Path(tmp)/'overlay.json';overlay.write_text(json.dumps({'Replace':{str(ROOT/source):str(target)}}))
  package='./'+str(Path(source).parent)
  cmd=['go','test','-vet=off','-overlay',str(overlay),package,'-run',test,'-count=1']
  proc=subprocess.run(cmd,cwd=ROOT,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
  (RAW/('mutation-'+name+'.log')).write_text(proc.stdout)
  killed=proc.returncode!=0 and '--- FAIL:' in proc.stdout and '[build failed]' not in proc.stdout
  results.append({'guard':name,'rc':proc.returncode,'assertion_red':killed,'test':test})
  print(name,proc.returncode,killed,flush=True)
for name in ['html','images','links','paths','commit','freshness']:
 if selected and 'browser-'+name not in selected:continue
 env=os.environ.copy();env['NODE_PATH']='/tmp/r5-browser-tools/node_modules'
 proc=subprocess.run(['node','view-service/openspec-read-browser-regression.js','--mutation='+name],cwd=ROOT,env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
 (RAW/('mutation-browser-'+name+'.log')).write_text(proc.stdout)
 killed=proc.returncode!=0 and 'AssertionError' in proc.stdout
 results.append({'guard':'browser-'+name,'rc':proc.returncode,'assertion_red':killed})
 print('browser-'+name,proc.returncode,killed,flush=True)
(RAW/('mutations-selected.json' if selected else 'mutations.json')).write_text(json.dumps(results,indent=2)+'\n')
raise SystemExit(0 if all(r['assertion_red'] for r in results) else 1)
