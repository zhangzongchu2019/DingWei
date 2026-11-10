"""Start ONLY an isolated test Hub, or run the isolated CIMD boundary fixture."""
import argparse,json,os,subprocess
from pathlib import Path
ROOT=Path(__file__).resolve().parents[3]
ap=argparse.ArgumentParser();ap.add_argument('--run-dir',type=Path,default=Path('/tmp/coder-r56-compat-native'));ap.add_argument('--browser-cut',choices=['connect-csp','selection-csp','loopback-csp']);ap.add_argument('--browser-chain',choices=['immediate','consent'],default='consent');ap.add_argument('--browser-mode',choices=['single','multi']);ap.add_argument('--mutation',choices=['codec','ports','default-client']);args=ap.parse_args()
p=args.run_dir.resolve()
p.mkdir(mode=0o700,parents=True,exist_ok=False)
for n in ['home','tmp','codex']:(p/n).mkdir(mode=0o700)
# Only the downloaded public PG cache is reused, never HOME configuration.
cache=Path.home()/'.embedded-postgres-go'
if cache.exists():(p/'home/.embedded-postgres-go').symlink_to(cache,target_is_directory=True)
env={'INTEROP_BROWSER_CHAIN':args.browser_chain, 'INTEROP_BROWSER_MODE':args.browser_mode or '', 'GOPROXY':'off','HUB_TEST_PG_STRICT':'1','INTEROP_ISS_UNSUPPORTED':os.environ.get('INTEROP_ISS_UNSUPPORTED','0'),'INTEROP_FOREIGN_TENANT':os.environ.get('INTEROP_FOREIGN_TENANT','0'),'PATH':os.environ['PATH'],'HOME':str(p/'home'),'TMPDIR':str(p/'tmp'),'CODEX_HOME':str(p/'codex'),'INTEROP_RUN_DIR':str(p),'GOCACHE':subprocess.check_output(['go','env','GOCACHE'],text=True).strip(),'GOMODCACHE':subprocess.check_output(['go','env','GOMODCACHE'],text=True).strip()}
name='hub'
target='cmd/workpulse/codex_interop_probe_test.go'
fixture='hub_probe_test.go.txt'
package='./cmd/workpulse'
pattern='^TestCodexInteropLocalHub$'
replacements={str(ROOT/target):str(Path(__file__).with_name(fixture))}
if args.mutation:
 relative={'codec':'internal/mcpgw/codec2025.go','ports':'internal/mcpgw/clients.go','default-client':'internal/mcpgw/oauth.go'}[args.mutation]
 source=(ROOT/relative).read_text()
 if args.mutation=='codec':
  needle='g.mcp2025(w, r)\n\t\treturn true';assert source.count(needle)==1;source=source.replace(needle,'return false')
 elif args.mutation=='default-client':
  needle='} else if q.Get("client_id") == codexDefaultClientID {';assert source.count(needle)==1;source=source.replace(needle,'} else if false {')
 else:
  pos=source.rfind('\t\treturn true');assert pos>0;source=source[:pos]+source[pos:].replace('return true','return false',1)
 mutant=p/'mutant.go';mutant.write_text(source);replacements[str(ROOT/relative)]=str(mutant)
if args.browser_cut:
 relative={'connect-csp':'internal/feishulogin/handler.go','selection-csp':'internal/mcpgw/selection.go','loopback-csp':'internal/mcpgw/oauth.go'}[args.browser_cut]
 source=(ROOT/relative).read_text()
 if args.browser_cut=='connect-csp':
  old='connectPageCSPFor(authorizeOriginOf(cfg.OAuth, cfg.RedirectURI))';new='connectPageCSPFor("")'
 elif args.browser_cut=='loopback-csp':
  old='withLoopbackFormAction(csp, a.Redirect)';new='csp'
 else:
  old=', a.Redirect)';new=')'
 assert source.count(old)==1
 source=source.replace(old,new,1)
 mutant=p/'browser-cut.go';mutant.write_text(source);replacements[str(ROOT/relative)]=str(mutant)
overlay=p/(name+'-overlay.json');overlay.write_text(json.dumps({'Replace':replacements}))
with (p/(name+'.stdout')).open('wb') as log:
 proc=subprocess.Popen(['go','test','-vet=off','-overlay='+str(overlay),package,'-run',pattern,'-count=1','-timeout=26m','-v'],env=env,cwd=ROOT,stdout=log,stderr=subprocess.STDOUT)
 try:rc=proc.wait()
 except KeyboardInterrupt:
  (p/'stop').touch()
  try:rc=proc.wait(timeout=90)
  except subprocess.TimeoutExpired:proc.terminate();rc=proc.wait(timeout=5)
(p/(name+'-exit.json')).write_text(json.dumps({'rc':rc,'inherited_env_names':sorted(env),'real_credentials_used':False},indent=2)+'\n')
raise SystemExit(rc)
