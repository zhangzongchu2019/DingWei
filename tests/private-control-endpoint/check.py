"""Local inert phase-one checker; no native CLI, online endpoint or model.

Run in the locked helper venv: python -B tests/private-control-endpoint/check.py OUTPUT
Every mutation runs only in a private copy, then restores and checks again.
The native version / PTY acceptance is a separate gate, never inferred here.
"""
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile


ROOT = Path(__file__).resolve().parents[2]
PRIVATE = 'sessionhelper/private_endpoint.py'
DRIVER = 'sessionhelper/codexDriver.py'

# id, file, old, new, exact regression, required assertion marker
MUTATIONS = [
    ('path-length', PRIVATE, "> 103:", "> 10000:", 'Paths.test_long_path_and_parent_traversal_refused', 'PRIVATE_ACCEPTED_path'),
    ('root-no-follow', PRIVATE, 'os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=fd)', 'os.O_DIRECTORY | os.O_CLOEXEC, dir_fd=fd)', 'Paths.test_unsafe_directory_modes_and_symlink_refused', 'PRIVATE_ACCEPTED_path'),
    ('private-mode', PRIVATE, 'if depth > state_depth and stat.S_IMODE(info.st_mode) != 0o700:', 'if False:', 'Paths.test_unsafe_directory_modes_and_symlink_refused', 'PRIVATE_ACCEPTED_permissions'),
    ('registry-mode', PRIVATE, 'or stat.S_IMODE(info.st_mode) != 0o600 or info.st_nlink != 1)', 'or False or info.st_nlink != 1)', 'Paths.test_registry_permissions_symlink_hardlink_and_size_refused', 'PRIVATE_ACCEPTED_file'),
    ('registry-size', PRIVATE, 'if not 0 < info.st_size <= LIMIT:', 'if False:', 'Paths.test_registry_permissions_symlink_hardlink_and_size_refused', 'PRIVATE_ACCEPTED_registry'),
    ('owner-kernel-binding', PRIVATE, "verify_peer(sock, python, OwnedDaemonIdentity(record['owner_pid'], record['owner_starttime']))", 'pass', 'Control.test_registry_is_locator_not_authority', 'REGISTRY_AUTHORIZED_FOREIGN_OWNER'),
    ('owner-live-lock', PRIVATE, 'if not lock_is_held(fd):\n                fail(\'owner\')', 'if False:\n                fail(\'owner\')', 'Control.test_listener_without_live_owner_lock_is_not_owner', 'PRIVATE_ACCEPTED_owner'),
    ('owner-config', PRIVATE, "or record.get('config') != self.spec.fingerprint()", 'or False', 'Control.test_config_implementation_and_owner_python_must_match', 'PRIVATE_ACCEPTED_registry'),
    ('owner-implementation', PRIVATE, "or record.get('implementation') != implementation_hash()", 'or False', 'Control.test_config_implementation_and_owner_python_must_match', 'PRIVATE_ACCEPTED_registry'),
    ('owner-python', PRIVATE, "if record.get('python') != python:", 'if False:', 'Control.test_config_implementation_and_owner_python_must_match', 'PRIVATE_ACCEPTED_owner'),
    ('owner-challenge', PRIVATE, "or reply.get('challenge') != challenge", 'or False', 'Control.test_replayed_challenge_wrong_boot_and_nonboolean_ok_refused', 'PRIVATE_ACCEPTED_owner'),
    ('owner-boot', PRIVATE, "or reply.get('boot') != record['boot']", 'or False', 'Control.test_replayed_challenge_wrong_boot_and_nonboolean_ok_refused', 'PRIVATE_ACCEPTED_owner'),
    ('owner-reply-bool', PRIVATE, "if type(reply.get('ok')) is not bool:\n                    fail('protocol')\n                if reply['ok'] is not True:", "if not reply['ok']:", 'Control.test_replayed_challenge_wrong_boot_and_nonboolean_ok_refused', 'PRIVATE_ACCEPTED_protocol'),
    ('writer-exclusion', PRIVATE, 'fcntl.flock(writer, fcntl.LOCK_EX | fcntl.LOCK_NB)', 'pass', 'Control.test_duplicate_helper_cannot_take_writer_but_restart_can', 'PRIVATE_ACCEPTED_busy'),
    ('writer-required', PRIVATE, 'if self._writer is None:\n            fail(\'binding\')', 'if False:\n            fail(\'binding\')', 'Control.test_duplicate_helper_cannot_take_writer_but_restart_can', 'PRIVATE_ACCEPTED_binding'),
    ('admission-live-child', PRIVATE, "or (peer.pid, peer.starttime) != (current['pid'], current['starttime'])", 'or False', 'Control.test_model_admission_compares_live_child_not_stale_rpc', 'PRIVATE_ACCEPTED_binding'),
    ('admission-stop-exclusion', PRIVATE, 'fcntl.flock(guard, fcntl.LOCK_SH | fcntl.LOCK_NB)', 'pass', 'Control.test_stop_exclusion_blocks_new_model_admission', 'PRIVATE_ACCEPTED_busy'),
    ('orphan-no-adopt', PRIVATE, 'if any(exists(fd, n) for n in (REGISTRY, CONTROL, APP)):', 'if False:', 'Paths.test_orphan_registry_never_spawns_or_adopts', 'ORPHAN_WAS_RESTARTED'),
    ('stopped-no-restart', PRIVATE, "if exists(fd, 'stopped.json'):", 'if False:', 'Paths.test_stopped_tombstone_refuses_automatic_start', 'STOPPED_OWNER_AUTORESTARTED'),
    ('native-owned-before-http', PRIVATE, 'verify_peer(sock, spec.expected_exe, binding)', 'pass', 'Paths.test_wrong_app_binding_receives_no_handshake_bytes', 'NATIVE_UNVERIFIED_HANDSHAKE_SENT'),
    ('native-exe-before-http', PRIVATE, 'verify_peer(sock, spec.expected_exe, binding)', 'pass', 'Paths.test_wrong_app_executable_receives_no_handshake_bytes', 'WRONG_NATIVE_EXE_RECEIVED_BYTES'),
    ('child-pid-reuse', PRIVATE, 'or process_starttime(self.child.pid) != self.child_start', 'or False', 'Lifecycle.test_child_binding_rechecks_starttime_and_restart_budget_is_finite', 'PRIVATE_ACCEPTED_child'),
    ('child-reap', PRIVATE, '        self.child.wait()\n        if self.app_identity', '        pass\n        if self.app_identity', 'Lifecycle.test_child_exit_reaps_actual_handle_before_one_replacement', "Expected 'wait' to be called once"),
    ('restart-budget', PRIVATE, 'if self.restarts >= len(RESTART_DELAYS):', 'if False:', 'Lifecycle.test_child_binding_rechecks_starttime_and_restart_budget_is_finite', 'RESTART_BUDGET_RAISED IndexError'),
    ('stop-human-confirmation', PRIVATE, 'if human_disconnected is not True:', 'if False:', 'Lifecycle.test_stop_requires_human_confirmation_and_idle_native_proof', 'PRIVATE_ACCEPTED_human_confirmation'),
    ('stop-helper-exclusion', PRIVATE, 'fcntl.flock(guard, fcntl.LOCK_EX | fcntl.LOCK_NB)', 'pass', 'Lifecycle.test_helper_admission_lock_prevents_owner_stop', 'PRIVATE_ACCEPTED_busy'),
    ('stop-page-complete', PRIVATE, "or result.get('nextCursor') is not None", 'or False', 'Lifecycle.test_incomplete_or_unbounded_loaded_list_cannot_prove_idle', 'PRIVATE_ACCEPTED_protocol'),
    ('stop-idle-only', PRIVATE, "or thread.get('status', {}).get('type') != 'idle'", 'or False', 'Lifecycle.test_stop_requires_human_confirmation_and_idle_native_proof', 'PRIVATE_ACCEPTED_busy'),
    ('private-enforce-only', DRIVER, "if self._daemon_identity_check != 'enforce' or self._manage_daemon is not True:", 'if self._manage_daemon is not True:', 'DriverWiring.test_private_observe_cannot_reach_transport_even_with_valid_peer', 'PRIVATE_ACCEPTED_config'),
    ('stop-reader-partial-frame', DRIVER, 'chunk = sock.recv(n - len(chunks))', 'chunk = self._sock.recv(n - len(chunks))', 'DriverWiring.test_stop_during_partial_frame_uses_captured_socket_or_oserror', "has no attribute 'recv'"),
]


