#!/usr/bin/env python3
"""核对原测试主体、顶层数量及源码改动范围，失败即返回非零。"""
import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base', default='329eb9f9c45d2d785a0429b01eaa0fa5264fe15d')
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[3]
    signature = r'func (Test\w+)\(t \*testing.T\) \{\n'
    restored, files, before, after, serial = 0, [], [], [], []
    for path in sorted((root / 'internal/hosted').glob('*_test.go')):
        current = path.read_text()
        after.extend(re.findall(signature, current))
        if path.name == 'parallel_test.go':
            continue
        relative = str(path.relative_to(root))
        original = subprocess.check_output(
            ['git', 'show', args.base + ':' + relative], cwd=root, text=True)
        before.extend(re.findall(signature, original))
        current, count = re.subn(
            r'(func Test\w+\(t \*testing.T\) \{\n)\tparallelHostedTest\(t\)\n',
            r'\1', current)
        restored += count
        for name in re.findall(signature, path.read_text()):
            if not re.search(r'func ' + name + r'\(t \*testing.T\) \{\n\tparallelHostedTest\(t\)', path.read_text()):
                serial.append(name)
        if path.name == 'store_test.go':
            current = re.sub(
                r'\t// Cold-cache toolBinary.*?\treturn pgtest.Pool\(t\)\n',
                '\treturn pgtest.Pool(t)\n', current, flags=re.S)
        assert current == original, relative + ' 原主体发生变化'
        files.append({'file': relative, 'scheduled': count,
                      'original_sha256': hashlib.sha256(original.encode()).hexdigest()})
    assert restored == 183
    assert len(before) == len(after) == 185
    assert sorted(serial) == ['TestReconcileSeatTimeoutDoesNotStallBatch', 'TestUpgradeUnknownPlanNotSwallowed']
    assert sorted(before) == sorted(after)
    changed = subprocess.check_output(
        ['git', 'diff', '--name-only', args.base], cwd=root, text=True).splitlines()
    assert all(p.startswith('docs/reviews/hosted-test-time/') or
               (p.startswith('internal/hosted/') and p.endswith('_test.go'))
               for p in changed), changed
    print(json.dumps({'base': args.base, 'top_level_before': len(before),
                      'top_level_after': len(after), 'restored_bodies': len(before),
                      'scheduled_parallel': restored, 'serial_global_writers': sorted(serial),
                      'original_files_unchanged_after_restoration': True,
                      'products_changed': False, 'files': files},
                     ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
