package oas

import (
	"fmt"
	"strings"
)

// resolveRefs inlines local $refs as the reference implementation does: a $ref is
// replaced by its target as found (not resolved further, so targets are shared and
// a $ref to a $ref stays one), objects are updated in place, arrays are copied.
func resolveRefs(root any) (any, error) {
	r := refResolver{root: root, open: map[any]bool{}}
	return r.resolve(root)
}

type refResolver struct {
	root any
	open map[any]bool // collections on the current path: a cycle overflows the JS stack
}

func (r refResolver) resolve(v any) (any, error) {
	switch x := v.(type) {
	case *object:
		if ref, ok := x.get("$ref"); ok && truthy(ref) {
			s, ok := ref.(string)
			if !ok {
				return nil, fmt.Errorf("$ref must be a string")
			}
			return resolveRef(s, r.root)
		}
		if r.open[x] {
			return nil, errCircular
		}
		r.open[x] = true
		defer delete(r.open, x)
		for _, k := range x.ownKeys() {
			v, err := r.resolve(x.vals[k])
			if err != nil {
				return nil, err
			}
			x.vals[k] = v
		}
	case *array:
		if r.open[x] {
			return nil, errCircular
		}
		r.open[x] = true
		defer delete(r.open, x)
		out := &array{items: make([]any, len(x.items))}
		for i, it := range x.items {
			v, err := r.resolve(it)
			if err != nil {
				return nil, err
			}
			out.items[i] = v
		}
		return out, nil
	}
	return v, nil
}

// resolveRef follows a local ref. Like the reference, it strips the first "#/" and
// does not decode JSON pointer escapes (~0, ~1).
func resolveRef(ref string, root any) (any, error) {
	cur := root
	for _, k := range strings.Split(strings.Replace(ref, "#/", "", 1), "/") {
		if !has(cur, k) {
			return nil, fmt.Errorf("cannot resolve $ref %s", ref)
		}
		cur = get(cur, k)
	}
	return cur, nil
}
