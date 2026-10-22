"""Own-process subreaper fixture: orphan remains live, is retained, then reaped."""
import ctypes,json,os,signal,sys,tempfile
from pathlib import Path
from sessionhelper import private_endpoint as ep
from sessionhelper.daemon_identity import OwnedDaemonIdentity


def run():
    # No flag changes to the unittest process or a running seat/helper.
    if ctypes.CDLL(None,use_errno=True).prctl(36,1,0,0,0)!=0:raise OSError(ctypes.get_errno())
    with tempfile.TemporaryDirectory(prefix='pcer-cr-') as tmp:
        base=Path(tmp);base.chmod(0o700);home=base/'ch';home.mkdir(mode=0o700)
        native=Path(__file__).with_name('private_endpoint_fake_native.py')
        wrapper=base/'native';wrapper.write_text('#!'+sys.executable+'\nimport runpy\nrunpy.run_path('+repr(str(native))+',run_name="__main__")\n');wrapper.chmod(0o700)
        spec=ep.Spec(str(base/'s'),'crash-fixture',str(home),str(base),str(wrapper),sys.executable)
        client=ep.Client(spec);child_fd=None;owner=None;child=None;child_reaped=False
        try:
            current=client.ensure();owner=client._launchers[0];child=current['child']['pid']
            sock=client.open_app(current);child_fd=os.pidfd_open(child);sock.close()
            owner.kill();owner.wait(timeout=5)
            ep.native_call(spec,OwnedDaemonIdentity(**current['child']),lambda request:request('fixture/idle',{}))
            module,retention=ep.retention_kernel(spec)
            try:retention.reclaim(current['pin'],module.State(retention.root,Path(ep.package_root())))
            except module.UpdateError as exc:assert str(exc)=='retain_stop'
            else:raise AssertionError('OWNER_CRASH_RECLAIMED_RUNTIME')
            result={'native_survived_owner':True,'pin_retained':True}
        finally:
            if owner is not None and owner.poll() is None:owner.kill();owner.wait(timeout=5)
            if child_fd is not None:
                try:signal.pidfd_send_signal(child_fd,signal.SIGKILL)
                except ProcessLookupError:pass
                os.close(child_fd)
                if child is not None:
                    os.waitpid(child,0);child_reaped=True
        result['child_reaped']=child_reaped;print(json.dumps(result))


if __name__=='__main__':run()
