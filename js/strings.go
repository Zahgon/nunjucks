package js

import (
	"math"
	"strconv"
	"strings"
)

// thisString is RequireObjectCoercible(this) followed by ToString, as every String.prototype method starts.
func thisString(this Value, name string) (string, error) {
	if IsNullish(this) {
		return "", NewTypeError("String.prototype.%s called on null or undefined", name)
	}
	return ToStringErr(this)
}

func stringMethods() map[string]method {
	m := map[string]method{}
	for _, name := range []string{"constructor", "localeCompare", "matchAll", "normalize"} {
		m[name] = unsupported(name)
	}
	str := func(name string, f func(s string, args []Value) (Value, error)) {
		m[name] = func(this Value, args []Value) (Value, error) {
			s, err := thisString(this, name)
			if err != nil {
				return nil, err
			}
			return f(s, args)
		}
	}
	str("toString", func(s string, _ []Value) (Value, error) { return s, nil })
	str("valueOf", func(s string, _ []Value) (Value, error) { return s, nil })
	str("toUpperCase", func(s string, _ []Value) (Value, error) { return UpperCase(s), nil })
	str("toLocaleUpperCase", func(s string, _ []Value) (Value, error) { return UpperCase(s), nil })
	str("toLowerCase", func(s string, _ []Value) (Value, error) { return LowerCase(s), nil })
	str("toLocaleLowerCase", func(s string, _ []Value) (Value, error) { return LowerCase(s), nil })
	str("trim", func(s string, _ []Value) (Value, error) { return TrimJS(s, true, true), nil })
	str("trimStart", func(s string, _ []Value) (Value, error) { return TrimJS(s, true, false), nil })
	str("trimLeft", func(s string, _ []Value) (Value, error) { return TrimJS(s, true, false), nil })
	str("trimEnd", func(s string, _ []Value) (Value, error) { return TrimJS(s, false, true), nil })
	str("trimRight", func(s string, _ []Value) (Value, error) { return TrimJS(s, false, true), nil })
	str("at", func(s string, args []Value) (Value, error) {
		u := U16(s)
		f, err := ToIntegerOrInfinity(Arg(args, 0))
		if err != nil {
			return nil, err
		}
		if f < 0 {
			f += float64(len(u))
		}
		if f < 0 || f >= float64(len(u)) {
			return Undefined, nil
		}
		return FromCodeUnit(u[int(f)]), nil
	})
	str("charAt", func(s string, args []Value) (Value, error) {
		u := U16(s)
		f, err := ToIntegerOrInfinity(Arg(args, 0))
		if err != nil {
			return nil, err
		}
		if f < 0 || f >= float64(len(u)) {
			return "", nil
		}
		return FromCodeUnit(u[int(f)]), nil
	})
	str("charCodeAt", func(s string, args []Value) (Value, error) {
		u := U16(s)
		f, err := ToIntegerOrInfinity(Arg(args, 0))
		if err != nil {
			return nil, err
		}
		if f < 0 || f >= float64(len(u)) {
			return math.NaN(), nil
		}
		return float64(u[int(f)]), nil
	})
	str("codePointAt", func(s string, args []Value) (Value, error) {
		u := U16(s)
		f, err := ToIntegerOrInfinity(Arg(args, 0))
		if err != nil {
			return nil, err
		}
		if f < 0 || f >= float64(len(u)) {
			return Undefined, nil
		}
		i := int(f)
		c := u[i]
		if c >= 0xD800 && c < 0xDC00 && i+1 < len(u) && u[i+1] >= 0xDC00 && u[i+1] < 0xE000 {
			return float64((int(c)-0xD800)<<10 + int(u[i+1]) - 0xDC00 + 0x10000), nil
		}
		return float64(c), nil
	})
	str("concat", func(s string, args []Value) (Value, error) {
		var b strings.Builder
		b.WriteString(s)
		for _, a := range args {
			t, err := ToStringErr(a)
			if err != nil {
				return nil, err
			}
			b.WriteString(t)
		}
		return b.String(), nil
	})
	str("includes", func(s string, args []Value) (Value, error) {
		if _, ok := Arg(args, 0).(*RegExp); ok {
			return nil, NewTypeError("First argument to String.prototype.includes must not be a regular expression")
		}
		sub, err := ToStringErr(Arg(args, 0))
		if err != nil {
			return nil, err
		}
		from, err := clampPos(Arg(args, 1), Len16(s), 0)
		if err != nil {
			return nil, err
		}
		return IndexOf16(s, sub, from) >= 0, nil
	})
	str("startsWith", func(s string, args []Value) (Value, error) {
		if _, ok := Arg(args, 0).(*RegExp); ok {
			return nil, NewTypeError("First argument to String.prototype.startsWith must not be a regular expression")
		}
		sub, err := ToStringErr(Arg(args, 0))
		if err != nil {
			return nil, err
		}
		from, err := clampPos(Arg(args, 1), Len16(s), 0)
		if err != nil {
			return nil, err
		}
		u, v := U16(s), U16(sub)
		return from+len(v) <= len(u) && eq16(u[from:from+len(v)], v), nil
	})
	str("endsWith", func(s string, args []Value) (Value, error) {
		if _, ok := Arg(args, 0).(*RegExp); ok {
			return nil, NewTypeError("First argument to String.prototype.endsWith must not be a regular expression")
		}
		sub, err := ToStringErr(Arg(args, 0))
		if err != nil {
			return nil, err
		}
		end, err := clampPos(Arg(args, 1), Len16(s), Len16(s))
		if err != nil {
			return nil, err
		}
		u, v := U16(s), U16(sub)
		start := end - len(v)
		return start >= 0 && eq16(u[start:end], v), nil
	})
	str("indexOf", func(s string, args []Value) (Value, error) {
		sub, err := ToStringErr(Arg(args, 0))
		if err != nil {
			return nil, err
		}
		from, err := clampPos(Arg(args, 1), Len16(s), 0)
		if err != nil {
			return nil, err
		}
		return float64(IndexOf16(s, sub, from)), nil
	})
	str("lastIndexOf", func(s string, args []Value) (Value, error) {
		sub, err := ToStringErr(Arg(args, 0))
		if err != nil {
			return nil, err
		}
		from := Len16(s)
		if p := Arg(args, 1); !IsUndefined(p) {
			n, err := ToNumber(p)
			if err != nil {
				return nil, err
			}
			if !math.IsNaN(n) {
				from = int(math.Max(0, math.Min(math.Trunc(n), float64(Len16(s)))))
			}
		}
		return float64(LastIndexOf16(s, sub, from)), nil
	})
	pad := func(atStart bool) func(s string, args []Value) (Value, error) {
		return func(s string, args []Value) (Value, error) {
			l, err := ToIntegerOrInfinity(Arg(args, 0))
			if err != nil {
				return nil, err
			}
			fill := " "
			if f := Arg(args, 1); !IsUndefined(f) {
				if fill, err = ToStringErr(f); err != nil {
					return nil, err
				}
			}
			n := Len16(s)
			if l <= float64(n) || fill == "" {
				return s, nil
			}
			if l > 1<<29 {
				return nil, NewRangeError("Invalid string length")
			}
			need := int(l) - n
			fu := U16(fill)
			var p []uint16
			for len(p) < need {
				p = append(p, fu...)
			}
			ps := FromU16(p[:need])
			if atStart {
				return ps + s, nil
			}
			return s + ps, nil
		}
	}
	str("padStart", pad(true))
	str("padEnd", pad(false))
	str("repeat", func(s string, args []Value) (Value, error) {
		n, err := ToIntegerOrInfinity(Arg(args, 0))
		if err != nil {
			return nil, err
		}
		if n < 0 || math.IsInf(n, 1) {
			return nil, NewRangeError("Invalid count value: %s", NumberToString(Num(Arg(args, 0))))
		}
		if s == "" || n == 0 {
			return "", nil
		}
		if float64(Len16(s))*n > 1<<29-24 {
			return nil, NewRangeError("Invalid string length")
		}
		return strings.Repeat(s, int(n)), nil
	})
	str("slice", func(s string, args []Value) (Value, error) {
		n := Len16(s)
		from, err := relIndex(Arg(args, 0), n, 0)
		if err != nil {
			return nil, err
		}
		to, err := relIndex(Arg(args, 1), n, n)
		if err != nil {
			return nil, err
		}
		return Slice16(s, from, to), nil
	})
	str("substring", func(s string, args []Value) (Value, error) {
		n := Len16(s)
		from, err := clampPos(Arg(args, 0), n, 0)
		if err != nil {
			return nil, err
		}
		to, err := clampPos(Arg(args, 1), n, n)
		if err != nil {
			return nil, err
		}
		if from > to {
			from, to = to, from
		}
		return Slice16(s, from, to), nil
	})
	str("substr", func(s string, args []Value) (Value, error) {
		n := Len16(s)
		start, err := relIndex(Arg(args, 0), n, 0)
		if err != nil {
			return nil, err
		}
		length := float64(n - start)
		if l := Arg(args, 1); !IsUndefined(l) {
			if length, err = ToIntegerOrInfinity(l); err != nil {
				return nil, err
			}
		}
		end := math.Min(float64(start)+math.Max(length, 0), float64(n))
		return Slice16(s, start, int(end)), nil
	})
	str("split", func(s string, args []Value) (Value, error) { return Split(s, Arg(args, 0), Arg(args, 1)) })
	str("replace", func(s string, args []Value) (Value, error) {
		return Replace(s, Arg(args, 0), Arg(args, 1), false)
	})
	str("replaceAll", func(s string, args []Value) (Value, error) {
		return Replace(s, Arg(args, 0), Arg(args, 1), true)
	})
	str("search", func(s string, args []Value) (Value, error) {
		re, err := toRegExp(Arg(args, 0))
		if err != nil {
			return nil, err
		}
		caps := re.Search(U16(s), 0, re.sticky)
		if caps == nil {
			return -1.0, nil
		}
		return float64(caps[0]), nil
	})
	str("match", func(s string, args []Value) (Value, error) {
		re, err := toRegExp(Arg(args, 0))
		if err != nil {
			return nil, err
		}
		if !re.global {
			return re.Exec(s)
		}
		re.LastIndex = 0.0
		out := &Array{}
		for {
			caps, err := re.execRaw(s)
			if err != nil {
				return nil, err
			}
			if caps == nil {
				break
			}
			out.Items = append(out.Items, FromU16(U16(s)[caps[0]:caps[1]]))
			if caps[0] == caps[1] {
				re.LastIndex = float64(caps[1] + 1)
			}
		}
		if len(out.Items) == 0 {
			return Null, nil
		}
		return out, nil
	})
	str("isWellFormed", func(s string, _ []Value) (Value, error) { return ToUTF8(s) == s, nil })
	str("toWellFormed", func(s string, _ []Value) (Value, error) { return ToUTF8(s), nil })
	html := func(name, tag, attr string) {
		str(name, func(s string, args []Value) (Value, error) {
			open := "<" + tag
			if attr != "" {
				v, err := ToStringErr(Arg(args, 0))
				if err != nil {
					return nil, err
				}
				open += " " + attr + "=\"" + strings.ReplaceAll(v, "\"", "&quot;") + "\""
			}
			return open + ">" + s + "</" + tag + ">", nil
		})
	}
	html("anchor", "a", "name")
	html("big", "big", "")
	html("blink", "blink", "")
	html("bold", "b", "")
	html("fixed", "tt", "")
	html("fontcolor", "font", "color")
	html("fontsize", "font", "size")
	html("italics", "i", "")
	html("link", "a", "href")
	html("small", "small", "")
	html("strike", "strike", "")
	html("sub", "sub", "")
	html("sup", "sup", "")
	return m
}

