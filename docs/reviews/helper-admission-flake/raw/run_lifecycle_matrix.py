"""Repeat the complete lifecycle test module under helper gate profile modes."""
import datetime, json, os, subprocess, time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[4]
OUT = Path(__file__).resolve().parents[1] / 'lifecycle-samples'
OUT.mkdir(parents=True, exist_ok=False)
PYTHON = '/tmp/lockvenv/bin/python3'
PROFILES = ('claude', 'codex')


def child(profile, label):
    home = f'/tmp/helper-admission-lifecycle-{label}-{profile}'
    Path(home).mkdir(parents=True, exist_ok=True)
    env = {'HOME': home, 'TMPDIR': '/tmp', 'PATH': '/tmp/lockvenv/bin:/usr/bin:/bin',
           'PYTHONDONTWRITEBYTECODE': '1'}
    return [PYTHON, 'scripts/test-helper-suite.py', '--driver', profile,
            '--pattern', 'test_dispatch_lifecycle.py'], env


def summarize(mode, iteration, profile, started, tick, before, proc, out):
    log = OUT / f'{mode}-{iteration:02d}-{profile}.log'
    log.write_bytes(out)
    lines = out.decode('utf-8', 'replace').splitlines()
    failures = [line for line in lines if line.startswith(('FAIL:', 'ERROR:', 'AssertionError:'))]
    duration = time.monotonic() - tick
    rec = {'mode': mode, 'iteration': iteration, 'profile': profile,
           'started_utc': started, 'loadavg_before': before,
           'loadavg_after': os.getloadavg(), 'elapsed_seconds': round(duration, 3),
           'rc': proc.returncode, 'failure_lines': failures, 'log': log.name}
    print(json.dumps(rec), flush=True)
    return rec


def run_profile(mode, iteration, profile):
    cmd, env = child(profile, f'{mode}-{iteration:02d}')
    started = datetime.datetime.now(datetime.timezone.utc).isoformat()
    tick, before = time.monotonic(), os.getloadavg()
    proc = subprocess.run(cmd, cwd=ROOT, env=env, stdout=subprocess.PIPE,
                          stderr=subprocess.STDOUT, timeout=150)
    return summarize(mode, iteration, profile, started, tick, before, proc, proc.stdout)


def main():
    records = []
    for i in range(1, 31):
        procs, meta = {}, {}
        for profile in PROFILES:
            cmd, env = child(profile, f'parallel-{i:02d}')
            started = datetime.datetime.now(datetime.timezone.utc).isoformat()
            meta[profile] = (started, time.monotonic(), os.getloadavg())
            procs[profile] = subprocess.Popen(cmd, cwd=ROOT, env=env,
                stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        for profile in PROFILES:
            out, _ = procs[profile].communicate(timeout=150)
            started, tick, before = meta[profile]
            records.append(summarize('parallel', i, profile, started, tick,
                                     before, procs[profile], out))
            (OUT / 'progress.json').write_text(json.dumps(records, indent=2) + '\n')
    for i in range(1, 31):
        for profile in PROFILES:
            records.append(run_profile('serial', i, profile))
            (OUT / 'progress.json').write_text(json.dumps(records, indent=2) + '\n')
    result = {'base': '9818b979e9e20f0bcfeffa6b78045dda71b1bd79',
              'command': 'scripts/test-helper-suite.py --driver PROFILE --pattern test_dispatch_lifecycle.py',
              'parallel_pairs': 30, 'serial_each_profile': 30, 'records': records,
              'failed_by_mode_profile': {
                f'{mode}/{profile}': [r['iteration'] for r in records
                    if r['mode'] == mode and r['profile'] == profile and r['rc']]
                for mode in ('parallel', 'serial') for profile in PROFILES}}
    (OUT / 'RESULTS.json').write_text(json.dumps(result, indent=2) + '\n')


if __name__ == '__main__':
    main()
