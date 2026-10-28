# Leader review increment

Parent ab6f4013692f52dac652399cacd15b66e3e4f12c. Authorization: leader UTC2026-10-28T03:41:03Z; implemented after deployment-script cross-review completion. One append, single writer. This changes runtime/test code and is not a docs-only append. No release bump or deployment is performed.

View removes the source HEALTH_VERSION literal. Each authenticated health request reads deployment VS_VERSION, accepting a canonical four-component decimal version. Missing/empty/malformed configuration returns503 with only health=unavailable; failed authentication remains401. The deployment must supply VS_VERSION from its approved VERSION artifact. There is no guessed value or checkout fallback. Tests exercise two arbitrary future deployment versions without source edits, current repository VERSION through actual HTTP, malformed/missing503 and authentication precedence. The internal/version package now runs these HTTP tests with an isolated environment as part of the existing VERSION/Go/helper version gate, so the next version-bit commit includes View too.

The public View proxy applies path.Clean before rejecting /internal. Tests cover doubled slash, dot and parent segments, encoded dot/internal, health and control paths; all rejected with zero upstream calls. Existing ordinary View proxy/routing/control regressions pass.

Validation (Go1.22.2, locked Python3.12.3, temporary HOME/minimal environment):

- Full View Python discovery146 tests: rc0,23.730s, zero SKIP.
- Go health/version/Console version checks count3: rc0,12.222s; zero SKIP. All four TestHealthAuth parents and five internal/version tests pass each round.
- Fourteen existing View proxy/routing/auth/control tests: rc0,4.142s; zero SKIP.
- Build-entrypoint contract suite: rc0,0.713s.
- Negative control: temporary View source returns0.2.3.15 again; rc1 at future deployment version9.8.7.6 (got old literal).
- Negative control: Go source overlay withdraws path.Clean; rc1 at //internal/health/auth (got200 instead of404). No mutant enters product source. Initial overlay attempt failed vet on changed imports and was not accepted as a criterion result; corrected overlay preserves the import with an inert reference and fails the intended assertion.

Exact commands, UTC/process time, raw logs, test-source SHA256 and mutation reproduction are under evidence/increment/. Tests ran on parent HEAD plus these incremental working-tree changes; RESULT.source therefore records the parent, while SUMMARY.source_sha256 binds the actual tested bytes. Full original m8 count3 remains evidence for d4ed802, not a claim about this new increment. Full m8/canonical not rerun here. Independent incremental approval remains with leader.
