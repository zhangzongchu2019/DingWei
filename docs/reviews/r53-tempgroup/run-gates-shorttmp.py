import os,subprocess,pathlib,json,datetime,concurrent.futures,tempfile
root=pathlib.Path('/data/ai-dev-data/r53-tempgroup');out=pathlib.Path('/tmp/r53-tempgroup-source9e19');out.mkdir(exist_ok=True)
sha=subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip()
env={k:v for k,v in os.environ.items() if not k.startswith(('SH_','DINGWEI_','DW_','DATAD_'))};env.update(HUB_TEST_PG_STRICT='1',TZ='UTC',PATH='/data/ai-dev-data/helper-deps-lock-venv/bin:'+env['PATH'],NODE_PATH='/tmp/coder-a3-playwright/node_modules',OPENSPEC_TELEMETRY='0',SOURCE_COMMIT=sha,OUT='/tmp/r53-tempgroup-artifacts-'+sha[:8],TMPDIR='/tmp')
commands={
 'packages':['go','test','-json','-count=1','-timeout=20m','./internal/tempgroup','./internal/extseat','./internal/mcpgw','./internal/extwire','./internal/store/migrate','./cmd/workpulse'],
 'race':['go','test','-json','-race','-count=3','-timeout=20m','./internal/tempgroup','./internal/extseat'],
 'release-build':['bash','build.sh'],
 'm8-directed':['go','test','-json','-count=1','./internal/m8','-run','^TestTempGroup|^TestExternal'],
 'browser':['node','view-service/console-temp-groups-regression.js'],
 'openspec':['/tmp/coder56solzzc-openspec-1.16.0/node_modules/.bin/openspec','validate','agent-http-mcp-gateway','--strict'],
 'boundary':['bash','scripts/repo-boundary-gate.sh'],
}
def run(item):
 name,cmd=item;start=datetime.datetime.now(datetime.timezone.utc).isoformat()
 with (out/(name+'.log')).open('w') as f:p=subprocess.run(cmd,cwd=root,env=env,stdout=f,stderr=subprocess.STDOUT)
 result={'gate':name,'source_commit':sha,'command':cmd,'HUB_TEST_PG_STRICT':1,'started_utc':start,'ended_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'rc':p.returncode}
 (out/(name+'.json')).write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result),flush=True)
with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:list(pool.map(run,commands.items()))
