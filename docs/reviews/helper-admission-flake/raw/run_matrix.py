"""Sample target admission and AA010 cases with parallel/serial profiles."""
import datetime
import json
import os
from pathlib import Path
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[4]
HERE = Path(__file__).resolve().parent
OUT = HERE.parent / 'samples'
OUT.mkdir(parents=True, exist_ok=False)
PYTHON = '/tmp/lockvenv/bin/python3'
PROFILES = ('claude', 'codex')


def env_for(profile, label):
    home = f'/tmp/helper-admission-{label}-{profile}'
    Path(home).mkdir(parents=True, exist_ok=True)
    return {
        'HOME': home,
        'TMPDIR': '/tmp',
        'PATH': '/tmp/lockvenv/bin:/usr/bin:/bin',
        'PYTHONPATH': os.pathsep.join((str(ROOT / 'tools/sessionhelper'),
                                       str(ROOT / 'tools/sessionhelper/tests'),
                                       str(ROOT / 'view-service'))),
        'PYTHONDONTWRITEBYTECODE': '1',
    }


def run(profile, mode, iteration):
    label = f'{mode}-{iteration:02d}'
    log = OUT / f'{label}-{profile}.log'
    started = datetime.datetime.now(datetime.timezone.utc).isoformat()
    before = os.getloadavg()
    tick = time.monotonic()
    proc = subprocess.run([PYTHON, str(HERE / 'sample_targets.py'), '--profile', profile],
                          cwd=ROOT, env=env_for(profile, label),
                          stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=90)
    output = proc.stdout
    log.write_bytes(output)
    elapsed = time.monotonic() - tick
    failure_lines = [line for line in output.decode('utf-8', 'replace').splitlines()
                     if line.startswith(('FAIL:', 'ERROR:', 'AssertionError:'))]
    result = {
        'mode': mode, 'iteration': iteration, 'profile': profile,
        'started_utc': started, 'loadavg_before': before,
        'loadavg_after': os.getloadavg(), 'elapsed_seconds': round(elapsed, 3),
        'rc': proc.returncode, 'failure_lines': failure_lines,
        'log': str(log.relative_to(OUT.parent)),
    }
    print(json.dumps(result), flush=True)
    return result


def main():
    records = []
    for i in range(1, 31):
        starts = {}
        procs = {}
        for profile in PROFILES:
            label = f'parallel-{i:02d}'
            log = OUT / f'parallel-{i:02d}-{profile}.log'
            started = datetime.datetime.now(datetime.timezone.utc).isoformat()
            starts[profile] = (time.monotonic(), started, os.getloadavg(), log)
            procs[profile] = subprocess.Popen(
                [PYTHON, str(HERE / 'sample_targets.py'), '--profile', profile],
                cwd=ROOT, env=env_for(profile, label), stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT)
        for profile in PROFILES:
            proc = procs[profile]
            output, _ = proc.communicate(timeout=90)
            log = starts[profile][3]
            log.write_bytes(output)
            tick, started, before, _ = starts[profile]
            failure_lines = [line for line in output.decode('utf-8', 'replace').splitlines()
                             if line.startswith(('FAIL:', 'ERROR:', 'AssertionError:'))]
            result = {'mode': 'parallel', 'iteration': i, 'profile': profile,
                      'started_utc': started, 'loadavg_before': before,
                      'loadavg_after': os.getloadavg(),
                      'elapsed_seconds': round(time.monotonic() - tick, 3),
                      'rc': proc.returncode, 'failure_lines': failure_lines,
                      'log': str(log.relative_to(OUT.parent))}
            records.append(result)
            print(json.dumps(result), flush=True)
        (OUT / 'progress.json').write_text(json.dumps(records, indent=2) + '\n')
    for i in range(1, 31):
        for profile in PROFILES:
            records.append(run(profile, 'serial', i))
            (OUT / 'progress.json').write_text(json.dumps(records, indent=2) + '\n')
    summary = {
        'base': '9818b979e9e20f0bcfeffa6b78045dda71b1bd79',
        'target_cases': 4,
        'parallel_pairs_requested': 30,
        'parallel_profiles_each': 30,
        'serial_profiles_each': 30,
        'records': records,
        'failed_by_mode_profile': {
            f'{mode}/{profile}': [r['iteration'] for r in records
                                  if r['mode'] == mode and r['profile'] == profile and r['rc']]
            for mode in ('parallel', 'serial') for profile in PROFILES
        },
    }
    (OUT / 'RESULTS.json').write_text(json.dumps(summary, indent=2) + '\n')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
