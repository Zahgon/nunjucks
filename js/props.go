package js

import (
	"math"
	"sort"
	"strconv"
)

// method is a built-in prototype method: it receives the this value and the arguments.
type method func(this Value, args []Value) (Value, error)

var (
	objectProto  map[string]method
	stringProto  map[string]method
	arrayProto   map[string]method
	numberProto  map[string]method
	booleanProto map[string]method
	regexpProto  map[string]method
	functionKeys = []string{"length", "name", "arguments", "caller", "constructor", "apply", "bind", "call", "toString"}
)

func init() {
	objectProto = map[string]method{
		"constructor":          unsupported("Object"),
		"__defineGetter__":     unsupported("__defineGetter__"),
		"__defineSetter__":     unsupported("__defineSetter__"),
		"__lookupGetter__":     unsupported("__lookupGetter__"),
		"__lookupSetter__":     unsupported("__lookupSetter__"),
		"hasOwnProperty":       objHasOwnProperty,
		"isPrototypeOf":        objIsPrototypeOf,
		"propertyIsEnumerable": objHasOwnProperty,
		"toString":             objToString,
		"toLocaleString":       objToLocaleString,
		"valueOf":              objValueOf,
	}
	stringProto = stringMethods()
	arrayProto = arrayMethods()
	numberProto = numberMethods()
	booleanProto = map[string]method{
		"constructor": unsupported("Boolean"),
		"toString": func(this Value, _ []Value) (Value, error) {
			b, ok := this.(bool)
			if !ok {
				return nil, NewTypeError("Boolean.prototype.toString requires that 'this' be a Boolean")
			}
			return ToString(b), nil
		},
		"valueOf": func(this Value, _ []Value) (Value, error) {
			b, ok := this.(bool)
			if !ok {
				return nil, NewTypeError("Boolean.prototype.valueOf requires that 'this' be a Boolean")
			}
			return b, nil
		},
	}
	regexpProto = regexpMethods()
}

// unsupported is a placeholder for built-in methods that exist on the prototype but that templates have no use
// for; calling one is an error.
func unsupported(name string) method {
	return func(Value, []Value) (Value, error) {
		return nil, NewTypeError("%s is not supported in templates", name)
	}
}

func objHasOwnProperty(this Value, args []Value) (Value, error) {
	k, err := ToPropertyKey(Arg(args, 0))
	if err != nil {
		return nil, err
	}
	return HasOwn(this, k), nil
}

func objIsPrototypeOf(Value, []Value) (Value, error) { return false, nil }

func objToString(this Value, _ []Value) (Value, error) {
	switch this.(type) {
	case undefinedType:
		return "[object Undefined]", nil
	case nullType:
		return "[object Null]", nil
	case *Array:
		return "[object Array]", nil
	case string:
		return "[object String]", nil
	case float64:
		return "[object Number]", nil
	case bool:
		return "[object Boolean]", nil
	case *Function:
		return "[object Function]", nil
	case *RegExp:
		return "[object RegExp]", nil
	}
	return "[object Object]", nil
}

func objToLocaleString(this Value, _ []Value) (Value, error) { return ToStringErr(this) }

func objValueOf(this Value, _ []Value) (Value, error) { return this, nil }

// ToPropertyKey converts a value used as a property name to its string key.
func ToPropertyKey(v Value) (string, error) {
	p, err := ToPrimitive(v)
	if err != nil {
		return "", err
	}
	return ToStringErr(p)
}

// HasOwn reports whether k is an own property of v.
func HasOwn(v Value, k string) bool {
	switch x := v.(type) {
	case *Object:
		return x.Has(k)
	case *Array:
		if k == "length" {
			return true
		}
		if i, ok := ArrayIndex(k); ok {
			return int(i) < len(x.Items)
		}
		return x.Extra != nil && x.Extra.Has(k)
	case string:
		if k == "length" {
			return true
		}
		i, ok := ArrayIndex(k)
		return ok && int(i) < Len16(x)
	case *SafeString:
		return k == "val" || k == "length"
	case *RegExp:
		return k == "lastIndex"
	case *Function:
		return k == "length" || k == "name"
	}
	return false
}

