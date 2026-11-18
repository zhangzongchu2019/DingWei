"""Run the timing-sensitive target tests in one isolated driver profile."""
import argparse
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[4]
sys.path.insert(0, str(ROOT / 'tools/sessionhelper/tests'))

from _suite_isolation import isolated_environment, assert_isolated, isolate_test_cases

TARGETS = (
    'test_dispatch_lifecycle.AdmissionDeadlineTest.test_missing_accepted_uses_admission_deadline_and_recovers',
    'test_dispatch_lifecycle.AdmissionDeadlineRaceTest.test_blocked_deliver_accepted_after_deadline_is_not_admission',
    'test_dispatch_lifecycle.AdmissionDeadlineRaceTest.test_slow_poll_accepted_after_deadline_is_not_admission',
    'test_sub_agent_helper.StoreTests.test_aa010_feature_off_drains_without_native_or_capability',
)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--profile', required=True, choices=('claude', 'codex'))
    args = parser.parse_args()
    with isolated_environment(args.profile) as directory:
        assert_isolated(directory, args.profile)
        sys.path[:0] = [str(ROOT / 'tools/sessionhelper'), str(ROOT / 'view-service')]
        suite = unittest.TestSuite()
        loader = unittest.defaultTestLoader
        for name in TARGETS:
            suite.addTests(loader.loadTestsFromName(name))
        with isolate_test_cases(args.profile):
            result = unittest.TextTestRunner(verbosity=2).run(suite)
        return int(not result.wasSuccessful())


if __name__ == '__main__':
    raise SystemExit(main())
