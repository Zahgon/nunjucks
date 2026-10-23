// Ported from nunjucks/src/filters.js and the helpers of nunjucks/src/lib.js of mozilla/nunjucks (BSD-2-Clause,
// see LICENSE).

package nunjucks

import (
	"math"
	"math/rand"
	"sort"
	"strings"

	"github.com/mozilla/nunjucks/js"
)

// filterFunc is a built-in filter: it receives the render context (the this of nunjucks filters) and the
// arguments, the filtered value first.
type filterFunc func(c *Context, args []js.Value) (js.Value, error)

func mustRegExp(src, flags string) *js.RegExp {
	re, err := js.NewRegExp(src, flags)
	if err != nil {
		panic(err)
	}
	return re
}

var (
	reCRLF        = `\r\n|\n`
	reTags        = `<\/?([a-z][a-z0-9]*)\b[^>]*>|<!--[\s\S]*?-->`
	reTrim        = `^\s*|\s*$`
	rePunc        = mustRegExp(`^(?:\(|<|&lt;)?(.*?)(?:\.|,|\)|\n|&gt;)?$`, "")
	reEmail       = mustRegExp(`^[\w.!#$%&'*+\-\/=?\^`+"`"+`{|}~]+@[a-z\d\-]+(\.[a-z\d\-]+)+$`, "i")
	reHTTP        = mustRegExp(`^https?:\/\/.*$`, "")
	reWWW         = mustRegExp(`^www\.`, "")
	reTLD         = mustRegExp(`\.(?:org|net|com)(?:\:|\/|$)`, "")
	reWordSplit   = `(\s+)`
	reWord        = `\w+`
	reLineSpaces  = `^ +| +$`
	reSpaces      = ` +`
	reCRLFGroup   = `(\r\n)`
	reManyNewline = `\n\n\n+`
	reWhitespace  = `\s+`
)

// replaceRe is str.replace(new RegExp(src, flags), repl) for a value that must be a string.
func replaceRe(str js.Value, src, flags string, repl js.Value, expr string) (js.Value, error) {
	re, err := js.NewRegExp(src, flags)
	if err != nil {
		return nil, err
	}
	return js.CallMethod(str, "replace", []js.Value{re, repl}, expr)
}

func normalize(v, def js.Value) js.Value {
	if js.IsNullish(v) || v == false {
		return def
	}
	return v
}

func call(v js.Value, name, expr string, args ...js.Value) (js.Value, error) {
	return js.CallMethod(v, name, args, expr)
}

func get(v js.Value, key js.Value) (js.Value, error) { return js.Get(v, key) }

// concat adds the values left to right with the JavaScript + operator.
func concat(vals ...js.Value) (js.Value, error) {
	acc := vals[0]
	for _, v := range vals[1:] {
		r, err := js.Add(acc, v)
		if err != nil {
			return nil, err
		}
		acc = r
	}
	return acc, nil
}

// repeatSpaces is lib.repeat(' ', n): n rounds up for fractional counts.
func repeatSpaces(n js.Value) string {
	var b strings.Builder
	for i := 0.0; ; i++ {
		ok, _ := js.Compare("<", i, n)
		if !ok {
			break
		}
		b.WriteByte(' ')
	}
	return b.String()
}

// mapValues is lib.map.
func mapValues(obj js.Value, fn func(v js.Value) (js.Value, error)) (js.Value, error) {
	if js.IsNullish(obj) {
		return &js.Array{}, nil
	}
	if a, ok := obj.(*js.Array); ok {
		out := &js.Array{}
		for _, it := range a.Items {
			v, err := fn(it)
			if err != nil {
				return nil, err
			}
			out.Items = append(out.Items, v)
		}
		return out, nil
	}
	out := &js.Array{}
	length := js.GetKey(obj, "length")
	for i := 0.0; ; i++ {
		ok, _ := js.Compare("<", i, length)
		if !ok {
			break
		}
		it, err := get(obj, i)
		if err != nil {
			return nil, err
		}
		v, err := fn(it)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, v)
	}
	if n, ok := length.(float64); ok && n == math.Trunc(n) && n >= 0 {
		for float64(len(out.Items)) < n {
			out.Items = append(out.Items, js.Undefined)
		}
	}
	return out, nil
}

// entries is lib._entries.
func entries(obj js.Value) *js.Array {
	out := &js.Array{}
	for _, k := range keysOf(obj) {
		out.Items = append(out.Items, js.NewArray(k, js.GetKey(obj, k)))
	}
	return out
}