// OwnKeys returns the own enumerable string keys of v in for-in order.
func OwnKeys(v Value) []string {
	switch x := v.(type) {
	case *Object:
		return x.Keys()
	case *Array:
		keys := make([]string, len(x.Items))
		for i := range x.Items {
			keys[i] = strconv.Itoa(i)
		}
		if x.Extra != nil {
			keys = append(keys, x.Extra.Keys()...)
		}
		return keys
	case string:
		n := Len16(x)
		keys := make([]string, n)
		for i := range keys {
			keys[i] = strconv.Itoa(i)
		}
		return keys
	case *SafeString:
		return []string{"val", "length"}
	}
	return nil
}

// HasProperty is the in operator: own properties and those of the prototype chain.
func HasProperty(v Value, k string) bool {
	if HasOwn(v, k) {
		return true
	}
	if k == "__proto__" {
		return true
	}
	if _, ok := objectProto[k]; ok {
		return true
	}
	switch v.(type) {
	case *Array:
		_, ok := arrayProto[k]
		return ok
	case *SafeString, string:
		_, ok := stringProto[k]
		return ok
	case *RegExp:
		_, ok := regexpProto[k]
		return ok || isRegExpAccessor(k)
	case *Function:
		for _, f := range functionKeys {
			if f == k {
				return true
			}
		}
	}
	return false
}

// Get is the property read v[key]. Reading from null or undefined is a TypeError.
func Get(v Value, key Value) (Value, error) {
	if IsNullish(v) {
		return nil, ReadPropertyErrorKey(v, key)
	}
	k, err := ToPropertyKey(key)
	if err != nil {
		return nil, err
	}
	return GetKey(v, k), nil
}

// GetKey reads the property k of a non-nullish value.
func GetKey(v Value, k string) Value {
	switch x := v.(type) {
	case *Object:
		if val, ok := x.Get(k); ok {
			return val
		}
	case *Array:
		if k == "length" {
			return float64(len(x.Items))
		}
		if i, ok := ArrayIndex(k); ok {
			if int(i) < len(x.Items) {
				return x.Items[i]
			}
			return Undefined
		}
		if x.Extra != nil {
			if val, ok := x.Extra.Get(k); ok {
				return val
			}
		}
		if m, ok := arrayProto[k]; ok {
			return bindMethod(k, m)
		}
	case string:
		if k == "length" {
			return float64(Len16(x))
		}
		if i, ok := ArrayIndex(k); ok {
			u := U16(x)
			if int(i) < len(u) {
				return FromCodeUnit(u[i])
			}
			return Undefined
		}
		if m, ok := stringProto[k]; ok {
			return bindMethod(k, m)
		}
	case *SafeString:
		switch k {
		case "val":
			return x.Val
		case "length":
			return float64(Len16(x.Val))
		}
		if m, ok := stringProto[k]; ok {
			return bindMethod(k, m)
		}
	case float64:
		if m, ok := numberProto[k]; ok {
			return bindMethod(k, m)
		}
	case bool:
		if m, ok := booleanProto[k]; ok {
			return bindMethod(k, m)
		}
	case *RegExp:
		if val, ok := x.accessor(k); ok {
			return val
		}
		if m, ok := regexpProto[k]; ok {
			return bindMethod(k, m)
		}
	case *Function:
		switch k {
		case "name":
			return x.Name
		case "length":
			return 0.0
		case "toString":
			return bindMethod(k, func(this Value, _ []Value) (Value, error) { return ToStringErr(this) })
		}
	}
	if k == "__proto__" {
		return NewObject()
	}
	if m, ok := objectProto[k]; ok {
		return bindMethod(k, m)
	}
	return Undefined
}

// bindMethod returns a built-in method as an unbound function value; the this value is supplied by the call.
func bindMethod(name string, m method) *Function {
	return &Function{Name: name, Call: m}
}

