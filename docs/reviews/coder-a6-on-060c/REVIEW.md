# A6 R1/R2 on the current second-wave base

Exact integration base: 060c7b9d4226746b68b71f130c68ca0ab6584e1e (includes approved A1 test-only update). Existing A6 author tip: 506f136b5bf5eb1d17e7e5199accd28b9f77159b. This delivery merges the exact base into that review branch without rewriting previous objects. Both are parents of this delivery. Automatic merge; no manual conflict resolution or additional runtime changes. Every file differing from 060c is in the existing A6 change set from 2d5b to506; current runtime pins are SOURCE-PINS.json here. Earlier source pins/logs retain their original commit scope and are historical, not claims about the merged renderer.

R1 retains helper30s / cache30s / page300s / Lifetime360s. The 330s normal-age renderer fixture is green. R2 retains subtype/2048-byte/current-connection/30s attempt checks before GetAgent/GetUser, under h.mu. Invalid eligible attempts consume the interval. The actual-handler lookup-counter cases pass.

On this actual merged tree: A6 Go checks, helper19, renderer27, A5 replay8, A5 real-gap9, integrity22, history7, A4 overflow4, managed3 and view-refresh5 all rc0. Exact commands/results and raw logs are in raw/. Initial helper command lacked PYTHONPATH and failed import before any test; retained separately. Corrected local module path rerun19 passes. No runtime workaround was made.

Reverse source overlays on temporary files: removing the R2 cheap preflight gives rc1 in real-handler lookup-count tests; restoring cache interval300 gives rc1 at the30s acceptance boundary. Existing candidate files are not mutated. Only relevant tests were rerun; no full build, live sampling, production DB, deployment or owner acceptance.

The A1 test-only update and all non-A6 base files are preserved exactly. Prior independent parent review and integration tests are not relabeled as this author's execution. This is an author candidate for independent delta/merge review.
