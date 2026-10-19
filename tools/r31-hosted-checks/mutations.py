#!/usr/bin/env python3
"""Inert R31 withdrawal checks: Go overlays, private temporary PG, exact failure witnesses.

No host connections, Docker mutations or source-tree rewrites. A compiler error,
crash, timeout or unrelated assertion is not a successful withdrawal check.
"""
import argparse
import concurrent.futures
import datetime
import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
# id, source, literal original, replacement, package, exact test, witness
CASES = [
    ('unified-migration-ledger', 'internal/hosted/store.go', '_, err := migrate.Migrate(ctx, s.pool, migrate.HubDomains())', '_ = migrate.HubDomains()\n _, err := s.pool.Exec(ctx, repo.SystemCtx(), SchemaDDL)', './internal/hosted', 'TestR31HostedApplySchemaUsesUnifiedLedger', 'hosted startup bypassed unified migration ledger'),
    ('hard-login-inventory', 'internal/nodeagent/seatcapacity.go', 'if loginID := c.Labels[loginLabel]; loginID != "" {', 'if loginID := c.Labels[loginLabel]; false && loginID != "" {', './internal/nodeagent', 'TestR31HardInventoryTracksLoginWriters', 'tracked login rejected'),
    ('patch-revocation-lock', 'internal/hosted/node_access.go', 'SELECT address,address_source,scheduling_state,access_scope FROM g4_node WHERE id=$1 FOR UPDATE', 'SELECT address,address_source,scheduling_state,access_scope FROM g4_node WHERE id=$1', './internal/hosted', 'TestR31StatePatchCannotRestoreConcurrentRevocation', 'state patch restored revoked access'),
    ('credential-private-intent', 'internal/m8/hub.go', 'PrivateClientIP: a.credentialIdentity,', 'PrivateClientIP: false,', './internal/m8', 'TestR31SourceRecordedOnlyAfterAdmissionAndRetained', 'credential admission omitted private IP clear intent'),
    ('historical-ip-read-redaction', 'internal/store/sqlite.go', 'if strings.HasPrefix(ep.KeyID, "u_") {', 'if false && strings.HasPrefix(ep.KeyID, "u_") {', './internal/store', 'TestR31PrivateSessionSourceClearsHistoricalIPAndPreservesLegacy', 'historical credential source visible before reconnect'),
    ('historical-ip-write-clear', 'internal/store/sqlite.go', "client_ip=CASE WHEN ?=1 THEN '' ELSE", "client_ip=CASE WHEN ?=0 THEN '' ELSE", './internal/store', 'TestR31PrivateSessionSourceClearsHistoricalIPAndPreservesLegacy', 'credential reconnect retained historical raw IP'),
    ('active-human', 'cmd/workpulse/hosted_access.go', 'if p.active != nil && !p.active(r, id) {', 'if false && p.active != nil && !p.active(r, id) {', './cmd/workpulse', 'TestR31HostedAccessModes', 'actor=disabled allowed=true want=false'),
    ('admin-role', 'cmd/workpulse/hosted_node_routes.go', 'err != nil || d.isAdmin == nil || !d.isAdmin(r, actor)', 'err != nil || d.isAdmin == nil || (false && !d.isAdmin(r, actor))', './cmd/workpulse', 'TestR31AdminNodeAPIAndOneTimeCredential', 'nonadmin fixture-manager: 201'),
    ('hidden-node-models', 'internal/hosted/readapi.go', 'if !allowed {', 'if false && !allowed {', './internal/hosted', 'TestR31NodeOptionsHideModelsAndQueuedRevocation', 'hidden node/model disclosure'),
    ('hard-resource-capacity', 'internal/hosted/orchestrator.go', 'if mode == "soft" || mode == "hard" {', 'if false && (mode == "soft" || mode == "hard") {', './internal/hosted', 'TestR31HardCapacityAllDimensionsAndLegacyUnknown', 'did not skip cpu'),
    ('pending-reservations', 'internal/hosted/seat_capacity.go', 'v = v.WithPending(pending)', '_ = pending', './internal/hosted', 'TestR31HardConcurrentReservationsDoNotOversell', 'expected two seats, admitted 10'),
    ('final-admission', 'internal/hosted/store.go', 'if admit {', 'if false && admit {', './internal/hosted', 'TestR31NodeDisableSurvivesHeartbeatAndBlocksFinalAdmission', 'disabled node admitted seat'),
    ('credential-hashing', 'internal/hosted/node_access.go', 'r.ID, hashNodeSecret(secret), by)', 'r.ID, secret, by)', './internal/hosted', 'TestR31NodeRegistrationCredentialAndAtomicAudit', 'plaintext or invalid credential hash'),
    ('registration-atomic-audit', 'internal/hosted/node_access.go', "_, err = tx.Exec(ctx, `INSERT INTO g4_audit(actor,action,target,detail)VALUES($1,'node.register',$2,$3)`, by, r.ID, detail)\n\t\treturn err", '_ = detail\n\t\treturn nil', './internal/hosted', 'TestR31NodeRegistrationCredentialAndAtomicAudit', 'registration ignored audit failure'),
    ('active-group-agent', 'internal/identity/hosted_node_access.go', "AND a.state='active'", "AND a.state IN ('active','suspended')", './internal/hosted', 'TestR31NodeGroupAccessAndRevocation', 'access member/nd_restricted got=true want=false'),
    ('ip-owner-privacy', 'internal/identity/agent_connection.go', "AND (a.owner_lc=$1 OR u.type='platform_admin')", "AND (true OR a.owner_lc=$1 OR u.type='platform_admin')", './internal/identity', 'TestR31ConnectionPrivacyPGAndMemory', 'IP disclosed to other'),
    ('ip-trusted-peer', 'internal/m8/agent_ws.go', 'h.TrustProxy && peer.IsLoopback() &&', 'h.TrustProxy && (true || peer.IsLoopback()) &&', './internal/m8', 'TestR31AgentSourceIPTrustBoundary/direct_forged', 'got "198.51.100.2" want "203.0.113.9"'),
]

