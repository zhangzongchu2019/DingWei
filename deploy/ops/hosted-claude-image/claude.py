#!/usr/bin/env python3
"""Do not expose token_file as an environment variable to the Claude child."""
import os
import sys

if os.environ.get('SH_CRED_KIND') != 'token_file':
    print('hosted-claude: invalid SH_CRED_KIND', file=sys.stderr)
    raise SystemExit(2)
os.execve('/opt/node/bin/node', ['node', '/opt/claude/node_modules/@anthropic-ai/claude-code/bin/claude.exe'] + sys.argv[1:], dict(os.environ))
