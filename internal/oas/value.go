// Package oas renders an API Gateway REST API body from an OpenAPI document.
//
// Output must match, byte for byte, a JavaScript implementation that parses with
// the `yaml` npm package and serialises with JSON.stringify. Values therefore
// follow JavaScript semantics: objects and arrays are shared by reference, and
// object keys iterate in JavaScript property order.
package oas

import (
	"math"
	"slices"
	"strconv"
)

// A value is *object, *array, string, float64, bool or nil (null).

// object is a JavaScript object.
type object struct {
	keys []string // insertion order
	vals map[string]any
}

// array is a JavaScript array.
type array struct {
	items []any
}

func newObject() *object { return &object{vals: map[string]any{}} }

func (o *object) get(k string) (any, bool) {
	v, ok := o.vals[k]
	return v, ok
}

// set keeps an existing key's position and appends a new one.
func (o *object) set(k string, v any) {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
}

func (o *object) del(k string) {
	if _, ok := o.vals[k]; !ok {
		return
	}
	delete(o.vals, k)
	o.keys = slices.DeleteFunc(o.keys, func(s string) bool { return s == k })
}

// ownKeys returns keys in JavaScript property order: array indices ascending,
// then the rest in insertion order.
func (o *object) ownKeys() []string {
	var idx, rest []string
	for _, k := range o.keys {
		if isArrayIndex(k) {
			idx = append(idx, k)
		} else {
			rest = append(rest, k)
		}
	}
	if len(idx) == 0 {
		return rest
	}
	slices.SortFunc(idx, func(a, b string) int {
		x, _ := strconv.ParseUint(a, 10, 32)
		y, _ := strconv.ParseUint(b, 10, 32)
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
		return 0
	})
	return append(idx, rest...)
}

// isArrayIndex reports whether k is a canonical integer in [0, 2^32-2].
func isArrayIndex(k string) bool {
	if k == "0" {
		return true
	}
	if k == "" || k[0] < '1' || k[0] > '9' || len(k) > 10 {
		return false
	}
	n, err := strconv.ParseUint(k, 10, 64)
	return err == nil && n <= math.MaxUint32-1
}

// truthy is JavaScript ToBoolean.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0 && !math.IsNaN(x)
	}
	return true
}

// has is JavaScript `k in v` for own data properties.
func has(v any, k string) bool {
	switch x := v.(type) {
	case *object:
		_, ok := x.get(k)
		return ok
	case *array:
		if k == "length" {
			return true
		}
		i, err := strconv.Atoi(k)
		return err == nil && isArrayIndex(k) && i < len(x.items)
	}
	return false
}

// get is JavaScript v[k] for own data properties; anything else is undefined (nil).
func get(v any, k string) any {
	switch x := v.(type) {
	case *object:
		r, _ := x.get(k)
		return r
	case *array:
		if k == "length" {
			return float64(len(x.items))
		}
		if has(x, k) {
			i, _ := strconv.Atoi(k)
			return x.items[i]
		}
	}
	return nil
}

// toString is JavaScript String(v), as used by template literals.
func toString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return numberToString(x)
	case bool:
		return strconv.FormatBool(x)
	case nil:
		return "null"
	case *array:
		var s string
		for i, it := range x.items {
			if i > 0 {
				s += ","
			}
			if it != nil {
				s += toString(it)
			}
		}
		return s
	}
	return "[object Object]"
}
