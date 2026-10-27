"""Pin actual Popen argv, including production-shaped explicit Claude commands."""
import io
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock, patch
from types import SimpleNamespace

from sessionhelper.claudeDriver import ClaudeProtocolDriver, claude_command
from sessionhelper.driver_adapter import DriverAdapter


class ClaudeLaunchContractTests(unittest.TestCase):
    def launch(self, command):
        with tempfile.TemporaryDirectory() as root:
            driver = ClaudeProtocolDriver(cmd=command, store_path=str(Path(root)/'ledger.db'))
            process = Mock(stdout=io.StringIO(''), stdin=io.StringIO())
            process.poll.return_value = None
            with patch('sessionhelper.claudeDriver.subprocess.Popen', return_value=process) as popen:
                driver.start()
            self.assertEqual(popen.call_count, 1)
            return popen.call_args.args[0]

    def protocol(self, argv):
        for flag in ['--include-partial-messages','--replay-user-messages','--verbose']:
            self.assertEqual(argv.count(flag), 1, 'Claude实际argv必须补齐且仅一次 '+flag)
        for flag in ['--input-format','--output-format']:
            self.assertEqual(argv.count(flag), 1, 'Claude实际argv必须补齐流式格式 '+flag)
            self.assertEqual(argv[argv.index(flag)+1], 'stream-json')

    def test_explicit_missing_flag_reaches_actual_popen_once(self):
        command=['/fixture/claude','-p','--input-format','stream-json','--output-format','stream-json']
        self.protocol(self.launch(command))
        self.assertNotIn('--include-partial-messages',command)

    def test_explicit_existing_and_duplicate_flags_collapse(self):
        command=['claude','-p','--include-partial-messages','--include-partial-messages','--verbose','--verbose',
                 '--input-format=stream-json','--output-format','stream-json','--replay-user-messages']
        self.protocol(self.launch(command))
        self.protocol(self.launch(['claude','--include-partial-messages']))

    def test_production_env_claude_exe_command(self):
        command=['env','IS_SANDBOX=1','/fixture/claude.exe','--model','opus','--dangerously-skip-permissions']
        actual=self.launch(command)
        self.assertEqual(actual[:len(command)],command)
        self.protocol(actual)
        self.assertIn('-p',actual)

    def test_binary_and_command_sources_without_env_edits(self):
        for key in ['SH_CLI_BIN','SH_CLAUDE_BIN']:
            with self.subTest(key=key), patch.dict(os.environ,{key:'/fixture/claude.exe'},clear=True):
                actual=self.launch(None)
                self.assertEqual(actual[0],'/fixture/claude.exe');self.protocol(actual)
        with patch.dict(os.environ,{'SH_CLI_CMD':'env IS_SANDBOX=1 /fixture/claude.exe --model opus'},clear=True):
            self.protocol(self.launch(None))

    def test_driver_adapter_honors_configured_explicit_command(self):
        with tempfile.TemporaryDirectory() as root:
            adapter=DriverAdapter.__new__(DriverAdapter)
            adapter.cfg=SimpleNamespace(state_dir=root,model='opus',cli_cmd=['env','IS_SANDBOX=1','/fixture/claude.exe'])
            driver=adapter._build_driver('claude')
            self.protocol(driver._cmd)
            self.assertEqual(driver._cmd[:3],adapter.cfg.cli_cmd)
            self.assertEqual(driver._cmd.count('--model'),1)

    def test_conflicting_non_stream_format_rejects_before_start(self):
        for value in ['text','json','']:
            command=['claude','--output-format',value]
            original=list(command)
            with patch('sessionhelper.claudeDriver.subprocess.Popen') as popen:
                with self.assertRaisesRegex(ValueError,'refusing non-stream command'):
                    self.launch(command)
                popen.assert_not_called()
            self.assertEqual(command,original)

    def test_non_claude_and_positional_data_are_not_rewritten(self):
        command=['codex','--output-format','text','--include-partial-messages']
        self.assertEqual(self.launch(command),command)
        with patch.dict(os.environ,{'SH_CLI_BIN':'codex'},clear=True):
            self.assertEqual(self.launch(command),command)
        with patch.dict(os.environ,{},clear=True):
            prompt=self.launch(['claude','--append-system-prompt','--verbose'])
            self.assertEqual(prompt[:3],['claude','--append-system-prompt','--verbose'])
            self.protocol(prompt[3:])
        actual=self.launch(['claude','--','--include-partial-messages'])
        options=actual[:actual.index('--')]
        self.protocol(options)
        self.assertEqual(actual[actual.index('--')+1:],['--include-partial-messages'])

    def test_repeated_normalization_is_idempotent(self):
        command=['claude','--model=opus']
        once=claude_command(command)
        self.assertEqual(claude_command(once),once)


if __name__=='__main__':unittest.main()
