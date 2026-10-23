// Ported from nunjucks/src/tests.js and nunjucks/src/globals.js of mozilla/nunjucks (BSD-2-Clause, see LICENSE).

package nunjucks

import (
	"github.com/mozilla/nunjucks/js"
)

// testFunc is a built-in test (the right side of "is").
type testFunc func(c *Context, args []js.Value) (js.Value, error)

func builtinTests() map[string]testFunc {
	one := func(f func(v js.Value) (js.Value, error)) testFunc {
		return func(_ *Context, a []js.Value) (js.Value, error) { return f(js.Arg(a, 0)) }
	}
	two := func(f func(x, y js.Value) (js.Value, error)) testFunc {
		return func(_ *Context, a []js.Value) (js.Value, error) { return f(js.Arg(a, 0), js.Arg(a, 1)) }
	}
	compare := func(op string) testFunc {
		return two(func(x, y js.Value) (js.Value, error) { return js.Compare(op, x, y) })
	}
	modEquals := func(div, want float64) func(v js.Value) (js.Value, error) {
		return func(v js.Value) (js.Value, error) {
			m, err := js.Arith("%", v, div)
			return js.StrictEquals(m, want), err
		}
	}
	caseTest := func(method string) func(v js.Value) (js.Value, error) {
		return func(v js.Value) (js.Value, error) {
			r, err := js.CallMethod(v, method, nil, "value")
			return js.StrictEquals(r, v), err
		}
	}
	strictEq := two(func(x, y js.Value) (js.Value, error) { return js.StrictEquals(x, y), nil })
	t := map[string]testFunc{
		"callable": one(func(v js.Value) (js.Value, error) { return js.TypeOf(v) == "function", nil }),
		"defined":  one(func(v js.Value) (js.Value, error) { return !js.IsUndefined(v), nil }),
		"divisibleby": two(func(x, y js.Value) (js.Value, error) {
			m, err := js.Arith("%", x, y)
			return js.StrictEquals(m, 0.0), err
		}),
		"escaped":     one(func(v js.Value) (js.Value, error) { _, ok := v.(*js.SafeString); return ok, nil }),
		"equalto":     strictEq,
		"eq":          strictEq,
		"sameas":      strictEq,
		"even":        one(modEquals(2, 0)),
		"falsy":       one(func(v js.Value) (js.Value, error) { return !js.Truthy(v), nil }),
		"ge":          compare(">="),
		"greaterthan": compare(">"),
		"gt":          compare(">"),
		"le":          compare("<="),
		"lessthan":    compare("<"),
		"lt":          compare("<"),
		"lower":       one(caseTest("toLowerCase")),
		"ne":          two(func(x, y js.Value) (js.Value, error) { return !js.StrictEquals(x, y), nil }),
		"null":        one(func(v js.Value) (js.Value, error) { return js.IsNull(v), nil }),
		"number":      one(func(v js.Value) (js.Value, error) { return js.TypeOf(v) == "number", nil }),
		"odd":         one(modEquals(2, 1)),
		"string":      one(func(v js.Value) (js.Value, error) { return js.TypeOf(v) == "string", nil }),
		"truthy":      one(func(v js.Value) (js.Value, error) { return js.Truthy(v), nil }),
		"undefined":   one(func(v js.Value) (js.Value, error) { return js.IsUndefined(v), nil }),
		"upper":       one(caseTest("toUpperCase")),
		"iterable": one(func(v js.Value) (js.Value, error) {
			switch {
			case js.IsNull(v):
				return nil, js.NewTypeError("object null is not iterable (cannot read property Symbol(Symbol.iterator))")
			case js.IsUndefined(v):
				return nil, js.NewTypeError("undefined is not iterable (cannot read property Symbol(Symbol.iterator))")
			}
			switch v.(type) {
			case string, *js.Array, *js.SafeString:
				return true, nil
			}
			return false, nil
		}),
		"mapping": one(func(v js.Value) (js.Value, error) {
			switch v.(type) {
			case *js.Object, *js.SafeString, *js.RegExp:
				return true, nil
			}
			return false, nil
		}),
	}
	return t
}

// newGlobals returns the default globals: range, cycler and joiner.
func newGlobals() *js.Object {
	g := js.NewObject()
	g.Set("range", js.NewFunction("range", func(_ js.Value, a []js.Value) (js.Value, error) {
		start, stop, step := js.Arg(a, 0), js.Arg(a, 1), js.Arg(a, 2)
		if js.IsUndefined(stop) {
			stop, start, step = start, 0.0, 1.0
		} else if !js.Truthy(step) {
			step = 1.0
		}
		arr := &js.Array{}
		cmp := "<"
		if !js.Greater(step, 0.0) {
			cmp = ">"
		}
		for i := start; ; {
			ok, err := js.Compare(cmp, i, stop)
			if err != nil {
				return nil, err
			}
			if !ok {
				break
			}
			arr.Items = append(arr.Items, i)
			if len(arr.Items) > 1<<24 {
				return nil, js.NewRangeError("Invalid array length")
			}
			if i, err = js.Add(i, step); err != nil {
				return nil, err
			}
		}
		return arr, nil
	}))
	g.Set("cycler", js.NewFunction("cycler", func(_ js.Value, a []js.Value) (js.Value, error) {
		items := append([]js.Value(nil), a...)
		index := -1
		obj := js.NewObject()
		obj.Set("current", js.Null)
		obj.Set("reset", js.NewFunction("reset", func(this js.Value, _ []js.Value) (js.Value, error) {
			index = -1
			return js.Undefined, js.Set(this, "current", js.Null)
		}))
		obj.Set("next", js.NewFunction("next", func(this js.Value, _ []js.Value) (js.Value, error) {
			index++
			if index >= len(items) {
				index = 0
			}
			cur := js.Arg(items, index)
			if err := js.Set(this, "current", cur); err != nil {
				return nil, err
			}
			return js.Get(this, "current")
		}))
		return obj, nil
	}))
	g.Set("joiner", js.NewFunction("joiner", func(_ js.Value, a []js.Value) (js.Value, error) {
		var sep js.Value = js.Arg(a, 0)
		if !js.Truthy(sep) {
			sep = ","
		}
		first := true
		return js.NewFunction("", func(js.Value, []js.Value) (js.Value, error) {
			var val js.Value = ""
			if !first {
				val = sep
			}
			first = false
			return val, nil
		}), nil
	}))
	return g
}
