#!/usr/bin/env python3
"""从本轮门禁 JSON 核验 S-HOSTED-TEST-TIME 的 a～g。"""
import json
import gzip
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
    audit = read('body-audit')
    assert audit['restored_bodies'] == 185
    assert audit['scheduled_parallel'] == 183
    assert audit['serial_global_writers'] == [
        'TestReconcileSeatTimeoutDoesNotStallBatch',
        'TestUpgradeUnknownPlanNotSwallowed']
    race = read('race3')
    assert race['rc'] == 0 and '-race' in race['cmd'] and '-count=3' in race['cmd']
    assert len(race['hosted_top']) == 555
    assert hosted(race)['counts']['fail'] == hosted(race)['counts']['skip'] == 0
    race_rounds = read('race-rounds')['rounds']
    assert len(race_rounds) == 3
    assert all(r['top_pass'] == 185 and set(r['names']) == names for r in race_rounds)
    control, mutant = read('global-plans-control'), read('global-plans-mutant')
    assert control['rc'] == 0 and mutant['rc'] == 1
    assert '-race' in control['cmd'] and '-race' in mutant['cmd']
    assert control['cmd'] == mutant['cmd']
    assert control['reader_sha256'] == mutant['reader_sha256']
    mutant_log = gzip.decompress((raw / 'global-plans-mutant.log.gz').read_bytes()).decode()
    assert 'GLOBAL_PLANS_SERIALIZATION_WITNESS' in mutant_log
    assert '[build failed]' not in mutant_log
    assert any(r['Test'] == 'TestHostedPlansParallelWitness' and r['Action'] == 'fail'
               for r in mutant['test_results'])
    assert any(r['Test'] == 'TestUpgradeUnknownPlanNotSwallowed' and r['Action'] == 'pass'
               for r in mutant['test_results'])
    assert read('global-state-audit')['type_checked'] is True
    assert hosted(before_all)['elapsed'][0] > 300  # 原串行调度使性能门转红。
    print(json.dumps({'a': '通过', 'hosted_seconds': hosted(after)['elapsed'][0],
                      'b': '通过', 'round_seconds': [r['seconds'] for r in rounds],
                      'c': '通过', 'top_before_after': [185, 185], 'hosted_skip': 0,
                      'd': '通过', 'e': '通过',
                      'race_round_seconds': [r['seconds'] for r in race_rounds],
                      'f': '本轮 a～d 全量重跑通过',
                      'g': '串行控制绿；重新并行后读者红',
                      'serial_global_writers': audit['serial_global_writers'],
                      'serial_control_over_budget': True},
                     ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
