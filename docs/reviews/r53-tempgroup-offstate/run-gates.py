import os,subprocess,pathlib,json,datetime,concurrent.futures
root=pathlib.Path('/data/ai-dev-data/r53-tempgroup-offstate'); out=pathlib.Path('/tmp/r53-offstate-gates');out.mkdir(exist_ok=True)
sha=subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip()
env={k:v for k,v in os.environ.items() if not k.startswith(('SH_','DINGWEI_','DW_','DATAD_'))};env.update(HUB_TEST_PG_STRICT='1',TZ='UTC',PATH='/data/ai-dev-data/helper-deps-lock-venv/bin:'+env['PATH'],NODE_PATH='/tmp/coder-a3-playwright/node_modules',OPENSPEC_TELEMETRY='0',SOURCE_COMMIT=sha,TMPDIR='/tmp')
commands={
 'm8':['go','test','-json','-count=1','-timeout=25m','./internal/m8'],
 'cmd':['go','test','-json','-count=1','-timeout=25m','./cmd/workpulse'],
 'browsers':['python3','tools/run-browser-regressions.py','--receipt',str(out/'browser-receipt.md')],
 'canonical':['bash','scripts/run-build-tests.sh'],
 'openspec':['/tmp/coder56solzzc-openspec-1.16.0/node_modules/.bin/openspec','validate','agent-http-mcp-gateway','--strict'],
}
def run(item):
 name,cmd=item;start=datetime.datetime.now(datetime.timezone.utc).isoformat()
 with (out/(name+'.log')).open('w') as f:p=subprocess.run(cmd,cwd=root,env=env,stdout=f,stderr=subprocess.STDOUT)
 result={'gate':name,'source_commit':sha,'command':cmd,'HUB_TEST_PG_STRICT':1,'TMPDIR':'/tmp','started_utc':start,'ended_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'rc':p.returncode}
 (out/(name+'.json')).write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result),flush=True)
with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:list(pool.map(run,commands.items()))