// attrGetter is lib.getAttrGetter.
func attrGetter(attribute js.Value) func(js.Value) (js.Value, error) {
	var parts []js.Value
	switch a := attribute.(type) {
	case string:
		if a != "" {
			for _, p := range strings.Split(a, ".") {
				parts = append(parts, p)
			}
		}
	default:
		if js.Truthy(a) {
			parts = []js.Value{a}
		}
	}
	return func(item js.Value) (js.Value, error) {
		cur := item
		for _, part := range parts {
			if js.IsNullish(cur) {
				return nil, js.NewTypeError("Cannot convert undefined or null to object")
			}
			k, err := js.ToPropertyKey(part)
			if err != nil {
				return nil, err
			}
			if !js.HasOwn(cur, k) {
				return js.Undefined, nil
			}
			cur = js.GetKey(cur, k)
		}
		return cur, nil
	}
}

func isString(v js.Value) bool { _, ok := v.(string); return ok }

func builtinFilters() map[string]filterFunc {
	f := map[string]filterFunc{
		"abs": func(_ *Context, a []js.Value) (js.Value, error) {
			n, err := js.ToNumber(js.Arg(a, 0))
			return math.Abs(n), err
		},
		"batch":       filterBatch,
		"capitalize":  func(_ *Context, a []js.Value) (js.Value, error) { return capitalize(js.Arg(a, 0)) },
		"center":      filterCenter,
		"default":     filterDefault,
		"dictsort":    filterDictsort,
		"dump":        filterDump,
		"escape":      filterEscape,
		"safe":        filterSafe,
		"first":       func(_ *Context, a []js.Value) (js.Value, error) { return get(js.Arg(a, 0), 0.0) },
		"forceescape": filterForceescape,
		"groupby":     filterGroupby,
		"indent":      filterIndent,
		"join":        filterJoin,
		"last":        filterLast,
		"length":      filterLength,
		"list":        func(_ *Context, a []js.Value) (js.Value, error) { return filterList(js.Arg(a, 0)) },
		"lower": func(_ *Context, a []js.Value) (js.Value, error) {
			return call(normalize(js.Arg(a, 0), ""), "toLowerCase", "str")
		},
		"nl2br":      filterNl2br,
		"random":     filterRandom,
		"reject":     selectOrReject(false),
		"rejectattr": func(_ *Context, a []js.Value) (js.Value, error) { return filterAttr(a, false) },
		"select":     selectOrReject(true),
		"selectattr": func(_ *Context, a []js.Value) (js.Value, error) { return filterAttr(a, true) },
		"replace":    filterReplace,
		"reverse":    filterReverse,
		"round":      filterRound,
		"slice":      filterSlice,
		"sum":        filterSum,
		"sort":       filterSort(),
		"string": func(_ *Context, a []js.Value) (js.Value, error) {
			v := js.Arg(a, 0)
			if js.IsNullish(v) {
				return nil, js.ReadPropertyError(v, "toString")
			}
			return copySafeness(v, v)
		},
		"striptags": filterStriptags,
		"title":     filterTitle,
		"trim":      func(_ *Context, a []js.Value) (js.Value, error) { return trimFilter(js.Arg(a, 0)) },
		"truncate":  filterTruncate,
		"upper": func(_ *Context, a []js.Value) (js.Value, error) {
			return call(normalize(js.Arg(a, 0), ""), "toUpperCase", "str")
		},
		"urlencode": filterUrlencode,
		"urlize":    filterUrlize,
		"wordcount": filterWordcount,
		"float": func(_ *Context, a []js.Value) (js.Value, error) {
			n, err := js.ParseFloat(js.Arg(a, 0))
			if err != nil {
				return nil, err
			}
			if math.IsNaN(n) {
				return js.Arg(a, 1), nil
			}
			return n, nil
		},
		"int": filterInt(),
	}
	f["d"] = f["default"]
	f["e"] = f["escape"]
	return f
}

