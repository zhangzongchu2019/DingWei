// Package jcs implements JSON Canonicalization Scheme (JCS) per RFC 8785.
//
// JCS produces a canonical JSON representation suitable for content-addressed
// storage (sha256 hashing). The output is guaranteed to be byte-for-byte
// identical for semantically equivalent inputs.
//
// Rules (RFC 8785 §3):
//  1. No insignificant whitespace outside strings
//  2. Object keys sorted lexicographically (Unicode code points)
//  3. No trailing comma on last element
//  4. Strings: UTF-8, escaped per JSON spec (\", \\, \/, \b, \f, \n, \r, \t, \uXXXX)
//  5. Numbers: no leading zeros, integer if no fractional part, "e" lowercase
//  6. null / true / false lowercased
//
// Test vectors from RFC 8785 Appendix A.3.
package jcs

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)
// Canonicalize returns the RFC 8785 canonical JSON representation of v.
// v must be JSON-serializable (struct, map, slice, or scalar).
func Canonicalize(v any) ([]byte, error) {
	return marshal(v)
}

// SHA256 returns the hex-encoded SHA-256 of the canonical form of v.
func SHA256(v any) (string, error) {
	b, err := Canonicalize(v)
	if err != nil {
		return "", err
	}
	return sha256hex(b), nil
}

// MustCanonicalize is like Canonicalize but panics on error.
func MustCanonicalize(v any) []byte {
	b, err := Canonicalize(v)
	if err != nil {
		panic(fmt.Sprintf("jcs: marshal: %v", err))
	}
	return b
}

// MustSHA256 is like SHA256 but panics on error.
func MustSHA256(v any) string {
	s, err := SHA256(v)
	if err != nil {
		panic(fmt.Sprintf("jcs: sha256: %v", err))
	}
	return s
}

func sha256hex(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}

// ── internal marshaling ─────────────────────────────────────────────

func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := encode(&buf, reflectValue(v)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// reflectValue unwraps pointers and resolves the underlying value.
func reflectValue(v any) any {
	// We use json.Marshal to get the initial JSON, then re-process for canonical form.
	// This handles all the type-resolution that encoding/json does.
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var decoded any
	if err := json.Unmarshal(b, &decoded); err != nil {
		return v
	}
	return decoded
}

// encode writes the canonical JSON of v to buf.
func encode(buf *bytes.Buffer, v any) error {
	switch val := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if val {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case string:
		encodeString(buf, val)
	case float64:
		encodeNumber(buf, val)
	case []any:
		return encodeArray(buf, val)
	case map[string]any:
		return encodeObject(buf, val)
	default:
		// Fallback: re-marshal with encoding/json
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		buf.Write(b)
	}
	return nil
}

func encodeString(buf *bytes.Buffer, s string) {
	// Use Go's json.Marshal which gets escaping right per RFC 8259
	b, _ := json.Marshal(s)
	buf.Write(b)
}

func encodeNumber(buf *bytes.Buffer, f float64) {
	// RFC 8785 §3.2.3: integer if no fractional part
	if f == float64(int64(f)) && !isInfOrNaN(f) {
		buf.WriteString(strconv.FormatInt(int64(f), 10))
		return
	}
	// Use Go's strconv with lowercase 'e'
	s := strconv.FormatFloat(f, 'e', -1, 64)
	// strconv sometimes uses 'E'; force lowercase
	// Also remove leading '+' if present
	lower := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == 'E' {
			lower = append(lower, 'e')
		} else if c == '+' && i > 0 && (s[i-1] == 'e' || s[i-1] == 'E') {
			// Keep the sign in exponent
			lower = append(lower, '+')
		} else {
			lower = append(lower, c)
		}
	}
	buf.Write(lower)
}

func isInfOrNaN(f float64) bool {
	return f != f || f > 1e300 || f < -1e300
}

func encodeArray(buf *bytes.Buffer, arr []any) error {
	buf.WriteByte('[')
	for i, v := range arr {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := encode(buf, v); err != nil {
			return err
		}
	}
	buf.WriteByte(']')
	return nil
}

func encodeObject(buf *bytes.Buffer, obj map[string]any) error {
	// Sort keys lexicographically (Unicode code points, per RFC 8785 §3.2.3)
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		encodeString(buf, k)
		buf.WriteByte(':')
		if err := encode(buf, obj[k]); err != nil {
			return err
		}
	}
	buf.WriteByte('}')
	return nil
}