// Set is the property write v[key] = val for objects and arrays.
func Set(v Value, key Value, val Value) error {
	k, err := ToPropertyKey(key)
	if err != nil {
		return err
	}
	switch x := v.(type) {
	case *Object:
		x.Set(k, val)
	case *Array:
		if i, ok := ArrayIndex(k); ok {
			for len(x.Items) <= int(i) {
				x.Items = append(x.Items, Undefined)
			}
			x.Items[i] = val
		} else if k == "length" {
			n := int(Num(val))
			if n < len(x.Items) {
				x.Items = x.Items[:n]
			}
		} else {
			if x.Extra == nil {
				x.Extra = NewObject()
			}
			x.Extra.Set(k, val)
		}
	case undefinedType, nullType:
		return NewTypeError("Cannot set properties of %s (setting '%s')", ToString(v), k)
	}
	return nil
}

// Call calls f with the this value and arguments; expr names the callee for the TypeError V8 raises when f is
// not callable.
func Call(f Value, this Value, args []Value, expr string) (Value, error) {
	fn, ok := f.(*Function)
	if !ok {
		return nil, NotFunctionError(expr)
	}
	return fn.Call(this, args)
}

// CallMethod reads v[name] and calls it with v as this, as the JavaScript expression expr.name(args).
func CallMethod(v Value, name string, args []Value, expr string) (Value, error) {
	if IsNullish(v) {
		return nil, ReadPropertyError(v, name)
	}
	return Call(GetKey(v, name), v, args, expr+"."+name)
}

// ---- Number.prototype ----

func thisNumber(this Value, name string) (float64, error) {
	n, ok := this.(float64)
	if !ok {
		return 0, NewTypeError("Number.prototype.%s requires that 'this' be a Number", name)
	}
	return n, nil
}

func numberMethods() map[string]method {
	return map[string]method{
		"constructor": unsupported("Number"),
		"toFixed": func(this Value, args []Value) (Value, error) {
			n, err := thisNumber(this, "toFixed")
			if err != nil {
				return nil, err
			}
			return ToFixed(n, Arg(args, 0))
		},
		"toPrecision": func(this Value, args []Value) (Value, error) {
			n, err := thisNumber(this, "toPrecision")
			if err != nil {
				return nil, err
			}
			return ToPrecision(n, Arg(args, 0))
		},
		"toExponential": func(this Value, args []Value) (Value, error) {
			n, err := thisNumber(this, "toExponential")
			if err != nil {
				return nil, err
			}
			return ToExponential(n, Arg(args, 0))
		},
		"toString": func(this Value, args []Value) (Value, error) {
			n, err := thisNumber(this, "toString")
			if err != nil {
				return nil, err
			}
			radix := 10.0
			if r := Arg(args, 0); !IsUndefined(r) {
				if radix, err = ToIntegerOrInfinity(r); err != nil {
					return nil, err
				}
			}
			if radix < 2 || radix > 36 {
				return nil, NewRangeError("toString() radix argument must be between 2 and 36")
			}
			if radix == 10 {
				return NumberToString(n), nil
			}
			return NumberToRadixString(n, int(radix)), nil
		},
		"toLocaleString": unsupported("toLocaleString"),
		"valueOf": func(this Value, _ []Value) (Value, error) {
			return thisNumber(this, "valueOf")
		},
	}
}

// ---- Array.prototype ----

func thisArray(this Value) (*Array, error) {
	a, ok := this.(*Array)
	if !ok {
		return nil, NewTypeError("Array.prototype method called on a non-array")
	}
	return a, nil
}

// relIndex resolves a relative index argument (negative counts from the end) clamped to [0, n].
func relIndex(v Value, n int, def int) (int, error) {
	if IsUndefined(v) {
		return def, nil
	}
	f, err := ToIntegerOrInfinity(v)
	if err != nil {
		return 0, err
	}
	if f < 0 {
		f = math.Max(float64(n)+f, 0)
	}
	return int(math.Min(f, float64(n))), nil
}

