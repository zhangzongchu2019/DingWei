"""Isolation shared by discovery, module fixtures and individual test cases."""
from contextlib import contextmanager
import os
from pathlib import Path
import tempfile
from unittest import mock

PREFIXES = ('SH_', 'DW_', 'DINGWEI_', 'DATAD_')


def clean_environment(environment):
    return {k: v for k, v in environment.items() if not k.startswith(PREFIXES)}


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
        if driver != 'unset':
            environment['SH_DRIVER'] = driver
        with mock.patch.dict(os.environ, environment, clear=True):
            yield Path(directory)


def assert_isolated(directory, driver='unset'):
    """Fail before importing or executing helper code if isolation is removed."""
    root = Path(directory).resolve()
    for key in ('HOME', 'SH_MCP_IPC_SOCK', 'DINGWEI_MCP_IPC_SOCK'):
        path = Path(os.environ.get(key, '/')).resolve()
        assert path != root and root in path.parents, f'isolation boundary: {key}'
    allowed = {'SH_MCP_IPC_SOCK', 'DINGWEI_MCP_IPC_SOCK', 'SH_PROVISION_NO_EXIT'}
    if driver != 'unset':
        allowed.add('SH_DRIVER')
    assert not {k for k in os.environ if k.startswith(PREFIXES)} - allowed, 'isolation boundary: inherited seat environment'


@contextmanager
def isolate_test_cases(driver):
    import unittest
    original = unittest.TestCase.run

    def run(case, result=None):
        with isolated_environment(driver) as directory:
            assert_isolated(directory, driver)
            return original(case, result)

    with mock.patch.object(unittest.TestCase, 'run', run):
        yield
