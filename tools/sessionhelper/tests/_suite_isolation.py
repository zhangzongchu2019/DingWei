"""Isolation shared by discovery, module fixtures and individual test cases."""
from contextlib import contextmanager
from contextvars import ContextVar
import os
from pathlib import Path
import tempfile
from unittest import mock

PREFIXES = ('SH_', 'DW_', 'DINGWEI_', 'DATAD_', 'CODEX_', 'CLAUDE_', 'ANTHROPIC_', 'OPENAI_', 'XDG_')
HOME_KEYS = ('CODEX_HOME', 'CLAUDE_CONFIG_DIR', 'XDG_CONFIG_HOME',
             'XDG_STATE_HOME', 'XDG_CACHE_HOME', 'XDG_DATA_HOME', 'XDG_RUNTIME_DIR',
             'PYTHONUSERBASE')


def clean_environment(environment):
    return {k: v for k, v in environment.items()
            if not k.startswith(PREFIXES) and k not in HOME_KEYS}


@contextmanager
def isolated_environment(driver='unset'):
    # Short Unix socket paths also work on platforms with a 104-byte limit.
    with tempfile.TemporaryDirectory(prefix='ht-', dir='/tmp') as directory:
        home = Path(directory) / 'home'
        home.mkdir()
        sock = str(Path(directory) / 'ipc.sock')
        environment = clean_environment(os.environ)
        environment.update(HOME=str(home), SH_MCP_IPC_SOCK=sock,
                           DINGWEI_MCP_IPC_SOCK=sock, SH_PROVISION_NO_EXIT='1')
        environment.update({key: str(home / key.lower()) for key in HOME_KEYS})
        Path(environment['XDG_RUNTIME_DIR']).mkdir(mode=0o700)
        if driver != 'unset':
            environment['SH_DRIVER'] = driver
        with mock.patch.dict(os.environ, environment, clear=True):
            yield Path(directory)


def assert_isolated(directory, driver='unset'):
    """Fail before importing or executing helper code if isolation is removed."""
    root = Path(directory).resolve()
    for key in ('HOME', 'SH_MCP_IPC_SOCK', 'DINGWEI_MCP_IPC_SOCK') + HOME_KEYS:
        path = Path(os.environ.get(key, '/')).resolve()
        assert path != root and root in path.parents, f'isolation boundary: {key}'
    allowed = {'SH_MCP_IPC_SOCK', 'DINGWEI_MCP_IPC_SOCK', 'SH_PROVISION_NO_EXIT'}
    # These native paths are generated above and checked to remain under root;
    # no other inherited value sharing their prefix may survive.
    allowed.update(HOME_KEYS)
    if driver != 'unset':
        allowed.add('SH_DRIVER')
    assert not {k for k in os.environ if k.startswith(PREFIXES)} - allowed, 'isolation boundary: inherited seat environment'


@contextmanager
def isolate_test_cases(driver):
    import unittest
    current = ContextVar('isolated_test_case', default=None)

    def wrap(original):
        def run(case, result=None):
            # The async implementation calls the base implementation. Keep
            # one environment around both, including event-loop shutdown.
            if current.get() is case:
                return original(case, result)
            with isolated_environment(driver) as directory:
                assert_isolated(directory, driver)
                token = current.set(case)
                try:
                    return original(case, result)
                finally:
                    current.reset(token)
        return run

    with mock.patch.object(unittest.TestCase, 'run', wrap(unittest.TestCase.run)), \
            mock.patch.object(unittest.IsolatedAsyncioTestCase, 'run',
                              wrap(unittest.IsolatedAsyncioTestCase.run)):
        yield
