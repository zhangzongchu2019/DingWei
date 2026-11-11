#!/usr/bin/env python3
"""从门禁 JSON 核验 S-HOSTED-TEST-TIME 四条判据。"""
import json
from pathlib import Path


def main():
    raw = Path(__file__).resolve().parent / 'raw'

    def read(name):
        return json.loads((raw / (name + '.json')).read_text())

    def hosted(result):
        return next(v for k, v in result['packages'].items()
                    if k.endswith('/internal/hosted'))

    before_single, before_all, after, repeat = map(
        read, ['before-hosted', 'before-all', 'after-all', 'repeat3'])
    assert all(r['rc'] == 0 for r in [before_single, before_all, after, repeat])
    assert not any(arg.startswith('-timeout') for arg in after['cmd'])
    assert after['cmd'][after['cmd'].index('-p') + 1] == '2'
    assert hosted(after)['elapsed'][0] <= 300
    names = {x['Test'] for x in before_single['hosted_top']}
    assert len(names) == len(before_single['hosted_top']) == 185
    for result in [before_all, after]:
        assert len(result['hosted_top']) == 185
        assert {x['Test'] for x in result['hosted_top']} == names
    for result in [before_single, before_all, after, repeat]:
        assert hosted(result)['counts']['fail'] == 0
        assert hosted(result)['counts']['skip'] == 0
    assert len(repeat['hosted_top']) == 555
    assert '-count=3' in repeat['cmd']
    rounds = read('rounds')['rounds']
    assert len(rounds) == 3
    assert all(r['top_pass'] == 185 and set(r['names']) == names for r in rounds)
    before_skips = sorted((x['package'], x['test']) for x in before_all['skips'])
    after_skips = sorted((x['package'], x['test']) for x in after['skips'])
    assert before_skips == after_skips
    assert read('build-tests')['rc'] == read('diff-check')['rc'] == 0
    assert read('body-audit')['restored_bodies'] == 185
    assert hosted(before_all)['elapsed'][0] > 300  # 原串行调度使性能门转红。
    print(json.dumps({'a': '通过', 'hosted_seconds': hosted(after)['elapsed'][0],
                      'b': '通过', 'round_seconds': [r['seconds'] for r in rounds],
                      'c': '通过', 'top_before_after': [185, 185], 'hosted_skip': 0,
                      'd': '通过', 'serial_control_over_budget': True},
                     ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
