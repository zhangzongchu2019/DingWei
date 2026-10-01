"""R1 机械检查：sessionhelper/ 下直接读环境变量只许出现在统一入口与下面写死的白名单里。

统一入口 = config.py 的 env_get / load_config。其余文件要读环境变量，一律 ``from .config import env_get``。
白名单按"文件 → (允许的出现次数, 理由)"写死；次数必须**精确相等**——新增一处直接读取会红，
把白名单里的某处迁走而忘了改白名单也会红，逼着改动者同步改理由。
"""
import ast
import unittest
from pathlib import Path

PKG = Path(__file__).resolve().parents[1] / "sessionhelper"

ALLOWED = {
    "config.py": (3, "统一入口本身：env_get 的实现，以及 load_config / detect_full_session_name "
                     "的 `env or os.environ` 缺省（调用方可传入替代的 env 字典）"),
    "claudeDriver.py": (1, "dict(os.environ)：把整份环境透传给子进程，语义是继承而非读配置值"),
    "codexDriver.py": (1, "os.environ.copy()：同上，子进程环境透传"),
    "openCodeDriver.py": (1, "os.environ.copy()：同上，子进程环境透传"),
    "app.py": (3, "启动时清理代理变量（1 处 pop）及同批变量的诊断打印（1 行里 2 处读取）：改写/检视同一批进程环境，"
                  "不是读配置；硬迁到入口会改变语义"),
    "mcp_shim.py": (1, "自包含入口：无包内 import，可能被当脚本直接启动，加相对 import 会改启动语义"),
    "dingwei_file.py": (1, "自包含入口：同 mcp_shim"),
    "send_dingwei.py": (2, "被拷贝成 ~/.dingwei/send.py 单文件独立运行，无法 import 包"),
}


def env_reads(source: str) -> list:
    """返回源码里所有直接触碰 os.environ / os.getenv 的位置（含 `_os` 之类的别名与 from os import）。"""
    hits = []
    for node in ast.walk(ast.parse(source)):
        if isinstance(node, ast.Attribute) and node.attr in ("environ", "getenv", "environb"):
            # value 不限于 Name：`__import__("os").environ` 之类也要抓
            base = node.value.id if isinstance(node.value, ast.Name) else ast.dump(node.value)[:30]
            hits.append((node.lineno, f"{base}.{node.attr}"))
        elif isinstance(node, ast.ImportFrom) and node.module == "os":
            for alias in node.names:
                if alias.name in ("environ", "getenv", "environb"):
                    hits.append((node.lineno, f"from os import {alias.name}"))
    return sorted(hits)


class EnvReadSingleEntryTest(unittest.TestCase):
    def test_direct_env_reads_match_whitelist_exactly(self):
        problems = []
        for path in sorted(PKG.glob("*.py")):
            hits = env_reads(path.read_text(encoding="utf-8"))
            allowed = ALLOWED.get(path.name, (0, ""))[0]
            if len(hits) != allowed:
                problems.append(f"{path.name}: 直接读环境变量 {len(hits)} 处，白名单允许 {allowed} 处 "
                                f"{[h for h in hits]}")
        self.assertEqual(problems, [], "改走 `from .config import env_get`；确属白名单类型的，"
                                       "到本文件 ALLOWED 里写明理由")

    def test_whitelist_entries_all_have_reasons_and_exist(self):
        for name, (count, reason) in ALLOWED.items():
            self.assertTrue((PKG / name).is_file(), name)
            self.assertGreater(count, 0, name)
            self.assertTrue(reason.strip(), f"{name} 缺理由")

    def test_checker_flags_direct_reads_in_all_forms(self):
        # 反向对照：各种写法的直接读取都必须被抓到，入口调用不得误报。
        self.assertEqual(len(env_reads('import os\nx = os.environ.get("A")\n')), 1)
        self.assertEqual(len(env_reads('import os\nx = os.getenv("A")\n')), 1)
        self.assertEqual(len(env_reads('import os as _os\nx = _os.environ["A"]\n')), 1)
        self.assertEqual(len(env_reads('from os import environ\nx = environ.get("A")\n')), 1)
        self.assertEqual(len(env_reads('x = __import__("os").environ.get("A")\n')), 1)
        self.assertEqual(env_reads('from .config import env_get\nx = env_get("A")\n'), [])

    def test_a_new_direct_read_in_a_clean_file_is_flagged(self):
        # 反向对照（对真文件）：给一个本应零读取的文件追加一处直接读取，检查必须判红。
        src = (PKG / "inbox.py").read_text(encoding="utf-8") + '\nimport os\n_X = os.environ.get("SH_X")\n'
        self.assertEqual(len(env_reads(src)), 1)
        self.assertNotEqual(len(env_reads(src)), ALLOWED.get("inbox.py", (0, ""))[0])


if __name__ == "__main__":
    unittest.main()
