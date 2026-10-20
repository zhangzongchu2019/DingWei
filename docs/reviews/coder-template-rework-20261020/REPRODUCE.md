# D1 rework reproduction

All commands run from a Git checkout of the delivered successor. Frozen inputs
are Git **trees**, not commits. Independently fetch both annotated evidence tags
before reproduction; fetching only the candidate is insufficient to obtain old
input root trees. Use ordinary git fetch for tree tags alone: --depth=1 without
a commit fails with No commits selected for shallow request. Source travels exclusively through wego-ai/dingwei Git.

Final input: refs/tags/evidence/coder/template-rework-final-b85f7fd6 → b85f7fd6c86f91d9d1a508f948d462b5d7c74a70.
Earlier input: refs/tags/evidence/coder/template-rework-input-e362f42b → e362f42b8b2c348711328fd6701290e7b9efaf91.

```
python3 tests/pilot-spec-template/withdrawals.py --commit b85f7fd6c86f91d9d1a508f948d462b5d7c74a70 --output /tmp/d1-review-withdrawals
python3 tests/pilot-spec-template/browser_gates.py --commit b85f7fd6c86f91d9d1a508f948d462b5d7c74a70 --output /tmp/d1-review-browsers
python3 tests/pilot-spec-template/gates.py --commit b85f7fd6c86f91d9d1a508f948d462b5d7c74a70 --output /tmp/d1-review-gates --gates vet canonical race go
python3 tests/pilot-spec-template/sqlite_compat.py --commit b85f7fd6c86f91d9d1a508f948d462b5d7c74a70 --output /tmp/d1-review-compat
python3 docs/reviews/coder-template-rework-20261020/VERIFY.py --commit HEAD
```

Runners export immutable Git bytes to a private local directory, use an isolated
HOME and a cleared environment, an existing locked helper Python venv and Go
module/cache directories with GOPROXY=off and GOSUMDB=off. Only the embedded-PG
binary cache is reused. No external DSN or live service is used. Browser tests
use installed Playwright from /tmp/coder-a3-playwright/node_modules and Chrome.
The old compatibility probe compiles the exact r32.1 store; it does not start an
old full Hub or authorize rollback writes. Log receipt `commit` fields in these
legacy runners contain the resolved tree ID for tree inputs.

The initial browser and withdrawal results are retained, never overwritten.
The browser assertion initially read the collapsed details section. Final copy
is visibly placed in the page header. The old role-pair withdrawal initially
survived because the new pre-review independence guard rejected its wrong pair;
its fixture now uses a different owned Agent that only violates role matching.