func filterBatch(_ *Context, a []js.Value) (js.Value, error) {
	arr, linecount, fillWith := js.Arg(a, 0), js.Arg(a, 1), js.Arg(a, 2)
	res := &js.Array{}
	tmp := &js.Array{}
	for i := 0.0; ; i++ {
		length, err := get(arr, "length")
		if err != nil {
			return nil, err
		}
		if ok, _ := js.Compare("<", i, length); !ok {
			break
		}
		m, err := js.Arith("%", i, linecount)
		if err != nil {
			return nil, err
		}
		if js.StrictEquals(m, 0.0) && len(tmp.Items) > 0 {
			res.Items = append(res.Items, tmp)
			tmp = &js.Array{}
		}
		v, err := get(arr, i)
		if err != nil {
			return nil, err
		}
		tmp.Items = append(tmp.Items, v)
	}
	if len(tmp.Items) > 0 {
		if js.Truthy(fillWith) {
			for i := float64(len(tmp.Items)); ; i++ {
				if ok, _ := js.Compare("<", i, linecount); !ok {
					break
				}
				tmp.Items = append(tmp.Items, fillWith)
			}
		}
		res.Items = append(res.Items, tmp)
	}
	return res, nil
}

func capitalize(v js.Value) (js.Value, error) {
	str := normalize(v, "")
	ret, err := call(str, "toLowerCase", "str")
	if err != nil {
		return nil, err
	}
	first, err := call(ret, "charAt", "ret", 0.0)
	if err != nil {
		return nil, err
	}
	upper, err := call(first, "toUpperCase", "ret.charAt(...)")
	if err != nil {
		return nil, err
	}
	rest, err := call(ret, "slice", "ret", 1.0)
	if err != nil {
		return nil, err
	}
	joined, err := concat(upper, rest)
	if err != nil {
		return nil, err
	}
	return copySafeness(str, joined)
}

func filterCenter(_ *Context, a []js.Value) (js.Value, error) {
	str := normalize(js.Arg(a, 0), "")
	width := js.Arg(a, 1)
	if !js.Truthy(width) {
		width = 80.0
	}
	length := js.GetKey(str, "length")
	if ok, _ := js.Compare(">=", length, width); ok {
		return str, nil
	}
	spaces, _ := js.Arith("-", width, length)
	half, _ := js.Arith("/", spaces, 2.0)
	mod, _ := js.Arith("%", spaces, 2.0)
	preN, _ := js.Arith("-", half, mod)
	padded, err := concat(repeatSpaces(preN), str, repeatSpaces(half))
	if err != nil {
		return nil, err
	}
	return copySafeness(str, padded)
}

func filterDefault(_ *Context, a []js.Value) (js.Value, error) {
	val, def, b := js.Arg(a, 0), js.Arg(a, 1), js.Arg(a, 2)
	if js.Truthy(b) {
		if js.Truthy(val) {
			return val, nil
		}
		return def, nil
	}
	if !js.IsUndefined(val) {
		return val, nil
	}
	return def, nil
}

func filterDictsort(_ *Context, a []js.Value) (js.Value, error) {
	val, caseSensitive, by := js.Arg(a, 0), js.Arg(a, 1), js.Arg(a, 2)
	if !js.IsObject(val) {
		return nil, templateError("dictsort filter: val must be an object", 0, 0)
	}
	var array []*js.Array
	for _, k := range js.OwnKeys(val) {
		array = append(array, js.NewArray(k, js.GetKey(val, k)))
	}
	si := 0
	switch {
	case js.IsUndefined(by) || by == "key":
	case by == "value":
		si = 1
	default:
		return nil, templateError("dictsort filter: You can only sort by either key or value", 0, 0)
	}
	var firstErr error
	key := func(t *js.Array) js.Value {
		v := t.Items[si]
		if !js.Truthy(caseSensitive) && isString(v) {
			u, err := call(v, "toUpperCase", "a")
			if err != nil && firstErr == nil {
				firstErr = err
			}
			return u
		}
		return v
	}
	sort.SliceStable(array, func(i, j int) bool {
		x, y := key(array[i]), key(array[j])
		if js.Greater(x, y) {
			return false
		}
		return !js.StrictEquals(x, y)
	})
	if firstErr != nil {
		return nil, firstErr
	}
	out := &js.Array{}
	for _, t := range array {
		out.Items = append(out.Items, t)
	}
	return out, nil
}

func filterDump(_ *Context, a []js.Value) (js.Value, error) {
	s, ok, err := js.Stringify(js.Arg(a, 0), js.Arg(a, 1))
	if err != nil {
		return nil, err
	}
	if !ok {
		return js.Undefined, nil
	}
	return s, nil
}

func filterEscape(_ *Context, a []js.Value) (js.Value, error) {
	str := js.Arg(a, 0)
	if _, ok := str.(*js.SafeString); ok {
		return str, nil
	}
	if js.IsNullish(str) {
		str = ""
	}
	s, err := escapeValue(str, "str")
	if err != nil {
		return nil, err
	}
	return markSafe(s), nil
}

