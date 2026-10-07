"""镜像 r4 start.py 离线用例（R-V2-IMAGE-R4-IMPL；方案 runbooks/PLAN-R-V2-ACCOUNT-PER-USER-opus50tc2k.md 第十五节 15.3）。

断言面＝跑完 prepare() 后 CODEX_HOME 里的 auth.json／标记／config.toml 与退出码、stdout／stderr；
实现承诺＝start.py 的 need_rewrite 三条判据、先 auth.json 后标记、config.toml 每次写、凭据校验不变。
每格在子进程里跑 prepare()（与生产同一入口，失败的 exit 2 与打印都在 fd 层捕获）；SEAT_UID 换成当前 uid、
CODEX_BIN 换成登录桩；不进镜像（Dockerfile 只 COPY start.py）。属主 10001 只在 106 真容器格验。
"""
import hashlib
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
MARKER = '.dw-applied-credential.sha256'

RUNNER = r'''
import json, os, sys
sys.path.insert(0, sys.argv[1])
import start
start.SEAT_UID = os.getuid()
if sys.argv[2]:
    start.CODEX_BIN = sys.argv[2]
if sys.argv[3] == 'fail-auth-write':
    orig = start._write_atomic
    def failing(home, name, data, prefix):
        if name == 'auth.json':
            raise OSError('injected auth.json write failure')
        return orig(home, name, data, prefix)
    start._write_atomic = failing
start.prepare(json.loads(sys.stdin.read()))
'''

# 登录桩：把收到的 stdin 原样回显到 stdout／stderr（若 start.py 不屏蔽就会漏出），计数，再写 auth.json。
LOGIN_STUB = r'''#!/bin/sh
input=$(cat)
echo "$input"; echo "$input" >&2
n=$(cat "$CODEX_HOME/.stub-calls" 2>/dev/null || echo 0)
echo $((n+1)) > "$CODEX_HOME/.stub-calls"
[ -n "$STUB_FAIL" ] && exit 1
printf '{"OPENAI_API_KEY":"%s"}' "$input" > "$CODEX_HOME/auth.json"
'''

TOKEN = b'{"tokens":{"access_token":"SECRET-TOKEN-AAA","refresh_token":"SECRET-REFRESH-AAA"}}'
TOKEN_NEW = b'{"tokens":{"access_token":"SECRET-TOKEN-BBB","refresh_token":"SECRET-REFRESH-BBB"}}'
RENEWED = b'{"tokens":{"access_token":"RENEWED-IN-SEAT","refresh_token":"RENEWED-REFRESH"}}'
APIKEY = b'sk-SECRET-LEAK-123'


def sha(data):
    return hashlib.sha256(data).hexdigest()


