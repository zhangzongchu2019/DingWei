#!/usr/bin/env python3
"""Hosted Claude boot contract.  Credential values are never printed."""
import importlib.util
import json
import os
from pathlib import Path
import stat
import sys
import tempfile

SEAT_UID = 10001
VENV_PYTHON = '/opt/dingwei/sessionhelper-venv/bin/python'
HELPER_ROOT = '/opt/dingwei/sessionhelper-src'
CLAUDE_BIN = '/usr/local/bin/claude'
SHARED_START = '/opt/dingwei/codex_start.py'
REQUIRED = ('SH_WS_BASE', 'SH_ENROLL_TOKEN_FILE', 'SH_CRED_FILE', 'SH_CRED_KIND', 'SH_CLI')


def fail(key):
    print('hosted-claude-start: invalid or missing ' + key, file=sys.stderr)
    raise SystemExit(2)


def _shared_guard():
    spec = importlib.util.spec_from_file_location('hosted_codex_start', SHARED_START)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    mod.check_factory_only_state()


def _credential(path):
    try:
        info = path.lstat()
        if not stat.S_ISREG(info.st_mode) or info.st_uid != SEAT_UID or (info.st_mode & 0o077) or info.st_size == 0:
            raise ValueError()
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
        with os.fdopen(fd, 'rb') as f:
            raw = f.read(64 << 10 | 1)
        if len(raw) > 64 << 10 or not isinstance(json.loads(raw), dict):
            raise ValueError()
        return raw
    except (OSError, ValueError, UnicodeError, json.JSONDecodeError):
        fail('SH_CRED_FILE')


def prepare(env):
    _shared_guard()
    if any(not env.get(key) for key in REQUIRED) or env['SH_CLI'] != 'claude' or env['SH_CRED_KIND'] != 'token_file':
        fail('SH_CLI')
    if os.getuid() != SEAT_UID:
        fail('UID')
    raw = _credential(Path(env['SH_CRED_FILE']))
    home = Path(env.get('HOME', '/work'))
    try:
        home.mkdir(mode=0o700, parents=True, exist_ok=True)
        if home.stat().st_uid != SEAT_UID:
            raise ValueError()
        with tempfile.NamedTemporaryFile(dir=home, prefix='.claude.', delete=False) as out:
            out.write(raw); tmp = Path(out.name)
        tmp.chmod(0o600)
        os.replace(tmp, home / '.claude.json')
        mcp_dir = home / '.claude'
        mcp_dir.mkdir(mode=0o700, exist_ok=True)
        mcp = {
            'mcpServers': {
                'dingwei-send': {
                    'command': VENV_PYTHON,
                    'args': ['-B', '-m', 'sessionhelper.mcp_shim'],
                    'env': {
                        'PYTHONPATH': HELPER_ROOT,
                        'SH_STATE_DIR': '/work/.dingwei',
                        'SH_IPC_SEND_BODY_ONLY': '1',
                        'DINGWEI_MCP_IPC_SOCK': '/work/.dingwei/ipc/helper.sock',
                    },
                },
            },
        }
        mcp_tmp = mcp_dir / '.mcp.json.tmp'
        mcp_tmp.write_text(json.dumps(mcp, separators=(',', ':')))
        mcp_tmp.chmod(0o600)
        os.replace(mcp_tmp, mcp_dir / 'mcp.json')
    except (OSError, ValueError):
        fail('CLAUDE_HOME')
    env.update(HOME=str(home), SH_MODE=env.get('SH_MODE', 'cli'), SH_CLI='claude',
               SH_CLI_CMD=CLAUDE_BIN, SH_CLI_CWD='/work', SH_COLLECT='0',
               SH_INBOX_DB='/work/inbox.sqlite3', SH_STATE_DIR='/work/.dingwei',
               CLAUDE_CONFIG_DIR=str(home / '.claude'),
               SH_IPC_UPLOAD_ONLY='0', SH_IPC_SEND_BODY_ONLY='1',
               PYTHONPATH=HELPER_ROOT, PYTHONDONTWRITEBYTECODE='1',
               PATH=str(Path(VENV_PYTHON).parent) + ':' + env.get('PATH', os.defpath))
    return env


if __name__ == '__main__':
    os.umask(0o077)
    os.execve(VENV_PYTHON, [VENV_PYTHON, '-B', '-m', 'sessionhelper'], prepare(dict(os.environ)))
