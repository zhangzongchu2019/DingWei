"""Local inert regression + withdraw/restore checks; never touches a live seat.

Run with the locked helper venv: python check.py /new/private/output-directory
NODE_PATH/PW_CHANNEL select an already installed local browser test environment.
No downloads or installs are performed. The temporary detached Git worktree is
retained under the output directory for review, with every mutation restored.
"""
import datetime
import json
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
DRIVER = 'tools/sessionhelper/sessionhelper/codexDriver.py'
ADAPTER = 'tools/sessionhelper/sessionhelper/driver_adapter.py'
VIEW = 'view-service/renderer-canary-sse.html'
TEST = 'test_codex_stale_thread'

MUTATIONS = [
    ('no-validation', DRIVER, '                    self._validate_bound_thread()\n', '                    pass\n', 'CodexStaleThread.test_loaded_thread_validated_before_admission', 'THREAD_ADMISSION_UNVALIDATED'),
    ('no-resume', DRIVER, '            result = self._request("thread/resume", {"threadId": self._thread_id, "excludeTurns": True}, timeout=15)\n', '            raise ThreadUnavailable("withdrawn resume")\n', 'CodexStaleThread.test_reconnect_restores_exact_persisted_thread_before_turn', 'THREAD_RESUME_NOT_PERFORMED'),
    ('bad-binding-accepted', DRIVER, '            raise ThreadUnavailable("thread identity, directory or admission status mismatch")\n', '            pass\n', 'CodexStaleThread.test_binding_result_identity_directory_and_admission_are_strict', 'INVALID_BINDING_ADMITTED'),
    ('missing-thread-replaced', DRIVER, '                    return self._admission_failure(envelope_id, body_hash, exc, uncertain=False)\n', '                    self._ensure_thread()\n                    return self._admission_failure(envelope_id, body_hash, exc, uncertain=False)\n', 'CodexStaleThread.test_missing_thread_never_creates_or_replays_and_retains_ledger', 'MISSING_THREAD_REPLACED'),
    ('historical-rebind', DRIVER, '                # Historical receipt lookup must not rebind the active session.\n', '                self._thread_id = existing.thread_id\n                self._session_id = existing.thread_id\n', 'CodexStaleThread.test_historical_receipts_do_not_change_active_binding_or_create_thread', 'HISTORICAL_RECEIPT_REBOUND_ACTIVE'),
    ('historical-poll-current', DRIVER, '            return self._read_turn_state(turn_id, thread_id=record.thread_id)\n', '            return self._read_turn_state(turn_id)\n', 'CodexStaleThread.test_historical_poll_and_state_update_use_record_thread', 'HISTORICAL_POLL_WRONG_THREAD'),
    ('historical-write-current', DRIVER, '                self._store.update_state(thread_id, envelope_id, state, detail)\n', '                self._store.update_state(self._thread_id, envelope_id, state, detail)\n', 'CodexStaleThread.test_historical_poll_and_state_update_use_record_thread', 'HISTORICAL_UPDATE_WRONG_THREAD'),
    ('uncertain-marked-failed', DRIVER, 'self._store.upsert(self._thread_id, envelope_id, "", body_hash, "unknown" if uncertain else "failed", detail)', 'self._store.upsert(self._thread_id, envelope_id, "", body_hash, "failed", detail)', 'CodexStaleThread.test_timeout_and_disconnect_after_dispatch_never_resubmit_across_restart', 'UNCERTAIN_ADMISSION_NOT_DURABLE'),
    ('no-predispatch-journal', DRIVER, '                self._store.upsert(self._thread_id, envelope_id, "", body_hash, "unknown", "model_admission_unknown: dispatch pending")\n', '', 'CodexStaleThread.test_pending_marker_precedes_dispatch_and_malformed_success_is_uncertain', 'DISPATCH_NOT_DURABLE'),
    ('uncertain-replayed', DRIVER, 'existing.state != "failed" or existing.turn_id', 'existing.state not in ("failed", "unknown") or existing.turn_id', 'CodexStaleThread.test_timeout_and_disconnect_after_dispatch_never_resubmit_across_restart', 'UNCERTAIN_ADMISSION_REPLAYED'),
    ('accepted-failed-replayed', DRIVER, 'existing.state != "failed" or existing.turn_id', 'existing.state != "failed"', 'CodexStaleThread.test_historical_receipts_do_not_change_active_binding_or_create_thread', 'HISTORICAL_ACCEPTED_REPLAYED'),
    ('rpc-shape-discarded', DRIVER, '            raise JsonRpcError(method, response["error"])\n', '            raise RuntimeError("withdrawn structured error")\n', 'RPCErrorShapes.test_both_transports_preserve_rpc_error_without_private_payload_in_string', 'RPC_ERROR_SHAPE_DISCARDED'),
    ('missing-classification-discarded', DRIVER, '                return "missing_thread"\n', '                return "rpc_error"\n', 'RPCErrorShapes.test_missing_thread_requires_exact_method_code_and_id', 'MISSING_ERROR_NOT_RECOGNIZED'),
    ('eof-not-signalled', DRIVER, '            for pending in list(self._pending.values()):\n                try:\n                    pending.put_nowait({"_transport_closed": True})\n                except queue.Full:\n                    pass\n', '', 'RPCErrorShapes.test_real_reader_eof_is_disconnect_and_pending_send_failure_is_cleaned', 'EOF_MISCLASSIFIED_AS_TIMEOUT'),
    ('raw-rpc-error-exposed', DRIVER, '        super().__init__(f"{method} failed: error_class={self.classify()} rpc_code={self.code}")\n', '        super().__init__(f"{method} failed: {self.message}")\n', 'RPCErrorShapes.test_both_transports_preserve_rpc_error_without_private_payload_in_string', 'RAW_RPC_ERROR_EXPOSED'),
    ('unknown-event-hidden', ADAPTER, '                    for admission_reason in ("session_unavailable", "model_admission_unknown"):\n', '                    for admission_reason in ():\n', 'CodexStaleThread.test_adapter_uncertain_admission_is_visible_and_not_a_success', 'ADMISSION_FAILURE_EVENT_NOT_VISIBLE'),
    ('injected-ack-removed', ADAPTER, '        if result.state == "queued":\n            self._work_available.set()\n            if on_injected is not None:\n                on_injected()\n', '        if result.state == "queued":\n            self._work_available.set()\n', 'CodexStaleThread.test_adapter_preserves_injected_ack_and_publishes_correlated_failure', 'INJECTED_ACK_SEMANTICS_CHANGED'),
    ('view-failure-hidden', VIEW, '      if (observeModelAdmissionFailure(ev)) return;\n', '', 'browser', 'VISIBLE_SESSION_UNAVAILABLE'),
    ('view-late-input-not-annotated', VIEW, '        paintModelAdmissionFailure(norm.deliveryId);\n', '', 'browser', 'FAILURE_BEFORE_INPUT_VISIBLE'),
    ('view-delivery-stage-overwritten', VIEW, '      pendingModelAdmissionFailures.delete(key);\n', '      if (entry.stageEl) entry.stageEl.textContent = "delivery failed";\n      pendingModelAdmissionFailures.delete(key);\n', 'browser', 'ACK_STAGE_UNCHANGED'),
    ('view-pending-unbounded', VIEW, '      while (pendingModelAdmissionFailures.size > 1000) {\n', '      while (false) {\n', 'browser', 'PENDING_BOUNDED'),
]

