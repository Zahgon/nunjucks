package js

import (
	"math"
	"math/big"
	"strconv"
	"strings"
)

// NumberToString is Number::toString(x) with radix 10: the shortest round-tripping digits, in fixed notation
// for decimal exponents from -6 to 20 and in exponential notation otherwise.
func NumberToString(x float64) string {
	switch {
	case math.IsNaN(x):
		return "NaN"
	case x == 0:
		return "0"
	case math.IsInf(x, 1):
		return "Infinity"
	case math.IsInf(x, -1):
		return "-Infinity"
	case x < 0:
		return "-" + NumberToString(-x)
	}
	digits, n := shortestDigits(x)
	k := len(digits)
	switch {
	case k <= n && n <= 21:
		return digits + strings.Repeat("0", n-k)
	case 0 < n && n <= 21:
		return digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0:
		return "0." + strings.Repeat("0", -n) + digits
	}
	e := n - 1
	sign := "+"
	if e < 0 {
		sign = "-"
		e = -e
	}
	if k == 1 {
		return digits + "e" + sign + strconv.Itoa(e)
	}
	return digits[:1] + "." + digits[1:] + "e" + sign + strconv.Itoa(e)
}

// shortestDigits returns the shortest decimal digits d1..dk of positive finite x and n such that
// x = 0.d1..dk * 10^n.
func shortestDigits(x float64) (string, int) {
	s := strconv.FormatFloat(x, 'e', -1, 64)
	mant, exp, _ := strings.Cut(s, "e")
	digits := strings.Replace(mant, ".", "", 1)
	e, _ := strconv.Atoi(exp)
	return digits, e + 1
}

// exactDecimal returns the exact decimal expansion of positive finite x as integer and fraction digit strings.
func exactDecimal(x float64) (string, string) {
	s := new(big.Float).SetPrec(64).SetFloat64(x).Text('f', 1100)
	ip, fp, _ := strings.Cut(s, ".")
	fp = strings.TrimRight(fp, "0")
	return ip, fp
}

// roundHalfUp rounds the decimal digit string digits (no sign) to keep digits, rounding half up; it returns the
// kept digits and whether a carry added a leading digit.
func roundHalfUp(digits string, keep int) (string, bool) {
	if keep >= len(digits) {
		return digits + strings.Repeat("0", keep-len(digits)), false
	}
	b := []byte(digits[:keep])
	if digits[keep] >= '5' {
		i := keep - 1
		for ; i >= 0; i-- {
			if b[i] == '9' {
				b[i] = '0'
				continue
			}
			b[i]++
			break
		}
		if i < 0 {
			return "1" + string(b), true
		}
	}
	return string(b), false
}

// ToFixed is Number.prototype.toFixed.
func ToFixed(x float64, fd Value) (string, error) {
	f, err := ToIntegerOrInfinity(fd)
	if err != nil {
		return "", err
	}
	if math.IsInf(f, 0) || f < 0 || f > 100 {
		return "", NewRangeError("toFixed() digits argument must be between 0 and 100")
	}
	if math.IsNaN(x) {
		return "NaN", nil
	}
	if math.Abs(x) >= 1e21 || math.IsInf(x, 0) {
		return NumberToString(x), nil
	}
	sign := ""
	if x < 0 {
		sign = "-"
		x = -x
	}
	digits := int(f)
	ip, fp := exactDecimal(x)
	all := ip + fp
	m, carry := roundHalfUp(all, len(ip)+digits)
	intLen := len(ip)
	if carry {
		intLen++
	}
	intPart := strings.TrimLeft(m[:intLen], "0")
	if intPart == "" {
		intPart = "0"
	}
	if digits == 0 {
		return sign + intPart, nil
	}
	return sign + intPart + "." + m[intLen:], nil
}

// significant returns the first p significant digits of positive finite x, rounded half up, and the decimal
// exponent e of the first digit.
func significant(x float64, p int) (string, int) {
	ip, fp := exactDecimal(x)
	all := strings.TrimLeft(ip+fp, "0")
	e := len(ip) - 1
	if strings.TrimLeft(ip, "0") == "" {
		lead := len(fp) - len(strings.TrimLeft(fp, "0"))
		e = -lead - 1
	}
	m, carry := roundHalfUp(all, p)
	if carry {
		m = m[:p]
		e++
	}
	return m, e
}

