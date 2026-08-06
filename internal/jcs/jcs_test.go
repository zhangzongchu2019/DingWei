package jcs

import (
	"errors"
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