func arrayMethods() map[string]method {
	m := map[string]method{}
	for _, name := range []string{"constructor", "copyWithin", "find", "findIndex", "findLast", "findLastIndex",
		"keys", "entries", "values", "forEach", "filter", "flatMap", "map", "every", "some", "reduce", "reduceRight",
		"toLocaleString", "toSpliced"} {
		m[name] = unsupported(name)
	}
	m["at"] = func(this Value, args []Value) (Value, error) {
		a, err := thisArray(this)
		if err != nil {
			return nil, err
		}
		f, err := ToIntegerOrInfinity(Arg(args, 0))
		if err != nil {
			return nil, err
		}
		if f < 0 {
			f += float64(len(a.Items))
		}
		if f < 0 || f >= float64(len(a.Items)) {
			return Undefined, nil
		}
		return a.Items[int(f)], nil
	}
	m["concat"] = func(this Value, args []Value) (Value, error) {
		a, err := thisArray(this)
		if err != nil {
			return nil, err
		}
		out := &Array{Items: append([]Value(nil), a.Items...)}
		for _, x := range args {
			if b, ok := x.(*Array); ok {
				out.Items = append(out.Items, b.Items...)
			} else {
				out.Items = append(out.Items, x)
			}
		}
		return out, nil
	}
	m["fill"] = func(this Value, args []Value) (Value, error) {
		a, err := thisArray(this)
		if err != nil {
			return nil, err
		}
		s, err := relIndex(Arg(args, 1), len(a.Items), 0)
		if err != nil {
			return nil, err
		}
		e, err := relIndex(Arg(args, 2), len(a.Items), len(a.Items))
		if err != nil {
			return nil, err
		}
		for i := s; i < e; i++ {
			a.Items[i] = Arg(args, 0)
		}
		return a, nil
	}
	m["includes"] = func(this Value, args []Value) (Value, error) {
		a, err := thisArray(this)
		if err != nil {
			return nil, err
		}
		from, err := relIndex(Arg(args, 1), len(a.Items), 0)
		if err != nil {
			return nil, err
		}
		x := Arg(args, 0)
		for _, it := range a.Items[from:] {
			if SameValueZero(it, x) {
				return true, nil
			}
		}
		return false, nil
	}
	m["indexOf"] = func(this Value, args []Value) (Value, error) {
		a, err := thisArray(this)
		if err != nil {
			return nil, err
		}
		from, err := relIndex(Arg(args, 1), len(a.Items), 0)
		if err != nil {
			return nil, err
		}
		for i := from; i < len(a.Items); i++ {
			if StrictEquals(a.Items[i], Arg(args, 0)) {
				return float64(i), nil
			}
		}
		return -1.0, nil
	}
	m["lastIndexOf"] = func(this Value, args []Value) (Value, error) {
		a, err := thisArray(this)
		if err != nil {
			return nil, err
		}
		from := len(a.Items) - 1
		if len(args) > 1 {
			f, err := ToIntegerOrInfinity(args[1])
			if err != nil {
				return nil, err
			}
			if f < 0 {
				f += float64(len(a.Items))
			}
			from = int(math.Min(f, float64(len(a.Items)-1)))
		}
		for i := from; i >= 0; i-- {
			if StrictEquals(a.Items[i], Arg(args, 0)) {
				return float64(i), nil
			}
		}
		return -1.0, nil
	}
	m["join"] = func(this Value, args []Value) (Value, error) {
		a, err := thisArray(this)
		if err != nil {
			return nil, err
		}
		sep := ","
		if s := Arg(args, 0); !IsUndefined(s) {
			if sep, err = ToStringErr(s); err != nil {
				return nil, err
			}
		}
		return Join(a, sep)
	}
	m["toString"] = func(this Value, _ []Value) (Value, error) {
		if a, ok := this.(*Array); ok {
			return Join(a, ",")
		}
		return objToString(this, nil)
	}
	m["pop"] = func(this Value, _ []Value) (Value, error) {
		a, err := thisArray(this)
		if err != nil || len(a.Items) == 0 {
			return Undefined, err
		}
		v := a.Items[len(a.Items)-1]
		a.Items = a.Items[:len(a.Items)-1]
		return v, nil
	}
	m["push"] = func(this Value, args []Value) (Value, error) {
		a, err := thisArray(this)
		if err != nil {
			return nil, err
		}
		a.Items = append(a.Items, args...)
		return float64(len(a.Items)), nil
	}
	m["shift"] = func(this Value, _ []Value) (Value, error) {
		a, err := thisArray(this)
		if err != nil || len(a.Items) == 0 {
			return Undefined, err
		}
		v := a.Items[0]
		a.Items = append([]Value(nil), a.Items[1:]...)
		return v, nil
	}
	m["unshift"] = func(this Value, args []Value) (Value, error) {
		a, err := thisArray(this)
		if err != nil {
			return nil, err
		}
		a.Items = append(append([]Value(nil), args...), a.Items...)
		return float64(len(a.Items)), nil
	}
	m["reverse"] = func(this Value, _ []Value) (Value, error) {
		a, err := thisArray(this)
		if err != nil {
			return nil, err
		}
		for i, j := 0, len(a.Items)-1; i < j; i, j = i+1, j-1 {
			a.Items[i], a.Items[j] = a.Items[j], a.Items[i]
		}
		return a, nil
	}
	m["toReversed"] = func(this Value, _ []Value) (Value, error) {
		a, err := thisArray(this)
		if err != nil {
			return nil, err
		}
		out := &Array{}
		for i := len(a.Items) - 1; i >= 0; i-- {
			out.Items = append(out.Items, a.Items[i])
		}
		return out, nil
	}
	m["slice"] = func(this Value, args []Value) (Value, error) {
		a, err := thisArray(this)
		if err != nil {
			return nil, err
		}
		s, err := relIndex(Arg(args, 0), len(a.Items), 0)
		if err != nil {
			return nil, err
		}
		e, err := relIndex(Arg(args, 1), len(a.Items), len(a.Items))
		if err != nil {
			return nil, err
		}
		if s > e {
			e = s
		}
		return &Array{Items: append([]Value(nil), a.Items[s:e]...)}, nil
	}
	m["splice"] = func(this Value, args []Value) (Value, error) {
		a, err := thisArray(this)
		if err != nil {
			return nil, err
		}
		n := len(a.Items)
		s, err := relIndex(Arg(args, 0), n, 0)
		if err != nil {
			return nil, err
		}
		del := 0
		switch {
		case len(args) == 0:
		case len(args) == 1:
			del = n - s
		default:
			f, err := ToIntegerOrInfinity(args[1])
			if err != nil {
				return nil, err
			}
			del = int(math.Min(math.Max(f, 0), float64(n-s)))
		}
		removed := &Array{Items: append([]Value(nil), a.Items[s:s+del]...)}
		var ins []Value
		if len(args) > 2 {
			ins = args[2:]
		}
		rest := append(append([]Value(nil), ins...), a.Items[s+del:]...)
		a.Items = append(a.Items[:s:s], rest...)
		return removed, nil
	}
	m["sort"] = func(this Value, args []Value) (Value, error) {
		a, err := thisArray(this)
		if err != nil {
			return nil, err
		}
		if err := SortValues(a.Items, Arg(args, 0)); err != nil {
			return nil, err
		}
		return a, nil
	}
	m["toSorted"] = func(this Value, args []Value) (Value, error) {
		a, err := thisArray(this)
		if err != nil {
			return nil, err
		}
		out := &Array{Items: append([]Value(nil), a.Items...)}
		return out, SortValues(out.Items, Arg(args, 0))
	}
	m["flat"] = func(this Value, args []Value) (Value, error) {
		a, err := thisArray(this)
		if err != nil {
			return nil, err
		}
		depth := 1.0
		if d := Arg(args, 0); !IsUndefined(d) {
			if depth, err = ToIntegerOrInfinity(d); err != nil {
				return nil, err
			}
		}
		return &Array{Items: flatten(a.Items, depth)}, nil
	}
	m["with"] = func(this Value, args []Value) (Value, error) {
		a, err := thisArray(this)
		if err != nil {
			return nil, err
		}
		f, err := ToIntegerOrInfinity(Arg(args, 0))
		if err != nil {
			return nil, err
		}
		if f < 0 {
			f += float64(len(a.Items))
		}
		if f < 0 || f >= float64(len(a.Items)) {
			return nil, NewRangeError("Invalid index : %s", NumberToString(Num(Arg(args, 0))))
		}
		out := &Array{Items: append([]Value(nil), a.Items...)}
		out.Items[int(f)] = Arg(args, 1)
		return out, nil
	}
	return m
}

