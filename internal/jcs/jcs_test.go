package jcs

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

// RFC 8785 canonical 子集向量(本平台负载:object/array/string/bool/null/整数)。
// 覆盖:键序(含 UTF-16 vs 码点区分性)、字符串转义、非 ASCII 原样、整数、嵌套、浮点拒绝。
func TestCanonicalizeVectors(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"键字典序", map[string]any{"b": int64(1), "a": int64(2)}, `{"a":2,"b":1}`},
		{"非ASCII键原样+短于长", map[string]any{"ü": int64(1), "a": int64(2)}, `{"a":2,"ü":1}`},
		{"字符串转义", "a\nb\"c\\d\te", `"a\nb\"c\\d\te"`},
		{"控制符\\u00xx小写", "\x00\x1f", "\"\\u0000\\u001f\""},
		{"非ASCII值原样UTF8", "é€", `"é€"`},
		{"整数负", map[string]any{"x": int64(-5)}, `{"x":-5}`},
		{"bool/null/数组嵌套", map[string]any{"z": []any{int64(1), "two", true, nil, false}}, `{"z":[1,"two",true,null,false]}`},
		{"空对象空数组", map[string]any{"o": map[string]any{}, "a": []any{}}, `{"a":[],"o":{}}`},
	}
	for _, c := range cases {
		got, err := Canonicalize(c.in)
		if err != nil {
			t.Fatalf("%s: err %v", c.name, err)
		}
		if string(got) != c.want {
			t.Fatalf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

// ★RFC 8785 §3.2.3 关键:键序按 UTF-16 code unit,非 Unicode 码点。
// 区分性向量:U+FFFD(BMP,UTF-16=0xFFFD) vs U+10000(增补面,UTF-16 代理对高位=0xD800)。
// 码点序:0xFFFD < 0x10000 → "�" 在前;UTF-16 序:0xD800 < 0xFFFD → "\U00010000" 在前。
// 正确实现(UTF-16)必把增补面键排在 U+FFFD 之前——朴素码点/rune 序会排反,此例钉死。
func TestKeyOrderingUTF16NotCodepoint(t *testing.T) {
	in := map[string]any{"�": int64(1), "\U00010000": int64(2)}
	got, err := Canonicalize(in)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"\U00010000\":2,\"�\":1}" // 增补面键在前(UTF-16 0xD800 < 0xFFFD)
	if string(got) != want {
		t.Fatalf("UTF-16 键序错(疑按码点排):\n got %q\nwant %q", got, want)
	}
}

// 浮点显式拒绝(fail-loud,非静默误规范化)——防两侧手搓一致地错的根因。
func TestFloatRejected(t *testing.T) {
	for _, v := range []any{1.5, float64(3.14), map[string]any{"x": 2.5}} {
		if _, err := Canonicalize(v); !errors.Is(err, ErrFloatUnsupported) {
			t.Fatalf("浮点 %v 应 ErrFloatUnsupported,got %v", v, err)
		}
	}
	// 整值 float64(json.Unmarshal 常态)放行为整数。
	got, err := Canonicalize(map[string]any{"g": float64(7)})
	if err != nil || string(got) != `{"g":7}` {
		t.Fatalf("整值float64应当整数: %q %v", got, err)
	}
}

// CanonicalizeJSON:json.Number 保精度,整数走整数路径,浮点拒。
func TestCanonicalizeJSON(t *testing.T) {
	got, err := CanonicalizeJSON([]byte(`{"b":1,"a":"x","c":[true,null]}`))
	if err != nil || string(got) != `{"a":"x","b":1,"c":[true,null]}` {
		t.Fatalf("JSON canonical: %q %v", got, err)
	}
	if _, err := CanonicalizeJSON([]byte(`{"f":1.5}`)); !errors.Is(err, ErrFloatUnsupported) {
		t.Fatalf("JSON 浮点应拒: %v", err)
	}
}

// ---- R3(dev56 R2-1)新增:官方向量 golden 文件 + fail-closed 门负例 ----

// TestGoldenVectors:testdata/golden.jsonl=RFC 8785 官方向量(整数域,含 §3.2.3 键序原例:
// 😀 代理对 D83D<FB33 须排 דּ 前=UTF-16 序判别点)。期望值由独立 Python 实现生成
// (UTF-16BE 字节序+最小转义),与本包交叉字节比对;B-lite 侧同引本文件(A/B 防"一致地错")。
func TestGoldenVectors(t *testing.T) {
	f, err := os.Open("testdata/golden.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var v struct {
			Name         string `json:"name"`
			Input        string `json:"input"`
			CanonicalB64 string `json:"canonical_b64"`
		}
		if err := json.Unmarshal(line, &v); err != nil {
			t.Fatal(err)
		}
		want, err := base64.StdEncoding.DecodeString(v.CanonicalB64)
		if err != nil {
			t.Fatalf("%s: b64 %v", v.Name, err)
		}
		got, err := CanonicalizeJSON([]byte(v.Input))
		if err != nil {
			t.Fatalf("%s: %v", v.Name, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s:\n got %q\nwant %q", v.Name, got, want)
		}
		n++
	}
	if n < 4 {
		t.Fatalf("golden 向量数 %d < 4(文件残缺?)", n)
	}
}

// TestTrailingRejected:首值后尾随第二值/标量/垃圾=ErrTrailingData(A1③)。
func TestTrailingRejected(t *testing.T) {
	for _, s := range []string{`{} {}`, `{"a":1} 2`, `1 1`, `[1] x`, `{"a":1}{"b":2}`, "true false"} {
		if _, err := CanonicalizeJSON([]byte(s)); !errors.Is(err, ErrTrailingData) {
			t.Fatalf("%q 应 ErrTrailingData, got %v", s, err)
		}
	}
	// 尾随空白=合法(不是内容)。
	if _, err := CanonicalizeJSON([]byte("{\"a\":1}  \n")); err != nil {
		t.Fatalf("尾随空白应合法: %v", err)
	}
	// RawMessage 内嵌路径同门。
	if _, err := Canonicalize(map[string]any{"r": json.RawMessage(`1 2`)}); !errors.Is(err, ErrTrailingData) {
		t.Fatal("RawMessage 尾随应拒")
	}
}

// TestInvalidUTF8Rejected:原文/Go 字符串/键 invalid UTF-8=ErrInvalidUTF8,不 U+FFFD 替换(A1④)。
func TestInvalidUTF8Rejected(t *testing.T) {
	if _, err := CanonicalizeJSON([]byte("{\"a\":\"\xff\xfe\"}")); !errors.Is(err, ErrInvalidUTF8) {
		t.Fatalf("原文 invalid UTF-8 应拒, got %v", err)
	}
	if _, err := Canonicalize("\xed\xa0\x80"); !errors.Is(err, ErrInvalidUTF8) { // UTF-8 编码的代理半区
		t.Fatal("Go 字符串 UTF-8 代理半区应拒")
	}
	if _, err := Canonicalize(map[string]any{"\xff": int64(1)}); !errors.Is(err, ErrInvalidUTF8) {
		t.Fatal("对象键 invalid UTF-8 应拒")
	}
}

// TestLoneSurrogateEscapeRejected:\uXXXX 孤立/倒置/残缺代理=ErrBadEscape
// (encoding/json 会静默 U+FFFD——A1④ 明拒,原文层自验)。
func TestLoneSurrogateEscapeRejected(t *testing.T) {
	bad := []string{
		`{"a":"\ud800"}`,        // 孤立高代理
		`{"a":"\udc00"}`,        // 孤立低代理
		`{"a":"\ud800x"}`,       // 高代理后非转义
		`{"a":"\ud800\ud800"}`,  // 高高
		`{"a":"\udfff\ud83d"}`,  // 倒置
		`{"a":"\ud83dA"}`,  // 高代理后非低代理
		`{"a":"\u12"}`,          // 残缺 hex
		`{"a":"\uZZZZ"}`,        // 非 hex
	}
	for _, s := range bad {
		if _, err := CanonicalizeJSON([]byte(s)); !errors.Is(err, ErrBadEscape) {
			t.Fatalf("%q 应 ErrBadEscape, got %v", s, err)
		}
	}
	// 合法代理对(😀)+合法 \u 转义放行。
	if _, err := CanonicalizeJSON([]byte(`{"a":"😀A"}`)); err != nil {
		t.Fatalf("合法代理对应放行: %v", err)
	}
	// 字符串外的 \u 形字面(如键外空白)不受影响;转义引号内嵌不误判字符串边界。
	if _, err := CanonicalizeJSON([]byte(`{"a\"b":"c\\"}`)); err != nil {
		t.Fatalf("转义引号/反斜杠边界: %v", err)
	}
}