// ToPrecision is Number.prototype.toPrecision.
func ToPrecision(x float64, pv Value) (string, error) {
	if IsUndefined(pv) {
		return NumberToString(x), nil
	}
	pf, err := ToIntegerOrInfinity(pv)
	if err != nil {
		return "", err
	}
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return NumberToString(x), nil
	}
	if pf < 1 || pf > 100 {
		return "", NewRangeError("toPrecision() argument must be between 1 and 100")
	}
	p := int(pf)
	sign := ""
	if x < 0 {
		sign = "-"
		x = -x
	}
	var m string
	var e int
	if x == 0 {
		m, e = strings.Repeat("0", p), 0
	} else {
		m, e = significant(x, p)
	}
	if e < -6 || e >= p {
		s := m[:1]
		if p > 1 {
			s += "." + m[1:]
		}
		if e >= 0 {
			return sign + s + "e+" + strconv.Itoa(e), nil
		}
		return sign + s + "e-" + strconv.Itoa(-e), nil
	}
	switch {
	case e == p-1:
		return sign + m, nil
	case e >= 0:
		return sign + m[:e+1] + "." + m[e+1:], nil
	}
	return sign + "0." + strings.Repeat("0", -(e+1)) + m, nil
}

// ToExponential is Number.prototype.toExponential.
func ToExponential(x float64, fd Value) (string, error) {
	f, err := ToIntegerOrInfinity(fd)
	if err != nil {
		return "", err
	}
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return NumberToString(x), nil
	}
	if f < 0 || f > 100 {
		return "", NewRangeError("toExponential() argument must be between 0 and 100")
	}
	sign := ""
	if x < 0 {
		sign = "-"
		x = -x
	}
	var m string
	var e int
	switch {
	case x == 0:
		n := 0
		if !IsUndefined(fd) {
			n = int(f)
		}
		m, e = strings.Repeat("0", n+1), 0
	case IsUndefined(fd):
		var n int
		m, n = shortestDigits(x)
		e = n - 1
	default:
		m, e = significant(x, int(f)+1)
	}
	s := m[:1]
	if len(m) > 1 {
		s += "." + m[1:]
	}
	if e >= 0 {
		return sign + s + "e+" + strconv.Itoa(e), nil
	}
	return sign + s + "e-" + strconv.Itoa(-e), nil
}

const radixChars = "0123456789abcdefghijklmnopqrstuvwxyz"

// NumberToRadixString is Number.prototype.toString(radix) for radix other than 10, following V8's
// DoubleToRadixCString.
func NumberToRadixString(value float64, radix int) string {
	switch {
	case math.IsNaN(value):
		return "NaN"
	case math.IsInf(value, 1):
		return "Infinity"
	case math.IsInf(value, -1):
		return "-Infinity"
	case value == 0:
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	integer := math.Floor(value)
	fraction := value - integer
	delta := 0.5 * (math.Nextafter(value, math.Inf(1)) - value)
	delta = math.Max(math.Nextafter(0, 1), delta)
	var frac []byte
	if fraction >= delta {
		frac = append(frac, '.')
		for {
			// Explicit conversions keep each product rounded: a fused multiply-add would change the digits.
			fraction = float64(fraction * float64(radix))
			delta = float64(delta * float64(radix))
			digit := int(fraction)
			frac = append(frac, radixChars[digit])
			fraction = float64(fraction - float64(digit))
			if fraction > 0.5 || (fraction == 0.5 && digit&1 == 1) {
				if float64(fraction+delta) > 1 {
					for {
						last := len(frac) - 1
						if last == 0 {
							integer++
							break
						}
						c := frac[last]
						d := strings.IndexByte(radixChars, c)
						if d+1 < radix {
							frac[last] = radixChars[d+1]
							break
						}
						frac = frac[:last]
					}
					break
				}
			}
			if fraction < delta {
				break
			}
		}
		if len(frac) == 1 {
			frac = nil
		}
	}
	var ints []byte
	for exponentOf(integer/float64(radix)) > 0 {
		integer /= float64(radix)
		ints = append(ints, '0')
	}
	for {
		rem := math.Mod(integer, float64(radix))
		ints = append(ints, radixChars[int(rem)])
		integer = (integer - rem) / float64(radix)
		if integer <= 0 {
			break
		}
	}
	for i, j := 0, len(ints)-1; i < j; i, j = i+1, j-1 {
		ints[i], ints[j] = ints[j], ints[i]
	}
	out := string(ints) + string(frac)
	if negative {
		return "-" + out
	}
	return out
}

// exponentOf is V8's Double::Exponent: the binary exponent of the value's 53-bit integer significand.
func exponentOf(d float64) int {
	bits := math.Float64bits(d)
	biased := int(bits>>52) & 0x7FF
	if biased == 0 {
		return -1074
	}
	return biased - 0x3FF - 52
}

// ToIntegerOrInfinity is the ECMAScript abstract operation of that name.
func ToIntegerOrInfinity(v Value) (float64, error) {
	n, err := ToNumber(v)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(n) || n == 0 {
		return 0, nil
	}
	return math.Trunc(n), nil
}

// ToInt32 is the ECMAScript ToInt32 of a number.
func ToInt32(n float64) int32 {
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return 0
	}
	n = math.Trunc(n)
	m := math.Mod(n, 4294967296)
	if m < 0 {
		m += 4294967296
	}
	return int32(uint32(m))
}

