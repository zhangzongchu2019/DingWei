# A3-DIRECT implementation scope

Leader reassignment uses exact r12 6c4600478a4c7c769cb6adaa1024e4504208f39b,
independent review/coder/a3-direct branch. No self-update commits are ancestors.
Frozen order eb9f64801d6e and reviewer scope record are the contract; no migration.

- Create/PATCH spec routes use dedicated AuthenticateAgent, active Agent+owner,
  trusted principal/Agent projection and explicit direct-v1 protocol. Capabilities,
  cookies and client created_by cannot substitute. Retained task/inference
  capability routes and selected inference-assist workflow are unchanged.
- AppendConsoleSpecRevisionByAgent checks revision1 kind/id inside the same
  SQLite transaction as revision insertion/CAS. Current editor/latest actor cannot
  establish ownership. Principal/unknown/other-Agent deny in fixed Chinese.
- No grant state map, approval page or three registered approval routes. Legacy
  authenticated WS request/poll returns upgrade_required immediately, no writes.
  Old MCP IPC calls likewise get upgrade_required without HTTP. New MCP performs
  one direct HTTP write per call; no request/poll/capability argument exists.
- agent_create/agent_update audit actions use authenticated actor, no body/secret.
  Detail shows creator/latest editor plus persisted revision times.
- Archived flag implements reversible deletion. Owner or deletion-authorized
  management can single/batch archive; batch validation/events/write commit are
  atomic. Default Specs list and task board omit archives. Toggle lists archives,
  one-row restore preserves revisions/tasks. Existing shared historical read
  remains readable, with no edit/split permission while archived (控写不控读).
- Full helper O3 deadlines35/30/15 and explicit unknown/no-blind-retry preserved.
  Actual real MCP/IPC/helper authenticated WS + Agent HTTP fixture covers direct
  create/update, two peer scopes, stale revision, public/list read, spoofed args,
  actual committed response loss and all five private log streams.

Test contract changes are intentional, not silent skips: retired approval
state-machine/decision/deadline tests are replaced by fast zero-write legacy
compatibility and direct authentication/ownership/revision/audit cases. Existing
MCP subprocess/private stream-capture helpers and scanner are retained; request
approval-only E2E cases are obsolete. Existing enroll pending-entry browser case
now asserts absence and no polling. Task-capability browser negative at the old
spec writer expects426 upgrade_required instead of401/403. Other task/inference
capability tests remain. Route registry floor is now exactly two retained
capability routes, plus an explicit two-direct-route guard check.

This first source checkpoint has passing targeted author checks. Full Go,
helper, browser matrix, canonical gate and surgical withdrawals are pending
separate exact-source evidence. No independent PASS, deployment, upload or
real Agent action is claimed.
