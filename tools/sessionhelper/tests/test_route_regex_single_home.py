"""R3 机械检查：路由正则／路由协议字面量只许出现在 routing.py（V9 验收"客户端路由正则命中 0"的前身）。

按 AST 扫 sessionhelper/*.py 的字符串常量（跳过各级 docstring），出现下面的"路由特征串"
而又不在 routing.py ⇒ 红。反向对照见 test_checker_*。
"""
import ast
import unittest
from pathlib import Path

PKG = Path(__file__).resolve().parents[1] / "sessionhelper"
HOME = "routing.py"
# 路由特征串：control 路由的标记，以及目标地址（#名 / @属主#名）的字符类写法。
SIGNATURES = ("DW_ROUTE_V1", "#[A-Za-z0-9_.-]+")


def route_literals(source: str) -> list:
    tree = ast.parse(source)
    docstrings = set()
    for node in ast.walk(tree):
        if isinstance(node, (ast.Module, ast.ClassDef, ast.FunctionDef, ast.AsyncFunctionDef)):
            body = node.body
            if body and isinstance(body[0], ast.Expr) and isinstance(body[0].value, ast.Constant) \
                    and isinstance(body[0].value.value, str):
                docstrings.add(id(body[0].value))
    hits = []
    for node in ast.walk(tree):
        if isinstance(node, ast.Constant) and isinstance(node.value, str) and id(node) not in docstrings:
            if any(sig in node.value for sig in SIGNATURES):
                hits.append((node.lineno, node.value[:40]))
    return sorted(hits)


class RouteRegexSingleHomeTest(unittest.TestCase):
    def test_route_literals_only_in_routing_module(self):
        offenders = []
        for path in sorted(PKG.glob("*.py")):
            if path.name == HOME:
                continue
            for lineno, text in route_literals(path.read_text(encoding="utf-8")):
                offenders.append(f"{path.name}:{lineno} {text!r}")
        self.assertEqual(offenders, [], "路由正则只许在 routing.py；改经 routing 接口调用")

    def test_routing_module_does_hold_them(self):
        # 防止检查"空转"：本家里必须真有这些字面量。
        hits = route_literals((PKG / HOME).read_text(encoding="utf-8"))
        self.assertGreaterEqual(len(hits), 3)

    def test_checker_flags_a_route_regex_anywhere_else(self):
        # 反向对照：各种写法的路由字面量都要被抓到；docstring 与无关字符串不误报。
        self.assertEqual(len(route_literals('import re\nR = re.compile(r"^(#[A-Za-z0-9_.-]+)\\\\s+(.+)$")\n')), 1)
        self.assertEqual(len(route_literals('M = "[[DW_ROUTE_V1"\n')), 1)
        self.assertEqual(route_literals('"""doc mentions DW_ROUTE_V1"""\nx = 1\n'), [])
        self.assertEqual(route_literals('def f():\n    """DW_ROUTE_V1"""\n    return "plain"\n'), [])


if __name__ == "__main__":
    unittest.main()
