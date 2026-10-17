from pathlib import Path
import json,subprocess,hashlib,re,time
lab=Path(__file__).parent;repo=lab.parent/'mutant';env=json.loads((lab/'ENVIRONMENT.json').read_text());env={k:v for k,v in env.items() if k not in ('GOCACHE','GOPATH')};p=repo/'deploy/ops/hosted-claude-image/build-image.sh';original=p.read_text();source='cb5d9d3a0e5eb81e94abd0e996fe5f0fd520f7b1'
pos='build_entry_uses_verified_context_offline_parameters_and_inspected_id'
cases=[('restore-unconditional-refusal',original[:original.index('actual_engine_id=$(docker')]+"echo 'packager is not approved' >&2\nexit 2\n",pos),
 ('remove-engine-id-check',original.replace('if [ -z "$actual_engine_id" ] || [ "$actual_engine_id" != "$HOSTED_BUILD_ENGINE_ID" ]; then','if false; then'),'build_entry_identity_mismatch'),
 ('remove-default-alias-check',original.replace("if path in (Path('/var/run/docker.sock').resolve(), Path('/run/docker.sock').resolve()):","if False:"),'default_alias_guard_even_when_default_socket_exists'),
 ('remove-socket-type-check',original.replace('if not stat.S_ISSOCK(info.st_mode):','if False:'),'build_entry_socket_alias_and_metadata_refusals'),
 ('remove-offline-network-flag',original.replace(' --network=none',''),pos),
 ('skip-image-inspect',original.replace('image_id=$(docker --host "$engine" image inspect --format \'{{.Id}}\' "$HOSTED_IMAGE_TAG")',"image_id=sha256:"+'a'*64),pos),
 ('remove-child-bytecode-protection',original.replace('python3 -B "$HERE/prepare_context.py"','python3 "$HERE/prepare_context.py"'),pos),
 ('remove-inspected-id-validation',original[:original.index('case "$image_id" in')]+"printf '%s\\n' \"$image_id\"\n",'build_entry_build_and_inspect_failures')]
cases.append(('remove-uri-ambiguity-check',original.replace("if any(c.isspace() or ord(c) < 32 or ord(c) == 127 or c in '%?#' for c in value):",'if False:'),'build_entry_rejects_ambiguous_uri'))
records=[]
try:
 for name,mutated,pattern in cases:
  assert mutated!=original
  record={'name':name,'source':source,'product_path':str(p.relative_to(repo))}
  for phase in ['baseline','withdrawal','restored']:
   p.write_text(mutated if phase=='withdrawal' else original);argv=['python3','-m','unittest','discover','-s','deploy/ops/hosted-claude-image','-p','test_claude_start.py','-k',pattern,'-v'];path=lab/'raw'/('control-'+name+'-'+phase+'.log');t=time.monotonic()
   with path.open('w') as out:r=subprocess.run(argv,cwd=repo,env=env,stdout=out,stderr=subprocess.STDOUT)
   text=path.read_text();match=re.search(r'Ran (\d+) tests?',text);row={'argv':argv,'rc':r.returncode,'elapsed_seconds':round(time.monotonic()-t,3),'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'product_sha256':hashlib.sha256(p.read_bytes()).hexdigest(),'tests':int(match[1]) if match else None,'assertion_red':'\nFAIL:' in text and 'FAILED (failures=' in text};record[phase]=row;print(name,phase,json.dumps(row),flush=True)
   assert r.returncode==(1 if phase=='withdrawal' else 0),(name,phase,text[-2500:]);assert row['tests']==1
   if phase=='withdrawal':assert row['assertion_red'] and 'errors=' not in text
  records.append(record)
finally:p.write_text(original)
(lab/'CONTROLS.json').write_text(json.dumps(records,indent=2)+'\n');print('BUILD_ENTRY_9_CONTROLS_OK',flush=True)
