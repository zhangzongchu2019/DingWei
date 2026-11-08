"""Isolated one-cut tenant/localhost negatives; never edit the working tree."""
from pathlib import Path
import argparse,hashlib,json,os,subprocess,tempfile
ROOT=Path(__file__).resolve().parents[3]
ap=argparse.ArgumentParser();ap.add_argument('--output',type=Path,required=True);a=ap.parse_args();a.output.mkdir(parents=True,exist_ok=False)
items=[('localhost','internal/mcpgw/clients.go','return u.Scheme == "http" && ip != nil && ip.IsLoopback()','return u.Scheme == "http" && ((ip != nil && ip.IsLoopback()) || u.Hostname() == "localhost")','./internal/mcpgw','^TestLoopbackPortMatchingAndLocalhostRejection$','LOCALHOST_METADATA_MUST_BE_REJECTED'),('tenant','internal/feishulogin/identity.go','if info.TenantKey == "" || info.TenantKey != allowedTenant {','if info.TenantKey == "" {','./internal/feishulogin','^TestCallbackRejectsWrongTenant$','wrong tenant was not rejected before issuance')]
results=[]
for name,relative,needle,replacement,package,pattern,target in items:
 before=(ROOT/relative).read_bytes();assert needle in before.decode()
 with tempfile.TemporaryDirectory(prefix='r56-'+name+'-') as tmp:
  env={'PATH':os.environ['PATH'],'HOME':tmp,'TMPDIR':tmp,'GOCACHE':'/home/dev/.cache/go-build','GOMODCACHE':'/home/dev/go/pkg/mod'}
  baseline=subprocess.run(['go','test',package,'-run',pattern,'-count=1','-v'],cwd=ROOT,env=env,capture_output=True);(a.output/(name+'-baseline.stdout')).write_bytes(baseline.stdout+baseline.stderr);assert baseline.returncode==0
  mutant=Path(tmp)/'mutant.go';mutant.write_text(before.decode().replace(needle,replacement,1));overlay=Path(tmp)/'overlay.json';overlay.write_text(json.dumps({'Replace':{str(ROOT/relative):str(mutant)}}))
  result=subprocess.run(['go','test','-vet=off','-overlay='+str(overlay),package,'-run',pattern,'-count=1','-v'],cwd=ROOT,env=env,capture_output=True)
  body=result.stdout+result.stderr;(a.output/(name+'-mutant.stdout')).write_bytes(body);assert result.returncode==1 and target in body.decode(),(name,body)
  assert (ROOT/relative).read_bytes()==before
  results.append({'kind':name,'baseline_rc':baseline.returncode,'mutant_rc':result.returncode,'target':target,'original_sha256':hashlib.sha256(before).hexdigest(),'worktree_unchanged':True})
(a.output/'RESULT.json').write_text(json.dumps(results,indent=2)+'\n');print(json.dumps(results))