func filterSafe(_ *Context, a []js.Value) (js.Value, error) {
	str := js.Arg(a, 0)
	if _, ok := str.(*js.SafeString); ok {
		return str, nil
	}
	if js.IsNullish(str) {
		str = ""
	}
	s, err := callToString(str, "str")
	if err != nil {
		return nil, err
	}
	return markSafe(s), nil
}

func filterForceescape(_ *Context, a []js.Value) (js.Value, error) {
	str := js.Arg(a, 0)
	if js.IsNullish(str) {
		str = ""
	}
	s, err := escapeValue(str, "str")
	if err != nil {
		return nil, err
	}
	return markSafe(s), nil
}

func filterGroupby(c *Context, a []js.Value) (js.Value, error) {
	arr, attr := js.Arg(a, 0), js.Arg(a, 1)
	result := js.NewObject()
	var iterator func(js.Value, float64) (js.Value, error)
	if fn, ok := attr.(*js.Function); ok {
		iterator = func(v js.Value, i float64) (js.Value, error) { return fn.Call(js.Undefined, []js.Value{v, i}) }
	} else {
		g := attrGetter(attr)
		iterator = func(v js.Value, _ float64) (js.Value, error) { return g(v) }
	}
	for i := 0.0; ; i++ {
		length, err := get(arr, "length")
		if err != nil {
			return nil, err
		}
		if ok, _ := js.Compare("<", i, length); !ok {
			break
		}
		value, err := get(arr, i)
		if err != nil {
			return nil, err
		}
		key, err := iterator(value, i)
		if err != nil {
			return nil, err
		}
		if js.IsUndefined(key) && c.env.opts.ThrowOnUndefined {
			return nil, js.NewTypeError(`groupby: attribute "%s" resolved to undefined`, js.ToString(attr))
		}
		k, err := js.ToPropertyKey(key)
		if err != nil {
			return nil, err
		}
		group := js.GetKey(result, k)
		if !js.Truthy(group) {
			group = &js.Array{}
			result.Set(k, group)
		}
		g, ok := group.(*js.Array)
		if !ok {
			return nil, js.NotFunctionError("(intermediate value).push")
		}
		g.Items = append(g.Items, value)
	}
	return result, nil
}

func filterIndent(_ *Context, a []js.Value) (js.Value, error) {
	str := normalize(js.Arg(a, 0), "")
	if str == "" {
		return "", nil
	}
	width := js.Arg(a, 1)
	if !js.Truthy(width) {
		width = 4.0
	}
	indentfirst := js.Truthy(js.Arg(a, 2))
	linesV, err := call(str, "split", "str", "\n")
	if err != nil {
		return nil, err
	}
	sp := repeatSpaces(width)
	lines := linesV.(*js.Array)
	out := &js.Array{}
	for i, l := range lines.Items {
		if i == 0 && !indentfirst {
			out.Items = append(out.Items, l)
		} else {
			out.Items = append(out.Items, sp+js.ToString(l))
		}
	}
	res, err := js.Join(out, "\n")
	if err != nil {
		return nil, err
	}
	return copySafeness(str, res)
}

func filterJoin(_ *Context, a []js.Value) (js.Value, error) {
	arr, del, attr := js.Arg(a, 0), js.Arg(a, 1), js.Arg(a, 2)
	if !js.Truthy(del) {
		del = ""
	}
	if js.Truthy(attr) {
		m, err := mapValues(arr, func(v js.Value) (js.Value, error) { return get(v, attr) })
		if err != nil {
			return nil, err
		}
		arr = m
	}
	return call(arr, "join", "arr", del)
}

func filterLast(_ *Context, a []js.Value) (js.Value, error) {
	arr := js.Arg(a, 0)
	length, err := get(arr, "length")
	if err != nil {
		return nil, err
	}
	idx, err := js.Arith("-", length, 1.0)
	if err != nil {
		return nil, err
	}
	return get(arr, idx)
}

func filterLength(_ *Context, a []js.Value) (js.Value, error) {
	value := normalize(js.Arg(a, 0), "")
	if o, ok := value.(*js.Object); ok {
		return float64(len(o.Keys())), nil
	}
	return js.GetKey(value, "length"), nil
}

