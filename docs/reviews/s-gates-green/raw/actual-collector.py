from pathlib import Path
import os,json,hashlib,subprocess,shutil,datetime
root=Path('/data/ai-dev-data/s-gates-green');raw=Path('/tmp/s-gates-green-20261108');dest=root/'docs/reviews/s-gates-green';target=dest/'raw';target.mkdir(parents=True,exist_ok=True)
required=['build-final','boundary-final','dependency-final','tenant-final','core-count3','race-extseat-full','m8-final-count1','store-final-count1','canonical','mutation-reverted-canonical','openspec-final','entrypoint-contract','pinyin-deps','skip-inventory','boundary-selftest','dependency-selftest']
required += ['race-store-shard-'+str(i) for i in range(8)]
report=json.loads((raw/'RACE-FINAL-REPORT.json').read_text());assert report['list_diff_rc']==0 and report['list_diff_bytes']==0 and not report['overlap']
assert sum(report['totals'].values())==379*3 and report['totals'].get('fail',0)==0
disposition=json.loads((raw/'ORIGINAL-RACE-DISPOSITION.json').read_text());assert disposition['finished_or_stopped'] is True and not disposition['unexpected_failure_signatures']
for name in required:
 m=json.loads((raw/(name+'.json')).read_text());assert m.get('rc')==0,(name,m)
for name in ['mut-extseat-audit','mut-store-audit','mut-release-boundary']:
 m=json.loads((raw/(name+'.json')).read_text());assert m.get('rc')==1,(name,m)
criteria=json.loads((raw/'CRITERION-MUTATIONS.json').read_text());assert len(criteria)==6 and all(x['rc']==1 and x['cause_found'] for x in criteria)
mutations=json.loads((raw/'MUTATIONS.json').read_text());assert len(mutations)==4 and mutations[-1]['rc']==0
frozen=json.loads((raw/'FROZEN-SOURCES.json').read_text());bad=[p for p,h in frozen.items() if hashlib.sha256((root/p).read_bytes()).hexdigest()!=h];assert not bad,bad
for p in (Path('/tmp/s-gates-green-mutations'),Path('/tmp/s-gates-green-criterion-mutations')):
 for name in ['internal/extseat/service.go','internal/store/spec_clarification_inbound.go']:
  assert (p/name).read_bytes()==(root/name).read_bytes(),name
assert not Path('/tmp/s-gates-green-mutations/internal/gatesmutant').exists()
manifest={}
for f in sorted(raw.iterdir()):
 if not f.is_file() or f.suffix not in ('.json','.log','.txt','.md','.diff'):continue
 b=f.read_bytes();parts=[]
 if not b:
  target.joinpath(f.name).write_bytes(b'');parts=[f.name]
 # Keep complete raw text without putting a single large artifact in Git.
 for i in range(0,len(b),500_000):
  name=f.name if len(b)<=500_000 else f.name+'.part'+str(i//500_000+1).zfill(3)
  target.joinpath(name).write_bytes(b[i:i+500_000]);parts.append(name)
 manifest[f.name]={'sha256':hashlib.sha256(b).hexdigest(),'bytes':len(b),'parts':parts}
for src,name in [('/tmp/s-gates-green-run.py','actual-runner.py'),('/tmp/s-gates-green-mutations.py','actual-mutations.py'),('/tmp/s-gates-green-criteria.py','actual-criteria.py'),('/tmp/s-gates-green-race-shards.py','actual-race-shards.py'),('/tmp/s-gates-green-race-report.py','actual-race-report.py'),('/tmp/s-gates-green-collect.py','actual-collector.py'),('/tmp/s-gates-green-stop-original.py','actual-stop-original.py')]:shutil.copyfile(src,target/name)
(target/'RAW-MANIFEST.json').write_text(json.dumps(manifest,indent=2,sort_keys=True)+'\n')
meta={'collected_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'CODEX_THREAD_ID':os.environ['CODEX_THREAD_ID'],'baseline':subprocess.check_output(['git','rev-parse','0fe09b9e'],cwd=root,text=True).strip(),'audit_source_parent':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),'frozen_files':len(frozen),'source_files_unchanged':True,'required_results':{n:json.loads((raw/(n+'.json')).read_text())['rc'] for n in required},'audit_and_release_mutations':mutations,'criterion_mutations':criteria,'no_production_actions':True,'race_mode':'authorized 8-shard entire store inventory eachcount3;extseat fullcount3 separately','race_coverage_totals':report['totals'],'original_full_race_disposition':disposition}
(dest/'RESULTS.json').write_text(json.dumps(meta,indent=2,ensure_ascii=False)+'\n');(raw/'COLLECTED.json').write_text(json.dumps(meta,indent=2,ensure_ascii=False)+'\n')
print('COLLECTED_OK',len(manifest),'logs/manifests; frozen_sources',len(frozen))
