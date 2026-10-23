package js

import (
	"math"
	"testing"
)

func TestNumberToString(t *testing.T) {
	tenth, fifth := 0.1, 0.2
	cases := []struct {
		in   float64
		want string
	}{
		{0, "0"}, {math.Copysign(0, -1), "0"}, {1e21, "1e+21"}, {1e20, "100000000000000000000"},
		{1e-7, "1e-7"}, {1e-6, "0.000001"}, {tenth + fifth, "0.30000000000000004"}, {-1.5, "-1.5"},
		{123456789012345680000, "123456789012345680000"}, {5e-324, "5e-324"}, {math.Inf(-1), "-Infinity"},
		{math.NaN(), "NaN"}, {1.7976931348623157e308, "1.7976931348623157e+308"},
	}
	for _, c := range cases {
		if got := NumberToString(c.in); got != c.want {
			t.Errorf("NumberToString(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNumberMethods(t *testing.T) {
	cases := []struct {
		name string
		fn   func() (string, error)
		want string
	}{
		{"(2.5).toFixed(0)", func() (string, error) { return ToFixed(2.5, 0.0) }, "3"},
		{"(1.005).toFixed(2)", func() (string, error) { return ToFixed(1.005, 2.0) }, "1.00"},
		{"(-0.0001).toFixed(2)", func() (string, error) { return ToFixed(-0.0001, 2.0) }, "-0.00"},
		{"(123.456).toPrecision(4)", func() (string, error) { return ToPrecision(123.456, 4.0) }, "123.5"},
		{"(0.000123).toPrecision(2)", func() (string, error) { return ToPrecision(0.000123, 2.0) }, "0.00012"},
		{"(1e21).toPrecision(3)", func() (string, error) { return ToPrecision(1e21, 3.0) }, "1.00e+21"},
		{"(123456).toExponential(2)", func() (string, error) { return ToExponential(123456, 2.0) }, "1.23e+5"},
		{"(255).toString(16)", func() (string, error) { return NumberToRadixString(255, 16), nil }, "ff"},
		{"(0.2).toString(36)", func() (string, error) { return NumberToRadixString(0.2, 36), nil }, "0.77777777778"},
	}
	for _, c := range cases {
		got, err := c.fn()
		if err != nil || got != c.want {
			t.Errorf("%s = %q (%v), want %q", c.name, got, err, c.want)
		}
	}
}

func TestPow(t *testing.T) {
	cases := []struct{ x, y, want float64 }{
		{2, 0.2, 1.148698354997035}, {-7, -2, 0.020408163265306124}, {10, -2, 0.01}, {2, 10, 1024},
	}
	for _, c := range cases {
		if got := Pow(c.x, c.y); got != c.want {
			t.Errorf("Pow(%v, %v) = %v, want %v", c.x, c.y, got, c.want)
		}
	}
	if got := Pow(1, math.Inf(1)); !math.IsNaN(got) {
		t.Errorf("Pow(1, Infinity) = %v, want NaN", got)
	}
}

func TestStrings(t *testing.T) {
	if got := Len16("a😀"); got != 3 {
		t.Errorf(`Len16("a😀") = %d, want 3`, got)
	}
	if got := ToUTF8(Slice16("😀", 0, 1)); got != "�" {
		t.Errorf("a lone surrogate prints as %q, want U+FFFD", got)
	}
	if got := UpperCase("straße ﬁ"); got != "STRASSE FI" {
		t.Errorf("UpperCase = %q, want %q", got, "STRASSE FI")
	}
	if got := LowerCase("ΟΔΟΣ"); got != "οδος" {
		t.Errorf("LowerCase = %q, want final sigma %q", got, "οδος")
	}
}

func TestLooseEquality(t *testing.T) {
	cases := []struct {
		a, b Value
		want bool
	}{
		{"1", 1.0, true}, {Null, Undefined, true}, {Null, 0.0, false}, {"", 0.0, true}, {&Array{}, false, true},
		{&Array{Items: []Value{3.0}}, "3", true}, {math.NaN(), math.NaN(), false}, {true, "1", true},
	}
	for _, c := range cases {
		got, err := LooseEquals(c.a, c.b)
		if err != nil || got != c.want {
			t.Errorf("LooseEquals(%v, %v) = %v (%v), want %v", c.a, c.b, got, err, c.want)
		}
	}
}

func TestRegExp(t *testing.T) {
	re, err := NewRegExp(`(\d)(\d)?`, "g")
	if err != nil {
		t.Fatalf("NewRegExp: %v", err)
	}
	got, err := Replace("a1b22c", re, "[$2$1]", false)
	if err != nil || got != "a[1]b[22]c" {
		t.Errorf("replace = %q (%v), want %q", got, err, "a[1]b[22]c")
	}
	if _, err := NewRegExp("(", ""); err == nil || err.Error() != "SyntaxError: Invalid regular expression: /(/: Unterminated group" {
		t.Errorf("NewRegExp(\"(\") error = %v, want the V8 SyntaxError", err)
	}
	words, _ := NewRegExp(`\s+`, "")
	parts, err := Split("a  b\tc", words, Undefined)
	if err != nil || len(parts.(*Array).Items) != 3 {
		t.Errorf("split on \\s+ = %v (%v), want 3 parts", parts, err)
	}
}

func TestJSONRoundTrip(t *testing.T) {
	v, err := ParseJSON(`{"b":1,"2":[true,null,"😀"],"a":{"x":-0.0},"b":3}`)
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}
	got, ok, err := Stringify(v, Undefined)
	want := `{"2":[true,null,"😀"],"b":3,"a":{"x":0}}`
	if err != nil || !ok || got != want {
		t.Errorf("Stringify = %q (%v), want %q", got, err, want)
	}
}
