"""Retain real file descriptions and durable pins; never inspect online seats."""
import copy
from contextlib import contextmanager
import hashlib,json,os,runpy,shutil,subprocess,sys,tempfile,threading,time,unittest
from pathlib import Path
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
from update_launcher import guard
from sessionhelper import private_endpoint as ep


@unittest.skipUnless(sys.platform == 'linux', 'private retention is Linux-only')
class RetainTests(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory(prefix='pcer-');self.addCleanup(self.tmp.cleanup)
        self.base=Path(self.tmp.name);self.base.chmod(0o700)
        self.root=self.base/'update';self.root.mkdir(mode=0o700)
        self.factory=self.base/'factory';self.package=self.slot('a'*64)
        self.make(self.factory)
        self.state=guard.State(self.root,self.factory);self.ret=guard.Retention(self.root)
        self.handles=[];self.addCleanup(self.close_handles)

    def make(self,path):
        (path/'sessionhelper').mkdir(parents=True,mode=0o700)
        path.chmod(0o700);(path/'sessionhelper').chmod(0o700)
        (path/'sessionhelper/__init__.py').write_text('VERSION="fixture"\n')
        (path/'sessionhelper/__main__.py').write_text('print("fixture")\n')
        return path

    def slot(self,sha):return self.make(self.root/'slots'/sha)

    def close_handles(self):
        while self.handles:os.close(self.handles.pop())

    def register(self,package=None):
        pin,handles=self.ret.register(package or self.package,'endpoint-fixture')
        self.handles.extend(handles);return pin

    def proof(self,pin):return {'pin':pin['id'],'boot':'b'*32,'challenge':'c'*32,'state':'stopped'}

    def stopped(self,pin):
        self.close_handles();self.ret.finish(pin['id'],pin['nonce'],self.proof(pin))

    def denied(self,marker,fn,reason=None):
        with self.assertRaises(guard.UpdateError,msg=marker) as error:fn()
        if reason:self.assertEqual(str(error.exception),reason,marker)

    def legacy_selection_and_arm(self, marker):
        item = dict.fromkeys(guard.TUPLE, 'legacy-fixture'); item['sha256']='a'*64
        try:
            self.assertEqual(self.state.select(), (self.factory, None), marker)
            self.assertEqual(self.state.arm(item), 'pending', marker)
            self.assertEqual(self.state.select(now=0)[0], self.package, marker)
        except guard.UpdateError as exc:
            self.fail(marker + ': ' + str(exc))
        self.assertFalse(os.path.lexists(self.ret.path), marker)

    def test_legacy_no_retain_group_writable_parent_unchanged(self):
        self.base.chmod(0o775)
        self.legacy_selection_and_arm('LEGACY_GROUP_PARENT_REJECTED')
        self.assertFalse((self.root/'retain.txn.lock').exists(), 'LEGACY_CREATED_RETAIN_LOCK')

    def test_legacy_no_retain_ignores_busy_transaction_lock(self):
        fd=os.open(self.root/'retain.txn.lock', os.O_CREAT|os.O_RDWR, 0o600)
        try:
            guard.fcntl.flock(fd, guard.fcntl.LOCK_EX|guard.fcntl.LOCK_NB)
            self.legacy_selection_and_arm('LEGACY_WAITED_FOR_UNUSED_RETAIN_LOCK')
        finally:
            os.close(fd)

    def test_first_registration_drift_after_legacy_snapshot_refused(self):
        # A registration creates retain before taking state.lock. The guard
        # must stop rather than miss that new pin or reverse the lock order.
        with self.ret.transaction(if_present=True) as fd:
            self.assertIsNone(fd)
            self.ret.path.mkdir(mode=0o700)
            self.denied('LEGACY_SNAPSHOT_IGNORED_FIRST_REGISTRATION',
                        lambda:self.ret.check_locked(fd,self.factory), 'retain_busy')

    def test_registration_is_durable_and_locks_every_resource_before_popen(self):
        pin=self.register()
        with self.ret.transaction() as fd:
            self.assertTrue(any(p['id']==pin['id'] and p['state']=='active' for p in self.ret.records(fd)),
                            'PIN_NOT_DURABLE_BEFORE_POPEN')
            for key in pin['locks']:
                probe=self.ret._file(fd,key+'.lock')
                try:
                    with self.assertRaises(BlockingIOError,msg='REGISTERED_RESOURCE_NOT_LOCKED'):
                        guard.fcntl.flock(probe,guard.fcntl.LOCK_EX|guard.fcntl.LOCK_NB)
                finally:os.close(probe)

    def test_fd_handoff_child_holds_package_after_parent_closes(self):
        pin=self.register();handles=list(self.handles)
        script='''import json,sys
from update_launcher.guard import Retention
root,package,ident,handles=json.loads(sys.stdin.readline())
Retention(root).accept(ident,handles,package,'endpoint-fixture')
print('PIN_HELD',flush=True)
sys.stdin.read(1)
'''
        p=subprocess.Popen([sys.executable,'-B','-c',script],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,
            text=True,pass_fds=tuple(handles),env={**os.environ,'PYTHONPATH':str(ROOT)})
        self.addCleanup(lambda: p.poll() is None and (p.terminate(),p.wait(timeout=5)))
        p.stdin.write(json.dumps([str(self.root),str(self.package),pin['id'],handles])+'\n');p.stdin.flush()
        self.assertEqual(p.stdout.readline().strip(),'PIN_HELD','PIN_FD_HANDOFF_MISSING')
        self.close_handles()
        self.denied('LIVE_OWNER_PIN_FINISHED',lambda:self.ret.finish(pin['id'],pin['nonce'],self.proof(pin)),'retain_busy')
        p.stdin.write('x');p.stdin.flush();p.wait(timeout=5)
        self.assertEqual(p.returncode,0,'PIN_FD_HANDOFF_MISSING')
        p.stdin.close();p.stdout.close();p.stderr.close()
        self.ret.finish(pin['id'],pin['nonce'],self.proof(pin))

    def test_owner_death_free_lock_does_not_authorize_reclaim(self):
        pin=self.register();self.close_handles()
        # A crash can interrupt the durable state transition; receipt-shaped
        # data alone must not authorize reclamation of an active pin.
        with self.ret.transaction() as fd:
            record=self.ret.records(fd)[0];record['stop']={**self.proof(pin),'nonce':pin['nonce']}
            self.ret._write(fd,'pin-'+pin['id']+'.json',record)
        self.denied('LOST_OWNER_RECLAIMED',lambda:self.ret.reclaim(pin['id'],self.state),'retain_stop')
        self.assertTrue(self.package.exists(),'LOST_OWNER_RECLAIMED')
        with self.ret.transaction() as fd:
            record=self.ret.records(fd)[0];record['state']='unknown';self.ret._write(fd,'pin-'+pin['id']+'.json',record)
        self.denied('UNKNOWN_PIN_RECLAIMED',lambda:self.ret.reclaim(pin['id'],self.state),'retain_stop')

    def test_replacing_pinned_slot_rejected_without_inspecting_pid(self):
        pin=self.register();self.close_handles()
        with self.ret.transaction() as fd:
            self.denied('PINNED_SLOT_REPLACED',lambda:self.ret.check_locked(fd,replacing=self.package),'retain_pinned')

    def test_any_package_or_runtime_change_requires_manual(self):
        self.register();candidate=self.slot('b'*64)
        self.ret.check(candidate)
        (candidate/'sessionhelper/__init__.py').write_text('VERSION="changed"\n')
        self.denied('OWNER_CLOSURE_CHANGED',lambda:self.ret.check(candidate),'manual_required')
        (candidate/'sessionhelper/__init__.py').write_text('VERSION="fixture"\n')
        old=guard.runtime_fingerprint();changed={**old,'exe':old['exe']+'-changed'}
        with mock.patch.object(guard,'runtime_fingerprint',return_value=changed):
            self.denied('OWNER_INTERPRETER_CHANGED',lambda:self.ret.check(candidate),'manual_required')

    def test_missing_or_wrong_inherited_descriptor_refused(self):
        pin=self.register()
        self.denied('MISSING_PIN_FD_ACCEPTED',lambda:self.ret.accept(pin['id'],[],self.package,'endpoint-fixture'))
        alien=os.open(self.base/'alien',os.O_CREAT|os.O_RDWR,0o600);self.addCleanup(os.close,alien)
        self.denied('FOREIGN_PIN_FD_ACCEPTED',lambda:self.ret.accept(pin['id'],[alien,*self.handles[1:]],self.package,'endpoint-fixture'))
        self.denied('WRONG_ENDPOINT_PIN_ACCEPTED',lambda:self.ret.accept(pin['id'],self.handles,self.package,'foreign'))

    def test_stop_receipt_nonce_and_held_lock_required(self):
        pin=self.register()
        self.denied('LIVE_STOP_CERT_ACCEPTED',lambda:self.ret.finish(pin['id'],pin['nonce'],self.proof(pin)),'retain_busy')
        self.close_handles()
        self.denied('FORGED_STOP_CERT_ACCEPTED',lambda:self.ret.finish(pin['id'],'wrong',self.proof(pin)),'retain_stop')
        proof=self.proof(pin);proof['state']='ready'
        self.denied('UNFINISHED_STOP_CERT_ACCEPTED',lambda:self.ret.finish(pin['id'],pin['nonce'],proof),'retain_stop')

    def test_clean_stop_reclaims_only_exact_unreferenced_slot(self):
        other=self.slot('b'*64);(other/'marker').write_text('keep')
        pin=self.register();self.stopped(pin)
        self.assertEqual(self.ret.reclaim(pin['id'],self.state)['state'],'reclaimed','CLEAN_SLOT_NOT_RECLAIMED')
        self.assertFalse(self.package.exists(),'CLEAN_SLOT_NOT_RECLAIMED')
        self.assertEqual((other/'marker').read_text(),'keep','OTHER_SLOT_RECLAIMED')
        self.assertTrue(Path(sys.prefix).exists(),'SHARED_INTERPRETER_RECLAIMED')

    def test_current_previous_pending_and_other_pin_block_reclaim(self):
        for role in ('current','previous','pending'):
            pin=self.register();self.stopped(pin)
            journal=self.state.read();journal[role]={'sha256':'a'*64};guard.atomic_json(self.state.path,journal)
            self.denied('REFERENCED_SLOT_RECLAIMED_'+role,lambda:self.ret.reclaim(pin['id'],self.state),'retain_pinned')
            journal[role]=None;guard.atomic_json(self.state.path,journal)
        pin=self.register();self.stopped(pin);self.register();self.close_handles()
        self.denied('SHARED_PACKAGE_RECLAIMED',lambda:self.ret.reclaim(pin['id'],self.state),'retain_pinned')

    def test_inode_swap_factory_and_arbitrary_path_never_reclaimed(self):
        pin=self.register();self.stopped(pin)
        self.package.rename(self.root/'saved');self.make(self.package)
        self.denied('SWAPPED_SLOT_RECLAIMED',lambda:self.ret.reclaim(pin['id'],self.state),'retain_invalid')
        pin=self.register(self.factory);self.stopped(pin)
        self.denied('FACTORY_RECLAIMED',lambda:self.ret.reclaim(pin['id'],self.state),'retain_invalid')
        self.denied('ARBITRARY_PATH_RECLAIMED',lambda:self.ret.reclaim('../other',self.state),'retain_invalid')

    def test_last_moment_inode_swap_before_rmtree_is_refused(self):
        pin=self.register();self.stopped(pin)
        original=guard.os.open;swapped=[]
        def race(path,*args,**kwargs):
            if path==self.package.parent and not swapped:
                swapped.append(True);self.package.rename(self.root/'saved')
                self.make(self.package);(self.package/'marker').write_text('replacement')
            return original(path,*args,**kwargs)
        with mock.patch.object(guard.os,'open',side_effect=race):
            self.denied('LATE_INODE_SWAP_RECLAIMED',lambda:self.ret.reclaim(pin['id'],self.state),'retain_invalid')
        self.assertEqual((self.package/'marker').read_text(),'replacement','LATE_INODE_SWAP_RECLAIMED')

    def test_bad_pin_symlink_permissions_and_storage_failure_fail_closed(self):
        pin=self.register();self.close_handles()
        file=self.ret.path/('pin-'+pin['id']+'.json');raw=file.read_bytes()
        file.write_text('{}');self.denied('BAD_PIN_IGNORED',lambda:self.ret.check(self.factory),'retain_invalid')
        file.write_bytes(raw);file.chmod(0o644)
        self.denied('PUBLIC_PIN_IGNORED',lambda:self.ret.check(self.factory),'retain_invalid')
        file.chmod(0o600);file.unlink();file.symlink_to(self.base/'absent')
        self.denied('PIN_SYMLINK_IGNORED',lambda:self.ret.check(self.factory),'retain_invalid')

    def test_storage_error_does_not_start_or_reclaim_anything(self):
        with mock.patch.object(self.ret,'_write',side_effect=OSError(28,'fixture disk full')):
            self.denied('PIN_DISK_FULL_IGNORED',lambda:self.register(),'retain_invalid')
        self.assertTrue(self.package.exists(),'PIN_DISK_FULL_RECLAIMED')

    def test_guard_arm_select_and_fallback_refuse_incompatible_runtime(self):
        self.register();bad=self.slot('b'*64);(bad/'sessionhelper/__init__.py').write_text('bad=True\n')
        item={'job_id':'job','release_id':'release','agent_id':'agent','owner_id':'owner','version':'9.0.0','sha256':'b'*64}
        self.denied('INCOMPATIBLE_ARM_ACCEPTED',lambda:self.state.arm(item),'manual_required')
        data=self.state.read();data['current']=item;guard.atomic_json(self.state.path,data)
        self.denied('INCOMPATIBLE_CURRENT_SELECTED',lambda:self.state.select(),'manual_required')
        data['current']=None;guard.atomic_json(self.state.path,data)
        (self.factory/'sessionhelper/__init__.py').write_text('old=True\n')
        self.denied('INCOMPATIBLE_FACTORY_SELECTED',lambda:self.state.select(),'manual_required')

    def test_pending_incompatible_upgrade_prevents_new_owner_pin(self):
        bad=self.slot('b'*64);(bad/'sessionhelper/__init__.py').write_text('bad=True\n')
        item={'job_id':'job','release_id':'release','agent_id':'agent','owner_id':'owner','version':'9.0.0','sha256':'b'*64}
        self.state.arm(item)
        self.denied('OWNER_RACED_PENDING_UPGRADE',lambda:self.register(),'manual_required')

    def test_private_root_must_match_launcher_update_root_if_bound(self):
        home=self.base/'ch';home.mkdir(mode=0o700)
        spec=ep.Spec(str(self.base/'s'),'seat',str(home),str(self.base),sys.executable,sys.executable)
        env={'SH_UPDATE_LAUNCHER':str(ROOT/'update_launcher/guard.py'),'SH_UPDATE_ROOT':str(self.root/'other')}
        with mock.patch.dict(os.environ,env):
            with self.assertRaises(ep.PrivateEndpointError,msg='PRIVATE_RETAIN_ROOT_MISMATCH_ACCEPTED'):
                ep.retention_kernel(spec)

    def test_old_or_absent_launcher_refuses_private(self):
        home=self.base/'home';home.mkdir(mode=0o700)
        spec=ep.Spec(str(self.base/'state'),'seat',str(home),str(self.base),sys.executable,sys.executable)
        old=self.base/'old_guard.py';old.write_text('from update_launcher.guard import Retention\nRETENTION_API=0\n');old.chmod(0o600)
        for value in ('',str(old)):
            with self.subTest(value=value),mock.patch.dict(os.environ,{'SH_UPDATE_LAUNCHER':value}):
                with self.assertRaises(ep.PrivateEndpointError,msg='OLD_LAUNCHER_PRIVATE_ACCEPTED'):
                    ep.retention_kernel(spec)

    def test_registration_serializes_with_reclaim_without_deletion_window(self):
        pin=self.register();self.stopped(pin)
        inside=threading.Event();release=threading.Event();gc_started=threading.Event();errors=[];gc=[]
        original=guard.package_fingerprint
        def stalled(path):
            if threading.current_thread().name=='pin-registrar' and not inside.is_set():
                inside.set()
                if not release.wait(2):raise AssertionError('PIN_REGISTER_GC_RACE')
            return original(path)
        def register():
            try:self.register()
            except Exception as error:errors.append(str(error))
        def reclaim():
            gc_started.set()
            try:self.ret.reclaim(pin['id'],self.state);gc.append('deleted')
            except guard.UpdateError as error:gc.append(str(error))
        with mock.patch.object(guard,'package_fingerprint',side_effect=stalled):
            worker=threading.Thread(target=register,name='pin-registrar');worker.start()
            try:
                self.assertTrue(inside.wait(2),'PIN_REGISTER_GC_RACE')
                collector=threading.Thread(target=reclaim);collector.start()
                self.assertTrue(gc_started.wait(2),'PIN_REGISTER_GC_RACE')
                time.sleep(.05)
            finally:release.set()
            worker.join(5);collector.join(5)
        self.assertFalse(errors,'PIN_REGISTER_GC_RACE '+repr(errors))
        self.assertEqual(gc,['retain_pinned'],'PIN_REGISTER_GC_RACE')
        self.assertTrue(self.package.exists(),'PIN_REGISTER_GC_RACE')

    def test_resource_lock_and_receipt_are_independent_reclaim_gates(self):
        pin=self.register();self.stopped(pin)
        with self.ret.transaction() as fd:
            lease=self.ret._file(fd,pin['locks'][1]+'.lock')
            guard.fcntl.flock(lease,guard.fcntl.LOCK_SH)
        try:
            self.denied('SHARED_RESOURCE_LOCK_IGNORED',lambda:self.ret.reclaim(pin['id'],self.state),'retain_busy')
        finally:os.close(lease)
        with self.ret.transaction() as fd:
            record=next(p for p in self.ret.records(fd) if p['id']==pin['id'])
            record['stop']['nonce']='0'*32;self.ret._write(fd,'pin-'+pin['id']+'.json',record)
        self.denied('GC_STOP_RECEIPT_IGNORED',lambda:self.ret.reclaim(pin['id'],self.state),'retain_stop')

    def test_pin_limit_refuses_before_registering_new_owner(self):
        with mock.patch.object(self.ret,'LIMIT',0):
            self.denied('PIN_LIMIT_IGNORED',lambda:self.register(),'retain_limit')

    def test_stopped_pin_collision_requires_explicit_reclaim_first(self):
        pin=self.register();self.stopped(pin)
        with self.ret.transaction() as fd:
            self.denied('STOPPED_PIN_REPLACED_WITHOUT_RECLAIM',
                        lambda:self.ret.check_locked(fd,replacing=self.package),'retain_pinned')
        self.ret.reclaim(pin['id'],self.state)
        with self.ret.transaction() as fd:self.ret.check_locked(fd,replacing=self.package)

    def test_bad_completed_pin_state_cannot_disable_update_protection(self):
        pin=self.register()
        with self.ret.transaction() as fd:
            record=self.ret.records(fd)[0];record['state']='stopped'
            self.ret._write(fd,'pin-'+pin['id']+'.json',record)
        self.denied('BAD_COMPLETED_PIN_ALLOWED_UPDATE',lambda:self.ret.check(self.factory),'retain_stop')

    def test_actual_owner_crash_does_not_reclaim_live_child(self):
        self.actual_owner_crash(False)

    def test_actual_owner_crash_without_python_pidfd_api(self):
        self.actual_owner_crash(True)

    def actual_owner_crash(self,without_pidfd):
        script = ROOT/'tests/pce_retention_crash_fixture.py'
        env={**os.environ,'PYTHONPATH':str(ROOT),'SH_UPDATE_LAUNCHER':str(ROOT/'update_launcher/guard.py')}
        command=[sys.executable,'-B',str(script)]
        if without_pidfd:
            bootstrap="import os,runpy,sys; os.__dict__.pop('pidfd_open',None); runpy.run_path(sys.argv[1],run_name='__main__')"
            command=[sys.executable,'-B','-c',bootstrap,str(script)]
        result=subprocess.run(command,env=env,capture_output=True,timeout=30)
        self.assertEqual(result.returncode,0,'OWNER_CRASH_RECLAIMED_RUNTIME '+result.stderr.decode(errors='replace'))
        data=json.loads(result.stdout)
        self.assertTrue(data['native_survived_owner'],'OWNER_CRASH_RECLAIMED_RUNTIME')
        self.assertTrue(data['pin_retained'],'OWNER_CRASH_RECLAIMED_RUNTIME')
        self.assertTrue(data['child_reaped'],'OWNER_CRASH_FIXTURE_LEAKED')

    def test_crash_fixture_refuses_to_signal_unowned_or_changed_pid(self):
        fixture=runpy.run_path(str(ROOT/'tests/pce_retention_crash_fixture.py'))
        reap=fixture['reap_owned_child']
        with mock.patch.object(os,'waitpid',side_effect=ChildProcessError),mock.patch.object(os,'kill') as kill:
            with self.assertRaises(ChildProcessError):reap(123,456)
            kill.assert_not_called()
        with mock.patch.object(os,'waitpid',return_value=(0,0)),mock.patch.dict(reap.__globals__,process_starttime=lambda pid:789),mock.patch.object(os,'kill') as kill:
            with self.assertRaisesRegex(AssertionError,'OWNER_CRASH_CHILD_CHANGED'):reap(123,456)
            kill.assert_not_called()

    def test_no_pins_legacy_contract_unchanged(self):
        self.assertEqual(self.state.select(),(self.factory,None),'LEGACY_WITHOUT_PIN_CHANGED')


if __name__=='__main__':unittest.main()
