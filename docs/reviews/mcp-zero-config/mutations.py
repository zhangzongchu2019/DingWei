"""One-cut overlays: each selected baseline must pass, mutant must hit its own assertion."""
import argparse, hashlib, json, os, subprocess, tempfile, time
from pathlib import Path
ROOT=Path(__file__).resolve().parents[3]
ap=argparse.ArgumentParser(); ap.add_argument('--output',type=Path,required=True); a=ap.parse_args()
a.output=a.output.resolve(); a.output.mkdir(parents=True,exist_ok=False)
items=[]
def add(name,file,old,new,test,target,package='./internal/mcpgw'):
 items.append((name,file,old,new,test,target,package))
add('default-local-resolution','oauth.go','q.Get("client_id") == codexDefaultClientID','false','TestZeroConfigExactLocalResolution','ZERO_EXACT_ID')
add('exact-id','oauth.go','q.Get("client_id") == codexDefaultClientID','strings.HasPrefix(q.Get("client_id"), codexDefaultClientID)','TestZeroConfigExactLocalResolution','ZERO_EXACT_ID')
add('pinned-original','codex_client.go','var codexOriginalMetadata []byte','var codexOriginalMetadata []byte\nfunc init() { codexOriginalMetadata = append(codexOriginalMetadata, byte(32)) }','TestZeroConfigPinnedDocument','ZERO_PROVENANCE')
add('ip-only-copy','codex_client.go','ip != nil && ip.IsLoopback()','ip == nil || ip.IsLoopback()','TestZeroConfigInvalidCopyFailsClosed/external_https','ZERO_COPY_external_https')
add('copy-fails-closed','codex_client.go','return Client{}, errParams','return Client{ID: codexDefaultClientID, Name: "mutant", Redirects: []string{"http://127.0.0.1/callback"}, AuthMethod: "none"}, nil','TestZeroConfigInvalidCopyFailsClosed/json','ZERO_COPY_json')
add('public-client','codex_client.go','c.AuthMethod != "none"','false','TestZeroConfigInvalidCopyFailsClosed/auth','ZERO_COPY_auth')
add('grant-intersection','codex_client.go','if grant == "authorization_code" {','if true {','TestZeroConfigPinnedDocument','ZERO_EFFECTIVE_POLICY')
add('response-intersection','codex_client.go','if response == "code" {','if false {','TestZeroConfigPinnedDocument','ZERO_EFFECTIVE_POLICY')
add('random-port','clients.go','\t\treturn true\n\t}\n\treturn false\n}', '\t\treturn false\n\t}\n\treturn false\n}', 'TestZeroConfigRedirectGuards/port$', 'ZERO_REDIRECT_port')
add('port-range','clients.go','err != nil || n < 1 || n > 65535','err != nil || n == -1','TestZeroConfigRedirectGuards/port_invalid','ZERO_REDIRECT_port_invalid')
add('localhost','clients.go','return !strings.EqualFold(u.Hostname(), "localhost") && (u.Scheme == "https" || u.Scheme == "http" && httpLoopback)','return u.Scheme == "https" || u.Scheme == "http" && (httpLoopback || strings.EqualFold(u.Hostname(), "localhost"))','TestLoopbackPortMatchingAndLocalhostRejection','LOCALHOST_METADATA_MUST_BE_REJECTED')
add('same-ip','clients.go',' || requested.Hostname() != host','', 'TestZeroConfigRedirectGuards/ipv6_not_registered','ZERO_REDIRECT_ipv6_not_registered')
add('scheme','clients.go','allowed.Scheme != requested.Scheme || strings.SplitN(raw, ":", 2)[0] != strings.SplitN(redirect, ":", 2)[0] || ','','TestZeroConfigRedirectGuards/scheme','ZERO_REDIRECT_scheme')
add('path','codex_client.go','{1,64}','{0,64}','TestZeroConfigRedirectGuards/path$','ZERO_REDIRECT_path')
add('query','clients.go','allowed.RawQuery != requested.RawQuery || ','','TestZeroConfigRedirectGuards/query','ZERO_REDIRECT_query')
add('fragment','clients.go',' || u.Fragment != ""','', 'TestZeroConfigRedirectGuards/fragment','ZERO_REDIRECT_fragment')
add('userinfo','clients.go',' || u.User != nil','', 'TestZeroConfigRedirectGuards/userinfo','ZERO_REDIRECT_userinfo')
add('pkce-S256','oauth.go',' || q.Get("code_challenge_method") != "S256"','', 'TestZeroConfigAuthorizationGuards/code_challenge_method','ZERO_AUTH_code_challenge_method')
add('pkce-challenge','oauth.go',' || !challengeRE.MatchString(q.Get("code_challenge"))','', 'TestZeroConfigAuthorizationGuards/code_challenge$','ZERO_AUTH_code_challenge')
add('authorize-resource','oauth.go',' || q.Get("resource") != g.cfg.Resource','', 'TestZeroConfigAuthorizationGuards/resource','ZERO_AUTH_resource')
add('scope','oauth.go','if !fixedScopes(q.Get("scope")) {','if false {','TestZeroConfigAuthorizationGuards/scope','ZERO_AUTH_scope')
add('token-client','oauth.go','a.ClientID != f.Get("client_id") || ','','TestZeroConfigTokenBindings/client_id','ZERO_TOKEN_client_id')
add('token-redirect','oauth.go','a.Redirect != f.Get("redirect_uri") || ','','TestZeroConfigTokenBindings/redirect_uri','ZERO_TOKEN_redirect_uri')
add('token-resource','oauth.go','f.Get("resource") != g.cfg.Resource || ','','TestZeroConfigTokenBindings/resource','ZERO_TOKEN_resource')
add('token-pkce','oauth.go','subtle.ConstantTimeCompare([]byte(a.Challenge), []byte(challenge)) != 1','subtle.ConstantTimeCompare([]byte(a.Challenge), []byte(challenge)) == -1','TestZeroConfigTokenBindings/code_verifier','ZERO_TOKEN_code_verifier')
add('request-auth','gateway.go','return g.cfg.Seats.Authenticate(r.Context(), p[1])','return extseat.Seat{}, nil','TestZeroConfigEveryRequestReauthenticates','ZERO_REQUEST_AUTH')
add('tenant','../feishulogin/identity.go','if info.TenantKey == "" || info.TenantKey != allowedTenant {','if info.TenantKey == "" {','TestCallbackRejectsWrongTenant','wrong tenant was not rejected before issuance','./internal/feishulogin')
add('audience','../extseat/service.go','s.Resource == "" || audience != s.Resource || scopes != extshared.ScopeString()','s.Resource == "" || audience == "MUTANT" || scopes != extshared.ScopeString()','TestTokenAudiencePG','wrong audience accepted','./internal/extseat')
add('native-callback-enabled','codex_client.go','u.Path, u.RawPath = "/callback", ""','u.Path, u.RawPath = u.Path, u.RawPath','TestCodexNativeCallbackPathsBothClients/default/desktop12','NATIVE_CALLBACK_desktop12')
add('native-length-bound','codex_client.go','{1,64}','{1,65}','TestCodexNativeCallbackPathsBothClients/default/over64','NATIVE_CALLBACK_over64')
add('native-safe-alphabet','codex_client.go','[A-Za-z0-9_-]','[A-Za-z0-9_.-]','TestCodexNativeCallbackPathsBothClients/default/dot$','NATIVE_CALLBACK_dot')
add('native-single-segment','codex_client.go','[A-Za-z0-9_-]','[A-Za-z0-9_/-]','TestCodexNativeCallbackPathsBothClients/default/slash','NATIVE_CALLBACK_slash')
add('native-exact-clients','oauth.go','if localClient || q.Get("client_id") == codexDefaultClientID {','if true {','TestCodexCallbackExtensionDoesNotApplyToOtherClients','NATIVE_CALLBACK_EXACT_CLIENTS_ONLY')
add('copy-http-only','codex_client.go',' && u.Scheme == "http"','','TestZeroConfigInvalidCopyFailsClosed/https_loopback','ZERO_COPY_https_loopback')
results=[]
for name,file,old,new,test,target,package in items:
 source=(ROOT/'internal/mcpgw'/file).resolve(); before=source.read_bytes()
 assert old in before.decode(), (name,'mutation needle missing')
 with tempfile.TemporaryDirectory(prefix='zc-mut-') as temp:
  d=Path(temp)
  cache=Path('/home/dev/.embedded-postgres-go')
  if cache.exists(): (d/'.embedded-postgres-go').symlink_to(cache,target_is_directory=True)
  env={'PATH':'/usr/bin:/bin','HOME':temp,'TMPDIR':temp,'GOCACHE':'/home/dev/.cache/go-build','GOMODCACHE':'/home/dev/go/pkg/mod'}
  command=['go','test',package,'-run','^'+test+'$','-count=1','-v']
  start=time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime())
  baseline=subprocess.run(command,cwd=ROOT,env=env,capture_output=True)
  (a.output/(name+'-baseline.stdout')).write_bytes(baseline.stdout+baseline.stderr)
  assert baseline.returncode==0 and b'--- PASS:' in baseline.stdout and b'[no tests to run]' not in baseline.stdout and b'warning: no tests to run' not in baseline.stdout, (name,baseline.stdout+baseline.stderr)
  mutant=d/('mutant.json' if source.suffix=='.json' else 'mutant.go')
  mutant.write_text(before.decode().replace(old,new,1))
  overlay=d/'overlay.json'; overlay.write_text(json.dumps({'Replace':{str(source):str(mutant)}}))
  command=command[:2]+['-vet=off','-overlay='+str(overlay)]+command[2:]
  p=subprocess.run(command,cwd=ROOT,env=env,capture_output=True)
  body=p.stdout+p.stderr; (a.output/(name+'-mutant.stdout')).write_bytes(body)
  assert p.returncode==1 and target.encode() in body and b'--- FAIL:' in body and b'[build failed]' not in body, (name,body)
  assert source.read_bytes()==before
  result={'guard':name,'source':str(source.relative_to(ROOT)),'test':test,'assertion':target,'baseline_rc':baseline.returncode,'mutant_rc':p.returncode,'status':'target_assertion_red','start_utc':start,'command':command,'source_sha256':hashlib.sha256(before).hexdigest(),'worktree_unchanged':True}
  results.append(result)
  (a.output/'RESULT.json').write_text(json.dumps(results,indent=2)+'\n')
  print(name+': target_assertion_red',flush=True)
print(json.dumps({'mutations':len(results),'target_assertion_red':len(results)}))