// clampPos converts a position argument to an integer clamped to [0, n].
func clampPos(v Value, n int, def int) (int, error) {
	if IsUndefined(v) {
		return def, nil
	}
	f, err := ToIntegerOrInfinity(v)
	if err != nil {
		return 0, err
	}
	return int(math.Max(0, math.Min(f, float64(n)))), nil
}

func toRegExp(v Value) (*RegExp, error) {
	if re, ok := v.(*RegExp); ok {
		return re, nil
	}
	src := ""
	if !IsUndefined(v) {
		s, err := ToStringErr(v)
		if err != nil {
			return nil, err
		}
		src = s
	}
	return NewRegExp(src, "")
}

// Split is String.prototype.split.
func Split(s string, sepV, limitV Value) (Value, error) {
	limit := uint32(math.MaxUint32)
	if !IsUndefined(limitV) {
		n, err := ToNumber(limitV)
		if err != nil {
			return nil, err
		}
		limit = uint32(ToInt32(n))
	}
	if re, ok := sepV.(*RegExp); ok {
		return splitRegExp(s, re, limit)
	}
	out := &Array{}
	if limit == 0 {
		return out, nil
	}
	if IsUndefined(sepV) {
		out.Items = []Value{s}
		return out, nil
	}
	sep, err := ToStringErr(sepV)
	if err != nil {
		return nil, err
	}
	u := U16(s)
	if sep == "" {
		for _, c := range u {
			if uint32(len(out.Items)) >= limit {
				break
			}
			out.Items = append(out.Items, FromCodeUnit(c))
		}
		return out, nil
	}
	v := U16(sep)
	start := 0
	for i := 0; i+len(v) <= len(u); {
		if eq16(u[i:i+len(v)], v) {
			out.Items = append(out.Items, FromU16(u[start:i]))
			if uint32(len(out.Items)) >= limit {
				return out, nil
			}
			i += len(v)
			start = i
			continue
		}
		i++
	}
	out.Items = append(out.Items, FromU16(u[start:]))
	return out, nil
}

