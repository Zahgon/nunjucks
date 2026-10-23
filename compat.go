// Ported from nunjucks/src/jinja-compat.js of mozilla/nunjucks (BSD-2-Clause, see LICENSE).

package nunjucks

import (
	"github.com/mozilla/nunjucks/js"
)

type memberFunc func(this js.Value, args []js.Value) (js.Value, error)

var arrayMembers = map[string]memberFunc{
	"pop": func(this js.Value, a []js.Value) (js.Value, error) {
		arr := this.(*js.Array)
		index := js.Arg(a, 0)
		if js.IsUndefined(index) {
			return js.CallMethod(arr, "pop", nil, "this")
		}
		ge, _ := js.Compare(">=", index, float64(len(arr.Items)))
		lt, _ := js.Compare("<", index, 0.0)
		if ge || lt {
			return nil, js.NewError("KeyError")
		}
		return js.CallMethod(arr, "splice", []js.Value{index, 1.0}, "this")
	},
	"append": func(this js.Value, a []js.Value) (js.Value, error) {
		return js.CallMethod(this, "push", []js.Value{js.Arg(a, 0)}, "this")
	},
	"remove": func(this js.Value, a []js.Value) (js.Value, error) {
		arr := this.(*js.Array)
		for i, it := range arr.Items {
			if js.StrictEquals(it, js.Arg(a, 0)) {
				return js.CallMethod(arr, "splice", []js.Value{float64(i), 1.0}, "this")
			}
		}
		return nil, js.NewError("ValueError")
	},
	"count": func(this js.Value, a []js.Value) (js.Value, error) {
		n := 0.0
		for _, it := range this.(*js.Array).Items {
			if js.StrictEquals(it, js.Arg(a, 0)) {
				n++
			}
		}
		return n, nil
	},
	"index": func(this js.Value, a []js.Value) (js.Value, error) {
		i, err := js.CallMethod(this, "indexOf", []js.Value{js.Arg(a, 0)}, "this")
		if err != nil {
			return nil, err
		}
		if js.StrictEquals(i, -1.0) {
			return nil, js.NewError("ValueError")
		}
		return i, nil
	},
	"find": func(this js.Value, a []js.Value) (js.Value, error) {
		return js.CallMethod(this, "indexOf", []js.Value{js.Arg(a, 0)}, "this")
	},
	"insert": func(this js.Value, a []js.Value) (js.Value, error) {
		return js.CallMethod(this, "splice", []js.Value{js.Arg(a, 0), 0.0, js.Arg(a, 1)}, "this")
	},
}

var objectMembers map[string]memberFunc

func init() {
	items := func(this js.Value, _ []js.Value) (js.Value, error) { return entries(this), nil }
	values := func(this js.Value, _ []js.Value) (js.Value, error) {
		out := &js.Array{}
		for _, k := range keysOf(this) {
			out.Items = append(out.Items, js.GetKey(this, k))
		}
		return out, nil
	}
	keys := func(this js.Value, _ []js.Value) (js.Value, error) {
		out := &js.Array{}
		for _, k := range keysOf(this) {
			out.Items = append(out.Items, k)
		}
		return out, nil
	}
	objectMembers = map[string]memberFunc{
		"items": items, "iteritems": items,
		"values": values, "itervalues": values,
		"keys": keys, "iterkeys": keys,
		"get": func(this js.Value, a []js.Value) (js.Value, error) {
			k, err := js.ToPropertyKey(js.Arg(a, 0))
			if err != nil {
				return nil, err
			}
			out := js.GetKey(this, k)
			if js.IsUndefined(out) {
				out = js.Arg(a, 1)
			}
			return out, nil
		},
		"has_key": func(this js.Value, a []js.Value) (js.Value, error) {
			k, err := js.ToPropertyKey(js.Arg(a, 0))
			if err != nil {
				return nil, err
			}
			return js.HasOwn(this, k), nil
		},
		"pop": func(this js.Value, a []js.Value) (js.Value, error) {
			k, err := js.ToPropertyKey(js.Arg(a, 0))
			if err != nil {
				return nil, err
			}
			out := js.GetKey(this, k)
			def := js.Arg(a, 1)
			switch {
			case js.IsUndefined(out) && !js.IsUndefined(def):
				out = def
			case js.IsUndefined(out):
				return nil, js.NewError("KeyError")
			default:
				if o, ok := this.(*js.Object); ok {
					o.Delete(k)
				}
			}
			return out, nil
		},
		"popitem": func(this js.Value, _ []js.Value) (js.Value, error) {
			ks := keysOf(this)
			if len(ks) == 0 {
				return nil, js.NewError("KeyError")
			}
			v := js.GetKey(this, ks[0])
			if o, ok := this.(*js.Object); ok {
				o.Delete(ks[0])
			}
			return js.NewArray(ks[0], v), nil
		},
		"setdefault": func(this js.Value, a []js.Value) (js.Value, error) {
			k, err := js.ToPropertyKey(js.Arg(a, 0))
			if err != nil {
				return nil, err
			}
			def := js.Arg(a, 1)
			if js.IsUndefined(def) {
				def = js.Null
			}
			if !js.HasProperty(this, k) {
				if err := js.Set(this, k, def); err != nil {
					return nil, err
				}
			}
			return js.GetKey(this, k), nil
		},
		"update": func(this js.Value, a []js.Value) (js.Value, error) {
			src := js.Arg(a, 0)
			for _, k := range keysOf(src) {
				if err := js.Set(this, k, js.GetKey(src, k)); err != nil {
					return nil, err
				}
			}
			return js.Null, nil
		},
	}
}