def run(output):
    out = Path(output).resolve(); out.mkdir(parents=True, exist_ok=False)
    head = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    rows = []; failures = []
    with tempfile.TemporaryDirectory(prefix='dwpe-check-') as tmp:
        sandbox = Path(tmp)
        package = sandbox / 'helper'
        shutil.copytree(ROOT / 'tools/sessionhelper', package,
                        ignore=shutil.ignore_patterns('__pycache__', '*.pyc', '.venv', 'venv'))
        home = sandbox / 'home'; home.mkdir(mode=0o700)
        env = {'HOME': str(home), 'PATH': str(Path(sys.executable).parent) + ':/usr/bin:/bin',
               'LANG': 'C.UTF-8', 'PYTHONDONTWRITEBYTECODE': '1',
               'PYTHONPATH': str(package) + os.pathsep + str(package / 'tests')}

        def test(label, case):
            command = [sys.executable, '-B', '-m', 'unittest', '-v', 'test_private_endpoint' + ('.' + case if case else '')]
            start = datetime.now(timezone.utc).isoformat()
            with (out / (label + '.stdout')).open('wb') as stdout, (out / (label + '.stderr')).open('wb') as stderr:
                result = subprocess.run(command, cwd=sandbox, env=env, stdout=stdout, stderr=stderr, timeout=90)
            (out / (label + '.rc')).write_text(str(result.returncode) + '\n')
            row = {'label': label, 'case': case, 'start': start, 'end': datetime.now(timezone.utc).isoformat(),
                   'rc': result.returncode, 'command': command}
            rows.append(row)
            return row, (out / (label + '.stderr')).read_text()

        row, _ = test('baseline', '')
        assert row['rc'] == 0, 'CHECKER_BASELINE_FAILED'
        for name, file, old, new, case, marker in MUTATIONS:
            target = package / file; original = target.read_text()
            assert original.count(old) == 1, 'MUTATION_TARGET_DRIFT_' + name
            try:
                target.write_text(original.replace(old, new, 1))
                row, text = test(name + '-withdrawn', case)
                red = row['rc'] != 0 and marker in text and ('FAIL:' in text or 'ERROR:' in text)
                row['required_marker'] = marker; row['designated_red'] = red
                if not red:
                    failures.append('MUTATION_NOT_DESIGNATED_RED_' + name)
            finally:
                target.write_text(original)
            row, _ = test(name + '-restored', case)
            assert row['rc'] == 0, 'MUTATION_RESTORE_FAILED_' + name
        row, _ = test('final-baseline', '')
        assert row['rc'] == 0, 'CHECKER_FINAL_FAILED'
    summary = {'source_head': head, 'scope': 'LOCAL_INERT_NOT_NATIVE_VERSION_ACCEPTANCE',
               'mutations': len(MUTATIONS), 'commands': len(rows), 'rows': rows, 'failures': failures}
    (out / 'RESULT.json').write_text(json.dumps(summary, indent=2) + '\n')
    assert not failures, ','.join(failures)
    print('PRIVATE_ENDPOINT_LOCAL_PASS mutations=' + str(len(MUTATIONS)))


if __name__ == '__main__':
    run(sys.argv[1])
