#!/usr/bin/env python3
"""Hosted Codex boot contract. Never print credential values."""
import os
import re
from pathlib import Path
import stat
import sys
import tempfile
import subprocess

REQUIRED = ('SH_WS_BASE', 'SH_ENROLL_TOKEN_FILE', 'SH_CRED_FILE', 'SH_CRED_KIND')

def fail(key):
    print('hosted-start: invalid or missing ' + key, file=sys.stderr)
    raise SystemExit(2)

def prepare(env):
    model = env.get('CODEX_MODEL', '')
    if model and (len(model) > 64 or re.fullmatch(r'[A-Za-z0-9._-]+', model) is None):
        fail('CODEX_MODEL')
    if 'SH_CLI' in env and env['SH_CLI'] != 'codex':
        fail('SH_CLI')
    for key in REQUIRED:
        if not env.get(key):
            fail(key)
    if env['SH_CRED_KIND'] not in ('api_key', 'token_file'):
        fail('SH_CRED_KIND')
    if os.getuid() != 10001:
        fail('USER')
    source = Path(env['SH_CRED_FILE'])
    try:
        info = source.lstat()
        if not stat.S_ISREG(info.st_mode) or info.st_uid != 10001 or stat.S_IMODE(info.st_mode) != 0o600:
            fail('SH_CRED_FILE')
        content = source.read_bytes()
        if not content or b'\0' in content:
            fail('SH_CRED_FILE')
        home = Path(env.get('CODEX_HOME', '/work/.codex'))
        home.mkdir(mode=0o700, parents=True, exist_ok=True)
        if home.is_symlink() or home.stat().st_uid != 10001:
            fail('CODEX_HOME')
        auth = home / 'auth.json'
        if env['SH_CRED_KIND'] == 'token_file':
            with tempfile.NamedTemporaryFile(dir=home, prefix='.auth-', delete=False) as out:
                temp = Path(out.name)
                out.write(content)
            os.replace(temp, auth)
        else:
            auth.unlink(missing_ok=True)
            login_env = dict(env, CODEX_HOME=str(home))
            login_env.pop('OPENAI_API_KEY', None)
            result = subprocess.run(['/opt/codex/codex', 'login', '--with-api-key'],
                                    input=content, env=login_env, stdout=subprocess.DEVNULL,
                                    stderr=subprocess.DEVNULL, timeout=60)
            if result.returncode != 0 or not auth.is_file():
                fail('SH_CRED_FILE')
        auth.chmod(0o600)
        if auth.stat().st_uid != 10001:
            fail('CODEX_HOME')
        config = 'check_for_update_on_startup = false\n'
        if model:
            config += 'model = "' + model + '"\n'
        config += '\n[projects."/work"]\ntrust_level = "trusted"\n'
        (home / 'config.toml').write_text(config)
        (home / 'config.toml').chmod(0o600)
    except (OSError, ValueError, UnicodeError, subprocess.TimeoutExpired):
        fail('SH_CRED_FILE')
    env.pop('OPENAI_API_KEY', None)
    env.update(CODEX_HOME=str(home), SH_ENROLL='1', SH_STATE_DIR='/work/.dingwei',
               SH_MODE=env.get('SH_MODE', 'cli'), SH_CLI='codex', SH_CLI_CMD='/usr/local/bin/codex',
               SH_CLI_CWD='/work', SH_COLLECT='0', SH_INBOX_DB='/work/inbox.sqlite3')
    return env

if __name__ == '__main__':
    os.umask(0o077)
    env = prepare(dict(os.environ))
    os.execvpe('python', ['python', '-m', 'sessionhelper'], env)
