import importlib.util,os,sys,subprocess,json
from pathlib import Path
HERE=Path(__file__).resolve().parent;ROOT=HERE.parents[2];RAW=HERE/'raw'
spec=importlib.util.spec_from_file_location('focused_browser_runner',ROOT/'tools/run-browser-regressions.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
negative='--without-status-query' in sys.argv
file_only='--file-only' in sys.argv
for key in list(os.environ):
 if key.startswith(('SH_','DINGWEI_','DW_','DATAD_')):os.environ.pop(key)
prefix='mutation-query' if negative else 'fixed-file' if file_only else 'focused'
m.SUITES=tuple(x for x in m.SUITES if x[0] in (('renderer_0206_file_render',) if negative or file_only else ('renderer_0206_file_render','renderer_flood_stress')))
assert len(m.SUITES)==(1 if negative or file_only else 2)
if negative:
 original=m.run
 def run(cmd,**kw):
  if cmd[-1].endswith('renderer-0206-file-render-regression.js'):cmd=cmd+['--without-status-query']
  return original(cmd,**kw)
 m.run=run
os.environ['STRESS_READINGS']=str(RAW/'focused-stress-readings.json')
sys.argv=[sys.argv[0],'--receipt',str(RAW/(prefix+'-receipt.txt'))]
rc=m.main();(RAW/(prefix+'-exit.json')).write_text(json.dumps({'rc':rc,'negative':negative})+'\n');raise SystemExit(rc)