func splitRegExp(s string, re *RegExp, limit uint32) (Value, error) {
	out := &Array{}
	if limit == 0 {
		return out, nil
	}
	u := U16(s)
	if len(u) == 0 {
		if re.MatchAt(u, 0) == nil {
			out.Items = append(out.Items, s)
		}
		return out, nil
	}
	p := 0
	for q := p; q < len(u); {
		caps := re.MatchAt(u, q)
		if caps == nil {
			q++
			continue
		}
		e := caps[1]
		if e == p {
			q++
			continue
		}
		out.Items = append(out.Items, FromU16(u[p:q]))
		if uint32(len(out.Items)) == limit {
			return out, nil
		}
		for i := 1; i <= re.groups; i++ {
			if caps[2*i] < 0 {
				out.Items = append(out.Items, Undefined)
			} else {
				out.Items = append(out.Items, FromU16(u[caps[2*i]:caps[2*i+1]]))
			}
			if uint32(len(out.Items)) == limit {
				return out, nil
			}
		}
		p = e
		q = p
	}
	out.Items = append(out.Items, FromU16(u[p:]))
	return out, nil
}

// Replace is String.prototype.replace (all false) and replaceAll (all true).
func Replace(s string, pattern, replacement Value, all bool) (Value, error) {
	fn, isFn := replacement.(*Function)
	replStr := ""
	if !isFn {
		r, err := ToStringErr(replacement)
		if err != nil {
			return nil, err
		}
		replStr = r
	}
	u := U16(s)
	type match struct {
		caps   []int
		groups Value
	}
	var matches []match
	ngroups := 0
	if re, ok := pattern.(*RegExp); ok {
		if all && !re.global {
			return nil, NewTypeError("replaceAll must be called with a global RegExp")
		}
		ngroups = re.groups
		var groups Value = Undefined
		if re.global {
			re.LastIndex = 0.0
			for {
				caps, err := re.execRaw(s)
				if err != nil {
					return nil, err
				}
				if caps == nil {
					break
				}
				if hasNames(re.names) {
					groups = re.matchArray(s, caps).Extra.props["groups"]
				}
				matches = append(matches, match{caps, groups})
				if caps[0] == caps[1] {
					re.LastIndex = float64(caps[1] + 1)
				}
			}
		} else {
			caps, err := re.execRaw(s)
			if err != nil {
				return nil, err
			}
			if caps != nil {
				if hasNames(re.names) {
					groups = re.matchArray(s, caps).Extra.props["groups"]
				}
				matches = append(matches, match{caps, groups})
			}
		}
	} else {
		pat, err := ToStringErr(pattern)
		if err != nil {
			return nil, err
		}
		v := U16(pat)
		pos := IndexOf16(s, pat, 0)
		for pos >= 0 {
			matches = append(matches, match{caps: []int{pos, pos + len(v)}, groups: Undefined})
			if !all {
				break
			}
			step := len(v)
			if step == 0 {
				step = 1
			}
			if pos+step > len(u) {
				break
			}
			pos = IndexOf16(s, pat, pos+step)
		}
	}
	var out []uint16
	last := 0
	for _, mt := range matches {
		start, end := mt.caps[0], mt.caps[1]
		out = append(out, u[last:start]...)
		capStr := func(i int) Value {
			if i >= len(mt.caps)/2 || mt.caps[2*i] < 0 {
				return Undefined
			}
			return FromU16(u[mt.caps[2*i]:mt.caps[2*i+1]])
		}
		if isFn {
			args := []Value{capStr(0)}
			for i := 1; i <= ngroups; i++ {
				args = append(args, capStr(i))
			}
			args = append(args, float64(start), s)
			if !IsUndefined(mt.groups) {
				args = append(args, mt.groups)
			}
			r, err := fn.Call(Undefined, args)
			if err != nil {
				return nil, err
			}
			rs, err := ToStringErr(r)
			if err != nil {
				return nil, err
			}
			out = append(out, U16(rs)...)
		} else {
			out = append(out, expandReplacement(U16(replStr), u, start, end, ngroups, capStr, mt.groups)...)
		}
		last = end
	}
	out = append(out, u[last:]...)
	return FromU16(out), nil
}

