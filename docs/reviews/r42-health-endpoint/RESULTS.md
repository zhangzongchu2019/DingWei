# Developer validation

Runtime/test source d4ed8023c94c4d666d0936b5f012814c61efd856; base d635aeed97b43f8919e138573c289fc4bd9f6f51. Evidence-only final commit changes no executable source.

Full m8 and Go view-service count=3: rc0, 495.796s process; each m8 round 775 PASS, 16 top-level and 18 nested SKIP, zero FAIL/panic. All four health tests and failure-notice driver pass each round. Go view-service has no Go tests. Full View Python discovery runs 143 tests in each of three rounds, all rc0, zero SKIP. Existing opt-in/deferred skips are enumerated in evidence/SKIP-LIST.txt, not waived. Canonical build is not claimed.

Dedicated Go and Python digest-check removals each produce rc1 at wrong_signature (got200 want401). Mutation provenance and exact commands, UTC, elapsed time, SHA256 are recorded in evidence/SUMMARY.json and RESULT records. Public proxy internal-path rejection and canonical entrypoint wiring checks pass.

Isolated local tests under dev uid1001, Go1.22.2 and locked Python3.12.3 with temporary HOME/minimal environment and embedded test PG. No deployment/restart/binary upload/R41 changes. Developer self-test does not replace independent review.
