#!/usr/bin/env python3
"""Validate one SP3 N1-N8 review registry. rc 0=PASS, 1=non-PASS, 2=malformed/unknown."""
from pathlib import Path
import re, sys, tempfile

ACS = {f"AC-SP3-{i:02d}" for i in range(1, 13)}
ROOT = Path(__file__).resolve().parents[2]
SPEC = ROOT / "specs/sp3/spec.md"

def fail(msg, rc=1): print("FAIL: " + msg); raise SystemExit(rc)

def protocol(spec=SPEC):
    text=Path(spec).read_text()
    blocks=re.findall(r'^<!-- REVIEW-PROTOCOL-SP3-BEGIN -->$(.*?)^<!-- REVIEW-PROTOCOL-SP3-END -->$',text,re.M|re.S)
    if len(blocks)!=1: fail(f"review protocol block count is {len(blocks)}, expected 1",2)
    got={}
    for line in blocks[0].splitlines():
        c=[x.strip().strip('`') for x in line.strip().strip('|').split('|')]
        if len(c)==2 and c[0] not in ('key','---') and not c[0].startswith('---'):
            if c[0] in got: fail(f"duplicate review protocol key: {c[0]}",2)
            got[c[0]]=c[1]
    required={'owner','primary_reviewers','narrow_replacement_pool','permanent_recusal'}
    if not required <= set(got): fail(f"review protocol missing keys: {sorted(required-set(got))}",2)
    reviewers=tuple(got['primary_reviewers'].split(',')); narrow=tuple(got['narrow_replacement_pool'].split(','))
    if len(reviewers)!=2 or len(set(reviewers))!=2 or not all(reviewers): fail("primary reviewer roster malformed",2)
    if not narrow or len(set(narrow))!=len(narrow) or not all(narrow): fail("narrow reviewer pool malformed",2)
    recusal={}
    if got['permanent_recusal']!='none':
        for item in got['permanent_recusal'].split(','):
            reviewer,surface=item.split(':',1); recusal.setdefault(reviewer,set()).add(surface)
    return got,reviewers,narrow,recusal

def parse(path):
    rows=[]
    for raw in Path(path).read_text().splitlines():
        if not raw or raw.startswith("#"): continue
        c=raw.split("\t")
        if len(c)!=8: fail(f"registry row must have 8 columns: {raw}",2)
        rows.append(dict(zip(("round","anchor","reviewer","authorization","verdict","covered","recused","independence"),c)))
    if not rows: fail("registry is empty",2)
    return rows

def csv(value): return set() if value=="-" else set(value.split(","))

def check(path, expected_anchor=None, spec=SPEC):
    cfg,reviewers,narrow_pool,permanent=protocol(spec)
    rows=parse(path); rounds={r["round"] for r in rows}; anchors={r["anchor"] for r in rows}
    if len(rounds)!=1 or len(anchors)!=1: fail("rows must share one round and anchor",2)
    rid=next(iter(rounds)); m=re.fullmatch(r"N([1-8])",rid)
    if not m: fail("only N1-N8 are authorized; N9+ forbidden")
    anchor=next(iter(anchors))
    if not re.fullmatch(r"[0-9a-f]{40}",anchor): fail("anchor must be 40 hex",2)
    if expected_anchor is not None and anchor!=expected_anchor: fail(f"registry anchor {anchor} != expected {expected_anchor}",2)
    n=int(m.group(1)); main=[r for r in rows if r["authorization"]=="ALL"]
    if len(main)!=1: fail(f"exactly one main ballot required, got {len(main)}")
    main=main[0]; expected=reviewers[(n-1)%2]
    if main["reviewer"]!=expected: fail(f"rotation mismatch: {rid} expects {expected}")
    if main["verdict"] not in {"PASS","NO","INCONCLUSIVE"}: fail("invalid main verdict",2)
    covered=csv(main["covered"]); recused=csv(main["recused"])
    if covered|recused != ACS or covered&recused: fail("main covered/recused must partition AC-SP3-01..12")
    if main["independence"]!="main": fail("main independence marker must be main",2)
    if not permanent.get(main["reviewer"],set()) <= recused: fail("main ballot omits a permanent recusal")
    narrow=[r for r in rows if r["authorization"]!="ALL"]
    if {r["authorization"] for r in narrow} - recused: fail("narrow ballot exists for non-recused surface")
    for surface in sorted(recused):
        votes=[r for r in narrow if r["authorization"]==surface]
        if len(votes)!=1: fail(f"{surface} requires exactly one narrow ballot, got {len(votes)}")
        v=votes[0]
        if v["covered"]!=surface or v["recused"]!="-": fail(f"{surface} narrow scope malformed",2)
        if v["independence"]!="no_involvement": fail(f"{surface} narrow reviewer lacks no-involvement declaration")
        if v["reviewer"] in {main["reviewer"],cfg['owner']}: fail(f"{surface} narrow reviewer is main or owner")
        if v["reviewer"] not in narrow_pool: fail(f"{surface} narrow reviewer is outside replacement pool")
        if v["verdict"]!="PASS": fail(f"{surface} narrow verdict is {v['verdict']}")
    if len(narrow)!=len(recused): fail("extra or duplicate narrow ballots")
    if main["verdict"]!="PASS": fail(f"main verdict is {main['verdict']}")
    print(f"PASS: {rid} main=1 recused={len(recused)} narrow={len(narrow)} all effective surfaces PASS")