UNIT_RUN = r'''
import json,sys,unittest
expected=sys.argv[2]
class Result(unittest.TextTestResult):
    hits=[]
    def addFailure(self,test,error):
        super().addFailure(test,error)
        if expected and expected in str(error[1]):
            self.hits.append(test.id())
selector=sys.argv[1]
suite=unittest.defaultTestLoader.loadTestsFromName(selector)
r=unittest.TextTestRunner(verbosity=2,resultclass=Result).run(suite)
print('SEMANTIC_ASSERTION_HITS='+json.dumps(r.hits))
sys.exit(0 if r.wasSuccessful() else 1)
'''


def main(output):
    out = Path(output).resolve()
    out.mkdir(mode=0o700, parents=True, exist_ok=False)
    source = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    wt = out / 'worktree'
    subprocess.run(['git', 'worktree', 'add', '--detach', str(wt), source], cwd=ROOT, check=True, stdout=subprocess.PIPE)
    env = {'PATH': str(Path(sys.executable).parent) + ':/usr/bin:/bin',
           'PYTHONPATH': str(wt / 'tools/sessionhelper') + ':' + str(wt / 'tools/sessionhelper/tests'),
           'PYTHONDONTWRITEBYTECODE': '1'}
    for key in ['NODE_PATH', 'PW_CHANNEL']:
        if key in os.environ:
            env[key] = os.environ[key]
    rows = []
    def run(name, test, marker=''):
        home = out / (name + '-home')
        home.mkdir(mode=0o700)
        local = dict(env, HOME=str(home))
        cmd = ['node', 'view-service/renderer-model-admission-regression.js'] if test == 'browser' else [sys.executable, '-c', UNIT_RUN, TEST + ('.' + test if test else ''), marker]
        row = {'name': name, 'test': test, 'marker': marker, 'start_utc': datetime.datetime.now(datetime.timezone.utc).isoformat()}
        proc = subprocess.run(cmd, cwd=wt, env=local, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=90)
        row.update(rc=proc.returncode, end_utc=datetime.datetime.now(datetime.timezone.utc).isoformat())
        text = proc.stdout
        (out / (name + '.log')).write_text(text)
        (out / (name + '.rc')).write_text(str(proc.returncode) + '\n')
        if marker and test != 'browser':
            hits = re.findall(r'^SEMANTIC_ASSERTION_HITS=(.*)$', text, re.M)
            row['specified_assertion_red'] = bool(hits and json.loads(hits[-1]))
        elif marker:
            row['specified_assertion_red'] = bool(re.search(r'^  FAIL ' + re.escape(marker) + r':', text, re.M))
        rows.append(row)
        (out / 'CHECKS.json').write_text(json.dumps({'source': source, 'rows': rows}, indent=2) + '\n')
        return row
    assert run('baseline-unit', '')['rc'] == 0
    assert run('baseline-browser', 'browser')['rc'] == 0
    for name, path, old, new, test, marker in MUTATIONS:
        file = wt / path
        original = file.read_text()
        count = original.count(old)
        assert count == (2 if name in ['rpc-shape-discarded', 'eof-not-signalled'] else 1), (name, count)
        try:
            file.write_text(original.replace(old, new))
            red = run(name + '-withdrawn', test, marker)
            assert red['rc'] != 0 and red['specified_assertion_red'], red
        finally:
            file.write_text(original)
        assert run(name + '-restored', test)['rc'] == 0, name
    assert run('final-unit', '')['rc'] == 0
    assert run('final-browser', 'browser')['rc'] == 0
    assert not subprocess.check_output(['git', 'diff', '--name-only'], cwd=wt, text=True).strip()
    result = {'source': source, 'regression_tests': 22, 'browser_checks': 15, 'mutations': len(MUTATIONS), 'all_specified_assertion_red': True, 'all_restored_green': True, 'scope': 'LOCAL_INERT_PASS_NOT_SEAT_ACCEPTANCE'}
    (out / 'RESULT.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(result))


if __name__ == '__main__':
    main(*sys.argv[1:])
