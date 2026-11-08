"""Repeat the complete fixture file, or apply one isolated deadline mutation."""
import argparse
import datetime
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[4]
TARGET = 'tools.sessionhelper.tests.test_dispatch_lifecycle'


def stamp():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def mutate():
    sys.path[:0] = [str(ROOT), str(ROOT / 'tools/sessionhelper')]
    from sessionhelper.driver_adapter import DriverAdapter
    from tools.sessionhelper.tests.test_dispatch_lifecycle import AdmissionDeadlineTest

    def restart_after_deliver(self, command):
        # One semantic mutation: fresh admission budget after deliver returns,
        # instead of counting time since the persisted dispatch intent.
        receipt = self.driver.deliver(command.delivery_id, command.text)
        command._dispatch_started = time.monotonic()
        return receipt

    suite = unittest.TestSuite([AdmissionDeadlineTest(
        'test_clock_includes_time_since_persisted_dispatch_intent')])
    with patch.object(DriverAdapter, '_bounded_deliver', restart_after_deliver):
        result = unittest.TextTestRunner(verbosity=2).run(suite)
    return 0 if result.wasSuccessful() else 1


def repeat(output, count):
    output.mkdir(parents=True, exist_ok=False)
    env = {**os.environ, 'PYTHONPATH': str(ROOT / 'tools/sessionhelper')}
    records = []
    for iteration in range(1, count + 1):
        started, load, before = stamp(), os.getloadavg(), time.monotonic()
        run = subprocess.run([sys.executable, '-m', 'unittest', '-v', TARGET],
                             cwd=ROOT, env=env, stdout=subprocess.PIPE,
                             stderr=subprocess.STDOUT)
        (output / f'{iteration:02d}.stdout').write_bytes(run.stdout)
        row = {'iteration': iteration, 'started_utc': started,
               'load_before': load, 'load_after': os.getloadavg(),
               'elapsed_seconds': time.monotonic() - before, 'rc': run.returncode}
        records.append(row)
        (output / 'RESULT.json').write_text(json.dumps({
            'requested': count, 'completed': len(records),
            'failures': sum(r['rc'] != 0 for r in records), 'runs': records}, indent=2) + '\n')
        print(json.dumps(row), flush=True)
        if run.returncode:
            return run.returncode
    return 0


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--mutation', action='store_true')
    parser.add_argument('--output', type=Path)
    parser.add_argument('--count', type=int, default=50)
    args = parser.parse_args()
    if args.mutation:
        sys.exit(mutate())
    if args.output is None or args.count < 1:
        parser.error('repeat requires --output and positive --count')
    sys.exit(repeat(args.output.resolve(), args.count))
