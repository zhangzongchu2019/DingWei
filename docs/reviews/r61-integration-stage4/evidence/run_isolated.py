from pathlib import Path
import sys,unittest
R=Path('/data/ai-dev-data/r61-integration');sys.path[:0]=[str(R/'tools/sessionhelper/tests'),str(R/'tools/sessionhelper'),str(R/'view-service')]
from _suite_isolation import isolated_environment,isolate_test_cases,assert_isolated
with isolated_environment('codex')as td:
 assert_isolated(td,'codex')
 suite=unittest.defaultTestLoader.loadTestsFromNames(sys.argv[1:])
 with isolate_test_cases('codex'):r=unittest.TextTestRunner(verbosity=2).run(suite)
sys.exit(0 if r.wasSuccessful()else 1)
