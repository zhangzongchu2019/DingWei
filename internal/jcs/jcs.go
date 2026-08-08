// Package jcs 实现 **RFC 8785(JCS)的整数子集**规范序列化——不是完整 RFC 8785:
// number 仅支持整数(int64 范围),真浮点显式返错(ErrFloatUnsupported),不实现 RFC 的
// ES6 double 序列化。除 number 域收窄外,object 键序(UTF-16 code unit)/字符串转义
// (§3.2.2.2)/结构序列化与 RFC 8785 逐字一致,由官方向量(整数域)钉死。
//
// 供 M5 A-lite(assign request_hash)与 B-lite(checkpoint 内容寻址 id)共用同一套
// canonical 化——两侧引用同一实现+同一 golden 向量(testdata 交叉字节比对),避免
// "两侧各自手搓、一致地错"(shenceclaude 实现期提醒二)。
//
// fail-closed 纪律(dev56 A1):任何非法输入(浮点/invalid UTF-8/非法 Unicode 转义/
// 尾随内容/不支持类型)一律返错,绝不静默替换(U+FFFD)或吞错序列化。调用方必须
// 处理 error,不得忽略。
package jcs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

// ErrFloatUnsupported:非整 number 不在本子集(见包注)。
var ErrFloatUnsupported = errors.New("jcs: 非整浮点数不在支持子集(本平台负载无浮点;fail-loud 防静默误规范化)")

// ErrInvalidUTF8:字符串/输入字节非合法 UTF-8(含 UTF-8 编码的代理半区)——拒,不 U+FFFD 替换。
var ErrInvalidUTF8 = errors.New("jcs: invalid UTF-8(拒绝静默 U+FFFD 替换)")

// ErrTrailingData:JSON 文本在首个完整值之后还有尾随内容(第二值/标量/垃圾)。
var ErrTrailingData = errors.New("jcs: JSON 值后有尾随内容")

// ErrBadEscape:JSON 字符串内 \uXXXX 转义构成非法 Unicode(孤立代理/残缺转义)。
var ErrBadEscape = errors.New("jcs: 非法 \\u 转义(孤立代理对/残缺)")

// Canonicalize 把一个 JSON 值(Go 侧:map[string]any/[]any/string/bool/nil/整数)序列化为
// canonical 字节(整数子集,见包注)。输入若来自 json.Unmarshal(number=float64/json.Number)
// 亦支持整值。字符串(含对象键)必须为合法 UTF-8,否则 ErrInvalidUTF8。
func Canonicalize(v any) ([]byte, error) {
	var b []byte
	b, err := appendValue(b, v)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// CanonicalizeJSON 把一段 JSON 文本先解析再 canonical 化(number 用 json.Number 保原文精度)。
// fail-closed 门(dev56 A1③④):①原文必须整体合法 UTF-8;②字符串转义不得含孤立代理对;
// ③首个 JSON 值之后必须 EOF(尾随第二值/标量/垃圾=ErrTrailingData)。
func CanonicalizeJSON(data []byte) ([]byte, error) {
	v, err := decodeStrict(data)
	if err != nil {
		return nil, err
	}
	return Canonicalize(v)
}

// decodeStrict 严格解析:UTF-8 全验+转义验+EOF 验。
func decodeStrict(data []byte) (any, error) {
	if !utf8.Valid(data) {
		return nil, ErrInvalidUTF8
	}
	if err := validateStringEscapes(data); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	// EOF 二次确认:Decode 只消费首个值,尾随内容在此拒(A1③)。
	if tok, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("%w(token=%v, err=%v)", ErrTrailingData, tok, err)
	}
	return v, nil
}

// validateStringEscapes 走一遍原文,校验 JSON 字符串字面量内的 \uXXXX 转义:
// 高代理(D800-DBFF)必须紧跟 \u 低代理(DC00-DFFF),孤立/倒置/残缺=ErrBadEscape。
// (encoding/json 对孤立代理静默替换 U+FFFD——正是 A1④ 要拒的行为,故在原文层自验。)
func validateStringEscapes(data []byte) error {
	inStr := false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if !inStr {
			if c == '"' {
				inStr = true
			}
			continue
		}
		switch c {
		case '"':
			inStr = false
		case '\\':
			if i+1 >= len(data) {
				return ErrBadEscape
			}
			esc := data[i+1]
			if esc != 'u' {
				i++ // \" \\ \/ \b \f \n \r \t:跳过被转义字符(合法性由 json 解码器把关)
				continue
			}
			cp, n, err := hex4(data, i+2)
			if err != nil {
				return err
			}
			i += 1 + n // 指向 XXXX 末位
			if cp >= 0xDC00 && cp <= 0xDFFF {
				return fmt.Errorf("%w: 低代理 \\u%04x 无前导高代理", ErrBadEscape, cp)
			}
			if cp >= 0xD800 && cp <= 0xDBFF { // 高代理:后面必须紧跟 \uDC00-\uDFFF
				if i+2 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
					return fmt.Errorf("%w: 高代理 \\u%04x 后无低代理", ErrBadEscape, cp)
				}
				lo, n2, err := hex4(data, i+3)
				if err != nil {
					return err
				}
				if lo < 0xDC00 || lo > 0xDFFF {
					return fmt.Errorf("%w: 高代理 \\u%04x 后跟非低代理 \\u%04x", ErrBadEscape, cp, lo)
				}
				i += 2 + n2
			}
		}
	}
	return nil
}

// hex4 解 data[at:at+4] 为 16 进制码元;残缺/非 hex=ErrBadEscape。
func hex4(data []byte, at int) (uint32, int, error) {
	if at+4 > len(data) {
		return 0, 0, fmt.Errorf("%w: \\u 后不足 4 位 hex", ErrBadEscape)
	}
	var cp uint32
	for j := 0; j < 4; j++ {
		c := data[at+j]
		var d uint32
		switch {
		case c >= '0' && c <= '9':
			d = uint32(c - '0')
		case c >= 'a' && c <= 'f':
			d = uint32(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = uint32(c-'A') + 10
		default:
			return 0, 0, fmt.Errorf("%w: \\u 后非 hex 字符 %q", ErrBadEscape, c)
		}
		cp = cp<<4 | d
	}
	return cp, 4, nil
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
		if !utf8.ValidString(t) {
			return nil, ErrInvalidUTF8
		}
		return appendString(b, t), nil
	case json.RawMessage:
		// 已是 JSON 文本:重走严格解析(UTF-8/转义/EOF 同门,A1③),保证规范化
		// (不信任其已 canonical,也不信任其无尾随)。
		inner, err := decodeStrict(t)
		if err != nil {
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
		if !utf8.ValidString(k) {
			return nil, ErrInvalidUTF8
		}
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
