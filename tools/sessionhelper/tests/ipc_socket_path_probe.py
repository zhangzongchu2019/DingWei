import asyncio,contextlib,io,os,tempfile
from sessionhelper.ipc_listener import serve_ipc,IPCSocketPathError,IPC_SOCKET_PATH_MAX_BYTES
async def main():
 with tempfile.TemporaryDirectory(dir="/tmp",prefix="len-red-") as root:
  path=root+"/"+"x"*(IPC_SOCKET_PATH_MAX_BYTES+1-len(os.fsencode(root+"/")))
  log=io.StringIO();bound=asyncio.Event()
  with contextlib.redirect_stderr(log):
   try: await serve_ipc(path,None,bound=bound)
   except IPCSocketPathError: pass
   except Exception: raise AssertionError("OS bind error escaped instead of explicit IPCSocketPathError") from None
   else: raise AssertionError("oversized address unexpectedly accepted")
  assert "ERROR" in log.getvalue(),"missing explicit diagnostic"
asyncio.run(main())
