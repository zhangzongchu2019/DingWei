"""在一次性本地源码副本逐刀撤保护，要求目标断言红且ERROR=0。"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[2]
PACKAGE = ROOT / 'tools/sessionhelper/sessionhelper'
TESTS = ROOT / 'tools/sessionhelper/tests'

# 文件、被撤保护、单刀替换、用例、目标断言。不得把导入失败算变异有效。
MUTATIONS = [
    ('duplicate', 'codexDriver.py', 'if len(candidates) != 1:', 'if not candidates:',
     'test_duplicate_client_id_is_ambiguous', 'DUPLICATE_CLIENT_ID_ADVANCED_ACK'),
    ('historical-binding', 'codexDriver.py', 'threads = {r.thread_id for r in records if r.thread_id}', 'threads = {self._thread_id}',
     'test_duplicate_client_id_on_historical_binding_is_ambiguous', 'HISTORICAL_DUPLICATE_ADVANCED_ACK'),
    ('budget', 'codexDriver.py', 'if remaining <= 0:', 'if remaining < 0:',
     'test_page_limit_200_is_incomplete_even_if_match_seen', 'TURN_BUDGET_NOT_INCOMPLETE'),
    ('pagination', 'codexDriver.py', 'if cursor is None:\n                        break', 'if True:\n                        break',
     'test_opaque_cursor_and_whole_scan_uniqueness', 'PAGINATION_EARLY_MATCH_ADVANCED_ACK'),
    ('nullable-time', 'codexDriver.py', 'if not _native_time(completed) or completed < started or completed > time.time() + NATIVE_TIME_TOLERANCE_SECONDS:', 'if False:',
     'test_nullable_and_nonfinite_times_keep_pending', 'INVALID_TIME_ADVANCED_ACK'),
    ('thread', 'codexDriver.py', "turn.get('threadId', tid) != tid", 'False',
     'test_wrong_thread_and_wrong_turn_keep_pending', 'WRONG_THREAD_ADVANCED_ACK'),
    ('generation', 'inbox_lifecycle.py', 'if expected_context is not None and any(', 'if False and any(',
     'test_old_generation_snapshot_cannot_advance_ack', 'OLD_GENERATION_ADVANCED_ACK'),
    ('terminal-conflict', 'inbox_lifecycle.py', "            if row['state']=='processed':\n                if row['turn_outcome']!=outcome:raise ValueError('native terminal cannot replace original outcome')\n                return\n", '',
     'test_conflicting_terminal_never_replaces_first_outcome', 'CONFLICTING_OUTCOME_REPLACED'),
    ('restart', 'codexDriver.py', 'for record in targets:\n                self._reconcile_event(record)', 'for record in []:\n                self._reconcile_event(record)',
     'test_restart_process_replays_persisted_proof_without_rpc', 'PROCESS_RESTART_LOST_PROOF'),
    ('reordered', 'codexDriver.py', "i.get('clientId') == old.envelope_id", "i.get('clientId') == 'disabled-correlation'",
     'test_notification_before_response_and_duplicate_merge', 'REORDERED_NOTIFICATION_LOST_TERMINAL'),
    ('second-tolerance', 'codexDriver.py', 'NATIVE_TIME_TOLERANCE_SECONDS = 1.0', 'NATIVE_TIME_TOLERANCE_SECONDS = 0.0',
     'test_native_second_precision_one_second_tolerance', 'SECOND_PRECISION_REJECTED'),
    ('old-time', 'inbox_lifecycle.py', "if admitted_at + NATIVE_TIME_TOLERANCE_SECONDS < row['dispatch_intent_at']:", 'if False:',
     'test_before_intent_more_than_one_second_rejected', 'OLD_TIME_ADVANCED_ACK'),
    ('rate', 'codexDriver.py', 'if any(now - stamp < RECONCILE_THREAD_INTERVAL_SECONDS for stamp in last.values()):', 'if False:',
     'test_thread_rate_limit_survives_driver_replacement', 'REPLACEMENT_RESET_THREAD_RATE_LIMIT'),
    ('seat-concurrency', 'codexDriver.py', "            except BlockingIOError:\n                return 'pending'", '            except BlockingIOError:\n                pass',
     'test_one_reconcile_request_per_seat_across_processes', 'SEAT_RECONCILE_OVERLAPPED'),
    ('busy-stall', 'codexDriver.py', '# Healthy native work can run beyond both admission and stall', "if True:\n                return 'session busy'\n            # Healthy native work can run beyond both admission and stall",
     'test_healthy_busy_wait_has_no_stall_deadline', 'HEALTHY_BUSY_TURN_EXPIRED_LOCAL_QUEUE'),
    ('local-clock', 'driver_adapter.py', 'return 0.0 if started is None else time.monotonic() - started', 'return time.monotonic() - self._dispatch_clock(command) if started is None else time.monotonic() - started',
     'test_local_queue_does_not_use_dispatch_intent_clock', 'LOCAL_QUEUE_USED_DISPATCH_CLOCK'),
    ('transport-clock', 'codexDriver.py', 'self.on_turn_start(params or {})\n                self._proc.stdin.write', 'pass\n                self._proc.stdin.write',
     'test_turn_start_clock_begins_after_transport_write_lock', 'TURN_START_CLOCK_NOT_RECORDED'),

    ('unknown-no-ledger', 'driver_adapter.py', "and local_row['state'] == 'processing' and local_row['admitted_at'] is None", "and local_row['admitted_at'] is None",
     'test_unknown_without_driver_row_never_automatically_resubmits', 'UNKNOWN_AUTOMATICALLY_RESUBMITTED'),

    ('healthy-response', 'driver_adapter.py', "if self._which == 'codex' and callable(required) and not required(self._admission_timeout):", 'if False:',
     'test_healthy_response_wait_does_not_replace_transport', 'HEALTHY_TRANSPORT_REPLACED_FOR_RESPONSE_WAIT'),

]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    output = Path(args.output).resolve(); output.mkdir(parents=True, exist_ok=False)
    results = []
    for name, filename, old, new, test, marker in MUTATIONS:
        original = (PACKAGE / filename).read_text()
        assert original.count(old) == 1, (name, original.count(old))
        with tempfile.TemporaryDirectory(prefix='reconcile-mutation-', dir='/tmp') as tmp:
            path = Path(tmp)
            # 仅拷贝受Git跟踪的Python产品文件，无env、私有状态或认证文件。
            tracked = subprocess.check_output(['git', 'ls-files', 'tools/sessionhelper/sessionhelper'], cwd=ROOT, text=True).splitlines()
            package = path / 'sessionhelper'; package.mkdir()
            for relative in tracked:
                source = ROOT / relative
                if source.suffix == '.py':
                    target = package / source.relative_to(PACKAGE)
                    target.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copyfile(source, target)
            assert all(p.suffix == '.py' for p in package.rglob('*') if p.is_file())
            (package / filename).write_text(original.replace(old, new, 1))
            # 进程级文件锁与对象锁分层；并发变异只撤对象锁时文件锁仍正确挡住。
            # 此刀仅撤跨进程flock分支，用子进程边界专门验证。
            environment = {'HOME': tmp, 'CODEX_HOME': tmp + '/codex', 'PATH': '/usr/bin:/bin',
                           'PYTHONPATH': os.pathsep.join([tmp, str(TESTS)])}
            command = [sys.executable, '-B', '-m', 'unittest', '-v', 'test_codex_delivery_reconcile.ReconcileBoundaries.' + test]
            run = subprocess.run(command, cwd=tmp, env=environment, text=True, capture_output=True, timeout=20)
            log = run.stdout + run.stderr
            (output / (name + '.log')).write_text(log)
            valid = run.returncode != 0 and marker in log and 'ERROR:' not in log and 'FAIL:' in log
            results.append({'mutation': name, 'test': test, 'rc': run.returncode, 'target_assertion_red': valid,
                            'log_sha256': hashlib.sha256(log.encode()).hexdigest()})
    (output / 'RESULT.json').write_text(json.dumps(results, indent=2) + '\n')
    print(json.dumps(results, indent=2))
    raise SystemExit(not all(r['target_assertion_red'] for r in results))


if __name__ == '__main__':
    main()
