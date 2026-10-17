"""机械检查：凡构造 SessionHelper / Provisioner 的测试模块，必须接 HOME 隔离助手，或在白名单里写明理由。

为什么：SessionHelper() 构造会往 ~/.dingwei 装 send.py（覆盖真席位的发信脚本）、写 inbox/audit 库，
Provisioner 的升级状态文件留在真实 HOME 里还会让下一轮测试进程 os._exit(0) 无声退出。
隔离是"按模块接入"的（tests/_home_isolation.py），新增模块若忘了接，没有任何东西会提醒——这条检查补上。

"接了助手" = 模块级有 `X = HomeIsolation()`，且 setUpModule 的函数体里**无条件、顶层**有一句 X.start()、
tearDownModule 里同样有一句 X.finish()（放在 if/try 里不算——"纸面接线"不算接线）；并且该模块**不得在
import 时（模块级/类体）构造**——那会早于 setUpModule 的 HOME 重定向，照样泄漏。
识别构造的写法（AST）：直接 SessionHelper(...)、属性链 app.SessionHelper(...)、别名导入
`from … import SessionHelper as SH; SH(...)`、变量别名 `CLS = SessionHelper; CLS(...)`、
子类 `class Fake(SessionHelper)` 的实例化、getattr(app, "SessionHelper")(...)。
局限：经由别的测试模块里的构造函数间接构造的扫不到；蓄意规避（exec/eval/动态 import 等）不防。
"""
import ast
import unittest
from pathlib import Path

TESTS = Path(__file__).resolve().parent
CONSTRUCTED = {"SessionHelper", "Provisioner"}

# 不接助手但允许构造的模块：文件名 → (理由, 必须仍然存在于该文件里的证据串)。
# 证据串用来防止"理由写了、实际卫生格已被删"：它不在了，本检查就红。
ALLOWED = {
    "hosted_body_hub_client.py": (
        "Go hosted body-send child: parent uses only PATH/PYTHONPATH plus HOME=t.TempDir; child asserts before helper construction",
        'assert Path.home().resolve() == home.resolve(), "integration child requires its synthetic HOME"',
    ),
    "claim_hub_integration_client.py": (
        "Go claim integration child: parent strips ambient seat variables and sets HOME=t.TempDir=state; child asserts before any enrollment/helper construction",
        'assert Path.home().resolve() == state.resolve(), "integration child requires its synthetic HOME"',
    ),
    "desktop_hub_roundtrip_client.py": (
        "Go desktop roundtrip child: parent strips ambient seat variables and sets HOME=t.TempDir=state; child asserts before helper construction",
        'assert Path.home().resolve() == state.resolve(), "integration child requires its synthetic HOME"',
    ),
    "test_hub_notice_no_inject.py": (
        "自带隔离：构造点在 mock.patch.dict(HOME=临时目录, SH_PROVISION_NO_EXIT=1) 内，配置路径全指向临时目录，"
        "并有 test_03c 在跑完后断言真实 HOME 下无 provision 残留",
        "test_03c_helper_does_not_write_into_the_real_home",
    ),
}


def _bound_names(tree: ast.AST) -> set:
    """收集指向 SessionHelper/Provisioner 的名字：别名导入、变量别名、子类（求不动点）。"""
    names = set(CONSTRUCTED)
    for node in ast.walk(tree):
        if isinstance(node, ast.ImportFrom):
            for al in node.names:
                if al.name in CONSTRUCTED:
                    names.add(al.asname or al.name)
    changed = True
    while changed:
        changed = False
        for node in ast.walk(tree):
            targets, value = [], None
            if isinstance(node, ast.Assign):
                targets, value = [t for t in node.targets if isinstance(t, ast.Name)], node.value
            elif isinstance(node, ast.AnnAssign) and isinstance(node.target, ast.Name) and node.value is not None:
                targets, value = [node.target], node.value
            if value is not None and _refers_to(value, names):
                for t in targets:
                    if t.id not in names:
                        names.add(t.id)
                        changed = True
            if isinstance(node, ast.ClassDef) and node.name not in names \
                    and any(_refers_to(b, names) for b in node.bases):
                names.add(node.name)
                changed = True
    return names