func filterList(val js.Value) (js.Value, error) {
	switch v := val.(type) {
	case string:
		return js.Split(v, "", js.Undefined)
	case *js.Object, *js.SafeString:
		out := &js.Array{}
		for _, e := range entries(v).Items {
			pair := e.(*js.Array)
			o := js.NewObject()
			o.Set("key", pair.Items[0])
			o.Set("value", pair.Items[1])
			out.Items = append(out.Items, o)
		}
		return out, nil
	case *js.Array:
		return v, nil
	}
	return nil, templateError("list filter: type not iterable", 0, 0)
}

func filterNl2br(_ *Context, a []js.Value) (js.Value, error) {
	str := js.Arg(a, 0)
	if js.IsNullish(str) {
		return "", nil
	}
	r, err := replaceRe(str, reCRLF, "g", "<br />\n", "str")
	if err != nil {
		return nil, err
	}
	return copySafeness(str, r)
}

func filterRandom(_ *Context, a []js.Value) (js.Value, error) {
	arr := js.Arg(a, 0)
	length, err := get(arr, "length")
	if err != nil {
		return nil, err
	}
	n := js.Num(length)
	return get(arr, math.Floor(rand.Float64()*n))
}

// toArray is lib.toArray (Array.prototype.slice.call).
func toArray(v js.Value) (*js.Array, error) {
	if js.IsNullish(v) {
		return nil, js.NewTypeError("Array.prototype.slice called on null or undefined")
	}
	m, err := mapValues(v, func(x js.Value) (js.Value, error) { return x, nil })
	if err != nil {
		return nil, err
	}
	return m.(*js.Array), nil
}

func selectOrReject(expected bool) filterFunc {
	return func(c *Context, a []js.Value) (js.Value, error) {
		arr, testName, secondArg := js.Arg(a, 0), js.Arg(a, 1), js.Arg(a, 2)
		if js.IsUndefined(testName) {
			testName = "truthy"
		}
		test, err := c.env.getTest(js.ToString(testName))
		if err != nil {
			return nil, err
		}
		items, err := toArray(arr)
		if err != nil {
			return nil, err
		}
		out := &js.Array{}
		for _, it := range items.Items {
			r, err := test(c, []js.Value{it, secondArg})
			if err != nil {
				return nil, err
			}
			if js.StrictEquals(r, expected) {
				out.Items = append(out.Items, it)
			}
		}
		return out, nil
	}
}

func filterAttr(a []js.Value, keep bool) (js.Value, error) {
	arr, attr := js.Arg(a, 0), js.Arg(a, 1)
	items, ok := arr.(*js.Array)
	if !ok {
		if js.IsNullish(arr) {
			return nil, js.ReadPropertyError(arr, "filter")
		}
		return nil, js.NotFunctionError("arr.filter")
	}
	out := &js.Array{}
	for _, it := range items.Items {
		v, err := get(it, attr)
		if err != nil {
			return nil, err
		}
		if js.Truthy(v) == keep {
			out.Items = append(out.Items, it)
		}
	}
	return out, nil
}

func filterReplace(_ *Context, a []js.Value) (js.Value, error) {
	str, old, repl, maxCount := js.Arg(a, 0), js.Arg(a, 1), js.Arg(a, 2), js.Arg(a, 3)
	originalStr := str
	if re, ok := old.(*js.RegExp); ok {
		return call(str, "replace", "str", re, repl)
	}
	if js.IsUndefined(maxCount) {
		maxCount = -1.0
	}
	switch o := old.(type) {
	case float64:
		old = js.NumberToString(o)
	case string:
	default:
		return str, nil
	}
	if n, ok := str.(float64); ok {
		str = js.NumberToString(n)
	}
	_, isSafe := str.(*js.SafeString)
	if !isString(str) && !isSafe {
		return str, nil
	}
	s := js.ToString(str)
	oldS := old.(string)
	if oldS == "" {
		parts, _ := js.Split(s, "", js.Undefined)
		joined, err := js.Join(parts.(*js.Array), js.ToString(repl))
		if err != nil {
			return nil, err
		}
		res, err := concat(repl, joined, repl)
		if err != nil {
			return nil, err
		}
		return copySafeness(str, res)
	}
	nextIndex := js.IndexOf16(s, oldS, 0)
	if js.StrictEquals(maxCount, 0.0) || nextIndex == -1 {
		return str, nil
	}
	var res js.Value = ""
	pos, count := 0, 0.0
	for nextIndex > -1 && (js.StrictEquals(maxCount, -1.0) || js.Less(count, maxCount)) {
		next, err := concat(res, js.Slice16(s, pos, nextIndex), repl)
		if err != nil {
			return nil, err
		}
		res = next
		pos = nextIndex + js.Len16(oldS)
		count++
		nextIndex = js.IndexOf16(s, oldS, pos)
	}
	if pos < js.Len16(s) {
		next, err := concat(res, js.Slice16(s, pos, js.Len16(s)))
		if err != nil {
			return nil, err
		}
		res = next
	}
	return copySafeness(originalStr, res)
}

