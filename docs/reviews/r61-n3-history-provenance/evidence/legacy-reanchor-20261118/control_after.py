import pathlib,subprocess,json,datetime,hashlib,time
out=pathlib.Path(__file__).parent;root=pathlib.Path('/data/ai-dev-data/wt-r61-n3-legacy-cuts');source='9313197f8ac3e2a585cd399935d5b313084b3b1e'
while not (out/'fixture-export.json').exists():
 if any(r['rc'] for r in json.loads((out/'gates.json').read_text())):raise SystemExit('master gate failed')
 time.sleep(2)
assert json.loads((out/'fixture-export.json').read_text())['rc']==0
env=json.loads((out/'env.json').read_text());p=root/'internal/m8/review_current_route_control_test.go';rows=[]
try:
 for sha,label in [('a32c877cf6ef21a909d2806f4b08e3e925d05d98','original-a32'),(source,'fixed-931')]:
  assert not subprocess.check_output(['git','status','--porcelain'],cwd=root).strip()
  subprocess.run(['git','switch','--detach',sha],cwd=root,check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
  p.write_bytes((out/'PROBE-current-route-control.go.txt').read_bytes())
  cmd=['go','test','./internal/m8','-run','^TestHistoryProvenanceCurrentRouteLegacyReanchorControl$','-count=1','-v'];env['SOURCE_COMMIT']=sha;r={'source_commit':sha,'test_overlay':'current real route + private-marker assertion + late legacy ACK','command':cmd,'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
  with (out/('route-control-'+label+'.log')).open('wb') as f:r['rc']=subprocess.run(cmd,cwd=root,env=env,stdout=f,stderr=subprocess.STDOUT).returncode
  r['ended_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();r['raw_sha256']=hashlib.sha256((out/('route-control-'+label+'.log')).read_bytes()).hexdigest();rows.append(r);p.unlink();(out/'current-route-controls.json').write_text(json.dumps(rows,indent=2)+'\n');print(label,r['rc'],flush=True)
finally:
 if p.exists():p.unlink()
 subprocess.run(['git','switch','--detach',source],cwd=root,check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
assert not subprocess.check_output(['git','status','--porcelain'],cwd=root).strip()
