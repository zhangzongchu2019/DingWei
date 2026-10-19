"""Validate the caller-visible schema; jsonschema is a test-only dependency."""
import copy
import json
from pathlib import Path
import sys

from jsonschema import Draft202012Validator

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / 'tools/sessionhelper'))
from sessionhelper.mcp_shim import Request, _dispatch

response = _dispatch(Request(jsonrpc='2.0', method='tools/list', params={}, id=1))
assert response.error is None
schema = next(t['inputSchema'] for t in response.result['tools'] if t['name'] == 'dw_spec_review_reply')
Draft202012Validator.check_schema(schema)
validator = Draft202012Validator(schema)
base = {'request_id': 'specreview_fixture', 'revision': 1, 'content_sha256': 'a' * 64}
issue = {'id': 'i1', 'text': 'Check an invariant'}
criterion = {'id': 'a1', 'text': 'Preserve data', 'owner_ref': 'original body', 'cases': ['rollback']}
review = {**base, 'kind': 'review', 'issues': []}
criteria = {**base, 'kind': 'criteria', 'criteria': [criterion]}
cases = [
    ('review_empty_valid', review, True),
    ('review_issue_valid', {**review, 'issues': [issue]}, True),
    ('criteria_valid', criteria, True),
    ('review_missing_issues', {**base, 'kind': 'review'}, False),
    ('criteria_missing_criteria', {**base, 'kind': 'criteria'}, False),
    ('review_mixed_payload', {**review, 'criteria': [criterion]}, False),
    ('criteria_mixed_payload', {**criteria, 'issues': []}, False),
    ('review_wrong_payload', {**base, 'kind': 'review', 'criteria': [criterion]}, False),
    ('criteria_wrong_payload', {**base, 'kind': 'criteria', 'issues': []}, False),
    ('unknown_kind', {**review, 'kind': 'other'}, False),
    ('criteria_empty', {**criteria, 'criteria': []}, False),
    ('issue_missing_text', {**review, 'issues': [{'id': 'i1'}]}, False),
    ('criterion_missing_cases', {**criteria, 'criteria': [{k: v for k, v in criterion.items() if k != 'cases'}]}, False),
    ('credential_argument', {**review, 'credential': 'fixture-only'}, False),
    ('spoofed_checked_by', {**criteria, 'criteria': [{**criterion, 'checked_by': 'forged'}]}, False),
]
for field in base:
    payload = copy.deepcopy(review)
    del payload[field]
    cases.append(('missing_' + field, payload, False))
records = []
for name, payload, expected in cases:
    actual = validator.is_valid(payload)
    assert actual == expected, 'R8_SCHEMA_' + name.upper() + '_ACCEPTANCE_MISMATCH'
    records.append({'case': name, 'expected_valid': expected, 'actual_valid': actual})
print(json.dumps({'draft': '2020-12', 'tool': 'dw_spec_review_reply', 'cases': records}, indent=2))