func filterReverse(_ *Context, a []js.Value) (js.Value, error) {
	val := js.Arg(a, 0)
	var arr *js.Array
	if isString(val) {
		l, err := filterList(val)
		if err != nil {
			return nil, err
		}
		arr = l.(*js.Array)
	} else {
		m, err := mapValues(val, func(v js.Value) (js.Value, error) { return v, nil })
		if err != nil {
			return nil, err
		}
		arr = m.(*js.Array)
	}
	for i, j := 0, len(arr.Items)-1; i < j; i, j = i+1, j-1 {
		arr.Items[i], arr.Items[j] = arr.Items[j], arr.Items[i]
	}
	if isString(val) {
		s, err := js.Join(arr, "")
		if err != nil {
			return nil, err
		}
		return copySafeness(val, s)
	}
	return arr, nil
}

func filterRound(_ *Context, a []js.Value) (js.Value, error) {
	val, precision, method := js.Arg(a, 0), js.Arg(a, 1), js.Arg(a, 2)
	if !js.Truthy(precision) {
		precision = 0.0
	}
	p, err := js.ToNumber(precision)
	if err != nil {
		return nil, err
	}
	factor := js.Pow(10, p)
	prod, err := js.Arith("*", val, factor)
	if err != nil {
		return nil, err
	}
	x := prod.(float64)
	var r float64
	switch method {
	case "ceil":
		r = math.Ceil(x)
	case "floor":
		r = math.Floor(x)
	default:
		r = js.Round(x)
	}
	return r / factor, nil
}

func filterSlice(_ *Context, a []js.Value) (js.Value, error) {
	arr, slices, fillWith := js.Arg(a, 0), js.Arg(a, 1), js.Arg(a, 2)
	length, err := get(arr, "length")
	if err != nil {
		return nil, err
	}
	q, _ := js.Arith("/", length, slices)
	sliceLength := math.Floor(q.(float64))
	extraV, _ := js.Arith("%", length, slices)
	extra := extraV.(float64)
	res := &js.Array{}
	offset := 0.0
	for i := 0.0; ; i++ {
		if ok, _ := js.Compare("<", i, slices); !ok {
			break
		}
		start := offset + i*sliceLength
		if i < extra {
			offset++
		}
		end := offset + (i+1)*sliceLength
		cur, err := call(arr, "slice", "arr", start, end)
		if err != nil {
			return nil, err
		}
		if js.Truthy(fillWith) && i >= extra {
			if _, err := call(cur, "push", "currSlice", fillWith); err != nil {
				return nil, err
			}
		}
		res.Items = append(res.Items, cur)
	}
	return res, nil
}

func filterSum(_ *Context, a []js.Value) (js.Value, error) {
	arr, attr, start := js.Arg(a, 0), js.Arg(a, 1), js.Arg(a, 2)
	if js.IsUndefined(start) {
		start = 0.0
	}
	if js.Truthy(attr) {
		m, err := mapValues(arr, func(v js.Value) (js.Value, error) { return get(v, attr) })
		if err != nil {
			return nil, err
		}
		arr = m
	}
	items, ok := arr.(*js.Array)
	if !ok {
		if js.IsNullish(arr) {
			return nil, js.ReadPropertyError(arr, "reduce")
		}
		return nil, js.NotFunctionError("arr.reduce")
	}
	var total js.Value = 0.0
	for _, it := range items.Items {
		r, err := js.Add(total, it)
		if err != nil {
			return nil, err
		}
		total = r
	}
	return js.Add(start, total)
}

