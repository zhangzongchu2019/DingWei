"""Compatibility error mutations: Git-tracked Python source copies, no live state."""
import argparse,json,os,pathlib,subprocess,sys,tempfile
ROOT=pathlib.Path(__file__).resolve().parents[2]
def main():
 p=argparse.ArgumentParser();p.add_argument('--legacy-tree',type=pathlib.Path,required=True);p.add_argument('--output',type=pathlib.Path,required=True);a=p.parse_args();a.output.mkdir(parents=True,exist_ok=True)
 cases=[('old-hub-404',"if exc.code==404 or code=='coordinate_review_not_enabled':","if code=='coordinate_review_not_enabled':",'old-hub.log','R6_COMPAT_CLEAR_DISABLED'),('dark-hub-code',"if exc.code==404 or code=='coordinate_review_not_enabled':","if exc.code==404:",'new-hub.log','R6_COMPAT_CLEAR_DISABLED'),('unknown-write',"if isinstance(payload,dict):code=payload.get('error')","if isinstance(payload,dict):code='coordinate_review_not_enabled'",None,'R6_COMPAT_WRITE_UNCERTAINTY_SWALLOWED')]
 results=[]
 with tempfile.TemporaryDirectory(prefix='r6-cmp-mut-') as temp:
  temp=pathlib.Path(temp);tracked=subprocess.check_output(['git','ls-files','-z','tools/sessionhelper/sessionhelper'],cwd=ROOT).decode().split('\x00')
  for name,old,new,logfile,marker in cases:
   helper=temp/name;helper.mkdir()
   for file in filter(None,tracked):
    rel=pathlib.Path(file).relative_to('tools/sessionhelper');target=helper/rel;target.parent.mkdir(parents=True,exist_ok=True);target.write_bytes((ROOT/file).read_bytes())
   source=helper/'sessionhelper/spec_review.py';text=source.read_text();assert text.count(old)==1;source.write_text(text.replace(old,new,1))
   target=a.output/name
   if logfile:
    cmd=[sys.executable,str(ROOT/'tests/r6-compat/run.py'),'--legacy-tree',str(a.legacy_tree),'--output',str(target),'--helper-tree',str(helper)];r=subprocess.run(cmd,cwd=ROOT,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
    details=(target/logfile).read_text() if (target/logfile).exists() else r.stdout
   else:
    env={'PATH':os.environ['PATH'],'HOME':str(temp/'home'),'PYTHONPATH':str(helper),'LANG':'C.UTF-8'}
    code="""import io,urllib.error
from unittest.mock import patch
from sessionhelper.spec_review import coordinate_review_request
error=urllib.error.HTTPError('http://local',503,'closed',{},io.BytesIO(b'{}'))
with patch('urllib.request.OpenerDirector.open',side_effect=error):
 result=coordinate_review_request('ws://127.0.0.1/ws/agent',{}, {'action':'open','body':{'request_id':'coordreview_'+'a'*64}})
assert result['outcome']=='unknown','R6_COMPAT_WRITE_UNCERTAINTY_SWALLOWED'
"""
    r=subprocess.run([sys.executable,'-c',code],cwd=temp,env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT);details=r.stdout
   ok=r.returncode!=0 and marker in details and 'ModuleNotFoundError' not in details and '[build failed]' not in details
   (a.output/(name+'.log')).write_text(details);results.append({'mutation':name,'rc':r.returncode,'expected_assertion':marker,'matched':ok});print(json.dumps(results[-1]),flush=True)
 (a.output/'results.json').write_text(json.dumps(results,indent=2)+'\n');return 0 if all(r['matched'] for r in results) else 1
if __name__=='__main__':sys.exit(main())
