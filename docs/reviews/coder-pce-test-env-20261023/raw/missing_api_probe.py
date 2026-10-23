"""Dedicated subreaper: reproduce the old failure without leaving test orphans."""
import ctypes,json,os,signal,subprocess,sys
from pathlib import Path

assert ctypes.CDLL(None,use_errno=True).prctl(36,1,0,0,0)==0
script=Path(sys.argv[1]).resolve()
bootstrap="import os,runpy,sys; del os.pidfd_open; runpy.run_path(sys.argv[1],run_name='__main__')"
env={**os.environ,'PYTHONPATH':str(script.parents[1]),'SH_UPDATE_LAUNCHER':str(script.parents[1]/'update_launcher/guard.py')}
result=subprocess.run([sys.executable,'-B','-c',bootstrap,str(script)],env=env,capture_output=True,timeout=30)
sys.stdout.buffer.write(result.stdout);sys.stderr.buffer.write(result.stderr)
rescued=[]
for item in Path(f'/proc/self/task/{os.getpid()}/children').read_text().split():
    pid=int(item)
    done,_=os.waitpid(pid,os.WNOHANG)
    if not done:
        os.kill(pid,signal.SIGKILL);os.waitpid(pid,0)
    rescued.append(pid)
print(json.dumps({'fixture_rc':result.returncode,'rescued_fixture_children':len(rescued)}),file=sys.stderr)
sys.exit(result.returncode)