// StringToNumber is ToNumber applied to a string.
func StringToNumber(s string) float64 {
	s = TrimJS(s, true, true)
	if s == "" {
		return 0
	}
	switch s {
	case "Infinity", "+Infinity":
		return math.Inf(1)
	case "-Infinity":
		return math.Inf(-1)
	}
	if len(s) > 2 && s[0] == '0' {
		radix := 0
		switch s[1] {
		case 'x', 'X':
			radix = 16
		case 'o', 'O':
			radix = 8
		case 'b', 'B':
			radix = 2
		}
		if radix != 0 {
			v, ok := parseDigits(s[2:], radix)
			if !ok || len(s[2:]) == 0 {
				return math.NaN()
			}
			return v
		}
	}
	if n := decimalPrefix(s); n != len(s) || n == 0 {
		return math.NaN()
	}
	return parseDecimal(s)
}

// decimalPrefix returns the length of the longest prefix of s that is a StrDecimalLiteral without Infinity.
func decimalPrefix(s string) int {
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	start := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	intDigits := i - start
	fracDigits := 0
	if i < len(s) && s[i] == '.' {
		j := i + 1
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		fracDigits = j - i - 1
		if intDigits > 0 || fracDigits > 0 {
			i = j
		}
	}
	if intDigits == 0 && fracDigits == 0 {
		return 0
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		k := j
		for k < len(s) && s[k] >= '0' && s[k] <= '9' {
			k++
		}
		if k > j {
			i = k
		}
	}
	return i
}

func parseDecimal(s string) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return v
		}
		if strings.HasSuffix(s, ".") {
			return parseDecimal(strings.TrimSuffix(s, "."))
		}
		return math.NaN()
	}
	return v
}

func parseDigits(s string, radix int) (float64, bool) {
	n, ok := new(big.Int).SetString(s, radix)
	if !ok {
		return 0, false
	}
	f, _ := new(big.Float).SetInt(n).Float64()
	return f, true
}

// ParseFloat is the global parseFloat.
func ParseFloat(v Value) (float64, error) {
	s, err := ToStringErr(v)
	if err != nil {
		return 0, err
	}
	s = TrimJS(s, true, false)
	for _, inf := range []string{"Infinity", "+Infinity"} {
		if strings.HasPrefix(s, inf) {
			return math.Inf(1), nil
		}
	}
	if strings.HasPrefix(s, "-Infinity") {
		return math.Inf(-1), nil
	}
	n := decimalPrefix(s)
	if n == 0 {
		return math.NaN(), nil
	}
	return parseDecimal(s[:n]), nil
}

// ParseInt is the global parseInt.
func ParseInt(v, radixV Value) (float64, error) {
	s, err := ToStringErr(v)
	if err != nil {
		return 0, err
	}
	s = TrimJS(s, true, false)
	sign := 1.0
	if s != "" && (s[0] == '+' || s[0] == '-') {
		if s[0] == '-' {
			sign = -1
		}
		s = s[1:]
	}
	rn, err := ToNumber(radixV)
	if err != nil {
		return 0, err
	}
	r := int(ToInt32(rn))
	strip := true
	if r != 0 {
		if r < 2 || r > 36 {
			return math.NaN(), nil
		}
		if r != 16 {
			strip = false
		}
	} else {
		r = 10
	}
	if strip && len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		s = s[2:]
		r = 16
	}
	end := 0
	for end < len(s) {
		c := s[end]
		d := -1
		switch {
		case c >= '0' && c <= '9':
			d = int(c - '0')
		case c >= 'a' && c <= 'z':
			d = int(c-'a') + 10
		case c >= 'A' && c <= 'Z':
			d = int(c-'A') + 10
		}
		if d < 0 || d >= r {
			break
		}
		end++
	}
	if end == 0 {
		return math.NaN(), nil
	}
	f, _ := parseDigits(strings.ToLower(s[:end]), r)
	return sign * f, nil
}
