package profile

import (
	"math"
	"strconv"
	"strings"
)

// jsNumberString formats f the way JavaScript's String(number) does.
func jsNumberString(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	case f == 0:
		return "0"
	}
	sign := ""
	if f < 0 {
		sign = "-"
		f = -f
	}
	// "d.ddde±xx": the shortest digits that round-trip, and the exponent.
	e := strconv.FormatFloat(f, 'e', -1, 64)
	mant, expText, _ := strings.Cut(e, "e")
	digits := strings.Replace(mant, ".", "", 1)
	exp, _ := strconv.Atoi(expText)
	k := len(digits)
	n := exp + 1 // the value is 0.digits * 10^n

	switch {
	case k <= n && n <= 21:
		return sign + digits + strings.Repeat("0", n-k)
	case 0 < n && n <= 21:
		return sign + digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0:
		return sign + "0." + strings.Repeat("0", -n) + digits
	}
	expSign := "+"
	if n-1 < 0 {
		expSign = "-"
	}
	abs := n - 1
	if abs < 0 {
		abs = -abs
	}
	if k == 1 {
		return sign + digits + "e" + expSign + strconv.Itoa(abs)
	}
	return sign + digits[:1] + "." + digits[1:] + "e" + expSign + strconv.Itoa(abs)
}
