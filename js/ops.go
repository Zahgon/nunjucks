package js

import (
	"math"
	"strings"
)

// ToPrimitive converts an object to a primitive with the default or number hint; arrays and plain objects go
// through toString, SafeString through valueOf.
func ToPrimitive(v Value) (Value, error) {
	switch x := v.(type) {
	case *SafeString:
		return x.Val, nil
	case *Object:
		return objectToPrimitive(x)
	case *Array, *RegExp, *Function:
		return ToStringErr(x)
	}
	return v, nil
}

// objectToPrimitive is OrdinaryToPrimitive of a plain object: an own valueOf or toString method is called, an
// own toString that is not callable leaves no way to convert the object.
func objectToPrimitive(o *Object) (Value, error) {
	for _, name := range []string{"valueOf", "toString"} {
		m, own := o.Get(name)
		if !own {
			if name == "toString" {
				return "[object Object]", nil
			}
			continue
		}
		fn, ok := m.(*Function)
		if !ok {
			continue
		}
		r, err := fn.Call(o, nil)
		if err != nil {
			return nil, err
		}
		if isPrimitive(r) {
			return r, nil
		}
	}
	return nil, NewTypeError("Cannot convert object to primitive value")
}

// ToNumber is the ECMAScript ToNumber.
func ToNumber(v Value) (float64, error) {
	switch x := v.(type) {
	case undefinedType:
		return math.NaN(), nil
	case nullType:
		return 0, nil
	case bool:
		if x {
			return 1, nil
		}
		return 0, nil
	case float64:
		return x, nil
	case string:
		return StringToNumber(x), nil
	}
	p, err := ToPrimitive(v)
	if err != nil {
		return 0, err
	}
	return ToNumber(p)
}

// Num is ToNumber for values whose conversion cannot throw.
func Num(v Value) float64 {
	n, _ := ToNumber(v)
	return n
}

// ToString is the ECMAScript ToString for values whose conversion cannot throw.
func ToString(v Value) string {
	s, _ := ToStringErr(v)
	return s
}

// ToStringErr is the ECMAScript ToString.
func ToStringErr(v Value) (string, error) {
	return toString(v, nil)
}

func toString(v Value, seen map[*Array]bool) (string, error) {
	switch x := v.(type) {
	case undefinedType:
		return "undefined", nil
	case nullType:
		return "null", nil
	case bool:
		if x {
			return "true", nil
		}
		return "false", nil
	case float64:
		return NumberToString(x), nil
	case string:
		return x, nil
	case *SafeString:
		return x.Val, nil
	case *Object:
		p, err := objectToPrimitive(x)
		if err != nil {
			return "", err
		}
		return toString(p, seen)
	case *RegExp:
		return x.String(), nil
	case *Function:
		if x.Source != "" {
			return x.Source, nil
		}
		return "function " + x.Name + "() { [native code] }", nil
	case *Array:
		return joinArray(x, ",", seen)
	}
	return "", NewTypeError("Cannot convert value to a string")
}

// joinArray is Array.prototype.join: nullish elements become empty strings and a cyclic reference joins as an
// empty string, as in V8.
func joinArray(a *Array, sep string, seen map[*Array]bool) (string, error) {
	if seen == nil {
		seen = map[*Array]bool{}
	}
	if seen[a] {
		return "", nil
	}
	seen[a] = true
	defer delete(seen, a)
	var b strings.Builder
	for i, it := range a.Items {
		if i > 0 {
			b.WriteString(sep)
		}
		if IsNullish(it) {
			continue
		}
		s, err := toString(it, seen)
		if err != nil {
			return "", err
		}
		b.WriteString(s)
	}
	return b.String(), nil
}

// Join is Array.prototype.join with an explicit separator.
func Join(a *Array, sep string) (string, error) { return joinArray(a, sep, nil) }

// Add is the binary + operator.
func Add(a, b Value) (Value, error) {
	pa, err := ToPrimitive(a)
	if err != nil {
		return nil, err
	}
	pb, err := ToPrimitive(b)
	if err != nil {
		return nil, err
	}
	_, sa := pa.(string)
	_, sb := pb.(string)
	if sa || sb {
		return ToString(pa) + ToString(pb), nil
	}
	return Num(pa) + Num(pb), nil
}

