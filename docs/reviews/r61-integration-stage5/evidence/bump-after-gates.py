from pathlib import Path
import json,subprocess,datetime
A=Path(__file__).resolve().parent;D=A/'serial';W=Path('/data/ai-dev-data/r61-integration');r=json.loads((D/'full-acceptance.json').read_text());assert r['passed'] and r['all_serial'];S=r['source'];m=json.loads((D/'http-four-mutation/summary.json').read_text());assert m['source']==S and m['business_red'] and m['restored']['rc']==0
assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=W).decode().strip()==S;assert subprocess.check_output(['git','status','--porcelain'],cwd=W)==b''
items=[('VERSION','0.2.3.40\n','0.2.3.41\n'),('internal/version/version.go','const Version = "0.2.3.40"','const Version = "0.2.3.41"'),('tools/sessionhelper/sessionhelper/__init__.py','__version__ = "0.2.3.40"','__version__ = "0.2.3.41"')]
for f,old,new in items:
 p=W/f;s=p.read_text();assert s.count(old)==1,(f,s);p.write_text(s.replace(old,new,1))
paths=subprocess.check_output(['git','diff','--name-only'],cwd=W,text=True).splitlines();assert sorted(paths)==sorted(x[0]for x in items)
subprocess.run(['git','add',*[x[0]for x in items]],cwd=W,check=True);subprocess.run(['git','-c','user.name=coder56solzzc','-c','user.email=coder56solzzc@dingwei.local','commit','-q','-m','chore(r61): bump version to 0.2.3.41 after serial integration gates'],cwd=W,check=True)
V=subprocess.check_output(['git','rev-parse','HEAD'],cwd=W,text=True).strip();P=A/'postversion';P.mkdir();base=json.loads((D/'env-go.json').read_text())
for kind in ['go','canonical']:
 e=base|{'SOURCE_COMMIT':V,'HOME':'/data/ai-dev-data/r61s5v'+kind+'h','TMPDIR':'/data/ai-dev-data/r61s5v'+kind+'t'}
 for k in ['HOME','TMPDIR']:Path(e[k]).mkdir(parents=True,exist_ok=True)
 (P/('env-'+kind+'.json')).write_text(json.dumps(e,indent=2)+'\n')
(P/'run.py').write_text((D/'run.py').read_text());g=(D/'run-go-policy.py').read_text().replace('m8-store-full3','m8-version-full1').replace('initial_full3_rc','initial_full1_rc').replace('/data/ai-dev-data/wt-r61-stage5-base530','/data/ai-dev-data/wt-r61-stage5-preversion8cf').replace("base='5305358fd2e37f2eabe6f5349877eba7b015d143'","base='"+S+"'").replace('/data/ai-dev-data/r61s5sbaseh','/data/ai-dev-data/r61s5vbaseh').replace('/data/ai-dev-data/r61s5sbaset','/data/ai-dev-data/r61s5vbaset');(P/'run-go-policy.py').write_text(g)
(P/'version-source.json').write_text(json.dumps({'source':V,'tree':subprocess.check_output(['git','rev-parse','HEAD^{tree}'],cwd=W,text=True).strip(),'parent_full_gate_source':S,'version':'0.2.3.41','only_paths':paths,'utc':datetime.datetime.now(datetime.timezone.utc).isoformat()},indent=2)+'\n')
(P/'run-post.py').write_text('''from pathlib import Path\nimport subprocess,sys,json,datetime\nP=Path(__file__).resolve().parent\np=subprocess.run([sys.executable,str(P/'run.py'),'go','m8-version-full1','go','test','./internal/m8','-count=1','-p=1','-timeout=60m','-v']);q=subprocess.run([sys.executable,str(P/'run-go-policy.py')]);assert q.returncode==0,'postversion Go policy STOP'\np=subprocess.run([sys.executable,str(P/'run.py'),'canonical','canonical-version','./build.sh','--test-only']);assert p.returncode==0,'postversion canonical STOP'\n(P/'post-acceptance.json').write_text(json.dumps({'source':json.loads((P/'env-go.json').read_text())['SOURCE_COMMIT'],'all_serial':True,'passed':True,'go_policy':json.loads((P/'go-policy.json').read_text()),'end_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()},indent=2)+'\\n');print('POST_VERSION_ACCEPTED')\n''')
print('VERSION_COMMIT',V)