// compatMemberLookup is the memberLookup the Jinja compatibility layer installs.
func compatMemberLookup(obj, key js.Value) (js.Value, error) {
	if !js.Truthy(obj) {
		obj = js.NewObject()
	}
	k, err := js.ToPropertyKey(key)
	if err != nil {
		return nil, err
	}
	bind := func(m memberFunc) js.Value {
		return js.NewFunction(k, func(_ js.Value, args []js.Value) (js.Value, error) { return m(obj, args) })
	}
	if _, ok := obj.(*js.Array); ok {
		if m, ok := arrayMembers[k]; ok {
			return bind(m), nil
		}
	}
	if js.IsObject(obj) {
		if m, ok := objectMembers[k]; ok {
			return bind(m), nil
		}
	}
	return memberLookup(obj, key)
}

// sliceLookup is obj[start:stop:step] of the Jinja compatibility layer.
func sliceLookup(obj, start, stop, step js.Value) (js.Value, error) {
	if !js.Truthy(obj) {
		obj = &js.Array{}
	}
	length := js.GetKey(obj, "length")
	var cmpErr error
	cmp := func(op string, a, b js.Value) bool {
		r, err := js.Compare(op, a, b)
		if err != nil && cmpErr == nil {
			cmpErr = err
		}
		return r
	}
	lt0 := func(v js.Value) bool { return cmp("<", v, 0.0) }
	if js.IsNull(start) {
		if lt0(step) {
			start, _ = js.Arith("-", length, 1.0)
		} else {
			start = 0.0
		}
	}
	if js.IsNull(stop) {
		if lt0(step) {
			stop = -1.0
		} else {
			stop = length
		}
	} else if lt0(stop) {
		stop, _ = js.Add(stop, length)
	}
	if lt0(start) {
		start, _ = js.Add(start, length)
	}
	results := &js.Array{}
	gt0 := func(v js.Value) bool { return cmp(">", v, 0.0) }
	for i := start; ; {
		if lt0(i) {
			break
		}
		if cmpErr != nil {
			return nil, cmpErr
		}
		if cmp(">", i, length) {
			break
		}
		if gt0(step) {
			if cmp(">=", i, stop) {
				break
			}
		}
		if lt0(step) {
			if cmp("<=", i, stop) {
				break
			}
		}
		v, err := compatMemberLookup(obj, i)
		if err != nil {
			return nil, err
		}
		results.Items = append(results.Items, v)
		if len(results.Items) > 1<<20 {
			return nil, js.NewRangeError("Invalid array length")
		}
		next, err := js.Add(i, step)
		if err != nil {
			return nil, err
		}
		i = next
	}
	return results, nil
}
