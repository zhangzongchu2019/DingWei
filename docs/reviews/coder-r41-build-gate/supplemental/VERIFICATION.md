# Additional build gate checks

Source under verification: 40bbd27fe69767ccc74edcf8015800b2c538174c, tree 0104dc9a424749cea8ccdf1e99ca43c8c227dc83. This supplements the full helper run; it does not declare that run PASS.

- Build entrypoint contracts: 16 tests PASS. Includes deletion of each of 87 actual files and each of 87 manifest entries independently, plus duplicate manifest rejection. Each mutation raises inventory mismatch before importing tests.
- Runner subprocess boundary checks (synthetic child results): silent rc0, short execution count, and nonzero child exit each make the gate return 1. The full-count rc0 control returns 0. Temporary HOME, Codex/XDG state and IPC roots were checked with synthetic inherited seat values.
- The unchanged 25 deploy/View Python files and release shell gate passed in a temporary HOME.
- Desktop shell initially selected system Python and failed dependency discovery (websockets absent). Repeated only that shell gate with PATH=/tmp/coder-0232-locked-venv/bin:/usr/bin:/bin; rc=0, all six real-localhost Hub integration test groups passed, including isolated PostgreSQL. Initial failure is retained in nonhelper.log/results, corrected run in desktop-locked-runtime.log/rc.
- No running helper commands, deployments, restarts or online database writes. Integration services and PostgreSQL are local test fixtures.

Full helper profile results are recorded separately. Guide duplicate remains item 7 and is not waived here.