def fixture(reviewers,narrow_pool,round_id="N1",main_verdict="PASS",recused="AC-SP3-08",narrow_verdict="PASS",narrow_count=1,anchor="1"*40):
    n=int(round_id[1:]) if round_id[1:].isdigit() else 9
    main=reviewers[(n-1)%2] if n<=8 else reviewers[0]
    rec=set() if recused=="-" else {recused}; covered=sorted(ACS-rec)
    lines=["\t".join((round_id,anchor,main,"ALL",main_verdict,",".join(covered),recused,"main"))]
    for i in range(narrow_count):
        reviewer=narrow_pool[i] if i < len(narrow_pool) else f"zzc-narrow{i}-2642"
        lines.append("\t".join((round_id,anchor,reviewer,recused,narrow_verdict,recused,"-","no_involvement")))
    return "\n".join(lines)+"\n"

def selftest():
    cfg,reviewers,narrow_pool,_=protocol(); good=fixture(reviewers,narrow_pool)
    main_line,narrow_line=good.splitlines(); no_rec=fixture(reviewers,narrow_pool,recused="-",narrow_count=0)
    tests=[]
    for n in range(1,9):
        rec="AC-SP3-08" if n%2 else "-"
        tests.append((f"legal N{n}",fixture(reviewers,narrow_pool,f"N{n}",recused=rec,narrow_count=1 if rec!="-" else 0),0,None))
    n9=fixture(reviewers,narrow_pool,"N9"); n9_auth=n9.replace("\tALL\t","\tAC-SP3-01\t",1)
    if n9_auth==n9: raise AssertionError("N9 authorization mutation did not change target")
    tests += [
      ("N9 forbidden",n9,1,None), ("N9 authorization cannot override prohibition",n9_auth,1,None),
      ("exactly one main",good+main_line+"\n",1,None), ("rotation",good.replace(reviewers[0],reviewers[1],1),1,None),
      ("coverage partitions all surfaces",good.replace("AC-SP3-01,","",1),1,None), ("permanent recusal",no_rec,1,None),
      ("no narrow on non-recused",no_rec+narrow_line.replace("AC-SP3-08","AC-SP3-01")+"\n",1,None),
      ("exactly one narrow",fixture(reviewers,narrow_pool,narrow_count=0),1,None),
      ("narrow scope",good.replace("\tAC-SP3-08\tPASS\tAC-SP3-08\t-\t","\tAC-SP3-08\tPASS\tAC-SP3-07\t-\t"),2,None),
      ("no-involvement declaration",good.replace("no_involvement","involved"),1,None),
      ("narrow differs from main",good.replace(narrow_pool[0],reviewers[0]),1,None),
      ("owner cannot cast narrow",good.replace(narrow_pool[0],cfg['owner']),1,None),
      ("narrow reviewer in pool",good.replace(narrow_pool[0],"zzc-outsider-2642"),1,None),
      ("narrow verdict PASS",fixture(reviewers,narrow_pool,narrow_verdict="NO"),1,None),
      ("no extra narrow",fixture(reviewers,narrow_pool,narrow_count=2),1,None),
      ("main verdict PASS",fixture(reviewers,narrow_pool,main_verdict="NO"),1,None),
      ("expected anchor",good,2,"2"*40),
    ]
    failed=0
    with tempfile.TemporaryDirectory() as d:
        p=Path(d)/"r.tsv"
        for name,data,want,anchor in tests:
            p.write_text(data); got=0
            try: check(p,anchor)
            except SystemExit as e: got=int(e.code)
            ok=(got==want); print(("✓" if ok else "✗")+f" {name}: rc={got}"); failed |= not ok
    print(f"--- SP3 review aggregate selftest {len(tests)} cases {'PASS' if not failed else 'FAIL'}")
    return failed

if __name__=="__main__":
    if len(sys.argv)==2 and sys.argv[1]=="--selftest": raise SystemExit(selftest())
    if len(sys.argv) not in (2,3): print("usage: review-aggregate.py <registry.tsv> [expected-anchor]|--selftest"); raise SystemExit(2)
    check(sys.argv[1],sys.argv[2] if len(sys.argv)==3 else None)
