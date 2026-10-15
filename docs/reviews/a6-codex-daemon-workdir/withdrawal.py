"""Run from repo root: replace only the binding method with exact r11 parent."""
import ast
import subprocess
import sys
import unittest

sys.path.insert(0, 'tools/sessionhelper')
import sessionhelper.driver_adapter as module

source = subprocess.check_output([
    'git', 'show',
    'b0f449bd7b2883808c3bce322ede635d56aa6a83:tools/sessionhelper/sessionhelper/driver_adapter.py',
], text=True)
cls = next(node for node in ast.parse(source).body
           if isinstance(node, ast.ClassDef) and node.name == 'DriverAdapter')
method = next(node for node in cls.body
              if isinstance(node, ast.FunctionDef) and node.name == 'agent_metrics_workdir')
exec(compile(ast.Module(body=[method], type_ignores=[]), '<exact-parent-binding>', 'exec'), module.__dict__)
module.DriverAdapter.agent_metrics_workdir = module.agent_metrics_workdir
suite = unittest.defaultTestLoader.discover(
    'tools/sessionhelper/tests', pattern='test_codex_daemon_disk_workdir.py')
result = unittest.TextTestRunner(verbosity=2).run(suite)
sys.exit(0 if result.wasSuccessful() else 1)