def _refers_to(node: ast.AST, names: set) -> bool:
    """表达式是不是"指向被关注的类"：Name、属性链末端、getattr(x, "SessionHelper")。"""
    if isinstance(node, ast.Name):
        return node.id in names
    if isinstance(node, ast.Attribute):
        return node.attr in CONSTRUCTED or node.attr in names
    if isinstance(node, ast.Call) and isinstance(node.func, ast.Name) and node.func.id == "getattr" \
            and len(node.args) >= 2 and isinstance(node.args[1], ast.Constant):
        return node.args[1].value in CONSTRUCTED
    return False


def constructions(tree: ast.AST) -> list:
    """返回 [(行号, 名字, 是否在 import 时执行)]；"import 时执行"＝不在任何函数/lambda 体内（模块级或类体）。"""
    names = _bound_names(tree)
    out = []

    def walk(node, in_func):
        for child in ast.iter_child_nodes(node):
            func = in_func or isinstance(child, (ast.FunctionDef, ast.AsyncFunctionDef, ast.Lambda))
            if isinstance(child, ast.Call) and _refers_to(child.func, names):
                f = child.func
                nm = f.id if isinstance(f, ast.Name) else f.attr if isinstance(f, ast.Attribute) else "getattr(...)"
                out.append((child.lineno, nm, not in_func))
            walk(child, func)

    walk(tree, False)
    return sorted(out)


def _top_level_call(func: ast.FunctionDef, var: str, method: str) -> bool:
    """函数体的**顶层语句**里有一句 var.method()（放在 if/try/with 里不算——防"纸面接线"）。"""
    for stmt in func.body:
        if isinstance(stmt, ast.Expr) and isinstance(stmt.value, ast.Call) and isinstance(stmt.value.func, ast.Attribute) \
                and stmt.value.func.attr == method and isinstance(stmt.value.func.value, ast.Name) \
                and stmt.value.func.value.id == var:
            return True
    return False


