"""Assertion-red daemon identity withdrawals in an isolated detached worktree."""
import datetime
import json
from pathlib import Path
import re
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[2]
IDENTITY = 'tools/sessionhelper/sessionhelper/daemon_identity.py'
DRIVER = 'tools/sessionhelper/sessionhelper/codexDriver.py'
SUITE = 'tools.sessionhelper.tests.test_daemon_identity'
GATE = '''            if type(self._identity_check) is not bool:
                raise DaemonIdentityError('invalid_switch')
            if self._identity_check:
                self.peer_identity = verify_peer(self._sock, self._expected_exe, self._owned_identity)
            else:
                log.warning('codex_daemon_identity_disabled identity_scope=unchecked')
'''
MUTATIONS = [
 ('uid', IDENTITY, [('if uid != os.getuid():', 'if False:')],
  'PeerChecks.test_foreign_uid_rejected', 'IDENTITY_ACCEPTED_uid_mismatch'),
 ('exe-inode', IDENTITY, [('if (actual.st_dev, actual.st_ino) != (expected.st_dev, expected.st_ino):', 'if False:')],
  'PeerChecks.test_wrong_executable_and_missing_expected_file', 'IDENTITY_ACCEPTED_exe_mismatch'),
 ('proc-readlink', IDENTITY, [('target = os.readlink(exe)', "target = '/fixture-codex'")],
  'PeerChecks.test_proc_exe_missing_deleted_or_stat_denied_is_unverifiable', 'IDENTITY_ACCEPTED_exe_unverifiable'),
 ('deleted-exe', IDENTITY, [(" or target.endswith(' (deleted)')", '')],
  'PeerChecks.test_proc_exe_missing_deleted_or_stat_denied_is_unverifiable', 'IDENTITY_ACCEPTED_exe_unverifiable'),
 ('starttime-stability', IDENTITY, [('if after != before:', 'if False:')],
  'PeerChecks.test_starttime_missing_and_process_change_rejected', 'IDENTITY_ACCEPTED_process_changed'),
 ('owned-pid', IDENTITY, [('if owned is not None and pid != owned.pid:', 'if False:')],
  'PeerChecks.test_owned_pid_starttime_and_types_are_strict', 'IDENTITY_ACCEPTED_owned_pid_mismatch'),
 ('owned-starttime', IDENTITY, [('if owned is not None and before != owned.starttime:', 'if False:')],
  'PeerChecks.test_owned_pid_starttime_and_types_are_strict', 'IDENTITY_ACCEPTED_owned_starttime_mismatch'),
 ('platform', IDENTITY, [("if sys.platform != 'linux' or not hasattr(socket, 'SO_PEERCRED'):", 'if False:')],
  'PeerChecks.test_unverifiable_peer_and_unsupported_platform', 'IDENTITY_ACCEPTED_unsupported'),
 ('stat-parser', IDENTITY, [('value = int(tail.split()[19])', 'value = 321')],
  'PeerChecks.test_stat_parser_handles_parentheses_and_fails_closed', 'BAD_PROC_STAT_ACCEPTED'),
 ('gate-call', DRIVER, [('self.peer_identity = verify_peer(self._sock, self._expected_exe, self._owned_identity)', 'self.peer_identity = None')],
  'DaemonWiring.test_wrong_exe_has_zero_handshake_bytes_and_unhealthy_closed_rpc', 'WRONG_EXE_CONNECTED'),
 ('pre-handshake-order', DRIVER, [(GATE, ''), ('            self._sock.sendall(request)\n', '            self._sock.sendall(request)\n' + GATE)],
  'DaemonWiring.test_wrong_exe_has_zero_handshake_bytes_and_unhealthy_closed_rpc', 'UNVERIFIED_PEER_RECEIVED_BYTES'),
 ('raw-fd-close', DRIVER, [('            self._reader_dead.set()\n            self._sock.close()\n', '            self._reader_dead.set()\n')],
  'DaemonWiring.test_rejected_fd_is_explicitly_closed_without_gc', 'REJECTED_FD_OPEN'),
 ('rpc-reset', DRIVER, [('                    self._rpc.stop()\n                self._rpc = None\n                self._initialized = False', '                    self._rpc.stop()\n                self._initialized = False')],
  'DaemonWiring.test_wrong_exe_has_zero_handshake_bytes_and_unhealthy_closed_rpc', 'REJECTED_RPC_RETAINED'),
 ('owned-wiring', DRIVER, [('owned_identity=self._owned_daemon_identity)', 'owned_identity=None)')],
  'DaemonWiring.test_owned_binding_wired_and_foreign_instance_rejected', 'OWNED_BINDING_NOT_WIRED'),
 ('reconnect', DRIVER, [('identity_check=self._daemon_identity_check,', 'identity_check=self._daemon_identity_check and self._connection_generation == 0,')],
  'DaemonWiring.test_default_enabled_real_kernel_then_initialize_and_reconnect', 'RECONNECT_IDENTITY_NOT_RECHECKED'),
 ('default-on', DRIVER, [("env_get('SH_CODEX_DAEMON_IDENTITY_CHECK', '1')", "env_get('SH_CODEX_DAEMON_IDENTITY_CHECK', '0')")],
  'DaemonWiring.test_env_default_one_zero_and_invalid_snapshot', 'IDENTITY_DEFAULT_NOT_ON'),
 ('invalid-switch', DRIVER, [('if type(self._identity_check) is not bool:', 'if False:')],
  'DaemonWiring.test_explicit_off_is_visible_and_invalid_setting_refused', 'INVALID_SWITCH_CONNECTED'),
 ('session-classification', DRIVER, [('                except DaemonIdentityError as exc:\n                    return self._admission_failure(envelope_id, body_hash, exc, uncertain=False)', '                except DaemonIdentityError as exc:\n                    return self._admission_failure(envelope_id, body_hash, exc, uncertain=True)')],
  'DaemonWiring.test_rejected_delivery_persists_and_never_starts_thread_or_turn', 'IDENTITY_NOT_SESSION_UNAVAILABLE'),
 ('observable-error', DRIVER, [('                if isinstance(exc, DaemonIdentityError):', '                if False:')],
  'DaemonWiring.test_wrong_exe_has_zero_handshake_bytes_and_unhealthy_closed_rpc', 'REJECTION_EVENT_MISSING'),
 ('durable-failure', DRIVER, [('        self._store.upsert(self._thread_id, envelope_id, "", body_hash, "unknown" if uncertain else "failed", detail)', '        pass  # withdrawn durable admission failure')],
  'DaemonWiring.test_rejected_delivery_persists_and_never_starts_thread_or_turn', 'IDENTITY_FAILURE_NOT_DURABLE'),
 ('no-replay', DRIVER, [('or existing.detail.startswith("session_unavailable:")):', 'or False):')],
  'DaemonWiring.test_rejected_delivery_persists_and_never_starts_thread_or_turn', 'IDENTITY_FAILURE_RETRIED'),
]


