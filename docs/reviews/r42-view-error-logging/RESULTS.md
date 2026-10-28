# Developer validation

Runtime/test source e47ab370402b2cbe3ef8b6a9207b5f6243f08bd4, independent branch from d635aeed. Final evidence-only commit does not change executable source.

Full View Python discovery: 132 tests, rc0, 14.387s process (see raw RESULT for authoritative elapsed time); no SKIP. Build-entrypoint contract suite rc0, no SKIP. Real chmod000 renderer request returns500; stderr includes PermissionError, exact renderer target and request path. The negative control removes the logging helper output in an isolated detached worktree: rc1, one assertion failure because records=[] while the 500 response assertion passes. Mutation source ed6c712 has identical View product/test bytes to e47ab370; only later gate wiring differs.

Execution: uid1001 dev, Go1.22.2 linux/amd64, locked Python3.12.3; isolated temporary HOME/minimal env, no external DSNs or provider credentials. See evidence/SUMMARY.json and individual result records for exact commands, UTC, elapsed time and SHA256. No deployment/build/service configuration changes. Author self-test is not independent review.
