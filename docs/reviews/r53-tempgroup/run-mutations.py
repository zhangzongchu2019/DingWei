import pathlib,tempfile,subprocess,json,os,concurrent.futures,datetime
root=pathlib.Path('/data/ai-dev-data/r53-tempgroup');out=pathlib.Path('/tmp/r53-tempgroup-gates/mutations');out=pathlib.Path('/tmp/r53-tempgroup-source9e19/mutations');out.mkdir(parents=True,exist_ok=True)
source=subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip()
env={k:v for k,v in os.environ.items() if not k.startswith(('SH_','DINGWEI_','DW_','DATAD_'))};env.update(HUB_TEST_PG_STRICT='1',TZ='UTC')
service=(root/'internal/tempgroup/service.go').read_text();console=(root/'internal/tempgroup/console.go').read_text();fixture=(root/'internal/tempgroup/service_pg_test.go').read_text()
def replace(text,old,new):
 if text.count(old)!=1:raise RuntimeError('mutation anchor count='+str(text.count(old))+': '+old)
 return text.replace(old,new,1)
reverse="`SELECT g.code,g.generation,g.target_agent_id,g.expires_at,g.dissolved_at FROM np_temp_group g JOIN np_temp_group_member m ON m.code=g.code WHERE m.agent_id=$1 AND m.role='external' FOR UPDATE OF g`, a.AgentID"
mutants={
 'mirror':({'internal/tempgroup/service.go':replace(service,"`INSERT INTO np_group_member(code,agent_id,created_at) VALUES($1,$2,$4),($1,$3,$4)`","`SELECT $1::text,$2::text,$3::text,$4::timestamptz`")},'^TestRealGroupsPG/MirrorReachabilityAndAgentIDRecheck$','mirror members missing'),
 'reverse':({'internal/tempgroup/service.go':replace(service,reverse,"`SELECT g.code,g.generation,g.target_agent_id,g.expires_at,g.dissolved_at FROM np_temp_group g WHERE g.code=$1 FOR UPDATE`, string(a.Group)")},'^TestRealGroupsPG/MirrorReachabilityAndAgentIDRecheck$','group dissolved or expired'),
 'counter':({'internal/tempgroup/service.go':replace(service,"SELECT nextval('np_temp_group_sequence')","UPDATE np_temp_group_counter SET last_seq=last_seq+1 RETURNING last_seq"),'internal/tempgroup/service_pg_test.go':replace(fixture,'identity.PGSchemaSQL + string(a) + string(d)','identity.PGSchemaSQL + string(a) + string(d) + `CREATE TABLE np_temp_group_counter(last_seq bigint NOT NULL);INSERT INTO np_temp_group_counter VALUES(0);`')},'^TestRealGroupsPG/SequenceBurnsRollbackAndConcurrentUnique$','sequence rollback reused number'),
 'filter':({'internal/tempgroup/console.go':replace(replace(console,'WHERE a.owner_lc=$1 AND','WHERE (a.owner_lc=$1 OR true) AND'),"(a.owner_lc=$1 AND a.state<>'revoked')","(a.state<>'revoked')")},'^TestRealGroupsPG/ConsoleOnlyInternalOwnerAndDissolveFence$','login-owner filter'),
}
def run(item):
 name,(files,pattern,expected)=item;folder=out/name;folder.mkdir(exist_ok=True);overlay={}
 for i,(path,body) in enumerate(files.items()):
  target=folder/('mutant'+str(i)+'.go');target.write_text(body);overlay[str(root/path)]=str(target)
 op=folder/'overlay.json';op.write_text(json.dumps({'Replace':overlay}));cmd=['go','test','-json','-overlay',str(op),'-count=1','./internal/tempgroup','-run',pattern]
 start=datetime.datetime.now(datetime.timezone.utc).isoformat()
 p=subprocess.run(cmd,cwd=root,env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True);(folder/'raw.log').write_text(p.stdout)
 hit=p.returncode!=0 and expected in p.stdout and '[build failed]' not in p.stdout
 result={'name':name,'source_commit':source,'command':cmd,'started_utc':start,'ended_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'rc':p.returncode,'expected':expected,'killed_by_expected_assertion':hit}
 (folder/'result.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result),flush=True);return hit
with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:ok=list(pool.map(run,mutants.items()))
raise SystemExit(0 if all(ok) else 1)