func flatten(items []Value, depth float64) []Value {
	var out []Value
	for _, it := range items {
		if b, ok := it.(*Array); ok && depth >= 1 {
			out = append(out, flatten(b.Items, depth-1)...)
			continue
		}
		out = append(out, it)
	}
	return out
}

// SameValueZero is the equality of Array.prototype.includes.
func SameValueZero(a, b Value) bool {
	x, ok1 := a.(float64)
	y, ok2 := b.(float64)
	if ok1 && ok2 && math.IsNaN(x) && math.IsNaN(y) {
		return true
	}
	return StrictEquals(a, b)
}

// SortValues is Array.prototype.sort: undefined last, then by the comparator or by string order; stable.
func SortValues(items []Value, cmp Value) error {
	if !IsUndefined(cmp) {
		if _, ok := cmp.(*Function); !ok {
			return NewTypeError("The comparison function must be either a function or undefined")
		}
	}
	var defined, undef []Value
	for _, it := range items {
		if IsUndefined(it) {
			undef = append(undef, it)
		} else {
			defined = append(defined, it)
		}
	}
	var firstErr error
	keys := make([]string, len(defined))
	if IsUndefined(cmp) {
		for i, it := range defined {
			s, err := ToStringErr(it)
			if err != nil {
				return err
			}
			keys[i] = s
		}
	}
	idx := make([]int, len(defined))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(i, j int) bool {
		if firstErr != nil {
			return false
		}
		if IsUndefined(cmp) {
			return CompareStrings(keys[idx[i]], keys[idx[j]]) < 0
		}
		r, err := Call(cmp, Undefined, []Value{defined[idx[i]], defined[idx[j]]}, "comparefn")
		if err != nil {
			firstErr = err
			return false
		}
		n, err := ToNumber(r)
		if err != nil {
			firstErr = err
			return false
		}
		return n < 0
	})
	if firstErr != nil {
		return firstErr
	}
	out := make([]Value, 0, len(items))
	for _, i := range idx {
		out = append(out, defined[i])
	}
	copy(items, append(out, undef...))
	return nil
}

