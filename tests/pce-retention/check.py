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
GUARD = 'update_launcher/guard.py'
UPDATE = 'sessionhelper/self_update.py'
DRIVER = 'sessionhelper/codexDriver.py'

# id, file, old, new, exact regression, required assertion marker
MUTATIONS = [
 ('durable-pin',GUARD,"self._write(fd, 'pin-' + ident + '.json', pin)\n                return pin, handles",'return pin, handles','test_pce_retention.RetainTests.test_registration_is_durable_and_locks_every_resource_before_popen','PIN_NOT_DURABLE_BEFORE_POPEN'),
 ('resource-shared-lock',GUARD,'fcntl.flock(f, fcntl.LOCK_SH | fcntl.LOCK_NB)','pass','test_pce_retention.RetainTests.test_registration_is_durable_and_locks_every_resource_before_popen','REGISTERED_RESOURCE_NOT_LOCKED'),
 ('inherited-fd-identity',GUARD,'if (st.st_dev, st.st_ino) != (actual.st_dev, actual.st_ino):','if False:','test_pce_retention.RetainTests.test_missing_or_wrong_inherited_descriptor_refused','FOREIGN_PIN_FD_ACCEPTED'),
 ('endpoint-pin-binding',GUARD,"or pin['endpoint'] != endpoint or pin['digest']", "or False or pin['digest']",'test_pce_retention.RetainTests.test_missing_or_wrong_inherited_descriptor_refused','WRONG_ENDPOINT_PIN_ACCEPTED'),
 ('popen-pass-fds',PRIVATE,'pass_fds=tuple(handles))','pass_fds=())','test_private_endpoint.RealOwnerProcess.test_double_start_helper_restart_child_exit_and_idle_stop','OWNER_DOUBLE_START_FAILED'),
 ('owner-package-closure',GUARD,"package_fingerprint(candidate) != pin['digest'] or runtime_fingerprint()", "False or runtime_fingerprint()",'test_pce_retention.RetainTests.test_any_package_or_runtime_change_requires_manual','OWNER_CLOSURE_CHANGED'),
 ('owner-interpreter',GUARD,"or runtime_fingerprint() != pin['runtime'])):", 'or False)):', 'test_pce_retention.RetainTests.test_any_package_or_runtime_change_requires_manual','OWNER_INTERPRETER_CHANGED'),
 ('pending-owner-race',GUARD,'if pending and package_fingerprint(state.slot(pending)) != package_fingerprint(package):','if False:','test_pce_retention.RetainTests.test_pending_incompatible_upgrade_prevents_new_owner_pin','OWNER_RACED_PENDING_UPGRADE'),
 ('prepare-collision',UPDATE,'retention.check_locked(retain_fd, replacing=destination)','pass','test_self_update_offer.OfferTests.test_pce_pinned_collision_and_incompatible_candidate_refused','PINNED_COLLISION_RMTREE'),
 ('prepare-before-arm',UPDATE,'retention.check_locked(retain_fd, slot)','pass','test_self_update_offer.OfferTests.test_pce_pinned_collision_and_incompatible_candidate_refused','OWNER_UPDATE_CLOSURE_CHANGED'),
 ('guard-arm',GUARD,'retention.check_locked(retain_fd, self.slot(item))','pass','test_pce_retention.RetainTests.test_guard_arm_select_and_fallback_refuse_incompatible_runtime','INCOMPATIBLE_ARM_ACCEPTED'),
 ('guard-current',GUARD,'retention.check_locked(retain_fd, self.slot(data[name]))','pass','test_pce_retention.RetainTests.test_guard_arm_select_and_fallback_refuse_incompatible_runtime','INCOMPATIBLE_CURRENT_SELECTED'),
 ('guard-factory',GUARD,'retention.check_locked(retain_fd, self.factory)','pass','test_pce_retention.RetainTests.test_guard_arm_select_and_fallback_refuse_incompatible_runtime','INCOMPATIBLE_FACTORY_SELECTED'),
 ('gc-active-unknown',GUARD,"or pin['state'] != 'stopped'",'or False','test_pce_retention.RetainTests.test_owner_death_free_lock_does_not_authorize_reclaim','LOST_OWNER_RECLAIMED'),
 ('gc-stop-receipt',GUARD,"or receipt.get('nonce') != pin['nonce']",'or False','test_pce_retention.RetainTests.test_resource_lock_and_receipt_are_independent_reclaim_gates','GC_STOP_RECEIPT_IGNORED'),
 ('gc-same-inode',GUARD,("or _retain_identity(path)[:2] != pin['package_identity']","if [before.st_dev,before.st_ino] != pin['package_identity']:"),('or False','if False:'),'test_pce_retention.RetainTests.test_inode_swap_factory_and_arbitrary_path_never_reclaimed','SWAPPED_SLOT_RECLAIMED'),
 ('gc-late-inode',GUARD,"if [before.st_dev,before.st_ino] != pin['package_identity']:",'if False:','test_pce_retention.RetainTests.test_last_moment_inode_swap_before_rmtree_is_refused','LATE_INODE_SWAP_RECLAIMED'),
 ('gc-journal-reference',GUARD,"if any(data.get(k) and data[k].get('sha256') == sha for k in ('current','previous','pending')):",'if False:','test_pce_retention.RetainTests.test_current_previous_pending_and_other_pin_block_reclaim','REFERENCED_SLOT_RECLAIMED_current'),
 ('gc-previous-reference',GUARD,"for k in ('current','previous','pending')):","for k in ('current','pending')):",'test_pce_retention.RetainTests.test_current_previous_pending_and_other_pin_block_reclaim','REFERENCED_SLOT_RECLAIMED_previous'),
 ('gc-pending-reference',GUARD,"for k in ('current','previous','pending')):","for k in ('current','previous')):",'test_pce_retention.RetainTests.test_current_previous_pending_and_other_pin_block_reclaim','REFERENCED_SLOT_RECLAIMED_pending'),
 ('gc-other-pin-reference',GUARD,"if any(p['id'] != ident and p['state'] in ('active','unknown') and p['package'] == str(path) for p in records):",'if False:','test_pce_retention.RetainTests.test_current_previous_pending_and_other_pin_block_reclaim','SHARED_PACKAGE_RECLAIMED'),
 ('gc-resource-locked',GUARD,"try: fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)\n                    except BlockingIOError",'try: pass\n                    except BlockingIOError','test_pce_retention.RetainTests.test_resource_lock_and_receipt_are_independent_reclaim_gates','SHARED_RESOURCE_LOCK_IGNORED'),
 ('stop-nonce',GUARD,"or pin['nonce'] != nonce",'or False','test_pce_retention.RetainTests.test_stop_receipt_nonce_and_held_lock_required','FORGED_STOP_CERT_ACCEPTED'),
 ('stop-held-lease',GUARD,"try: fcntl.flock(probe, fcntl.LOCK_EX | fcntl.LOCK_NB)\n                except BlockingIOError: raise UpdateError('retain_busy')", "try: pass\n                except BlockingIOError: raise UpdateError('retain_busy')",'test_pce_retention.RetainTests.test_stop_receipt_nonce_and_held_lock_required','LIVE_STOP_CERT_ACCEPTED'),
 ('register-gc-transaction',GUARD,'fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)\n                        break','pass\n                        break','test_pce_retention.RetainTests.test_registration_serializes_with_reclaim_without_deletion_window','PIN_REGISTER_GC_RACE'),
 ('private-pin-permissions',GUARD,'or stat.S_IMODE(info.st_mode) != 0o600 or info.st_nlink != 1','or False or info.st_nlink != 1','test_pce_retention.RetainTests.test_bad_pin_symlink_permissions_and_storage_failure_fail_closed','PUBLIC_PIN_IGNORED'),
 ('pin-storage-limit',GUARD,'if len(records) >= self.LIMIT:','if False:','test_pce_retention.RetainTests.test_pin_limit_refuses_before_registering_new_owner','PIN_LIMIT_IGNORED'),
 ('authenticated-stop-commit',PRIVATE,"retention.finish(record['pin'], record['pin_nonce'],\n                                {k: reply[k] for k in ('pin','boot','challenge','state')})",'pass','test_private_endpoint.RealOwnerProcess.test_double_start_helper_restart_child_exit_and_idle_stop','AUTHENTICATED_STOP_PIN_NOT_CERTIFIED'),
 ('old-launcher-refusal',PRIVATE,"if getattr(module, 'RETENTION_API', None) != 1:",'if False:','test_pce_retention.RetainTests.test_old_or_absent_launcher_refuses_private','OLD_LAUNCHER_PRIVATE_ACCEPTED'),
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
            command = [sys.executable, '-B', '-m', 'unittest', '-v', *( [case] if case else ['test_pce_retention','test_private_endpoint','test_self_update_offer'])]
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
            changes = list(zip(old, new)) if type(old) is tuple else [(old, new)]
            mutated = original
            for before, after in changes:
                assert mutated.count(before) == 1, 'MUTATION_TARGET_DRIFT_' + name
                mutated = mutated.replace(before, after, 1)
            try:
                target.write_text(mutated)
                row, text = test(name + '-withdrawn', case)
                red = row['rc'] != 0 and marker in text and ('FAIL:' in text and 'AssertionError' in text and 'ERROR:' not in text)
                row['required_marker'] = marker; row['designated_red'] = red
                if not red:
                    failures.append('MUTATION_NOT_DESIGNATED_RED_' + name)
            finally:
                target.write_text(original)
            row, _ = test(name + '-restored', case)
            assert row['rc'] == 0, 'MUTATION_RESTORE_FAILED_' + name
        row, _ = test('final-baseline', '')
        assert row['rc'] == 0, 'CHECKER_FINAL_FAILED'
    summary = {'source_head': head, 'scope': 'ISOLATED_RETENTION_AND_OWNER_FIXTURES_NOT_PRODUCTION',
               'mutations': len(MUTATIONS), 'commands': len(rows), 'rows': rows, 'failures': failures}
    (out / 'RESULT.json').write_text(json.dumps(summary, indent=2) + '\n')
    assert not failures, ','.join(failures)
    print('PCE_RETENTION_LOCAL_PASS mutations=' + str(len(MUTATIONS)))


if __name__ == '__main__':
    run(sys.argv[1])