def main(output):
 out=Path(output).resolve();out.mkdir(mode=0o700,parents=True,exist_ok=False)
 source=subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip()
 wt=out/'worktree';subprocess.run(['git','worktree','add','--detach',str(wt),source],cwd=ROOT,check=True,capture_output=True)
 home=out/'home';home.mkdir(mode=0o700)
 with tempfile.TemporaryDirectory(prefix='daemon-m-') as tmp:
  env={'HOME':str(home),'TMPDIR':tmp,'PATH':str(Path(sys.executable).parent)+':/usr/bin:/bin',
       'PYTHONPATH':str(wt/'tools/sessionhelper'),'PYTHONDONTWRITEBYTECODE':'1'}
  rows=[]
  def run(name,test='',marker=''):
   cmd=[sys.executable,'-B','-m','unittest','-v',SUITE+('.'+test if test else '')]
   start=datetime.datetime.now(datetime.timezone.utc).isoformat()
   p=subprocess.run(cmd,cwd=wt,env=env,capture_output=True,text=True,timeout=45)
   for suffix,data in [('stdout',p.stdout),('stderr',p.stderr),('rc',str(p.returncode)+'\n')]:
    (out/(name+'.'+suffix)).write_text(data)
   red=bool(marker and p.returncode==1 and marker in p.stderr and 'AssertionError' in p.stderr
            and 'FAIL:' in p.stderr and not re.search(r'^ERROR:',p.stderr,re.M))
   row={'name':name,'command':cmd,'start':start,'end':datetime.datetime.now(datetime.timezone.utc).isoformat(),
        'rc':p.returncode,'marker':marker,'assertion_red':red};rows.append(row)
   (out/'CHECKS.json').write_text(json.dumps({'source':source,'rows':rows},indent=2)+'\n')
   assert red if marker else p.returncode==0,row
  run('baseline')
  for name,path,replacements,test,marker in MUTATIONS:
   target=wt/path;original=target.read_text();modified=original
   for before,after in replacements:
    assert modified.count(before)==1,(name,before)
    modified=modified.replace(before,after,1)
   try:
    target.write_text(modified);run(name+'-red',test,marker)
   finally:target.write_text(original)
   run(name+'-restored',test)
  run('final-restored')
  (out/'RESULT.json').write_text(json.dumps({'source':source,'mutations':len(MUTATIONS),'commands':len(rows),
   'result':'LOCAL_INERT_PASS_NOT_SEAT_ACCEPTANCE'},indent=2)+'\n')
  print('DAEMON_IDENTITY_MUTATIONS_PASS mutations='+str(len(MUTATIONS))+' commands='+str(len(rows)))


if __name__=='__main__':main(sys.argv[1])