// expandReplacement is GetSubstitution: $$, $&, $`, $', $n, $nn and $<name>.
func expandReplacement(r, u []uint16, start, end, ngroups int, capStr func(int) Value, groups Value) []uint16 {
	var out []uint16
	for i := 0; i < len(r); i++ {
		c := r[i]
		if c != '$' || i+1 >= len(r) {
			out = append(out, c)
			continue
		}
		n := r[i+1]
		switch {
		case n == '$':
			out = append(out, '$')
			i++
		case n == '&':
			out = append(out, u[start:end]...)
			i++
		case n == '`':
			out = append(out, u[:start]...)
			i++
		case n == '\'':
			out = append(out, u[end:]...)
			i++
		case n >= '0' && n <= '9':
			d := int(n - '0')
			consumed := 1
			if i+2 < len(r) && r[i+2] >= '0' && r[i+2] <= '9' {
				dd := d*10 + int(r[i+2]-'0')
				if dd >= 1 && dd <= ngroups {
					d = dd
					consumed = 2
				}
			}
			if d < 1 || d > ngroups {
				out = append(out, c)
				continue
			}
			if v := capStr(d); !IsUndefined(v) {
				out = append(out, U16(v.(string))...)
			}
			i += consumed
		case n == '<':
			if IsUndefined(groups) {
				out = append(out, c)
				continue
			}
			closeIdx := -1
			for j := i + 2; j < len(r); j++ {
				if r[j] == '>' {
					closeIdx = j
					break
				}
			}
			if closeIdx < 0 {
				out = append(out, c)
				continue
			}
			name := FromU16(r[i+2 : closeIdx])
			if g, ok := groups.(*Object); ok {
				if v, ok := g.Get(name); ok && !IsUndefined(v) {
					out = append(out, U16(ToString(v))...)
				}
			}
			i = closeIdx
		default:
			out = append(out, c)
		}
	}
	return out
}

// Quote is the JSON string literal of s.
func Quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	u := U16(s)
	for i := 0; i < len(u); i++ {
		c := u[i]
		switch {
		case c == '"':
			b.WriteString(`\"`)
		case c == '\\':
			b.WriteString(`\\`)
		case c == '\b':
			b.WriteString(`\b`)
		case c == '\f':
			b.WriteString(`\f`)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\r':
			b.WriteString(`\r`)
		case c == '\t':
			b.WriteString(`\t`)
		case c < 0x20:
			b.WriteString(`\u00` + strconv.FormatInt(int64(c>>4), 16) + strconv.FormatInt(int64(c&15), 16))
		case c >= 0xD800 && c < 0xDC00 && i+1 < len(u) && u[i+1] >= 0xDC00 && u[i+1] < 0xE000:
			b.WriteString(FromU16(u[i : i+2]))
			i++
		case c >= 0xD800 && c < 0xE000:
			b.WriteString(`\u` + strconv.FormatInt(int64(c), 16))
		default:
			b.WriteString(FromU16(u[i : i+1]))
		}
	}
	b.WriteByte('"')
	return b.String()
}
