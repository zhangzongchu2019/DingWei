import pathlib,subprocess,json,hashlib,datetime,re
out=pathlib.Path(__file__).parent;root=pathlib.Path('/data/ai-dev-data/wt-r61-n3-legacy-cuts');env=json.loads((out/'env.json').read_text());path=root/'internal/m8/hub.go';original=path.read_bytes();anchor='h.publishInputObservation(d.KeyID, d.SessionName, original, d.ID, "helper_received", "primary", false, nil)';assert original.decode().count(anchor)==1
rows=[]
for name,after,want in [('legacy_history_target',anchor.replace(', nil)',')'),'legacy reanchor minted captured provenance'),('legacy_realtime_preserved','// cut realtime reanchor','legacy receipt lost realtime input reanchor')]:
 try:
  path.write_text(original.decode().replace(anchor,after,1));row={'name':name,'before':anchor,'after':after,'source':env['SOURCE_COMMIT'],'original_sha256':hashlib.sha256(original).hexdigest(),'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
  for stage,cmd in [('build',['go','test','./internal/m8','-run','^$','-count=1']),('test',['go','test','./internal/m8','-run','^TestHistoryProvenanceLegacyReanchorDoesNotFollowReusedName$','-count=1','-v'])]:
   with (out/(name+'-'+stage+'.log')).open('wb') as f:rc=subprocess.run(cmd,cwd=root,env=env,stdout=f,stderr=subprocess.STDOUT).returncode
   row[stage+'_rc']=rc;raw=(out/(name+'-'+stage+'.log')).read_bytes();row[stage+'_raw_sha256']=hashlib.sha256(raw).hexdigest()
   if stage=='test':row['business_assertion']=want in raw.decode() and b'panic:' not in raw
 finally:path.write_bytes(original)
 row['restored_sha256']=hashlib.sha256(path.read_bytes()).hexdigest();row['ended_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();rows.append(row);(out/'new-mutations.json').write_text(json.dumps(rows,indent=2)+'\n');print(name,row['build_rc'],row['test_rc'],row['business_assertion'],flush=True)
 assert row['build_rc']==0 and row['test_rc']!=0 and row['business_assertion']
assert not subprocess.check_output(['git','status','--porcelain'],cwd=root).strip()
