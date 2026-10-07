#!/usr/bin/env python3
"""Hosted Codex boot contract. Never print credential values."""
import hashlib
import json
import os
import re
from pathlib import Path
import stat
import sys
import tempfile
import subprocess

REQUIRED = ('SH_WS_BASE', 'SH_ENROLL_TOKEN_FILE', 'SH_CRED_FILE', 'SH_CRED_KIND')

# r4（R-V2-IMAGE-R4-IMPL；方案 runbooks/PLAN-R-V2-ACCOUNT-PER-USER-opus50tc2k.md 第十五节）：
# 只在必要时重写 auth.json，免得每次重启都用池内旧副本覆盖席位里已续期的登录。
# 三个常量只为离线测试可替换，生产值不变、不新增 env 面。
SEAT_UID = 10001
CODEX_BIN = '/opt/codex/codex'
# 与节点 internal/nodeagent/seat.go appliedMarkerName 同名同值（Go 用例钉住）；节点刷新凭据时先删它再写凭据件。
MARKER_NAME = '.dw-applied-credential.sha256'
_HEX64 = re.compile(r'[0-9a-f]{64}')


def fail(key):
    print('hosted-start: invalid or missing ' + key, file=sys.stderr)
    raise SystemExit(2)


def _write_atomic(home, name, data, prefix):
    """同目录临时文件 → 0600 → os.replace（崩溃时要么旧件、要么新件）。"""
    with tempfile.NamedTemporaryFile(dir=home, prefix=prefix, delete=False) as out:
        temp = Path(out.name)
        out.write(data)
    temp.chmod(0o600)
    os.replace(temp, home / name)


def _auth_usable(auth):
    """auth.json 存在、是常规文件、非空且是合法 JSON；否则视为不存在（15.1 第 1 条）。"""
    try:
        info = auth.lstat()
    except FileNotFoundError:
        return None
    if not stat.S_ISREG(info.st_mode) or info.st_size == 0:
        return None
    try:
        json.loads(auth.read_bytes())
    except (ValueError, UnicodeError):
        return None
    return info


def _read_marker(home):
    """摘要标记按 lstat 判：须为常规文件、可读、内容恰为 64 位小写十六进制；
    符号链接、目录、不可读、内容不合规一律视为不存在，且不跟随链接读取（15.1 第 2 条）。"""
    path = home / MARKER_NAME
    try:
        if not stat.S_ISREG(path.lstat().st_mode):
            return None
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
        with os.fdopen(fd, 'rb') as f:
            data = f.read(129)
    except OSError:
        return None
    text = data.decode('ascii', 'replace').strip()
    return text if _HEX64.fullmatch(text) else None


def _write_marker(home, digest):
    """标记是尽力而为（复核必改）：auth.json 此时已就绪，写不成（含非空目录占位清不掉）只打一行不含凭据的提示、
    继续启动；下一次启动无标记 ⇒ 落第 3 条（凭据件不晚于 auth.json 就不重写），安全。"""
    marker_path = home / MARKER_NAME
    try:
        if not marker_path.is_symlink() and marker_path.is_dir():
            marker_path.rmdir()  # 空目录占位 ⇒ 清掉再写；非空 ⇒ OSError ⇒ 下面只提示
        _write_atomic(home, MARKER_NAME, (digest + '\n').encode('ascii'), '.dw-marker-')
    except OSError:
        print('hosted-start: marker not written', file=sys.stderr)


def need_rewrite(home, source_info, digest):
    """15.1：任一成立即重写——① auth.json 缺／空／非法 JSON；② 标记存在且≠当前凭据摘要；
    ③ 无标记且凭据件 st_mtime_ns 严格大于 auth.json（相等不重写）。"""
    auth_info = _auth_usable(home / 'auth.json')
    if auth_info is None:
        return True
    marker = _read_marker(home)
    if marker is not None:
        return marker != digest
    return source_info.st_mtime_ns > auth_info.st_mtime_ns


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
    if os.getuid() != SEAT_UID:
        fail('USER')
    source = Path(env['SH_CRED_FILE'])
    try:
        info = source.lstat()
        if not stat.S_ISREG(info.st_mode) or info.st_uid != SEAT_UID or stat.S_IMODE(info.st_mode) != 0o600:
            fail('SH_CRED_FILE')
        content = source.read_bytes()
        if not content or b'\0' in content:
            fail('SH_CRED_FILE')
        home = Path(env.get('CODEX_HOME', '/work/.codex'))
        home.mkdir(mode=0o700, parents=True, exist_ok=True)
        if home.is_symlink() or home.stat().st_uid != SEAT_UID:
            fail('CODEX_HOME')
        auth = home / 'auth.json'
        digest = hashlib.sha256(content).hexdigest()
        if need_rewrite(home, info, digest):
            if env['SH_CRED_KIND'] == 'token_file':
                _write_atomic(home, 'auth.json', content, '.auth-')
            else:
                auth.unlink(missing_ok=True)
                login_env = dict(env, CODEX_HOME=str(home))
                login_env.pop('OPENAI_API_KEY', None)
                result = subprocess.run([CODEX_BIN, 'login', '--with-api-key'],
                                        input=content, env=login_env, stdout=subprocess.DEVNULL,
                                        stderr=subprocess.DEVNULL, timeout=60)
                if result.returncode != 0 or not auth.is_file():
                    fail('SH_CRED_FILE')
        auth.chmod(0o600)
        if auth.stat().st_uid != SEAT_UID:
            fail('CODEX_HOME')
        # 🔴 顺序固定：auth.json（需要时）先写好，再原子写标记——反过来，崩溃会留下「标记说已应用、auth.json 却是旧的」。
        # 不论是否重写都写／更新标记；标记里只有摘要，无凭据值。
        _write_marker(home, digest)
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