// Arith applies -, *, / or % to the numeric values of a and b.
func Arith(op string, a, b Value) (Value, error) {
	x, err := ToNumber(a)
	if err != nil {
		return nil, err
	}
	y, err := ToNumber(b)
	if err != nil {
		return nil, err
	}
	switch op {
	case "-":
		return x - y, nil
	case "*":
		return float64(x * y), nil
	case "/":
		return float64(x / y), nil
	case "%":
		return Mod(x, y), nil
	}
	return nil, NewError("unknown operator " + op)
}

// Mod is the JavaScript % operator on numbers.
func Mod(x, y float64) float64 {
	if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || y == 0 {
		return math.NaN()
	}
	if math.IsInf(y, 0) || x == 0 {
		return x
	}
	return math.Mod(x, y)
}

// Pow is Math.pow.
func Pow(x, y float64) float64 {
	if math.IsNaN(y) {
		return math.NaN()
	}
	if y == 0 {
		return 1
	}
	if (x == 1 || x == -1) && math.IsInf(y, 0) {
		return math.NaN()
	}
	return fdlibmPow(x, y)
}

// Round is Math.round: ties go towards positive infinity.
func Round(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) || x == math.Trunc(x) {
		return x
	}
	if x > 0 && x < 0.5 {
		return 0
	}
	if x < 0 && x >= -0.5 {
		return math.Copysign(0, -1)
	}
	f := math.Floor(x)
	if x-f >= 0.5 {
		return f + 1
	}
	return f
}

// StrictEquals is ===.
func StrictEquals(a, b Value) bool {
	switch x := a.(type) {
	case undefinedType:
		return IsUndefined(b)
	case nullType:
		return IsNull(b)
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case float64:
		y, ok := b.(float64)
		return ok && x == y
	case string:
		y, ok := b.(string)
		return ok && x == y
	}
	return a == b
}

// LooseEquals is ==.
func LooseEquals(a, b Value) (bool, error) {
	if TypeOf(a) == TypeOf(b) && IsNull(a) == IsNull(b) {
		return StrictEquals(a, b), nil
	}
	if IsNullish(a) && IsNullish(b) {
		return true, nil
	}
	if IsNullish(a) || IsNullish(b) {
		return false, nil
	}
	switch x := a.(type) {
	case float64:
		if s, ok := b.(string); ok {
			return x == StringToNumber(s), nil
		}
	case string:
		if y, ok := b.(float64); ok {
			return StringToNumber(x) == y, nil
		}
	}
	if x, ok := a.(bool); ok {
		return LooseEquals(boolNum(x), b)
	}
	if y, ok := b.(bool); ok {
		return LooseEquals(a, boolNum(y))
	}
	if isPrimitive(a) && !isPrimitive(b) {
		pb, err := ToPrimitive(b)
		if err != nil {
			return false, err
		}
		return LooseEquals(a, pb)
	}
	if !isPrimitive(a) && isPrimitive(b) {
		pa, err := ToPrimitive(a)
		if err != nil {
			return false, err
		}
		return LooseEquals(pa, b)
	}
	return false, nil
}

func boolNum(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func isPrimitive(v Value) bool {
	switch v.(type) {
	case undefinedType, nullType, bool, float64, string:
		return true
	}
	return false
}

// Compare applies <, >, <= or >=.
func Compare(op string, a, b Value) (bool, error) {
	pa, err := ToPrimitive(a)
	if err != nil {
		return false, err
	}
	pb, err := ToPrimitive(b)
	if err != nil {
		return false, err
	}
	sa, oka := pa.(string)
	sb, okb := pb.(string)
	if oka && okb {
		c := CompareStrings(sa, sb)
		switch op {
		case "<":
			return c < 0, nil
		case ">":
			return c > 0, nil
		case "<=":
			return c <= 0, nil
		}
		return c >= 0, nil
	}
	x, y := Num(pa), Num(pb)
	if math.IsNaN(x) || math.IsNaN(y) {
		return false, nil
	}
	switch op {
	case "<":
		return x < y, nil
	case ">":
		return x > y, nil
	case "<=":
		return x <= y, nil
	}
	return x >= y, nil
}

// Less is a < b for sort comparators, which have no way to fail: a conversion error compares as false.
func Less(a, b Value) bool {
	r, _ := Compare("<", a, b)
	return r
}

// Greater is a > b for sort comparators; a conversion error compares as false.
func Greater(a, b Value) bool {
	r, _ := Compare(">", a, b)
	return r
}
