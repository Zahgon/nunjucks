package js

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ParseJSON is JSON.parse without a reviver. Objects keep their keys in JavaScript order (a repeated key keeps
// its first position and takes its last value); strings may hold unpaired surrogates.
func ParseJSON(text string) (Value, error) {
	p := &jsonParser{s: text}
	p.ws()
	v, err := p.value()
	if err != nil {
		return nil, err
	}
	p.ws()
	if p.i != len(p.s) {
		return nil, p.fail()
	}
	return v, nil
}

type jsonParser struct {
	s string
	i int
}

func (p *jsonParser) fail() error {
	if p.i >= len(p.s) {
		return NewSyntaxError("Unexpected end of JSON input")
	}
	return NewSyntaxError("Unexpected token in JSON at position %d", p.i)
}

func (p *jsonParser) ws() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t' || p.s[p.i] == '\n' || p.s[p.i] == '\r') {
		p.i++
	}
}

func (p *jsonParser) value() (Value, error) {
	if p.i >= len(p.s) {
		return nil, p.fail()
	}
	switch c := p.s[p.i]; {
	case c == '{':
		p.i++
		obj := NewObject()
		p.ws()
		if p.i < len(p.s) && p.s[p.i] == '}' {
			p.i++
			return obj, nil
		}
		for {
			p.ws()
			if p.i >= len(p.s) || p.s[p.i] != '"' {
				return nil, p.fail()
			}
			k, err := p.str()
			if err != nil {
				return nil, err
			}
			p.ws()
			if p.i >= len(p.s) || p.s[p.i] != ':' {
				return nil, p.fail()
			}
			p.i++
			p.ws()
			v, err := p.value()
			if err != nil {
				return nil, err
			}
			obj.Set(k, v)
			p.ws()
			if p.i < len(p.s) && p.s[p.i] == ',' {
				p.i++
				continue
			}
			if p.i < len(p.s) && p.s[p.i] == '}' {
				p.i++
				return obj, nil
			}
			return nil, p.fail()
		}
	case c == '[':
		p.i++
		arr := &Array{Items: []Value{}}
		p.ws()
		if p.i < len(p.s) && p.s[p.i] == ']' {
			p.i++
			return arr, nil
		}
		for {
			p.ws()
			v, err := p.value()
			if err != nil {
				return nil, err
			}
			arr.Items = append(arr.Items, v)
			p.ws()
			if p.i < len(p.s) && p.s[p.i] == ',' {
				p.i++
				continue
			}
			if p.i < len(p.s) && p.s[p.i] == ']' {
				p.i++
				return arr, nil
			}
			return nil, p.fail()
		}
	case c == '"':
		return p.str()
	case c == 't' && strings.HasPrefix(p.s[p.i:], "true"):
		p.i += 4
		return true, nil
	case c == 'f' && strings.HasPrefix(p.s[p.i:], "false"):
		p.i += 5
		return false, nil
	case c == 'n' && strings.HasPrefix(p.s[p.i:], "null"):
		p.i += 4
		return Null, nil
	case c == '-' || (c >= '0' && c <= '9'):
		return p.num()
	}
	return nil, p.fail()
}

func (p *jsonParser) num() (Value, error) {
	start := p.i
	if p.s[p.i] == '-' {
		p.i++
	}
	digits := func() int {
		n := 0
		for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
			p.i++
			n++
		}
		return n
	}
	if p.i < len(p.s) && p.s[p.i] == '0' {
		p.i++
	} else if digits() == 0 {
		return nil, p.fail()
	}
	if p.i < len(p.s) && p.s[p.i] == '.' {
		p.i++
		if digits() == 0 {
			return nil, p.fail()
		}
	}
	if p.i < len(p.s) && (p.s[p.i] == 'e' || p.s[p.i] == 'E') {
		p.i++
		if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
			p.i++
		}
		if digits() == 0 {
			return nil, p.fail()
		}
	}
	return parseDecimal(p.s[start:p.i]), nil
}