func filterSort() filterFunc {
	var ctx *Context
	m := makeMacro([]string{"value", "reverse", "case_sensitive", "attribute"}, nil, func(args []js.Value) (js.Value, error) {
		arr, reversed, caseSens, attr := js.Arg(args, 0), js.Arg(args, 1), js.Arg(args, 2), js.Arg(args, 3)
		copyV, err := mapValues(arr, func(v js.Value) (js.Value, error) { return v, nil })
		if err != nil {
			return nil, err
		}
		array := copyV.(*js.Array)
		getAttribute := attrGetter(attr)
		var firstErr error
		cmp := func(a, b js.Value) int {
			x, y := a, b
			if js.Truthy(attr) {
				var err error
				if x, err = getAttribute(a); err == nil {
					y, err = getAttribute(b)
				}
				if err != nil {
					firstErr = err
					return 0
				}
			}
			if ctx.env.opts.ThrowOnUndefined && js.Truthy(attr) && (js.IsUndefined(x) || js.IsUndefined(y)) {
				firstErr = js.NewTypeError(`sort: attribute "%s" resolved to undefined`, js.ToString(attr))
				return 0
			}
			if !js.Truthy(caseSens) && isString(x) && isString(y) {
				x, y = js.LowerCase(x.(string)), js.LowerCase(y.(string))
			}
			switch {
			case js.Less(x, y):
				if js.Truthy(reversed) {
					return 1
				}
				return -1
			case js.Greater(x, y):
				if js.Truthy(reversed) {
					return -1
				}
				return 1
			}
			return 0
		}
		var defined, undef []js.Value
		for _, it := range array.Items {
			if js.IsUndefined(it) {
				undef = append(undef, it)
			} else {
				defined = append(defined, it)
			}
		}
		sort.SliceStable(defined, func(i, j int) bool {
			if firstErr != nil {
				return false
			}
			return cmp(defined[i], defined[j]) < 0
		})
		if firstErr != nil {
			return nil, firstErr
		}
		array.Items = append(defined, undef...)
		return array, nil
	})
	return func(c *Context, a []js.Value) (js.Value, error) {
		ctx = c
		return m.Call(js.Undefined, a)
	}
}

func filterStriptags(_ *Context, a []js.Value) (js.Value, error) {
	input := normalize(js.Arg(a, 0), "")
	stripped, err := replaceRe(input, reTags, "gi", "", "input")
	if err != nil {
		return nil, err
	}
	trimmed, err := trimFilter(stripped)
	if err != nil {
		return nil, err
	}
	var res js.Value
	if js.Truthy(js.Arg(a, 1)) {
		res = trimmed
		for _, step := range []struct{ re, flags, repl string }{
			{reLineSpaces, "gm", ""}, {reSpaces, "g", " "}, {reCRLFGroup, "g", "\n"}, {reManyNewline, "g", "\n\n"},
		} {
			if res, err = replaceRe(res, step.re, step.flags, step.repl, "trimmedInput"); err != nil {
				return nil, err
			}
		}
	} else if res, err = replaceRe(trimmed, reWhitespace, "gi", " ", "trimmedInput"); err != nil {
		return nil, err
	}
	return copySafeness(input, res)
}

func filterTitle(_ *Context, a []js.Value) (js.Value, error) {
	str := normalize(js.Arg(a, 0), "")
	wordsV, err := call(str, "split", "str", " ")
	if err != nil {
		return nil, err
	}
	words := wordsV.(*js.Array)
	out := &js.Array{}
	for _, w := range words.Items {
		c, err := capitalize(w)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, c)
	}
	s, err := js.Join(out, " ")
	if err != nil {
		return nil, err
	}
	return copySafeness(str, s)
}

func trimFilter(str js.Value) (js.Value, error) {
	r, err := replaceRe(str, reTrim, "g", "", "str")
	if err != nil {
		return nil, err
	}
	return copySafeness(str, r)
}

func filterTruncate(_ *Context, a []js.Value) (js.Value, error) {
	orig := js.Arg(a, 0)
	input := normalize(orig, "")
	length, killwords, endV := js.Arg(a, 1), js.Arg(a, 2), js.Arg(a, 3)
	if !js.Truthy(length) {
		length = 255.0
	}
	if ok, _ := js.Compare("<=", js.GetKey(input, "length"), length); ok {
		return input, nil
	}
	var cut js.Value
	var err error
	if js.Truthy(killwords) {
		cut, err = call(input, "substring", "input", 0.0, length)
	} else {
		var idx js.Value
		if idx, err = call(input, "lastIndexOf", "input", " ", length); err != nil {
			return nil, err
		}
		if js.StrictEquals(idx, -1.0) {
			idx = length
		}
		cut, err = call(input, "substring", "input", 0.0, idx)
	}
	if err != nil {
		return nil, err
	}
	if js.IsNullish(endV) {
		endV = "..."
	}
	res, err := concat(cut, endV)
	if err != nil {
		return nil, err
	}
	return copySafeness(orig, res)
}

