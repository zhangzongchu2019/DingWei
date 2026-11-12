#!/usr/bin/env python3
"""仅在一次性副本撤掉保护；每刀要求指定用例从绿转红。"""
from pathlib import Path
import argparse
import datetime
import json
import os
import shutil
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[3]
TEST = 'test_codex_stale_thread.ConfiguredRootBinding.'
LANE = 'test_sub_agent_helper.ConfiguredRootLaneTests.test_root_configuration_does_not_bind_children_or_relax_broker'
CASES = [
    ('A-root-config', 'driver_adapter.py',
     'root_binding = dict(bound_thread_id=bound_thread, require_bound_thread=True) if bound_thread else {}',
     'root_binding = {}', TEST + 'test_configured_root_read_before_exact_turn'),
    ('B-no-create', 'codexDriver.py',
     '''        if self._required_bound_thread_id:
            self._thread_id = self._session_id = self._required_bound_thread_id
            self._validate_bound_thread()
            if self._private_endpoint is not None:
                self._private_endpoint.save_thread(self._thread_id)
            return
''', '', TEST + 'test_unavailable_never_creates_and_failed_receipt_is_durable'),
    ('C-unset', 'config.py',
     'codex_bound_thread_id=env.get("SH_CODEX_BOUND_THREAD_ID", "").strip()',
     'codex_bound_thread_id=env.get("SH_CODEX_BOUND_THREAD_ID", "unexpected-default-thread").strip()',
     TEST + 'test_unset_factory_preserves_legacy_constructor_arguments'),
    ('D-private-conflict', 'codexDriver.py',
     'if saved_thread and saved_thread != self._required_bound_thread_id:',
     'if False and saved_thread and saved_thread != self._required_bound_thread_id:',
     TEST + 'test_private_binding_conflict_stops_startup_and_reports_both_ids'),
    ('G-child-isolation', 'sub_agent_runtime.py',
     'require_bound_thread=bool(not node_id and root._required_bound_thread_id)',
     'require_bound_thread=bool(root._required_bound_thread_id)', LANE),
    ('G-root-replacement', 'sub_agent_runtime.py',
     'require_bound_thread=bool(not node_id and root._required_bound_thread_id)',
     'require_bound_thread=False', LANE),
    ('B-adapter-reason', 'driver_adapter.py',
     'for admission_reason in ("session_unavailable", "bound_thread_unavailable", "model_admission_unknown"):',
     'for admission_reason in ("session_unavailable", "model_admission_unknown"):',
     'test_codex_stale_thread.CodexStaleThread.test_adapter_bound_thread_failure_has_no_injected_ack_and_publishes_reason'),
]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=True)
    started = datetime.datetime.now(datetime.timezone.utc).isoformat()
    results = []
    with tempfile.TemporaryDirectory(prefix='hbt-mutation-') as temporary:
        temp = Path(temporary)
        shutil.copytree(ROOT / 'tools/sessionhelper/sessionhelper', temp / 'sessionhelper',
                        ignore=shutil.ignore_patterns('__pycache__', '*.pyc'))
        shutil.copytree(ROOT / 'tools/sessionhelper/tests', temp / 'tests',
                        ignore=shutil.ignore_patterns('__pycache__', '*.pyc'))
        (temp / 'home').mkdir()
        (temp / 'tmp').mkdir()
        env = dict(HOME=str(temp / 'home'), TMPDIR=str(temp / 'tmp'),
                   PATH=os.environ.get('PATH', '/usr/local/bin:/usr/bin:/bin'),
                   PYTHONPATH=str(temp) + ':' + str(temp / 'tests'),
                   PYTHONDONTWRITEBYTECODE='1', LANG='C.UTF-8', TZ='UTC')
        def run(name, selectors):
            with (args.output / (name + '.log')).open('w') as log:
                return subprocess.run([sys.executable, '-B', '-m', 'unittest', '-v', *selectors],
                                      cwd=temp, env=env, stdout=log, stderr=subprocess.STDOUT,
                                      timeout=60).returncode
        selectors = list(dict.fromkeys(case[4] for case in CASES))
        control = run('control', selectors)
        if control != 0:
            raise SystemExit('green control failed rc=' + str(control))
        for name, filename, before, after, selector in CASES:
            target = temp / 'sessionhelper' / filename
            original = target.read_text()
            if original.count(before) != 1:
                raise SystemExit('mutation anchor count mismatch: ' + name)
            try:
                target.write_text(original.replace(before, after, 1))
                rc = run(name, [selector])
                log = (args.output / (name + '.log')).read_text()
                killed = rc == 1 and ('FAIL:' in log or 'ERROR:' in log) and selector.split('.')[-1] in log
                results.append(dict(name=name, test=selector, rc=rc, killed=killed))
            finally:
                target.write_text(original)
    summary = dict(start=started, end=datetime.datetime.now(datetime.timezone.utc).isoformat(),
                   control_rc=control, killed=sum(r['killed'] for r in results), total=len(results), results=results)
    (args.output / 'mutations.json').write_text(json.dumps(summary, ensure_ascii=False, indent=2) + '\n')
    print(json.dumps(summary, ensure_ascii=False))
    return 0 if all(r['killed'] for r in results) else 1


if __name__ == '__main__':
    raise SystemExit(main())
