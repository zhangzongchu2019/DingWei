"""Own both isolated Hub and official CLI probe lifecycle; never use real secrets."""
import argparse,json,os,subprocess,time
from pathlib import Path
ROOT=Path(__file__).resolve().parents[3];HERE=Path(__file__).resolve().parent
p=argparse.ArgumentParser();p.add_argument('--run-dir',type=Path,required=True);a=p.parse_args();run=a.run_dir.resolve()
if run.exists():raise SystemExit('refusing to reuse existing probe directory')
env={k:v for k,v in os.environ.items() if not k.startswith(('SH_','DINGWEI_')) and k not in ('PYTHONPATH',)}
env['PATH']='/tmp/coder-0232-locked-venv/bin:/usr/bin:/bin:/home/dev/.local/bin';env['GOPROXY']='off';env['HUB_TEST_PG_STRICT']='1'
hub=subprocess.Popen(['python3',str(HERE/'probe_launcher.py'),'--run-dir',str(run)],cwd=ROOT,env=env)
rc=1
try:
 deadline=time.monotonic()+120
 while time.monotonic()<deadline and not (run/'ready.json').exists():
  if hub.poll() is not None:raise RuntimeError('isolated Hub failed')
  time.sleep(.1)
 assert (run/'ready.json').exists(),'isolated Hub readiness timeout'
 binary='/home/dev/.codex/packages/standalone/releases/0.167.0-x86_64-unknown-linux-musl/bin/codex'
 result=subprocess.run(['python3',str(HERE/'native_probe.py'),'--binary',binary,'--label','url-only'],env={**env,'INTEROP_RUN_DIR':str(run)},cwd=ROOT,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=180)
 (run/'native.stdout').write_bytes(result.stdout);rc=result.returncode
finally:
 if run.exists():(run/'stop').touch()
 try:hub.wait(timeout=60)
 except subprocess.TimeoutExpired:hub.terminate();hub.wait(timeout=10)
 if run.exists():(run/'LIFECYCLE.json').write_text(json.dumps({'native_rc':rc,'hub_rc':hub.returncode,'real_credentials_used':False},indent=2)+'\n')
raise SystemExit(rc or hub.returncode)
