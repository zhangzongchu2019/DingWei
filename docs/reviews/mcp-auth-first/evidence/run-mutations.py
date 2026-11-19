import datetime,hashlib,json,subprocess
from pathlib import Path
R=Path(__file__).resolve().parent;W=Path('/data/ai-dev-data/wt-mcp-auth-first-mutations');E=json.loads((R/'env-source.json').read_text());D=R/'mutations';D.mkdir(exist_ok=True)
CASES=[
('M1-version-before-auth','internal/mcpgw/gateway.go','func (g *Gateway) mcp(w http.ResponseWriter, r *http.Request) {','''func (g *Gateway) mcp(w http.ResponseWriter, r *http.Request) {
 if singleHeader(r, "MCP-Protocol-Version") != CompatProtocolVersion {
  rpcError(w, 400, nil, wireError{Code:-32022, Message:"Unsupported protocol version"}); return
 }''','TestMCPAuthFirstThreeVersionsAndMissingHeaders','AUTH_BEFORE_PROTOCOL'),
('M2-reject-proposed-version','internal/mcpgw/codec2025.go','p.Version == ""','p.Version != CompatProtocolVersion','TestMCPInitializeNegotiatesSupportedVersionAndUsesItForTools','VERSION_NEGOTIATION'),
('M3-bypass-authenticated-origin','internal/mcpgw/gateway.go','''g.guard(func(w http.ResponseWriter, r *http.Request) {
		g.mcpAuthorized(w, r, seat)
	})(w, r)''','g.mcpAuthorized(w, r, seat)','TestMCPMissingVersionMeansMarchNotPrivateEnvelope','authenticated Origin guard bypassed'),
('M4-allow-unsupported-operating-version','internal/mcpgw/codec2025.go','(q.Method != "initialize" && version != CompatProtocolVersion)','false','TestMCPMissingVersionMeansMarchNotPrivateEnvelope','DEFAULT_MARCH_VERSION')]
def run(n,cmd):
 start=datetime.datetime.now(datetime.timezone.utc).isoformat()
 with (D/(n+'.log')).open('w') as f:p=subprocess.run(cmd,cwd=W,env=E,stdout=f,stderr=subprocess.STDOUT)
 b=(D/(n+'.log')).read_bytes();return {'command':cmd,'rc':p.returncode,'start_utc':start,'end_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'log':n+'.log','sha256':hashlib.sha256(b).hexdigest()}
basecmd=['go','test','./internal/mcpgw','-run','^TestMCP','-count=1','-v'];result={'source':E['SOURCE_COMMIT'],'baseline':run('baseline',basecmd),'mutations':[]};assert result['baseline']['rc']==0
for name,path,old,new,test,marker in CASES:
 f=W/path;b=f.read_bytes();assert b.decode().count(old)==1,(name,b.decode().count(old))
 try:
  f.write_text(b.decode().replace(old,new,1));build=run(name+'-build',['go','test','./internal/mcpgw','-run','^$','-count=1'])
  testresult=run(name,['go','test','./internal/mcpgw','-run','^'+test+'$','-count=1','-v']) if build['rc']==0 else None
  text=(D/(name+'.log')).read_text() if testresult else ''
  result['mutations'].append({'name':name,'path':path,'old':old,'new':new,'build':build,'test':testresult,'business_red':bool(testresult and testresult['rc']!=0 and marker in text and 'panic:' not in text),'failed_assertions':[s for s in text.splitlines() if 'auth_first_test.go:' in s]})
 finally:f.write_bytes(b)
result['final_baseline']=run('final-baseline',basecmd);result['porcelain']=subprocess.check_output(['git','status','--porcelain'],cwd=W,text=True);(D/'summary.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n');print([(m['name'],m['business_red']) for m in result['mutations']])