def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--out', type=Path, required=True)
    p.add_argument('--jobs', type=int, default=2)
    p.add_argument('--browser', action='store_true', help='also withdraw browser credential clearing; requires Playwright in NODE_PATH')
    args = p.parse_args()
    args.out.mkdir(parents=True, exist_ok=True)
    env = dict(os.environ, TZ='UTC')
    # Never inherit externally supplied databases; pgtest owns an isolated local cluster.
    env.pop('HUB_TEST_PG_DSN', None)
    env.pop('DATAD_TEST_PG_DSN', None)
    env.pop('GOFLAGS', None)
    env['HUB_TEST_PG_EMBEDDED'] = '1'
    env['HUB_TEST_PG_STRICT'] = '1'
    env['HUB_TEST_PG_SKIP_OK'] = '0'

    def run(case):
        name, source, old, new, package, test, witness = case
        started = now()
        original = (ROOT / source).read_text()
        if original.count(old) != 1:
            return dict(id=name, passed=False, error='mutation anchor must occur exactly once', count=original.count(old))
        with tempfile.TemporaryDirectory(prefix='r31-overlay-') as td:
            td = Path(td)
            replacement = td / Path(source).name
            replacement.write_text(original.replace(old, new, 1))
            overlay = td / 'overlay.json'
            overlay.write_text(json.dumps({'Replace': {str(ROOT/source): str(replacement)}}))
            cmd = ['go', 'test', '-overlay', str(overlay), package, '-run', '^'+test+'$', '-count=1', '-timeout=3m', '-v']
            try:
                proc = subprocess.run(cmd, cwd=ROOT, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=210)
                output, rc = proc.stdout, proc.returncode
            except subprocess.TimeoutExpired:
                return dict(id=name, passed=False, error='timeout', started=started, ended=now())
            (args.out / (name+'.log')).write_text(output)
            rejected = any(mark in output for mark in ('[build failed]', 'panic:', 'test timed out', 'setup failed', '--- SKIP:'))
            passed = rc == 1 and witness in output and '--- FAIL:' in output and not rejected
            return dict(id=name, source=source, original_sha256=hashlib.sha256(original.encode()).hexdigest(), test=test, witness=witness, rc=rc, passed=passed, started=started, ended=now(), log=name+'.log')

    started = now()
    with concurrent.futures.ThreadPoolExecutor(max_workers=args.jobs) as pool:
        results = list(pool.map(run, CASES))
    if args.browser:
        source = ROOT / 'internal/m8/console_auth.go'
        original = source.read_text()
        old = "if(el)el.textContent='';"
        if original.count(old) != 1:
            results.append(dict(id='one-time-credential-clear', passed=False, error='browser mutation anchor is not unique'))
        else:
            with tempfile.TemporaryDirectory(prefix='r31-ui-overlay-') as td:
                td = Path(td)
                modified = td/'console_auth.go'
                modified.write_text(original.replace(old, "if(el)el.textContent=el.textContent;", 1))
                overlay = td/'overlay.json'
                overlay.write_text(json.dumps({'Replace': {str(source):str(modified)}}))
                browser_env = dict(env, GOFLAGS='-overlay='+str(overlay))
                cmd = ['node', 'view-service/r31-hosted-nodes-browser-regression.js']
                proc = subprocess.run(cmd, cwd=ROOT, env=browser_env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=90)
                (args.out/'one-time-credential-clear.log').write_text(proc.stdout)
                passed = proc.returncode == 1 and 'ONE_TIME_CREDENTIAL_CLEAR_MISSING' in proc.stdout and 'registration displays credential once' in proc.stdout
                results.append(dict(id='one-time-credential-clear',source=str(source.relative_to(ROOT)),original_sha256=hashlib.sha256(original.encode()).hexdigest(),witness='ONE_TIME_CREDENTIAL_CLEAR_MISSING',rc=proc.returncode,passed=passed,log='one-time-credential-clear.log'))
    report = dict(started=started, ended=now(), host=platform.node(), uid=os.getuid(), scope='local inert Go overlays and pgtest-owned temporary PostgreSQL only; no deployment', results=results)
    (args.out/'mutations.json').write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')
    for r in results:
        print(('PASS' if r['passed'] else 'FAIL')+' '+r['id'], flush=True)
    raise SystemExit(0 if all(r['passed'] for r in results) else 1)

if __name__ == '__main__':
    main()