func filterUrlencode(_ *Context, a []js.Value) (js.Value, error) {
	obj := js.Arg(a, 0)
	if s, ok := obj.(string); ok {
		return js.EncodeURIComponent(s)
	}
	var keyvals *js.Array
	if arr, ok := obj.(*js.Array); ok {
		keyvals = arr
	} else {
		keyvals = entries(obj)
	}
	var parts []string
	for _, kv := range keyvals.Items {
		pair, ok := kv.(*js.Array)
		var k, v js.Value
		switch {
		case ok:
			k, v = js.Arg(pair.Items, 0), js.Arg(pair.Items, 1)
		case isString(kv):
			u := js.U16Points(kv.(string))
			k, v = js.Arg(u, 0), js.Arg(u, 1)
		default:
			return nil, js.NewTypeError("%s is not iterable (cannot read property Symbol(Symbol.iterator))", js.TypeOf(kv)+" "+js.ToString(kv))
		}
		ek, err := js.EncodeURIComponent(k)
		if err != nil {
			return nil, err
		}
		ev, err := js.EncodeURIComponent(v)
		if err != nil {
			return nil, err
		}
		parts = append(parts, ek+"="+ev)
	}
	return strings.Join(parts, "&"), nil
}

func filterUrlize(_ *Context, a []js.Value) (js.Value, error) {
	str, length, nofollow := js.Arg(a, 0), js.Arg(a, 1), js.Arg(a, 2)
	if n, ok := length.(float64); ok && math.IsNaN(n) {
		length = math.Inf(1)
	}
	noFollowAttr := ""
	if nofollow == true {
		noFollowAttr = ` rel="nofollow"`
	}
	splitRe, err := js.NewRegExp(reWordSplit, "")
	if err != nil {
		return nil, err
	}
	wordsV, err := call(str, "split", "str", splitRe)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, w := range wordsV.(*js.Array).Items {
		word, ok := w.(string)
		if !ok || word == "" {
			continue
		}
		possibleUrl := word
		if m, _ := rePunc.Exec(word); !js.IsNull(m) {
			if s, ok := m.(*js.Array).Items[1].(string); ok {
				possibleUrl = s
			} else {
				possibleUrl = "undefined"
			}
		}
		shortV, err := call(possibleUrl, "substr", "possibleUrl", 0.0, length)
		if err != nil {
			return nil, err
		}
		shortURL := js.ToString(shortV)
		test := func(re *js.RegExp) bool { ok, _ := re.Test(possibleUrl); return ok }
		switch {
		case test(reHTTP):
			out = append(out, `<a href="`+possibleUrl+`"`+noFollowAttr+`>`+shortURL+`</a>`)
		case test(reWWW):
			out = append(out, `<a href="http://`+possibleUrl+`"`+noFollowAttr+`>`+shortURL+`</a>`)
		case test(reEmail):
			out = append(out, `<a href="mailto:`+possibleUrl+`">`+possibleUrl+`</a>`)
		case test(reTLD):
			out = append(out, `<a href="http://`+possibleUrl+`"`+noFollowAttr+`>`+shortURL+`</a>`)
		default:
			out = append(out, word)
		}
	}
	return strings.Join(out, ""), nil
}

func filterWordcount(_ *Context, a []js.Value) (js.Value, error) {
	str := normalize(js.Arg(a, 0), "")
	if !js.Truthy(str) {
		return js.Null, nil
	}
	re, err := js.NewRegExp(reWord, "g")
	if err != nil {
		return nil, err
	}
	words, err := call(str, "match", "str", re)
	if err != nil {
		return nil, err
	}
	if arr, ok := words.(*js.Array); ok {
		return float64(len(arr.Items)), nil
	}
	return js.Null, nil
}

func filterInt() filterFunc {
	m := makeMacro([]string{"value", "default", "base"}, nil, func(args []js.Value) (js.Value, error) {
		value, def, base := js.Arg(args, 0), js.Arg(args, 1), js.Arg(args, 2)
		if js.IsUndefined(base) {
			base = 10.0
		}
		n, err := js.ParseInt(value, base)
		if err != nil {
			return nil, err
		}
		if math.IsNaN(n) {
			return def, nil
		}
		return n, nil
	})
	return func(_ *Context, a []js.Value) (js.Value, error) { return m.Call(js.Undefined, a) }
}
