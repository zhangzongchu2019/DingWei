#!/usr/bin/env python3
"""mcs-canonical-json-v1 参考实现+golden vectors 校验器(契约 v0.3 §1.3 F5)。

canonical 形式=RFC 8785 (JCS):
  - object key 按 UTF-16 code units 排序(非 code point;补充平面字符经代理对参与排序);
  - 字符串最小转义(" 与 \\ 转义;控制符用 \\b \\t \\n \\f \\r 简写,其余 \\u00xx 小写;非 ASCII 字面输出);
  - 数字按 ECMAScript Number::toString 完整序列化(整值 float 输出为整数;-0 输出 0;
    定点区间=10^-6≤|x|<10^21,界外指数记法 d[.ddd]e±N;最短往返数字;禁 NaN/Infinity);
  - 数组保序不排序;无空白。
digest 输入框架(O1/F5):
  {"digest_schema":"mcs-canonical-json-v1","op_kind":<op>,
   "payload":<payload 剥除 transport-only 字段 from/to>,"payload_schema_version":<v>}
payload_digest = sha256(canonical JSON bytes, UTF-8) 十六进制小写。

用法: jcs_vectors_check.py [vectors.json]   全过 exit 0,任一失败 exit 1。
Go/其他语言实现必须逐条复现本文件 vectors 的 canonical 与 digest。
"""
import hashlib
import json
import math
import sys

TRANSPORT_ONLY = ("from", "to")


def _esc(s):
    out = []
    for ch in s:
        if ch == '"':
            out.append('\\"')
        elif ch == "\\":
            out.append("\\\\")
        elif ch == "\b":
            out.append("\\b")
        elif ch == "\t":
            out.append("\\t")
        elif ch == "\n":
            out.append("\\n")
        elif ch == "\f":
            out.append("\\f")
        elif ch == "\r":
            out.append("\\r")
        elif ord(ch) < 0x20:
            out.append("\\u%04x" % ord(ch))
        else:
            out.append(ch)
    return '"' + "".join(out) + '"'


def _num(x):
    if isinstance(x, bool):  # bool 是 int 子类,先拦
        return "true" if x else "false"
    if isinstance(x, int):
        return str(x)
    if math.isnan(x) or math.isinf(x):
        raise ValueError("NaN/Infinity 禁入 canonical JSON")
    if x == 0:
        return "0"  # 含 -0.0(ECMAScript String(-0)="0")
    if x == int(x) and abs(x) < 2**53:
        return str(int(x))  # 1.0 → 1(ECMAScript 整值)
    # ECMA-262 Number::toString(radix 10) 完整实现。
    # CPython repr=IEEE754 double 最短往返数字,与 ECMAScript 同(均最短且最近);
    # 差异仅在记法阈值,以下按 ECMA 规则重排。
    r = repr(x)
    mant, _, e = r.partition("e")
    exp = int(e) if e else 0
    neg = mant.startswith("-")
    if neg:
        mant = mant[1:]
    ip, _, fp = mant.partition(".")
    digits = (ip + fp).lstrip("0")
    last_exp = exp - len(fp)  # 最末位数字的十进制位权
    stripped = digits.rstrip("0")
    last_exp += len(digits) - len(stripped)
    digits = stripped
    k = len(digits)  # 有效数字个数
    n = last_exp + k  # 小数点应在位置:value = 0.digits × 10^n
    if k <= n <= 21:
        s = digits + "0" * (n - k)
    elif 0 < n <= 21:
        s = digits[:n] + "." + digits[n:]
    elif -6 < n <= 0:
        s = "0." + "0" * (-n) + digits
    else:
        tail = "e" + ("+" if n - 1 >= 0 else "-") + str(abs(n - 1))
        s = (digits if k == 1 else digits[0] + "." + digits[1:]) + tail
    return ("-" if neg else "") + s


def canonical(o):
    if o is None:
        return "null"
    if isinstance(o, bool):
        return "true" if o else "false"
    if isinstance(o, (int, float)):
        return _num(o)
    if isinstance(o, str):
        return _esc(o)
    if isinstance(o, list):
        return "[" + ",".join(canonical(v) for v in o) + "]"
    if isinstance(o, dict):
        items = sorted(o.items(), key=lambda kv: kv[0].encode("utf-16-be"))
        return "{" + ",".join(_esc(k) + ":" + canonical(v) for k, v in items) + "}"
    raise TypeError(type(o))


def digest_input(op_kind, payload_schema_version, payload):
    p = {k: v for k, v in payload.items() if k not in TRANSPORT_ONLY}
    return {"digest_schema": "mcs-canonical-json-v1", "op_kind": op_kind,
            "payload": p, "payload_schema_version": payload_schema_version}


def payload_digest(op_kind, payload_schema_version, payload):
    c = canonical(digest_input(op_kind, payload_schema_version, payload))
    return hashlib.sha256(c.encode("utf-8")).hexdigest()


class _DupKeyGuard(dict):
    def __init__(self, pairs):
        seen = set()
        for k, _ in pairs:
            if k in seen:
                raise ValueError("duplicate key: %r" % k)
            seen.add(k)
        super().__init__(pairs)


def main():
    path = sys.argv[1] if len(sys.argv) > 1 else \
        __file__.rsplit("/", 1)[0] + "/jcs-golden-vectors-v1.json"
    vecs = json.load(open(path))
    fails = 0
    for v in vecs["vectors"]:
        vid = v["id"]
        try:
            if v["kind"] == "canonical":
                got = canonical(v["input"])
                ok = got == v["expected_canonical"]
                got2 = hashlib.sha256(got.encode("utf-8")).hexdigest()
                ok = ok and got2 == v["expected_sha256"]
            elif v["kind"] == "digest":
                got = payload_digest(v["op_kind"], v["payload_schema_version"], v["input"])
                ok = got == v["expected_sha256"]
            elif v["kind"] == "reject":
                try:
                    if v.get("raw_json") is not None:
                        json.loads(v["raw_json"], object_pairs_hook=_DupKeyGuard)
                        canonical(json.loads(v["raw_json"]))
                    else:
                        inp = v["input"]
                        if isinstance(inp, str):  # NaN/Infinity 以字面名传入
                            inp = float(inp)
                        canonical(inp)
                    ok = False  # 本应拒
                except (ValueError, TypeError):
                    ok = True
            else:
                raise ValueError("unknown kind")
        except Exception as e:  # noqa: BLE001 校验器对坏 vector 报错不崩
            print("FAIL %s (exception: %s)" % (vid, e))
            fails += 1
            continue
        if ok:
            print("PASS %s" % vid)
        else:
            print("FAIL %s" % vid)
            fails += 1
    print("%d/%d PASS" % (len(vecs["vectors"]) - fails, len(vecs["vectors"])))
    sys.exit(1 if fails else 0)


if __name__ == "__main__":
    main()
