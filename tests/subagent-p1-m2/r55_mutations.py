"""复用r55变异，替换已被混精度边界用例取代的旧selector；不改冻结runner。"""
import importlib.util
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
path = ROOT/'tests/helper-codex-delivery-reconcile/mutations.py'
spec = importlib.util.spec_from_file_location('frozen_r55_mutations', path)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
updated = []
for row in module.MUTATIONS:
    if row[0] == 'fractional-observation':
        row = ('fractional-observation', 'inbox_lifecycle.py',
               'if self._terminal_precedes_injected_ack(db, row, processed_at):', 'if False:',
               'test_native_seconds_preserve_fractional_acked_admission_boundary',
               'MIXED_PRECISION_MUST_REMAIN_PENDING')
    updated.append(row)
module.MUTATIONS = updated

if __name__ == '__main__': module.main()
