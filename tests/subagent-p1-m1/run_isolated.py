"""Run a review command with an isolated HOME and short temporary path.

No environment values are written to evidence. Source is exchanged through Git.
"""
import argparse
import datetime
import json
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--python-bin', type=Path)
    parser.add_argument('name')
    parser.add_argument('command', nargs=argparse.REMAINDER)
    args = parser.parse_args()
    command = args.command
    if command[:1] == ['--']:
        command = command[1:]
    if not command or Path(args.name).name != args.name:
        parser.error('name must be a basename and command is required')
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    home = output / args.name / 'home'
    home.mkdir(parents=True, exist_ok=True)
    original_home = Path.home()
    env = {k: v for k, v in os.environ.items()
           if not k.startswith(('SH_', 'DINGWEI_', 'DW_', 'DATAD_'))
           and k not in ('CODEX_HOME', 'CLAUDE_CONFIG_DIR')}
    env.update(HOME=str(home), TZ='UTC', OPENSPEC_TELEMETRY='0')
    env.setdefault('GOCACHE', str(original_home / '.cache/go-build'))
    env.setdefault('GOMODCACHE', str(original_home / 'go/pkg/mod'))
    if args.python_bin:
        if not args.python_bin.is_dir():
            parser.error('locked Python bin directory is unavailable')
        env['PATH'] = str(args.python_bin.resolve()) + os.pathsep + env['PATH']
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    start = datetime.datetime.now(datetime.timezone.utc).isoformat()
    with tempfile.TemporaryDirectory(prefix='sm1-', dir='/tmp') as directory:
        env['TMPDIR'] = directory
        with (output / (args.name + '.stdout')).open('w') as log:
            result = subprocess.run(command, cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT)
    evidence = dict(command=command, start=start, end=datetime.datetime.now(datetime.timezone.utc).isoformat(),
                    rc=result.returncode, uid=os.getuid(), source_commit=revision,
                    environment='isolated HOME; short TMPDIR; SH_/DINGWEI_/DW_/DATAD_ stripped; UTC')
    (output / (args.name + '.json')).write_text(json.dumps(evidence, ensure_ascii=False, indent=2) + '\n')
    print(json.dumps(evidence, ensure_ascii=False))
    return result.returncode


if __name__ == '__main__':
    raise SystemExit(main())
