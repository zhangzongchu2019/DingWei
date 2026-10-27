import os
import asyncio
import json
import subprocess
import sys
from pathlib import Path
import unittest
from unittest import mock

from _suite_isolation import (assert_isolated, clean_environment,
                              isolated_environment, isolate_test_cases)

IMPORT_HOME = os.environ.get('HOME')
IMPORT_SOCKET = os.environ.get('SH_MCP_IPC_SOCK')


class SuiteIsolationTests(unittest.TestCase):
    def test_module_profile_uses_gate_selection_in_stripped_environment(self):
        from _home_isolation import HomeIsolation
        for profile in ('claude', 'codex'):
            for outer in (None, 'codex' if profile == 'claude' else 'claude'):
                with self.subTest(profile=profile, outer=outer):
                    environment = {'HELPER_TEST_DRIVER': profile}
                    if outer is not None:
                        environment['SH_DRIVER'] = outer
                    with mock.patch.dict(os.environ, environment, clear=True):
                        fixture = HomeIsolation()
                        fixture.start()
                        try:
                            self.assertEqual(os.environ.get('SH_DRIVER'), profile)
                        finally:
                            fixture.finish()
                        self.assertEqual(dict(os.environ), environment)

    def test_module_profile_falls_back_to_outer_driver_then_unset(self):
        for environment, expected in (({}, 'unset'), ({'SH_DRIVER': 'claude'}, 'claude'),
                                      ({'SH_DRIVER': 'codex'}, 'codex'),
                                      ({'HELPER_TEST_DRIVER': 'invalid', 'SH_DRIVER': 'claude'}, 'claude')):
            with self.subTest(environment=environment):
                with mock.patch.dict(os.environ, environment, clear=True):
                    with isolated_environment() as directory:
                        assert_isolated(directory, expected)
                        self.assertEqual(os.environ.get('SH_DRIVER', 'unset'), expected)

    def test_profile_guard_rejects_silent_driver_substitution(self):
        with isolated_environment('claude') as directory:
            for actual in ('codex', 'unset'):
                with mock.patch.dict(os.environ):
                    if actual == 'unset':
                        os.environ.pop('SH_DRIVER')
                    else:
                        os.environ['SH_DRIVER'] = actual
                    with self.assertRaisesRegex(AssertionError, 'isolation profile: selected=claude actual_SH_DRIVER='):
                        assert_isolated(directory, 'claude')

    def test_runner_installs_case_isolation_before_setup(self):
        self.assertNotEqual(os.environ.get('HOME'), IMPORT_HOME,
                            'isolation boundary: case HOME must differ from discovery HOME')
        self.assertNotEqual(os.environ.get('SH_MCP_IPC_SOCK'), IMPORT_SOCKET,
                            'isolation boundary: case socket must differ from discovery socket')

    def test_seat_environment_is_removed_without_reading_values(self):
        dirty = {'SH_SECRET': 'fixture', 'SH_OWNER': 'fixture',
                 'DINGWEI_REPLY_CONTEXT': 'fixture', 'PATH': '/fixture',
                 'CODEX_HOME': '/outside-native-config',
                 'CODEX_FUTURE_SETTING': 'fixture',
                 'CLAUDE_FUTURE_SETTING': 'fixture',
                 'XDG_FUTURE_SETTING': 'fixture',
                 'ANTHROPIC_FUTURE_SETTING': 'fixture',
                 'OPENAI_FUTURE_SETTING': 'fixture'}
        self.assertEqual(clean_environment(dirty), {'PATH': '/fixture'})
        with mock.patch.dict(os.environ, dirty):
            with isolated_environment('codex') as directory:
                assert_isolated(directory, 'codex')
                self.assertEqual(os.environ['SH_DRIVER'], 'codex')
                for key in ('SH_SECRET', 'CODEX_FUTURE_SETTING',
                            'CLAUDE_FUTURE_SETTING', 'XDG_FUTURE_SETTING',
                            'ANTHROPIC_FUTURE_SETTING', 'OPENAI_FUTURE_SETTING'):
                    self.assertNotIn(key, os.environ)
                # The guard must reject a newly inherited prefixed variable too,
                # while allowing only the generated native HOME keys.
                for prefix in ('CODEX_', 'CLAUDE_', 'ANTHROPIC_', 'OPENAI_', 'XDG_'):
                    with mock.patch.dict(os.environ, {prefix + 'FUTURE_SETTING': 'fixture'}):
                        with self.assertRaisesRegex(AssertionError, 'inherited seat environment'):
                            assert_isolated(directory, 'codex')
                home = Path(os.environ['HOME'])
                (home / 'sentinel').write_text('fixture')
                self.assertFalse(Path(os.environ['SH_MCP_IPC_SOCK']).exists())
            self.assertFalse(home.exists())

    def test_native_configuration_child_reads_and_writes_only_case_paths(self):
        from sessionhelper.config import load_config
        keys = ('CODEX_HOME', 'CLAUDE_CONFIG_DIR', 'XDG_CONFIG_HOME',
                'XDG_DATA_HOME', 'XDG_STATE_HOME', 'XDG_CACHE_HOME', 'XDG_RUNTIME_DIR')
        home = Path(os.environ['HOME']).resolve()
        self.assertEqual(Path(load_config(dict(os.environ, SH_SESSION_NAME='configprobe',
                                             SH_OWNER='zzc', SH_KEY_ID='FB-2642',
                                             SH_SECRET='synthetic')).codex_home).resolve(),
                         Path(os.environ['CODEX_HOME']).resolve())
        for key in keys:
            self.assertIn(home, Path(os.environ[key]).resolve().parents)
        for key in ('CLAUDE_FOO', 'CODEX_BAR', 'ANTHROPIC_SYNTHETIC',
                    'OPENAI_SYNTHETIC', 'XDG_SYNTHETIC'):
            self.assertFalse(key in os.environ, 'inherited native variable: ' + key)
        self.assertEqual(Path(os.environ['XDG_RUNTIME_DIR']).stat().st_mode & 0o777, 0o700)
        # Real child configuration I/O through inherited native HOME paths.
        # It must never discover the caller's sentinel configuration directory.
        code = """
import json, os
from pathlib import Path
keys = json.loads(os.environ['ISOLATION_CONFIG_KEYS'])
for key in keys:
    directory = Path(os.environ[key])
    directory.mkdir(parents=True, exist_ok=True)
    config = directory / 'settings.json'
    config.write_text(json.dumps({'key': key, 'value': 'synthetic'}))
    assert json.loads(config.read_text()) == {'key': key, 'value': 'synthetic'}
print(json.dumps({'configurations_written_and_read': len(keys)}))
"""
        env = dict(os.environ, ISOLATION_CONFIG_KEYS=json.dumps(keys))
        result = subprocess.run([sys.executable, '-c', code], env=env, text=True,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True)
        self.assertEqual(json.loads(result.stdout)['configurations_written_and_read'], len(keys))
        for key in keys:
            self.assertTrue((Path(os.environ[key]) / 'settings.json').is_file())

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

        cancelled = []

        class AsyncProbe(unittest.IsolatedAsyncioTestCase):
            async def test_cleanup(inner):
                home = Path(os.environ['HOME'])

                async def pending():
                    try:
                        await asyncio.Future()
                    finally:
                        cancelled.append((Path(os.environ['HOME']) == home, home.exists()))

                asyncio.create_task(pending())
                await asyncio.sleep(0)

        with isolate_test_cases('claude'):
            unittest.TestSuite([AsyncProbe('test_cleanup')]).run(result)
        self.assertTrue(result.wasSuccessful(), result.errors + result.failures)
        self.assertEqual(cancelled, [(True, True)], 'async cleanup escaped the case HOME')
        self.assertEqual(dict(os.environ), before)

    def test_removed_isolation_fails_at_boundary_before_helper_execution(self):
        with isolated_environment('unset') as outer:
            with mock.patch.dict(os.environ, HOME=str(outer.parent / 'outside')):
                with self.assertRaisesRegex(AssertionError, 'isolation boundary: HOME'):
                    assert_isolated(outer)


if __name__ == '__main__':
    unittest.main()
