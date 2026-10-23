// Package js models the JavaScript values a nunjucks template works with and the ECMAScript operations the
// original JavaScript implementation applies to them: type conversion, operators, property access and the
// built-in methods templates can call. Strings are JavaScript strings: sequences of UTF-16 code units, held in
// Go strings as WTF-8 so that unpaired surrogates survive.
package js

import (
	"math"
	"sort"
	"strconv"
)

// Value is one JavaScript value: Undefined, Null, bool, float64, string, *Array, *Object, *SafeString,
// *Function or *RegExp.
type Value any

type undefinedType struct{}
type nullType struct{}

// Undefined and Null are the two JavaScript nullish values.
var (
	Undefined Value = undefinedType{}
	Null      Value = nullType{}
)

// IsUndefined reports whether v is undefined.
func IsUndefined(v Value) bool { _, ok := v.(undefinedType); return ok }

// IsNull reports whether v is null.
func IsNull(v Value) bool { _, ok := v.(nullType); return ok }

// IsNullish reports whether v is null or undefined.
func IsNullish(v Value) bool { return IsUndefined(v) || IsNull(v) }

// Array is a JavaScript array. Arrays are shared by reference, like in JavaScript.
type Array struct {
	Items []Value
	// Extra holds the named own properties of a match result array (index, input, groups).
	Extra *Object
}

// NewArray returns an array holding items.
func NewArray(items ...Value) *Array { return &Array{Items: items} }

// Object is a plain JavaScript object (prototype Object.prototype). Own properties are enumerated in
// ECMAScript order: array-index keys ascending, then the other keys in insertion order.
type Object struct {
	props map[string]Value
	order []string
}

// NewObject returns an empty plain object.
func NewObject() *Object { return &Object{props: map[string]Value{}} }

// Get returns the own property k and whether it exists.
func (o *Object) Get(k string) (Value, bool) {
	v, ok := o.props[k]
	return v, ok
}

// Set creates or updates the own property k; an existing key keeps its position.
func (o *Object) Set(k string, v Value) {
	if _, ok := o.props[k]; !ok {
		o.order = append(o.order, k)
	}
	o.props[k] = v
}

// Has reports whether k is an own property.
func (o *Object) Has(k string) bool {
	_, ok := o.props[k]
	return ok
}

// Delete removes the own property k.
func (o *Object) Delete(k string) {
	if _, ok := o.props[k]; !ok {
		return
	}
	delete(o.props, k)
	for i, key := range o.order {
		if key == k {
			o.order = append(o.order[:i:i], o.order[i+1:]...)
			break
		}
	}
}

// Keys returns the own property keys in ECMAScript enumeration order.
func (o *Object) Keys() []string {
	var idx []string
	var rest []string
	for _, k := range o.order {
		if _, ok := ArrayIndex(k); ok {
			idx = append(idx, k)
		} else {
			rest = append(rest, k)
		}
	}
	sort.Slice(idx, func(i, j int) bool {
		a, _ := ArrayIndex(idx[i])
		b, _ := ArrayIndex(idx[j])
		return a < b
	})
	return append(idx, rest...)
}

// Len returns the number of own properties.
func (o *Object) Len() int { return len(o.order) }

// ArrayIndex reports whether k is a canonical array index ("0" to "4294967294") and returns it.
func ArrayIndex(k string) (uint32, bool) {
	if k == "" || len(k) > 10 || (len(k) > 1 && k[0] == '0') {
		return 0, false
	}
	n, err := strconv.ParseUint(k, 10, 64)
	if err != nil || n >= math.MaxUint32 {
		return 0, false
	}
	return uint32(n), true
}

// SafeString is the nunjucks runtime.SafeString object: a string marked as already escaped. It behaves as an
// object whose own enumerable properties are val and length and whose prototype chain includes
// String.prototype.
type SafeString struct {
	Val string
}

// Function is a callable JavaScript value. Call receives the this value and the arguments.
type Function struct {
	Name string
	Call func(this Value, args []Value) (Value, error)
	// Source is the text Function.prototype.toString gives, when it is known.
	Source string
}

// NewFunction returns a function value.
func NewFunction(name string, call func(this Value, args []Value) (Value, error)) *Function {
	return &Function{Name: name, Call: call}
}

// TypeOf returns the JavaScript typeof of v.
func TypeOf(v Value) string {
	switch v.(type) {
	case undefinedType:
		return "undefined"
	case bool:
		return "boolean"
	case float64:
		return "number"
	case string:
		return "string"
	case *Function:
		return "function"
	default:
		return "object"
	}
}

// IsObject reports whether lib.isObject holds: Object.prototype.toString gives "[object Object]".
func IsObject(v Value) bool {
	switch v.(type) {
	case *Object, *SafeString:
		return true
	}
	return false
}

// Truthy is ToBoolean.
func Truthy(v Value) bool {
	switch x := v.(type) {
	case undefinedType, nullType:
		return false
	case bool:
		return x
	case float64:
		return x != 0 && !math.IsNaN(x)
	case string:
		return x != ""
	}
	return true
}

// Arg returns args[i], or undefined when it is missing.
func Arg(args []Value, i int) Value {
	if i < len(args) {
		return args[i]
	}
	return Undefined
}
