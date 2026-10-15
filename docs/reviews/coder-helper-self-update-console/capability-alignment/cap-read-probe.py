from pathlib import Path
import tempfile,subprocess,json
root=Path.cwd();test=root/'internal/m8/console_helper_updates_test.go';source=root/'internal/m8/console_helper_updates.go'
s=test.read_text();anchor='\t// A claimed target version alone is not proof of updater support.\n';assert s.count(anchor)==1
extra='''\tif st, raw := call("GET", v.tokZZC, "", ""); st != 200 || strings.Contains(raw, `"eligible":true`) {
\t\tt.Fatalf("without capability advertised actionable: %d %s", st, raw)
\t}
'''
s=s.replace(anchor,anchor+extra)
Path('/tmp/coder-console-helper-tests-with-cap-get.go').write_text(s)
results=[]
with tempfile.TemporaryDirectory(prefix='console-cap-probe-') as td:
 p=Path(td);testcopy=p/'test.go';testcopy.write_text(s);codecopy=p/'handler.go'
 a='capable := out.Online && h.sessionSupportsHelperUpdate(c)';original=source.read_text();assert original.count(a)==1;codecopy.write_text(original.replace(a,'capable := out.Online'))
 for label,replace in [('positive',{str(test):str(testcopy)}),('withdraw-cap-read',{str(test):str(testcopy),str(source):str(codecopy)})]:
  overlay=p/(label+'.json');overlay.write_text(json.dumps({'Replace':replace}));log=Path('/tmp/coder-console-'+label+'.log')
  with log.open('w') as f:r=subprocess.run(['go','test','-race','-overlay='+str(overlay),'./internal/m8','-run','^TestConsoleHelperUpdateAuthorizationAndCatalogOnly$','-count=1','-timeout','3m'],stdout=f,stderr=subprocess.STDOUT)
  results.append(dict(case=label,rc=r.returncode,intended=('without capability advertised actionable' in log.read_text()) if label!='positive' else r.returncode==0));print(results[-1],flush=True)
Path('/tmp/coder-console-cap-read-probe.json').write_text(json.dumps(results,indent=2)+'\n')
assert results[0]['rc']==0 and results[1]['rc']==1 and results[1]['intended']
