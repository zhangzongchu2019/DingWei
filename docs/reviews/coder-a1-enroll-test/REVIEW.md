# A1 enroll regression: current owner group-code contract

Test-only parent 2d5bde739deba3876a5b63ce41d8ce1301271735. Review ref review/coder/a1-enroll-test. First-batch integration/release refs and all product source remain untouched.

Only the obsolete owner-S5 test in view-service/console-agent-mgmt-enroll-regression.js changes. List/create assertions remain. The replacement requires the visible group-code panel, maxlength32, the complete set of currently owned Agent options, rejection of 31/33-character and invalid-character codes, rejection of a DOM-injected foreign Agent, a case-preserving 32-character code including letters/digits/underscore/hyphen, and an explicit POST members containing only agent_id. Its successful membership response is intercepted by a local Playwright route; no actual membership is changed. The client checks do not substitute for server ownership/authentication tests or relax those rules.

PROVENANCE.json proves the prefix/suffix outside that one case are exact parent bytes and pins the original 60905ca3 evidence and both relevant Console sources. All other 18 test cases and fixture/server/mutation infrastructure remain byte-identical. No test is skipped or filtered.

Three complete local-browser runs (original stdout and rc in raw/):

- current.log: new test + exact frozen 2d5b Console source: passed19 failed0, rc0.
- old-S5-on-2d5b.log: exact old test + frozen 2d5b Console source: passed18 failed1, rc1; sole FAIL is the original `groupCodeJoin must be gone` contract mismatch.
- withdraw-A1-entry-ff49.log: new test + exact ff49 Console source (A1 withdrawal): passed18 failed1, rc1; sole FAIL is `current A1 join-by-code entry must exist`. Thus the replacement detects removal of the entry and does not green a missing feature. This is a true parent-source withdrawal, not a stub of the new assertion or a mutation of the candidate product file.

Each run uses a disposable directory with just the selected exact test file and console_auth.go. The test extracts the production Console HTML as usual and serves it through its own 127.0.0.1 port0 mock. No live page, business DB, real Agent enrollment or service action is used. The updated code is syntax-checked and git diff --check passes.

Local environment differs from the upstream reviewer: private Playwright1.64.1 and installed Chrome151.0.7922.108, not that seat's downloaded Chromium153. ENVIRONMENT.json records actual paths/versions. Dependency install remains outside the repository. Reproduce with the usual command after providing Playwright and a browser:

    cd view-service
    NODE_PATH=/path/to/private/node_modules PW_CHANNEL=chrome node console-agent-mgmt-enroll-regression.js

For withdrawal, copy the updated script to a temporary tree's view-service/, write `git show ff49d4d974f37beeee32c3733b717dc0809fe88b:internal/m8/console_auth.go` to its internal/m8/console_auth.go, and run the same command there; expect rc1 and the sole entry-missing FAIL above.

The initial author log initial-maxlength-fixture.log is retained: Playwright fill truncated33 to32, so that case submitted a valid32-character code instead of exercising the scripted-oversize guard. Only the new test case was corrected to set its oversize value explicitly; the final three runs above follow that correction. This initial harness failure is not presented as a product regression.

This is author fixture evidence, not independent CR or deployment approval. No old full suite was run.
