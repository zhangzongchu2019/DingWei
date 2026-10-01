"""机械检查：`if __name__ == "__main__":` 守卫之后不许再定义用例/类。

守卫写在文件中间时，直接运行该文件，unittest.main() 在守卫处就跑完并退出，
其后定义的用例被无声漏掉，退出码仍是 0。
"""
import ast
import unittest
from pathlib import Path

TESTS = Path(__file__).resolve().parent


def _is_main_guard(node: ast.stmt) -> bool:
    if not isinstance(node, ast.If) or not isinstance(node.test, ast.Compare):
        return False
    t = node.test
    return (isinstance(t.left, ast.Name) and t.left.id == "__name__"
            and len(t.comparators) == 1
            and isinstance(t.comparators[0], ast.Constant)
            and t.comparators[0].value == "__main__")


def offenders_in(source: str, name: str) -> list:
    body = ast.parse(source).body
    guards = [i for i, n in enumerate(body) if _is_main_guard(n)]
    if not guards:
        return []
    return [f"{name}:{n.lineno} {n.name}" for n in body[guards[0] + 1:]
            if isinstance(n, (ast.ClassDef, ast.FunctionDef, ast.AsyncFunctionDef))]


class MainGuardIsLastTest(unittest.TestCase):
    def test_no_definitions_after_main_guard(self):
        offenders = []
        for path in sorted(TESTS.glob("*.py")):
            offenders += offenders_in(path.read_text(encoding="utf-8"), path.name)
        self.assertEqual(offenders, [])

    def test_checker_detects_a_misplaced_guard(self):
        # 反向对照：守卫后有类定义必须被判为违规，守卫在末尾必须放行。
        bad = 'if __name__ == "__main__":\n    pass\n\n\nclass T: pass\n'
        good = 'class T: pass\n\n\nif __name__ == "__main__":\n    pass\n'
        self.assertEqual(offenders_in(bad, "x.py"), ["x.py:5 T"])
        self.assertEqual(offenders_in(good, "x.py"), [])


if __name__ == "__main__":
    unittest.main()
