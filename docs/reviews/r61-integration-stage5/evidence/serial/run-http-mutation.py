from pathlib import Path
import json,subprocess,hashlib,datetime
D=Path(__file__).resolve().parent;W=Path('/data/ai-dev-data/r61-integration');T=Path('/data/ai-dev-data/wt-r61-stage5-mutations');E=json.loads((D/'env-http.json').read_text());S=E['SOURCE_COMMIT']
assert json.loads((D/'full-acceptance.json').read_text())['passed'];assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=W).decode().strip()==S
assert subprocess.check_output(['git','status','--porcelain'],cwd=T)==b'';assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=T).decode().strip()==S
E.update(HOME='/data/ai-dev-data/r61s5smuth',TMPDIR='/data/ai-dev-data/r61s5smutt',PYTHONPATH=str(T/'tools/sessionhelper'))
for k in ['HOME','TMPDIR']:Path(E[k]).mkdir(parents=True,exist_ok=True)
A=D/'http-four-mutation';A.mkdir();(A/'env.json').write_text(json.dumps(E,indent=2)+'\n');results={'source':S,'physical_knives':1,'serial_after_full_gates':True,'scope':'integration HTTP4 order guard; do not combine with component12 or independent5'}
def run(name,cmd):
 start=datetime.datetime.now(datetime.timezone.utc).isoformat()
 with(A/(name+'.log')).open('w')as out:p=subprocess.run(cmd,cwd=T,env=E,stdout=out,stderr=subprocess.STDOUT)
 b=(A/(name+'.log')).read_bytes();results[name]={'command':cmd,'rc':p.returncode,'start_utc':start,'end_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'sha256':hashlib.sha256(b).hexdigest()};return p.returncode,b
cmd=['go','test','./internal/mcpgw','-run','^TestR61MCPUnauthenticatedFourBaselinesOverRealHTTP$','-count=1','-v'];assert run('baseline',cmd)[0]==0
p=T/'internal/mcpgw/gateway.go';orig=p.read_bytes();old=b'func (g *Gateway) mcp(w http.ResponseWriter, r *http.Request) {';new=old+b'\n if singleHeader(r, "MCP-Protocol-Version") != CompatProtocolVersion { rpcError(w, 400, nil, wireError{Code:-32022, Message:"Unsupported protocol version"}); return }';assert orig.count(old)==1;results.update(path='internal/mcpgw/gateway.go',old=old.decode(),new=new.decode())
try:
 p.write_bytes(orig.replace(old,new,1));assert run('build',['go','test','./internal/mcpgw','-run','^$','-count=1'])[0]==0
 rc,b=run('mutated',cmd);assert rc!=0 and b'REAL_HTTP_AUTH_FIRST' in b and b'panic:' not in b and b'[build failed]' not in b;results['business_red']=True
finally:p.write_bytes(orig)
assert run('restored',cmd)[0]==0;assert subprocess.check_output(['git','status','--porcelain'],cwd=T)==b'';results.update(restored_sha256=hashlib.sha256(orig).hexdigest(),porcelain='');(A/'summary.json').write_text(json.dumps(results,indent=2)+'\n');print('HTTP4_SERIAL_MUTATION_OK')
