"""机械检查：凡构造 SessionHelper / Provisioner 的测试模块，必须接 HOME 隔离助手，或在白名单里写明理由。

为什么：SessionHelper() 构造会往 ~/.dingwei 装 send.py（覆盖真席位的发信脚本）、写 inbox/audit 库，
Provisioner 的升级状态文件留在真实 HOME 里还会让下一轮测试进程 os._exit(0) 无声退出。
隔离是"按模块接入"的（tests/_home_isolation.py），新增模块若忘了接，没有任何东西会提醒——这条检查补上。

"接了助手" = 模块级有 `X = HomeIsolation()`，且 setUpModule 里调 X.start()、tearDownModule 里调 X.finish()。
检查按 AST 扫 tests/*.py 里对 SessionHelper(...) / Provisioner(...) 的直接调用（含 app.SessionHelper 这类属性写法）。
局限：经由别的测试模块里的构造函数间接构造的，扫不到。
"""
import ast
import unittest
from pathlib import Path

TESTS = Path(__file__).resolve().parent
CONSTRUCTED = {"SessionHelper", "Provisioner"}

# 不接助手但允许构造的模块：文件名 → (理由, 必须仍然存在于该文件里的证据串)。
# 证据串用来防止"理由写了、实际卫生格已被删"：它不在了，本检查就红。
ALLOWED = {
    "test_hub_notice_no_inject.py": (
        "自带隔离：构造点在 mock.patch.dict(HOME=临时目录, SH_PROVISION_NO_EXIT=1) 内，配置路径全指向临时目录，"
        "并有 test_03c 在跑完后断言真实 HOME 下无 provision 残留",
        "test_03c_helper_does_not_write_into_the_real_home",
    ),
}


def constructions(tree: ast.AST) -> list:
    out = []
    for node in ast.walk(tree):
        if isinstance(node, ast.Call):
            f = node.func
            name = f.id if isinstance(f, ast.Name) else f.attr if isinstance(f, ast.Attribute) else None
            if name in CONSTRUCTED:
                out.append((node.lineno, name))
    return sorted(out)


def _calls_method_on(func: ast.FunctionDef, var: str, method: str) -> bool:
    return any(isinstance(n, ast.Call) and isinstance(n.func, ast.Attribute) and n.func.attr == method
               and isinstance(n.func.value, ast.Name) and n.func.value.id == var
               for n in ast.walk(func))


def wired_to_isolation(tree: ast.Module) -> bool:
    """模块级：X = HomeIsolation()；setUpModule 调 X.start()；tearDownModule 调 X.finish()。"""
    holders = {t.id for n in tree.body if isinstance(n, ast.Assign) and isinstance(n.value, ast.Call)
               and isinstance(n.value.func, ast.Name) and n.value.func.id == "HomeIsolation"
               for t in n.targets if isinstance(t, ast.Name)}
    funcs = {n.name: n for n in tree.body if isinstance(n, ast.FunctionDef)}
    up, down = funcs.get("setUpModule"), funcs.get("tearDownModule")
    return bool(up and down and any(_calls_method_on(up, h, "start") and _calls_method_on(down, h, "finish")
                                    for h in holders))


def problems_for(filename: str, source: str, allowed=ALLOWED) -> list:
    tree = ast.parse(source)
    built = constructions(tree)
    out = []
    if built:
        if filename in allowed:
            if allowed[filename][1] not in source:
                out.append(f"{filename}: 白名单的证据串 {allowed[filename][1]!r} 已不在文件里，理由失效")
        elif not wired_to_isolation(tree):
            out.append(f"{filename}: 构造了 {sorted({n for _, n in built})}（行 {[l for l, _ in built][:3]}…）"
                       "却没接 HomeIsolation（setUpModule 调 start、tearDownModule 调 finish），也不在白名单")
    elif filename in allowed:
        out.append(f"{filename}: 在白名单里但已不再构造 SessionHelper/Provisioner，请移出白名单")
    return out


class HomeIsolationWiringTest(unittest.TestCase):
    def test_every_constructing_test_module_is_isolated_or_allowlisted(self):
        problems = []
        for path in sorted(TESTS.glob("*.py")):
            if path.name in ("_home_isolation.py", Path(__file__).name):
                continue
            problems += problems_for(path.name, path.read_text(encoding="utf-8"))
        # 白名单里的文件必须真实存在
        for name in ALLOWED:
            if not (TESTS / name).is_file():
                problems.append(f"{name}: 白名单里的文件不存在")
        self.assertEqual(problems, [], "新增构造点：接 tests/_home_isolation.py 的 HomeIsolation，"
                                       "或到本文件 ALLOWED 里写明理由与证据串")

    def test_the_scan_is_not_vacuous(self):
        # 防"空转"：真实仓里确有模块构造并接了助手（否则上面的检查什么都没扫到）。
        wired = [p.name for p in sorted(TESTS.glob("test_*.py"))
                 if constructions(ast.parse(p.read_text(encoding="utf-8")))
                 and wired_to_isolation(ast.parse(p.read_text(encoding="utf-8")))]
        self.assertIn("test_sessionhelper.py", wired)
        self.assertIn("test_unified_queue.py", wired)

    def test_checker_reverse_controls(self):
        helper_import = "from _home_isolation import HomeIsolation\n"
        body = "from sessionhelper.app import SessionHelper\n\ndef t():\n    return SessionHelper(None)\n"
        wired = (helper_import + "_H = HomeIsolation()\n\ndef setUpModule():\n    _H.start()\n\n"
                 "def tearDownModule():\n    _H.finish()\n\n" + body)
        no_wiring = body
        only_setup = helper_import + "_H = HomeIsolation()\n\ndef setUpModule():\n    _H.start()\n\n" + body
        no_finish = (helper_import + "_H = HomeIsolation()\n\ndef setUpModule():\n    _H.start()\n\n"
                     "def tearDownModule():\n    pass\n\n" + body)
        attr_form = "import sessionhelper.app as app\n\ndef t():\n    return app.SessionHelper(None)\n"
        prov = "from sessionhelper.provision import Provisioner\n\ndef t():\n    return Provisioner(None)\n"
        clean = "def t():\n    return 1\n"
        self.assertEqual(problems_for("x.py", wired), [])                       # 接了 ⇒ 过
        self.assertEqual(len(problems_for("x.py", no_wiring)), 1)               # 没接 ⇒ 红
        self.assertEqual(len(problems_for("x.py", only_setup)), 1)              # 只 start 不 finish ⇒ 红
        self.assertEqual(len(problems_for("x.py", no_finish)), 1)               # tearDown 不调 finish ⇒ 红
        self.assertEqual(len(problems_for("x.py", attr_form)), 1)               # app.SessionHelper(...) 也抓
        self.assertEqual(len(problems_for("x.py", prov)), 1)                    # Provisioner 也抓
        self.assertEqual(problems_for("x.py", clean), [])                       # 不构造 ⇒ 过
        ok = {"x.py": ("理由", "EVIDENCE")}
        self.assertEqual(problems_for("x.py", no_wiring + "# EVIDENCE\n", ok), [])          # 白名单＋证据在 ⇒ 过
        self.assertEqual(len(problems_for("x.py", no_wiring, ok)), 1)                        # 证据没了 ⇒ 红
        self.assertEqual(len(problems_for("x.py", clean, ok)), 1)                            # 过期白名单 ⇒ 红


if __name__ == "__main__":
    unittest.main()
