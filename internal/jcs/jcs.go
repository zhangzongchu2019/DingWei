// Package jcs 实现 RFC 8785 JSON Canonicalization Scheme(JCS)的规范序列化,
// 供 M5 A-lite(assign request_hash)与 B-lite(checkpoint 内容寻址 id)共用同一套
// canonical 化——两侧引用同一实现,由本包测试集(RFC 8785 官方向量)钉死,避免"两侧
// 各自手搓、一致地错"(shenceclaude 实现期提醒二)。
//
// 支持子集:object / array / string / bool / null / 整数(int,int64,以及整值 float64/
// json.Number)。非整 number(真浮点)显式返错——本平台负载(派发/checkpoint)不含浮点,
// 浮点出现即 bug;静默按非 RFC 8785 方式序列化正是要防的假绿,故 fail-loud 不 fail-silent。
package jcs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

// ErrFloatUnsupported:非整 number 不在本子集(见包注)。
var ErrFloatUnsupported = errors.New("jcs: 非整浮点数不在支持子集(本平台负载无浮点;fail-loud 防静默误规范化)")

// Canonicalize 把一个 JSON 值(Go 侧:map[string]any/[]any/string/bool/nil/整数)序列化为
// RFC 8785 canonical 字节。输入若来自 json.Unmarshal(number=float64/json.Number)亦支持整值。
func Canonicalize(v any) ([]byte, error) {
	var b []byte
	b, err := appendValue(b, v)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// CanonicalizeJSON 把一段 JSON 文本先解析再 canonical 化(number 用 json.Number 保原文精度)。
func CanonicalizeJSON(data []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return Canonicalize(v)
}

func appendValue(b []byte, v any) ([]byte, error) {
	switch t := v.(type) {
	case nil:
		return append(b, "null"...), nil
	case bool:
		if t {
			return append(b, "true"...), nil
		}
		return append(b, "false"...), nil
	case string:
		return appendString(b, t), nil
	case json.RawMessage:
		// 已是 JSON 文本:重解析走标准路径,保证规范化(不信任其已 canonical)。
		var inner any
		dec := json.NewDecoder(bytes.NewReader(t))
		dec.UseNumber()
		if err := dec.Decode(&inner); err != nil {
			return nil, err
		}
		return appendValue(b, inner)
	case json.Number:
		return appendNumberString(b, t.String())
	case int:
		return strconv.AppendInt(b, int64(t), 10), nil
	case int64:
		return strconv.AppendInt(b, t, 10), nil
	case int32:
		return strconv.AppendInt(b, int64(t), 10), nil
	case float64:
		return appendFloat(b, t)
	case float32:
		return appendFloat(b, float64(t))
	case map[string]any:
		return appendObject(b, t)
	case []any:
		return appendArray(b, t)
	default:
		return nil, fmt.Errorf("jcs: 不支持的类型 %T", v)
	}
}

func appendObject(b []byte, m map[string]any) ([]byte, error) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortUTF16(keys)
	b = append(b, '{')
	for i, k := range keys {
		if i > 0 {
			b = append(b, ',')
		}
		b = appendString(b, k)
		b = append(b, ':')
		var err error
		b, err = appendValue(b, m[k])
		if err != nil {
			return nil, err
		}
	}
	return append(b, '}'), nil
}

func appendArray(b []byte, a []any) ([]byte, error) {
	b = append(b, '[')
	for i, e := range a {
		if i > 0 {
			b = append(b, ',')
		}
		var err error
		b, err = appendValue(b, e)
		if err != nil {
			return nil, err
		}
	}
	return append(b, ']'), nil
}

func appendNumberString(b []byte, s string) ([]byte, error) {
	// json.Number 原文:整数直出;含 . 或 e/E(浮点)拒。
	i, err := strconv.ParseInt(s, 10, 64)
	if err == nil {
		return strconv.AppendInt(b, i, 10), nil
	}
	return nil, ErrFloatUnsupported
}

func appendFloat(b []byte, f float64) ([]byte, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil, fmt.Errorf("jcs: NaN/Inf 非法 JSON number")
	}
	if f == math.Trunc(f) && math.Abs(f) < 9.007199254740992e15 { // 整值且在安全整数范围
		return strconv.AppendInt(b, int64(f), 10), nil
	}
	return nil, ErrFloatUnsupported
}

// appendString 按 RFC 8785 §3.2.2.2 序列化 JSON 字符串:仅转义 " \ 与控制符
// (\b\t\n\f\r + 其余 U+0000..U+001F 走 \u00xx 小写);非 ASCII 原样 UTF-8 输出。
func appendString(b []byte, s string) []byte {
	b = append(b, '"')
	for _, r := range s {
		switch r {
		case '"':
			b = append(b, '\\', '"')
		case '\\':
			b = append(b, '\\', '\\')
		case '\b':
			b = append(b, '\\', 'b')
		case '\t':
			b = append(b, '\\', 't')
		case '\n':
			b = append(b, '\\', 'n')
		case '\f':
			b = append(b, '\\', 'f')
		case '\r':
			b = append(b, '\\', 'r')
		default:
			if r < 0x20 {
				b = append(b, '\\', 'u', '0', '0',
					hexLower[(r>>4)&0xf], hexLower[r&0xf])
			} else {
				b = utf8.AppendRune(b, r)
			}
		}
	}
	return append(b, '"')
}

const hexLower = "0123456789abcdef"

// sortUTF16 按 RFC 8785 §3.2.3:对象键序=键的 UTF-16 code unit 逐单元升序
// (BMP 内等同码点序;含增补面字符时按代理对区分,与朴素 rune 序不同)。
func sortUTF16(keys []string) {
	sort.Slice(keys, func(i, j int) bool {
		return lessUTF16(keys[i], keys[j])
	})
}

func lessUTF16(a, b string) bool {
	ua := utf16.Encode([]rune(a))
	ub := utf16.Encode([]rune(b))
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			return ua[i] < ub[i]
		}
	}
	return len(ua) < len(ub)
}
