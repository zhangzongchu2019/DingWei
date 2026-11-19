from pathlib import Path
import json,subprocess,hashlib,difflib,datetime,re,sys
A=Path(__file__).resolve().parent;w=Path('/data/ai-dev-data/mcp-dcr');e=json.loads((A/'frozen-env.json').read_text());source=e['SOURCE_COMMIT'];assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=w,text=True).strip()==source
plans=json.loads((A/'mutation-plan.json').read_text());D=A/'mutations';D.mkdir(exist_ok=True);results=[]
def sha(b):return hashlib.sha256(b).hexdigest()
def run(name,cmd):
 start=datetime.datetime.now(datetime.timezone.utc).isoformat()
 with (D/(name+'.log')).open('w') as out:
  try:p=subprocess.run(cmd,cwd=w,env=e,stdout=out,stderr=subprocess.STDOUT,timeout=65);rc=p.returncode
  except subprocess.TimeoutExpired:rc=124
 return {'command':cmd,'rc':rc,'start_utc':start,'end_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'log_sha256':sha((D/(name+'.log')).read_bytes())}
for m in plans:
 f=w/m['file'];original=subprocess.check_output(['git','show',source+':'+m['file']],cwd=w);assert f.read_bytes()==original
 txt=original.decode();start=-1
 for _ in range(m['occurrence']+1):start=txt.index(m['old'],start+1)
 mutant=(txt[:start]+m['new']+txt[start+len(m['old']):]).encode();diff=''.join(difflib.unified_diff(txt.splitlines(True),mutant.decode().splitlines(True),fromfile='a/'+m['file'],tofile='b/'+m['file']))
 (D/(m['id']+'.patch')).write_text(diff);r={'source':source,**m,'original_sha256':sha(original),'mutant_sha256':sha(mutant),'patch_sha256':sha(diff.encode())}
 try:
  f.write_bytes(mutant)
  r['build']=run(m['id']+'-build',['go','test','./internal/mcpgw','-run','^$','-count=1'])
  if r['build']['rc']==0:
   r['test']=run(m['id']+'-test',['go','test','./internal/mcpgw','-run','^'+m['test']+'$','-count=1','-v','-timeout=45s'])
   log=(D/(m['id']+'-test.log')).read_text();r['assertions']=[line for line in re.findall(r'(?m)^\s+[^\n]+\.go:\d+:.*$',log) if 'DCR_PG_VERSION' not in line]
   r['killed']=r['test']['rc']!=0 and bool(r['assertions']) and ('--- FAIL: '+m['test']) in log and not any(x in log for x in ['panic:','[build failed]','SQLSTATE','test timed out','DATA RACE'])
  else:r['killed']=False
 finally:
  f.write_bytes(original);r['restored_sha256']=sha(f.read_bytes());assert r['restored_sha256']==r['original_sha256']
 r['baseline']=run(m['id']+'-restored',['go','test','./internal/mcpgw','-run','^'+m['test']+'$','-count=1','-v','-timeout=45s'])
 r['accepted']=r['killed'] and r['baseline']['rc']==0
 (D/(m['id']+'.json')).write_text(json.dumps(r,ensure_ascii=False,indent=2)+'\n');results.append(r);(D/'summary.json').write_text(json.dumps(results,ensure_ascii=False,indent=2)+'\n')
 print(m['id'],'KILLED' if r['accepted'] else 'STOP',r.get('assertions',[])[:1],flush=True)
 if not r['accepted']:sys.exit(1)
assert subprocess.check_output(['git','diff','--name-only',source,'--','internal','cmd','tools','scripts','deploy','go.mod','go.sum','VERSION'],cwd=w,text=True)==''
print('MUTATIONS_OK',len(results),flush=True)
