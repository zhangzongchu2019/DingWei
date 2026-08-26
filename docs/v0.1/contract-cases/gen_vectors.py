#!/usr/bin/env python3
"""golden vectors 生成器(一次性,产物=jcs-golden-vectors-v1.json;留仓存证可复现)。"""
import hashlib
import importlib.util
import json
import os

_d = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("jcs", os.path.join(_d, "jcs_vectors_check.py"))
jcs = importlib.util.module_from_spec(spec)
spec.loader.exec_module(jcs)


def cv(vid, note, inp):
    c = jcs.canonical(inp)
    return {"id": vid, "kind": "canonical", "note": note, "input": inp,
            "expected_canonical": c,
            "expected_sha256": hashlib.sha256(c.encode("utf-8")).hexdigest()}


def dv(vid, note, op, ver, payload):
    return {"id": vid, "kind": "digest", "note": note, "op_kind": op,
            "payload_schema_version": ver, "input": payload,
            "expected_sha256": jcs.payload_digest(op, ver, payload)}


vectors = [
    cv("V01-key-order-ascii", "同义 key 顺序:ASCII 排序", {"b": 1, "a": 2, "A": 3}),
    cv("V02-key-order-utf16",
       "UTF-16 code unit 排序:U+1F600(代理对 d83d de00)排在 U+FFFD 之前(与 code point 序相反);z<e-acute",
       {"�": 1, "\U0001F600": 2, "z": 3, "é": 4}),
    cv("V03-num-int", "整数与安全上界", {"n0": 0, "n1": -1, "n2": 42, "n3": 9007199254740991}),
    cv("V04-num-frac", "定点小数最短往返", {"a": 0.75, "b": -0.1, "c": 1.5}),
    cv("V05-num-intfloat", "1.0 归一为 1(与 V05b 同 canonical 同 digest)", {"n": 1.0}),
    cv("V05b-num-int-equiv", "与 V05 等价对照", {"n": 1}),
    cv("V06-string-escape", "最小转义:控制符简写+\\u00xx 小写+引号反斜杠+非 ASCII 字面",
       {"s": "\b\n\"\\中文é"}),
    cv("V07-array-order", "数组保序不排序", {"arr": [3, 1, 2, ["b", "a"]]}),
    cv("V08-null-kept", "null 保留(与 V08b absent 不同 canonical 不同 digest=缺省不等于 null)",
       {"a": None}),
    cv("V08b-absent", "空对象对照", {}),
    cv("V09-nested", "嵌套对象递归 canonical",
       {"outer": {"y": {"k2": None, "k1": [True, False]}, "x": 1}}),
    dv("V10-digest-frame", "digest 框架:含 op_kind+payload_schema_version,剥 transport-only from/to",
       "board.Send", "1",
       {"node_id": "n1", "rid": "r-001", "payload": {"cmd": "run"},
        "from": "zzc-a-1", "to": "zzc-b-1"}),
    dv("V10b-digest-no-transport",
       "与 V10 同 payload 无 from/to:digest 必须相同(判据=V10.expected==V10b.expected)",
       "board.Send", "1", {"node_id": "n1", "rid": "r-001", "payload": {"cmd": "run"}}),
    dv("V11-digest-op-differs",
       "同 payload 异 op_kind 得异 digest(O1 同 rid 异 op=idempotency_conflict 的判据基础)",
       "board.Terminal", "1", {"node_id": "n1", "rid": "r-001", "payload": {"cmd": "run"}}),
    {"id": "V12-reject-nan", "kind": "reject", "note": "NaN 拒", "input": "nan"},
    {"id": "V13-reject-inf", "kind": "reject", "note": "Infinity 拒", "input": "inf"},
    {"id": "V14-reject-dupkey", "kind": "reject", "note": "重复 key 拒",
     "raw_json": "{\"a\":1,\"a\":2}"},
    cv("V15-num-exp-pos-threshold",
       "ECMA 正阈值:1e20 定点(20个0),1e21 起指数 1e+21",
       {"hi20": 1e20, "hi21": 1e21}),
    cv("V16-num-exp-neg-threshold",
       "ECMA 负阈值:1e-6 定点 0.000001,1e-7 起指数 1e-7(负指数无前导0)",
       {"lo6": 1e-06, "lo7": 1e-07}),
    cv("V17-num-extremes",
       "极值与符号:double 最大/最小次正规/负小数指数/-0 归 0",
       {"max": 1.7976931348623157e+308, "min": 5e-324,
        "m": -1.5e-09, "negz": -0.0}),
]

_c15 = [v for v in vectors if v["id"] == "V15-num-exp-pos-threshold"][0]["expected_canonical"]
assert _c15 == '{"hi20":100000000000000000000,"hi21":1e+21}', _c15
_c16 = [v for v in vectors if v["id"] == "V16-num-exp-neg-threshold"][0]["expected_canonical"]
assert _c16 == '{"lo6":0.000001,"lo7":1e-7}', _c16
_c17 = [v for v in vectors if v["id"] == "V17-num-extremes"][0]["expected_canonical"]
assert _c17 == '{"m":-1.5e-9,"max":1.7976931348623157e+308,"min":5e-324,"negz":0}', _c17

_by = {v["id"]: v for v in vectors}
assert _by["V05-num-intfloat"]["expected_sha256"] == _by["V05b-num-int-equiv"]["expected_sha256"]
assert _by["V10-digest-frame"]["expected_sha256"] == _by["V10b-digest-no-transport"]["expected_sha256"]
assert _by["V11-digest-op-differs"]["expected_sha256"] != _by["V10-digest-frame"]["expected_sha256"]

out = {"digest_schema": "mcs-canonical-json-v1",
       "spec": "契约 v0.3 §1.3 F5;canonical=RFC 8785 (JCS);参考实现+校验器=jcs_vectors_check.py(同目录)",
       "note": "跨语言实现必须逐条复现 expected_canonical 与 expected_sha256;"
               "V05/V05b 必须同 digest;V10/V10b 必须同 digest;V12-V14 必须拒;"
               "V15-V17=ECMAScript Number::toString 全域阈值钉点(1e20/1e21,1e-6/1e-7,极值,-0)",
       "vectors": vectors}
with open(os.path.join(_d, "jcs-golden-vectors-v1.json"), "w") as f:
    json.dump(out, f, ensure_ascii=False, indent=1)
print("vectors written:", len(vectors))
