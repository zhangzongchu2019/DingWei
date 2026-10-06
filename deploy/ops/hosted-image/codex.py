#!/usr/bin/env python3
"""Read API key only in the Codex child; helper never receives it."""
import os
from pathlib import Path
import sys

env = dict(os.environ)
kind = env.get('SH_CRED_KIND')
if kind == 'api_key':
    try:
        key = Path(env['SH_CRED_FILE']).read_text().strip()
        if not key or '\0' in key:
            raise ValueError()
        env['OPENAI_API_KEY'] = key
    except (OSError, KeyError, ValueError, UnicodeError):
        print('hosted-codex: invalid SH_CRED_FILE or CODEX_HOME', file=sys.stderr)
        raise SystemExit(2)
elif kind == 'token_file':
    env.pop('OPENAI_API_KEY', None)
elif kind is not None:
    print('hosted-codex: invalid SH_CRED_KIND', file=sys.stderr)
    raise SystemExit(2)
os.execve('/opt/codex/codex', ['codex'] + sys.argv[1:], env)