def wired_to_isolation(tree: ast.Module) -> bool:
    """模块级：X = HomeIsolation()；setUpModule 顶层调 X.start()；tearDownModule 顶层调 X.finish()。"""
    holders = {t.id for n in tree.body if isinstance(n, ast.Assign) and isinstance(n.value, ast.Call)
               and isinstance(n.value.func, ast.Name) and n.value.func.id == "HomeIsolation"
               for t in n.targets if isinstance(t, ast.Name)}
    funcs = {n.name: n for n in tree.body if isinstance(n, ast.FunctionDef)}
    up, down = funcs.get("setUpModule"), funcs.get("tearDownModule")
    return bool(up and down and any(_top_level_call(up, h, "start") and _top_level_call(down, h, "finish")
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
            out.append(f"{filename}: 构造了 {sorted({n for _, n, _ in built})}（行 {[l for l, _, _ in built][:3]}…）"
                       "却没接 HomeIsolation（setUpModule 顶层调 start、tearDownModule 顶层调 finish），也不在白名单")
        else:
            at_import = [(l, n) for l, n, imp in built if imp]
            if at_import:
                out.append(f"{filename}: 已接 HomeIsolation，但在 import 时就构造了 {at_import[:3]}"
                           "——早于 setUpModule 的 HOME 重定向，照样泄漏；请挪进测试方法/setUp 里")
    elif filename in allowed:
        out.append(f"{filename}: 在白名单里但已不再构造 SessionHelper/Provisioner，请移出白名单")
    return out


class HomeIsolationWiringTest(unittest.TestCase):
    def test_go_children_refuse_a_home_outside_bootstrap_state(self):
        import asyncio, importlib.util, io, json, os, tempfile
        from unittest.mock import patch
        for name in ('claim_hub_integration_client.py', 'desktop_hub_roundtrip_client.py'):
            spec = importlib.util.spec_from_file_location('isolated_child_' + name[:-3], TESTS / name)
            module = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(module)
            with tempfile.TemporaryDirectory() as home, tempfile.TemporaryDirectory() as state:
                request = json.dumps({'state': state}) + '\n'
                with patch.dict(os.environ, HOME=home), patch('sys.stdin', io.StringIO(request)), patch.object(module, 'SessionHelper', side_effect=AssertionError('must not construct helper')) as helper:
                    with self.assertRaisesRegex(AssertionError, 'requires its synthetic HOME'):
                        if name.startswith('desktop'):
                            asyncio.run(module.main())
                        else:
                            module.main()
                    helper.assert_not_called()


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

    def test_checker_reverse_controls_for_evasions(self):
        # 反向对照（来自 R1/R3 之外的复验反馈）：别名/变量/getattr/子类 都要抓；
        # "接了但 import 时就构造""纸面接线" 也要抓。
        up = "def setUpModule():\n    _H.start()\n\ndef tearDownModule():\n    _H.finish()\n\n"
        head = "from _home_isolation import HomeIsolation\n_H = HomeIsolation()\n\n"
        wired = head + up
        imp = "from sessionhelper.app import SessionHelper as SH\n"
        evasions = {
            "① 别名导入": imp + "def t():\n    return SH(None)\n",
            "② 变量别名": "from sessionhelper.app import SessionHelper\nCLS = SessionHelper\ndef t():\n    return CLS(None)\n",
            "③ getattr": "import sessionhelper.app as app\ndef t():\n    return getattr(app, 'SessionHelper')(None)\n",
            "④ 子类": "from sessionhelper.app import SessionHelper\nclass Fake(SessionHelper):\n    pass\ndef t():\n    return Fake(None)\n",
            "④b 子类的别名链": "from sessionhelper.app import SessionHelper as S\nclass Fake(S):\n    pass\nF2 = Fake\ndef t():\n    return F2(None)\n",
            "②b 带类型注解的别名": "from sessionhelper.app import SessionHelper\nCLS: type = SessionHelper\ndef t():\n    return CLS(None)\n",
        }
        for label, src in evasions.items():
            self.assertEqual(len(problems_for("x.py", src)), 1, label + " 未被抓到")
            # 接上隔离后（构造在函数里）都应放行
            self.assertEqual(problems_for("x.py", wired + src), [], label + " 接线后不应报")
        # ⑤ 接了助手，但 import 时就构造（模块级、类体内）⇒ 红；放在函数/lambda 里 ⇒ 过
        mod_level = wired + "from sessionhelper.app import SessionHelper\nH = SessionHelper(None)\n"
        cls_level = wired + "from sessionhelper.app import SessionHelper\nclass T:\n    H = SessionHelper(None)\n"
        in_lambda = wired + "from sessionhelper.app import SessionHelper\nmk = lambda: SessionHelper(None)\n"
        in_func = wired + "from sessionhelper.app import SessionHelper\ndef mk():\n    return SessionHelper(None)\n"
        self.assertEqual(len(problems_for("x.py", mod_level)), 1)
        self.assertIn("import 时", problems_for("x.py", mod_level)[0])
        self.assertEqual(len(problems_for("x.py", cls_level)), 1)
        self.assertEqual(problems_for("x.py", in_lambda), [])
        self.assertEqual(problems_for("x.py", in_func), [])
        # ⑥ 纸面接线：start/finish 放在 if/try 里、或根本不在函数顶层 ⇒ 红
        body = "from sessionhelper.app import SessionHelper\ndef t():\n    return SessionHelper(None)\n"
        paper_if = head + "def setUpModule():\n    if False:\n        _H.start()\n\ndef tearDownModule():\n    _H.finish()\n\n" + body
        paper_if_down = head + "def setUpModule():\n    _H.start()\n\ndef tearDownModule():\n    if False:\n        _H.finish()\n\n" + body
        paper_try = head + "def setUpModule():\n    try:\n        _H.start()\n    except Exception:\n        pass\n\ndef tearDownModule():\n    _H.finish()\n\n" + body
        for label, src in (("start 在 if False 里", paper_if), ("finish 在 if False 里", paper_if_down), ("start 在 try 里", paper_try)):
            self.assertEqual(len(problems_for("x.py", src)), 1, label + " 未被抓到")
        # 阴性：不相关的同名变量、字符串里提到 SessionHelper、不构造 ⇒ 不误报
        self.assertEqual(problems_for("x.py", "x = 'SessionHelper(None)'\ndef t():\n    return 1\n"), [])
        self.assertEqual(problems_for("x.py", "from sessionhelper.app import SessionHelper\ndef t():\n    return SessionHelper.is_delivery\n"), [])
        self.assertEqual(problems_for("x.py", "from sessionhelper.app import SessionHelper as SH\ndef t():\n    return SH.__name__\n"), [])


if __name__ == "__main__":
    unittest.main()
