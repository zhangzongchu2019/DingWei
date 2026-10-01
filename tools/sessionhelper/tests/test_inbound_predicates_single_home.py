"""R3-B 机械检查：收件分类的 16 个判定，定义只许在 routing.py；别处同名只能是"委托"。

按 AST 扫 sessionhelper/*.py（routing.py 除外）里与这 16 个判定同名的 def：函数体必须只有一条
`return <routing 里的函数>(...)`（SessionHelper 的实例方法保留旧名以兼容，内部委托）；
把判定逻辑重新内联回去 ⇒ 红。
"""
import ast
import unittest
from pathlib import Path

PKG = Path(__file__).resolve().parents[1] / "sessionhelper"
NAMES = {
    "truthy_meta", "is_broadcast_envelope", "is_no_mirror_envelope", "is_hub_notice_envelope",
    "is_online_directory_text", "is_client_onboarding_text", "is_mirror_control", "is_provision_envelope",
    "is_delivery", "is_delivery_cancel", "is_agent_network_skill", "is_terminal_input", "is_terminal_interrupt",
    "is_terminal_resize", "is_terminal_refresh", "is_file_capability_response",
}


def non_delegating(source: str, filename: str) -> list:
    out = []
    for node in ast.walk(ast.parse(source)):
        if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)) and node.name in NAMES:
            body = list(node.body)
            if body and isinstance(body[0], ast.Expr) and isinstance(body[0].value, ast.Constant):
                body = body[1:]  # 跳过 docstring
            ok = (len(body) == 1 and isinstance(body[0], ast.Return) and isinstance(body[0].value, ast.Call)
                  and isinstance(body[0].value.func, ast.Name))
            if not ok:
                out.append(f"{filename}:{node.lineno} {node.name}")
    return sorted(out)


class InboundPredicatesSingleHomeTest(unittest.TestCase):
    def test_no_inline_predicates_outside_routing(self):
        offenders = []
        for path in sorted(PKG.glob("*.py")):
            if path.name != "routing.py":
                offenders += non_delegating(path.read_text(encoding="utf-8"), path.name)
        self.assertEqual(offenders, [], "判定只许定义在 routing.py；别处只能委托")

    def test_routing_defines_all_module_level_predicates(self):
        defined = {n.name for n in ast.parse((PKG / "routing.py").read_text(encoding="utf-8")).body
                   if isinstance(n, ast.FunctionDef)}
        module_level = {"truthy_meta", "is_broadcast_envelope", "is_no_mirror_envelope", "is_hub_notice_envelope",
                        "is_online_directory_text", "is_client_onboarding_text", "is_mirror_control", "is_provision_envelope"}
        instance_level = {n + "_envelope" for n in NAMES - module_level}
        self.assertEqual((module_level | instance_level) - defined, set())

    def test_checker_flags_inlined_logic_and_accepts_delegation(self):
        # 反向对照
        inlined = 'class S:\n    def is_delivery(self, env):\n        return env.get("meta", {}).get("type") == "delivery"\n'
        delegating = 'class S:\n    def is_delivery(self, env):\n        return is_delivery_envelope(env)\n'
        module_inlined = 'def is_hub_notice_envelope(env):\n    return True\n'
        self.assertEqual(non_delegating(inlined, "x.py"), ["x.py:2 is_delivery"])
        self.assertEqual(non_delegating(delegating, "x.py"), [])
        self.assertEqual(non_delegating(module_inlined, "x.py"), ["x.py:1 is_hub_notice_envelope"])


if __name__ == "__main__":
    unittest.main()
