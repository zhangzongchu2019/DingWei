#!/usr/bin/env python3
"""Do not expose token_file as an environment variable to the Claude child."""
import os
import sys

if os.environ.get('SH_CRED_KIND') != 'token_file':
    print('hosted-claude: invalid SH_CRED_KIND', file=sys.stderr)
    raise SystemExit(2)
os.execve('/opt/claude/claude', ['claude'] + sys.argv[1:], dict(os.environ))
