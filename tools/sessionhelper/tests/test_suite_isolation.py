import os
from pathlib import Path
import unittest
from unittest import mock

from _suite_isolation import (assert_isolated, clean_environment,
                              isolated_environment, isolate_test_cases)

IMPORT_HOME = os.environ.get('HOME')
IMPORT_SOCKET = os.environ.get('SH_MCP_IPC_SOCK')


class SuiteIsolationTests(unittest.TestCase):
    def test_runner_installs_case_isolation_before_setup(self):
        self.assertNotEqual(os.environ.get('HOME'), IMPORT_HOME,
                            'isolation boundary: case HOME must differ from discovery HOME')
        self.assertNotEqual(os.environ.get('SH_MCP_IPC_SOCK'), IMPORT_SOCKET,
                            'isolation boundary: case socket must differ from discovery socket')

    def test_seat_environment_is_removed_without_reading_values(self):
        dirty = {'SH_SECRET': 'fixture', 'SH_OWNER': 'fixture',
                 'DINGWEI_REPLY_CONTEXT': 'fixture', 'PATH': '/fixture'}
        self.assertEqual(clean_environment(dirty), {'PATH': '/fixture'})
        with mock.patch.dict(os.environ, dirty):
            with isolated_environment('codex') as directory:
                assert_isolated(directory, 'codex')
                self.assertEqual(os.environ['SH_DRIVER'], 'codex')
                self.assertNotIn('SH_SECRET', os.environ)
                home = Path(os.environ['HOME'])
                (home / 'sentinel').write_text('fixture')
                self.assertFalse(Path(os.environ['SH_MCP_IPC_SOCK']).exists())
            self.assertFalse(home.exists())

    def test_each_case_gets_distinct_home_and_socket_and_restores_environment(self):
        homes, sockets = [], []
        before = dict(os.environ)

        class Probe(unittest.TestCase):
            def runTest(inner):
                home = Path(os.environ['HOME'])
                inner.assertFalse((home / 'sentinel').exists())
                (home / 'sentinel').write_text('fixture')
                homes.append(home)
                sockets.append(os.environ['SH_MCP_IPC_SOCK'])
                os.environ['SH_CASE_LEAK'] = 'fixture'

        result = unittest.TestResult()
        with isolate_test_cases('claude'):
            unittest.TestSuite([Probe(), Probe()]).run(result)
        self.assertTrue(result.wasSuccessful(), result.errors + result.failures)
        self.assertEqual(result.testsRun, 2)
        self.assertEqual(len(set(homes)), 2)
        self.assertEqual(len(set(sockets)), 2)
        self.assertTrue(all(not home.exists() for home in homes))
        self.assertEqual(dict(os.environ), before)

    def test_removed_isolation_fails_at_boundary_before_helper_execution(self):
        with isolated_environment() as outer:
            with mock.patch.dict(os.environ, HOME=str(outer.parent / 'outside')):
                with self.assertRaisesRegex(AssertionError, 'isolation boundary: HOME'):
                    assert_isolated(outer)


if __name__ == '__main__':
    unittest.main()
