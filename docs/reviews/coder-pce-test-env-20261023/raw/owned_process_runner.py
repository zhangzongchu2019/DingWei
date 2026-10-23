"""Run test subprocesses in a dedicated subreaper, clean only adopted children."""
import ctypes,json,os,signal,subprocess,sys
from pathlib import Path
assert ctypes.CDLL(None,use_errno=True).prctl(36,1,0,0,0)==0
result=subprocess.run(sys.argv[1:],capture_output=True,timeout=90)
sys.stdout.buffer.write(result.stdout);sys.stderr.buffer.write(result.stderr)
rescued=[]
for item in Path(f'/proc/self/task/{os.getpid()}/children').read_text().split():
    pid=int(item);done,_=os.waitpid(pid,os.WNOHANG)
    if not done:
        os.kill(pid,signal.SIGKILL);os.waitpid(pid,0)
    rescued.append(pid)
print(json.dumps({'test_rc':result.returncode,'rescued_fixture_children':len(rescued)}),file=sys.stderr)
sys.exit(result.returncode)