func (p *jsonParser) str() (string, error) {
	p.i++
	var u []uint16
	for {
		if p.i >= len(p.s) {
			return "", NewSyntaxError("Unterminated string in JSON at position %d", p.i)
		}
		c := p.s[p.i]
		switch {
		case c == '"':
			p.i++
			return FromU16(u), nil
		case c < 0x20:
			return "", NewSyntaxError("Bad control character in string literal in JSON at position %d", p.i)
		case c == '\\':
			if p.i+1 >= len(p.s) {
				return "", p.fail()
			}
			e := p.s[p.i+1]
			p.i += 2
			switch e {
			case '"', '\\', '/':
				u = append(u, uint16(e))
			case 'b':
				u = append(u, '\b')
			case 'f':
				u = append(u, '\f')
			case 'n':
				u = append(u, '\n')
			case 'r':
				u = append(u, '\r')
			case 't':
				u = append(u, '\t')
			case 'u':
				if p.i+4 > len(p.s) {
					return "", p.fail()
				}
				v, err := strconv.ParseUint(p.s[p.i:p.i+4], 16, 16)
				if err != nil {
					return "", p.fail()
				}
				u = append(u, uint16(v))
				p.i += 4
			default:
				p.i--
				return "", p.fail()
			}
		default:
			r, size := utf8.DecodeRuneInString(p.s[p.i:])
			u = append(u, U16(string(r))...)
			p.i += size
		}
	}
}

// Stringify is JSON.stringify(v, null, space). The second result is false when the value has no JSON form
// (JSON.stringify returns undefined).
func Stringify(v Value, space Value) (string, bool, error) {
	gap := ""
	switch s := space.(type) {
	case float64:
		n := math.Min(10, math.Trunc(s))
		if n >= 1 {
			gap = strings.Repeat(" ", int(n))
		}
	case string:
		gap = Slice16(s, 0, 10)
	}
	st := &stringifier{gap: gap}
	out, ok, err := st.serialize(v, "")
	return out, ok, err
}

type stringifier struct {
	gap   string
	stack []Value
}

func (st *stringifier) serialize(v Value, indent string) (string, bool, error) {
	switch x := v.(type) {
	case nullType:
		return "null", true, nil
	case bool:
		return ToString(x), true, nil
	case string:
		return Quote(x), true, nil
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return "null", true, nil
		}
		return NumberToString(x), true, nil
	case undefinedType, *Function:
		return "", false, nil
	}
	for _, s := range st.stack {
		if s == v {
			return "", false, NewTypeError("Converting circular structure to JSON")
		}
	}
	st.stack = append(st.stack, v)
	defer func() { st.stack = st.stack[:len(st.stack)-1] }()
	inner := indent + st.gap
	var parts []string
	if a, ok := v.(*Array); ok {
		for _, it := range a.Items {
			s, ok, err := st.serialize(it, inner)
			if err != nil {
				return "", false, err
			}
			if !ok {
				s = "null"
			}
			parts = append(parts, s)
		}
		return st.wrap("[", "]", parts, indent, inner), true, nil
	}
	for _, k := range OwnKeys(v) {
		s, ok, err := st.serialize(GetKey(v, k), inner)
		if err != nil {
			return "", false, err
		}
		if !ok {
			continue
		}
		sep := ":"
		if st.gap != "" {
			sep = ": "
		}
		parts = append(parts, Quote(k)+sep+s)
	}
	return st.wrap("{", "}", parts, indent, inner), true, nil
}

func (st *stringifier) wrap(open, close string, parts []string, indent, inner string) string {
	if len(parts) == 0 {
		return open + close
	}
	if st.gap == "" {
		return open + strings.Join(parts, ",") + close
	}
	return open + "\n" + inner + strings.Join(parts, ",\n"+inner) + "\n" + indent + close
}

// EncodeURIComponent is the global encodeURIComponent.
func EncodeURIComponent(v Value) (string, error) {
	s, err := ToStringErr(v)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	runes := decodeWTF16(U16(s))
	for _, r := range runes {
		if r < 0 {
			return "", &Error{Name: "URIError", Message: "URI malformed"}
		}
		if r < 0x80 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_.!~*'()", r)) {
			b.WriteRune(r)
			continue
		}
		var buf [4]byte
		n := utf8.EncodeRune(buf[:], r)
		for _, c := range buf[:n] {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String(), nil
}
