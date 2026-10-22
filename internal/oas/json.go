package oas

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// maxBodySize bounds the output: a $ref target is written out in full at every use.
const maxBodySize = 64 << 20

var (
	errCircular = errors.New("the document is circular (a $ref or alias contains itself)")
	errTooLarge = errors.New("the rendered body exceeds 64 MiB")
)

// toJSON is JSON.stringify(v) without replacer or indent.
func toJSON(v any) (string, error) {
	var b strings.Builder
	if err := writeJSON(&b, v, map[any]bool{}); err != nil {
		return "", err
	}
	return b.String(), nil
}

// writeJSON writes v; open holds the collections being written, to detect cycles.
func writeJSON(b *strings.Builder, v any, open map[any]bool) error {
	if b.Len() > maxBodySize {
		return errTooLarge
	}
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			b.WriteString("null")
		} else {
			b.WriteString(numberToString(x))
		}
	case string:
		quote(b, x)
	case *array:
		if open[x] {
			return errCircular
		}
		open[x] = true
		b.WriteByte('[')
		for i, it := range x.items {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeJSON(b, it, open); err != nil {
				return err
			}
		}
		b.WriteByte(']')
		delete(open, x)
	case *object:
		if open[x] {
			return errCircular
		}
		open[x] = true
		b.WriteByte('{')
		for i, k := range x.ownKeys() {
			if i > 0 {
				b.WriteByte(',')
			}
			quote(b, k)
			b.WriteByte(':')
			if err := writeJSON(b, x.vals[k], open); err != nil {
				return err
			}
		}
		b.WriteByte('}')
		delete(open, x)
	}
	return nil
}

// quote is JSON.stringify's string quoting: unlike encoding/json, it escapes
// neither HTML characters nor U+2028/U+2029.
func quote(b *strings.Builder, s string) {
	const hex = "0123456789abcdef"
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if c < 0x20 {
				b.WriteString(`\u00`)
				b.WriteByte(hex[c>>4])
				b.WriteByte(hex[c&0xf])
			} else {
				b.WriteByte(c) // multi-byte UTF-8 passes through byte by byte
			}
		}
	}
	b.WriteByte('"')
}

// numberToString is ECMAScript Number::toString for finite and non-finite values.
func numberToString(f float64) string {
	switch {
	case f == 0:
		return "0"
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	}
	sign := ""
	if f < 0 {
		sign, f = "-", -f
	}
	// Shortest round-trip digits; the exponent n places the decimal point.
	mant, exp, _ := strings.Cut(strconv.FormatFloat(f, 'e', -1, 64), "e")
	digits := strings.Replace(mant, ".", "", 1)
	e, _ := strconv.Atoi(exp)
	k, n := len(digits), e+1
	switch {
	case k <= n && n <= 21:
		return sign + digits + strings.Repeat("0", n-k)
	case 0 < n && n <= 21:
		return sign + digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0:
		return sign + "0." + strings.Repeat("0", -n) + digits
	}
	m := digits[:1]
	if k > 1 {
		m += "." + digits[1:]
	}
	if e < 0 {
		return sign + m + "e-" + strconv.Itoa(-e)
	}
	return sign + m + "e+" + strconv.Itoa(e)
}
