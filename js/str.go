package js

import (
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// JavaScript strings are UTF-16. They are stored in Go strings as WTF-8: UTF-8 in which an unpaired surrogate
// code unit is written as its three-byte generalised UTF-8 form.

// U16 returns the UTF-16 code units of the JavaScript string s.
func U16(s string) []uint16 {
	out := make([]uint16, 0, len(s))
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c < 0x80:
			out = append(out, uint16(c))
			i++
		case c >= 0xE0 && c < 0xF0 && i+2 < len(s):
			r := rune(c&0x0F)<<12 | rune(s[i+1]&0x3F)<<6 | rune(s[i+2]&0x3F)
			out = append(out, uint16(r))
			i += 3
		default:
			r, size := utf8.DecodeRuneInString(s[i:])
			if r >= 0x10000 {
				a, b := utf16.EncodeRune(r)
				out = append(out, uint16(a), uint16(b))
			} else {
				out = append(out, uint16(r))
			}
			i += size
		}
	}
	return out
}

// FromU16 returns the JavaScript string made of the code units u.
func FromU16(u []uint16) string {
	var b strings.Builder
	b.Grow(len(u))
	for i := 0; i < len(u); i++ {
		c := u[i]
		switch {
		case c < 0x80:
			b.WriteByte(byte(c))
		case c >= 0xD800 && c < 0xDC00 && i+1 < len(u) && u[i+1] >= 0xDC00 && u[i+1] < 0xE000:
			b.WriteRune(utf16.DecodeRune(rune(c), rune(u[i+1])))
			i++
		case c >= 0xD800 && c < 0xE000:
			b.WriteByte(0xE0 | byte(c>>12))
			b.WriteByte(0x80 | byte(c>>6)&0x3F)
			b.WriteByte(0x80 | byte(c)&0x3F)
		default:
			b.WriteRune(rune(c))
		}
	}
	return b.String()
}

// Len16 returns the JavaScript length (UTF-16 code units) of s.
func Len16(s string) int {
	n := 0
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c < 0x80:
			i++
			n++
		case c >= 0xF0:
			i += 4
			n += 2
		case c >= 0xE0:
			i += 3
			n++
		default:
			i += 2
			n++
		}
	}
	return n
}

// Slice16 returns the code units [from, to) of s.
func Slice16(s string, from, to int) string {
	u := U16(s)
	if from < 0 {
		from = 0
	}
	if to > len(u) {
		to = len(u)
	}
	if from >= to {
		return ""
	}
	return FromU16(u[from:to])
}

// FromCodeUnit returns the one-unit string holding c.
func FromCodeUnit(c uint16) string { return FromU16([]uint16{c}) }

// ToUTF8 converts the JavaScript string s to well-formed UTF-8, replacing unpaired surrogates with U+FFFD, as
// Node.js does when it writes a string to a stream.
func ToUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	u := U16(s)
	for i := 0; i < len(u); i++ {
		c := u[i]
		switch {
		case c >= 0xD800 && c < 0xDC00 && i+1 < len(u) && u[i+1] >= 0xDC00 && u[i+1] < 0xE000:
			b.WriteRune(utf16.DecodeRune(rune(c), rune(u[i+1])))
			i++
		case c >= 0xD800 && c < 0xE000:
			b.WriteRune(utf8.RuneError)
		default:
			b.WriteRune(rune(c))
		}
	}
	return b.String()
}

// FromUTF8 decodes bytes as Node.js decodes 'utf-8' text: invalid sequences become U+FFFD.
func FromUTF8(data []byte) string {
	if utf8.Valid(data) {
		return string(data)
	}
	return strings.ToValidUTF8(string(data), "�")
}

// CompareStrings compares two JavaScript strings by UTF-16 code units.
func CompareStrings(a, b string) int {
	if a == b {
		return 0
	}
	ua, ub := U16(a), U16(b)
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			if ua[i] < ub[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(ua) < len(ub):
		return -1
	case len(ua) > len(ub):
		return 1
	}
	return 0
}

// IndexOf16 is String.prototype.indexOf in code units, searching from position from.
func IndexOf16(s, sub string, from int) int {
	u, v := U16(s), U16(sub)
	if from < 0 {
		from = 0
	}
	if from > len(u) {
		from = len(u)
	}
	for i := from; i+len(v) <= len(u); i++ {
		if eq16(u[i:i+len(v)], v) {
			return i
		}
	}
	return -1
}

// LastIndexOf16 is String.prototype.lastIndexOf in code units, searching backwards from position from.
func LastIndexOf16(s, sub string, from int) int {
	u, v := U16(s), U16(sub)
	if from > len(u)-len(v) {
		from = len(u) - len(v)
	}
	for i := from; i >= 0; i-- {
		if eq16(u[i:i+len(v)], v) {
			return i
		}
	}
	return -1
}

func eq16(a, b []uint16) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// IsJSWhitespace reports whether c is WhiteSpace or LineTerminator in ECMAScript (what String.prototype.trim
// and the \s regular expression class remove).
func IsJSWhitespace(c uint16) bool {
	switch c {
	case 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x20, 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return c >= 0x2000 && c <= 0x200A
}

// TrimJS trims ECMAScript whitespace from the selected ends of s.
func TrimJS(s string, start, end bool) string {
	u := U16(s)
	i, j := 0, len(u)
	if start {
		for i < j && IsJSWhitespace(u[i]) {
			i++
		}
	}
	if end {
		for j > i && IsJSWhitespace(u[j-1]) {
			j--
		}
	}
	return FromU16(u[i:j])
}

// U16Points splits s into its code points, as iterating a JavaScript string does; an unpaired surrogate is its
// own element.
func U16Points(s string) []Value {
	var out []Value
	for _, r := range decodeWTF16(U16(s)) {
		if r < 0 {
			out = append(out, FromCodeUnit(uint16(-r)))
		} else {
			out = append(out, string(r))
		}
	}
	return out
}