class StartR4Test(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        root = Path(self.tmp.name)
        self.work = root / 'work'
        self.home = self.work / '.codex'
        self.home.mkdir(parents=True, mode=0o700)
        self.cred = self.work / 'account-credential'
        self.stub = root / 'codex-stub'
        self.stub.write_text(LOGIN_STUB)
        self.stub.chmod(0o755)
        self.auth = self.home / 'auth.json'
        self.marker = self.home / MARKER

    def tearDown(self):
        for p in self.home.rglob('*'):
            try:
                p.chmod(0o700)
            except OSError:
                pass
        self.tmp.cleanup()

    def put_cred(self, data, mtime_ns=None):
        self.cred.write_bytes(data)
        self.cred.chmod(0o600)
        if mtime_ns is not None:
            os.utime(self.cred, ns=(mtime_ns, mtime_ns))

    def put_auth(self, data, mtime_ns=None):
        self.auth.write_bytes(data)
        self.auth.chmod(0o600)
        if mtime_ns is not None:
            os.utime(self.auth, ns=(mtime_ns, mtime_ns))

    def run_prepare(self, kind='token_file', model='', hook='', extra_env=None):
        env = {'SH_WS_BASE': 'wss://hub.invalid', 'SH_ENROLL_TOKEN_FILE': str(self.work / 'enroll-token.json'),
               'SH_CRED_FILE': str(self.cred), 'SH_CRED_KIND': kind, 'CODEX_HOME': str(self.home)}
        if model:
            env['CODEX_MODEL'] = model
        env.update(extra_env or {})  # 进 prepare 的 env（登录桩收到的就是它派生的 env）
        return subprocess.run([sys.executable, '-c', RUNNER, str(HERE), str(self.stub), hook],
                              input=json.dumps(env), capture_output=True, text=True, timeout=60)

    def assert_ok(self, res):
        self.assertEqual(res.returncode, 0, res.stderr)

    def marker_text(self):
        return self.marker.read_text().strip()

    # M1：无 auth.json ⇒ 写 auth.json、写标记、写 config.toml
    def test_m1_no_auth_writes_everything(self):
        self.put_cred(TOKEN)
        self.assert_ok(self.run_prepare())
        self.assertEqual(self.auth.read_bytes(), TOKEN)
        self.assertEqual(self.marker_text(), sha(TOKEN))
        self.assertIn('trust_level = "trusted"', (self.home / 'config.toml').read_text())

    # M2（核心）：标记＝当前摘要、auth.json 已被续期改写 ⇒ auth.json 不变
    def test_m2_marker_matches_keeps_renewed_login(self):
        self.put_cred(TOKEN, 1_000_000_000_000)
        self.put_auth(RENEWED, 2_000_000_000_000)
        self.marker.write_text(sha(TOKEN) + '\n')
        self.assert_ok(self.run_prepare())
        self.assertEqual(self.auth.read_bytes(), RENEWED)
        self.assertEqual(self.marker_text(), sha(TOKEN))

    # M3：标记≠当前摘要 ⇒ 重写、更新标记
    def test_m3_marker_differs_rewrites(self):
        self.put_cred(TOKEN_NEW, 1_000_000_000_000)
        self.put_auth(RENEWED, 2_000_000_000_000)
        self.marker.write_text(sha(TOKEN) + '\n')
        self.assert_ok(self.run_prepare())
        self.assertEqual(self.auth.read_bytes(), TOKEN_NEW)
        self.assertEqual(self.marker_text(), sha(TOKEN_NEW))

    # M4：无标记、凭据件 mtime 早于 auth.json（存量首升）⇒ 不写、补标记
    def test_m4_no_marker_older_cred_keeps_auth(self):
        self.put_cred(TOKEN, 1_000_000_000_000)
        self.put_auth(RENEWED, 2_000_000_000_000)
        self.assert_ok(self.run_prepare())
        self.assertEqual(self.auth.read_bytes(), RENEWED)
        self.assertEqual(self.marker_text(), sha(TOKEN))

    # M5：无标记、凭据件 mtime 晚于 auth.json ⇒ 写、补标记
    def test_m5_no_marker_newer_cred_rewrites(self):
        self.put_cred(TOKEN, 3_000_000_000_000)
        self.put_auth(RENEWED, 2_000_000_000_000)
        self.assert_ok(self.run_prepare())
        self.assertEqual(self.auth.read_bytes(), TOKEN)
        self.assertEqual(self.marker_text(), sha(TOKEN))

    # M6：api_key——需要时删 auth.json 并调登录一次；不需要时零次、auth.json 不动
    def test_m6_api_key_logs_in_only_when_needed(self):
        self.put_cred(APIKEY)
        self.assert_ok(self.run_prepare(kind='api_key'))
        self.assertEqual((self.home / '.stub-calls').read_text().strip(), '1')
        first = self.auth.read_bytes()
        self.assert_ok(self.run_prepare(kind='api_key'))
        self.assertEqual((self.home / '.stub-calls').read_text().strip(), '1', 'no second login')
        self.assertEqual(self.auth.read_bytes(), first)

    # M7：标记 0600；凭据件不合规 ⇒ exit 2（属主 10001 只在 106 验）
    def test_m7_marker_mode_and_bad_credential(self):
        self.put_cred(TOKEN)
        self.assert_ok(self.run_prepare())
        self.assertEqual(self.marker.stat().st_mode & 0o777, 0o600)
        self.assertEqual(self.marker.stat().st_uid, os.getuid())
        self.cred.chmod(0o644)
        res = self.run_prepare()
        self.assertEqual(res.returncode, 2)
        self.assertIn('invalid or missing SH_CRED_FILE', res.stderr)

    # M8：同 sha 刷新——标记已被节点删、凭据件内容相同但 mtime 较新 ⇒ 重写
    def test_m8_same_sha_refresh_rewrites(self):
        self.put_cred(TOKEN, 3_000_000_000_000)
        self.put_auth(RENEWED, 2_000_000_000_000)
        self.assert_ok(self.run_prepare())
        self.assertEqual(self.auth.read_bytes(), TOKEN)

    # M9a／M9b：auth.json 为空／半截 JSON ⇒ 重写（即使标记一致）
    def test_m9_empty_or_broken_auth_rewrites(self):
        for broken in (b'', b'{"tokens":{"access_'):
            with self.subTest(broken=broken):
                self.put_cred(TOKEN, 1_000_000_000_000)
                self.put_auth(broken, 2_000_000_000_000)
                self.marker.write_text(sha(TOKEN) + '\n')
                self.assert_ok(self.run_prepare())
                self.assertEqual(self.auth.read_bytes(), TOKEN)

    # M10：auth.json 不重写时 config.toml 仍按当前 model 重写
    def test_m10_config_follows_current_model(self):
        self.put_cred(TOKEN, 1_000_000_000_000)
        self.put_auth(RENEWED, 2_000_000_000_000)
        self.marker.write_text(sha(TOKEN) + '\n')
        (self.home / 'config.toml').write_text('model = "old-model"\n')
        self.assert_ok(self.run_prepare(model='gpt-5.5'))
        self.assertEqual(self.auth.read_bytes(), RENEWED)
        cfg = (self.home / 'config.toml').read_text()
        self.assertIn('model = "gpt-5.5"', cfg)
        self.assertNotIn('old-model', cfg)

    # M11：写 auth.json 失败（标记在其后）⇒ 本次 exit 2、标记不更新 ⇒ 下次启动重写
    def test_m11_failure_before_marker_rewrites_next_time(self):
        self.put_cred(TOKEN_NEW, 1_000_000_000_000)
        self.put_auth(TOKEN, 2_000_000_000_000)
        self.marker.write_text(sha(TOKEN) + '\n')
        res = self.run_prepare(hook='fail-auth-write')
        self.assertEqual(res.returncode, 2)
        self.assertEqual(self.marker_text(), sha(TOKEN), 'marker must not claim the new credential')
        self.assert_ok(self.run_prepare())
        self.assertEqual(self.auth.read_bytes(), TOKEN_NEW)
        self.assertEqual(self.marker_text(), sha(TOKEN_NEW))

    # M12：标记为符号链接 ⇒ 当不存在、不跟随读取（链接指向的文件写着当前摘要，若跟随就会误判「一致」）
    def test_m12_symlink_marker_is_ignored(self):
        self.put_cred(TOKEN, 3_000_000_000_000)
        self.put_auth(RENEWED, 2_000_000_000_000)
        target = self.work / 'elsewhere'
        target.write_text(sha(TOKEN) + '\n')
        self.marker.symlink_to(target)
        self.assert_ok(self.run_prepare())
        self.assertEqual(self.auth.read_bytes(), TOKEN, 'treated as no marker ⇒ mtime rule ⇒ rewrite')
        self.assertFalse(self.marker.is_symlink())
        self.assertEqual(target.read_text().strip(), sha(TOKEN), 'link target untouched')

    # M12b–d（复核补）：标记为空目录／不可读／内容非 64 位十六进制 ⇒ 都当不存在。
    # 凭据件较旧：「当不存在」⇒ mtime 规则 ⇒ 保住 auth.json；若被当成「标记存在且不一致」就会误重写——两者在这里分得开。
    def test_m12_other_bad_markers_are_ignored(self):
        def as_dir():
            self.marker.mkdir()

        def unreadable():
            self.marker.write_text(sha(TOKEN) + '\n')
            self.marker.chmod(0o000)

        def not_hex():
            self.marker.write_text('not-a-sha256\n')
        for name, make in (('dir', as_dir), ('unreadable', unreadable), ('not-hex', not_hex)):
            with self.subTest(case=name):
                if self.marker.is_dir() and not self.marker.is_symlink():
                    self.marker.rmdir()
                elif self.marker.exists():
                    self.marker.chmod(0o600)
                    self.marker.unlink()
                self.put_cred(TOKEN, 1_000_000_000_000)
                self.put_auth(RENEWED, 2_000_000_000_000)
                make()
                self.assert_ok(self.run_prepare())
                self.assertEqual(self.auth.read_bytes(), RENEWED, name)
                self.assertTrue(self.marker.is_file())
                self.assertEqual(self.marker_text(), sha(TOKEN))

    # M13：无标记、st_mtime_ns 相等 ⇒ 不写、补标记
    def test_m13_equal_mtime_does_not_rewrite(self):
        self.put_cred(TOKEN, 2_000_000_000_000)
        self.put_auth(RENEWED, 2_000_000_000_000)
        self.assert_ok(self.run_prepare())
        self.assertEqual(self.auth.read_bytes(), RENEWED)
        self.assertEqual(self.marker_text(), sha(TOKEN))

    # 复核补 1：成功与失败路径下 stdout／stderr 都不含凭据片段（含 api_key 登录桩收到的输入）；标记只有摘要
    def test_no_credential_leaks_on_any_path(self):
        secrets = ('SECRET', 'sk-')
        self.put_cred(APIKEY)
        outputs = [self.run_prepare(kind='api_key')]
        self.auth.unlink()
        outputs.append(self.run_prepare(kind='api_key', extra_env={'STUB_FAIL': '1'}))
        self.assertEqual(outputs[1].returncode, 2)
        self.put_cred(TOKEN)
        self.marker.unlink(missing_ok=True)
        outputs.append(self.run_prepare())
        self.cred.chmod(0o644)
        outputs.append(self.run_prepare())
        for res in outputs:
            for s in secrets:
                self.assertNotIn(s, res.stdout + res.stderr)
        self.cred.chmod(0o600)
        self.assert_ok(self.run_prepare())
        self.assertRegex(self.marker.read_text(), r'^[0-9a-f]{64}\n$')
        self.assertNotIn('SECRET', (self.home / 'config.toml').read_text())


if __name__ == '__main__':
    unittest.main()