// ---- RegExp.prototype ----

func isRegExpAccessor(k string) bool {
	switch k {
	case "source", "flags", "global", "ignoreCase", "multiline", "sticky", "unicode", "dotAll", "hasIndices", "unicodeSets":
		return true
	}
	return false
}

func (re *RegExp) accessor(k string) (Value, bool) {
	switch k {
	case "lastIndex":
		return re.LastIndex, true
	case "source":
		return escapeSource(re.Source), true
	case "flags":
		return re.Flags, true
	case "global":
		return re.global, true
	case "ignoreCase":
		return re.ignore, true
	case "multiline":
		return re.multiline, true
	case "sticky":
		return re.sticky, true
	case "dotAll":
		return re.dotAll, true
	case "unicode", "hasIndices", "unicodeSets":
		return false, true
	}
	return nil, false
}

func regexpMethods() map[string]method {
	thisRe := func(this Value, name string) (*RegExp, error) {
		re, ok := this.(*RegExp)
		if !ok {
			return nil, NewTypeError("RegExp.prototype.%s requires that 'this' be a RegExp object", name)
		}
		return re, nil
	}
	return map[string]method{
		"constructor": unsupported("RegExp"),
		"compile":     unsupported("compile"),
		"exec": func(this Value, args []Value) (Value, error) {
			re, err := thisRe(this, "exec")
			if err != nil {
				return nil, err
			}
			s, err := ToStringErr(Arg(args, 0))
			if err != nil {
				return nil, err
			}
			return re.Exec(s)
		},
		"test": func(this Value, args []Value) (Value, error) {
			re, err := thisRe(this, "test")
			if err != nil {
				return nil, err
			}
			s, err := ToStringErr(Arg(args, 0))
			if err != nil {
				return nil, err
			}
			return re.Test(s)
		},
		"toString": func(this Value, _ []Value) (Value, error) {
			re, err := thisRe(this, "toString")
			if err != nil {
				return nil, err
			}
			return re.String(), nil
		},
	}
}
