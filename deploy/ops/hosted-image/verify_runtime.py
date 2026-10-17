"""Build-stage metadata verification; no helper, credentials or network."""
import importlib.metadata
from pathlib import Path
import re
import sys


def verify(lock):
    pins = dict(re.findall(r'^([A-Za-z0-9_-]+)==([^\s\\]+)', Path(lock).read_text(), re.M))
    actual = {d.metadata['Name'].lower().replace('_', '-'): d.version for d in importlib.metadata.distributions()}
    if actual != pins:
        raise SystemExit('runtime distributions differ from lock')


if __name__ == '__main__':
    verify(sys.argv[1])
