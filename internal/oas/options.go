package oas

import (
	"cmp"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Options controls what Render adds to the document. Attribute names in
// ParseOptions are the function's `options` argument.
type Options struct {
	Integration          *Integration // default backend
	SecondaryIntegration *Integration // takes precedence on the paths it matches
	Authorizer           *Authorizer
	StripPrefix          *string // removed from public paths; nil disables
	ExcludedPaths        []string
	PathRewrites         []PathRewrite
	CatchAllNotFound     bool // add a /{proxy+} MOCK that returns 404
}

// Integration is an HTTP_PROXY backend for operations without their own integration.
type Integration struct {
	BaseURI               string   // the operation's path is appended
	ConnectionID          *string  // VPC link ID; nil for an internet integration
	IntegrationTarget     *string  // load balancer ARN, for VPC links v2
	Paths                 []string // segment-aware prefixes; empty uses the default
	IgnoredPaths          []string
	PreserveForwardedHost bool // map X-Forwarded-Host from context.domainName
}

// Authorizer is a Lambda token authorizer for operations without their own security.
type Authorizer struct {
	SecurityScheme               string
	URI                          string
	Credentials                  *string
	ResultTTLInSeconds           float64
	Header                       string
	IdentityValidationExpression *string
	RequestHeaders               []Header // mapped to the backend, in order
}

// Header maps a backend request header from a value such as context.authorizer.x.
type Header struct {
	Name  string
	Value string
}

// PathRewrite sends a public path to a different backend path on the same host.
type PathRewrite struct {
	Path        string
	BackendPath string
}

// ParseOptions decodes the function's `options` argument: nil, or a map with
// string, bool, float64, []any and map[string]any values. Unknown attributes are errors.
func ParseOptions(v any) (Options, error) {
	var o Options
	r := newReader(v, "integration", "secondary_integration", "authorizer",
		"strip_prefix", "excluded_paths", "path_rewrites", "catch_all_not_found")
	o.StripPrefix = r.str("strip_prefix", false)
	o.ExcludedPaths = r.strs("excluded_paths")
	o.CatchAllNotFound = r.boolean("catch_all_not_found")
	o.PathRewrites = r.rewrites("path_rewrites")
	o.Integration = r.integration("integration")
	o.SecondaryIntegration = r.integration("secondary_integration")
	o.Authorizer = r.authorizer("authorizer")
	return o, *r.err
}

// reader decodes one object. The first error sticks; later calls return zero values.
type reader struct {
	path string
	m    map[string]any
	err  *error
}

func newReader(v any, allowed ...string) *reader {
	var err error
	return newChild(&err, "", v, allowed)
}

func newChild(err *error, path string, v any, allowed []string) *reader {
	r := &reader{path: path, err: err, m: map[string]any{}}
	if v == nil || *err != nil {
		return r
	}
	m, ok := v.(map[string]any)
	if !ok {
		r.fail("%s must be an object", cmp.Or(path, "options"))
		return r
	}
	var unknown []string
	for k := range m {
		if !slices.Contains(allowed, k) {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		r.fail("%s: unsupported attribute(s) %s; supported: %s",
			cmp.Or(path, "options"), strings.Join(unknown, ", "), strings.Join(allowed, ", "))
		return r
	}
	r.m = m
	return r
}

func (r *reader) child(k string, allowed ...string) *reader {
	if r.m[k] == nil {
		return nil
	}
	return newChild(r.err, r.name(k), r.m[k], allowed)
}

func (r *reader) name(k string) string {
	if r.path == "" {
		return k
	}
	return r.path + "." + k
}

func (r *reader) fail(format string, a ...any) {
	if *r.err == nil {
		*r.err = fmt.Errorf(format, a...)
	}
}

func (r *reader) str(k string, required bool) *string {
	switch v := r.m[k].(type) {
	case string:
		return &v
	case nil:
		if required {
			r.fail("%s is required", r.name(k))
		}
	default:
		r.fail("%s must be a string", r.name(k))
	}
	return nil
}

func (r *reader) boolean(k string) bool {
	switch v := r.m[k].(type) {
	case bool:
		return v
	case nil:
	default:
		r.fail("%s must be a bool", r.name(k))
	}
	return false
}

func (r *reader) number(k string, def float64) float64 {
	switch v := r.m[k].(type) {
	case float64:
		return v
	case nil:
	default:
		r.fail("%s must be a number", r.name(k))
	}
	return def
}

func (r *reader) strs(k string) []string {
	if r.m[k] == nil {
		return nil
	}
	l, ok := r.m[k].([]any)
	out := make([]string, len(l))
	for i, it := range l {
		if out[i], ok = it.(string); !ok {
			break
		}
	}
	if !ok {
		r.fail("%s must be a list of strings", r.name(k))
		return nil
	}
	return out
}

// rewrites returns a map's entries sorted by key, as Terraform orders map keys.
func (r *reader) rewrites(k string) []PathRewrite {
	if r.m[k] == nil {
		return nil
	}
	m, ok := r.m[k].(map[string]any)
	var out []PathRewrite
	for _, p := range sortedKeys(m) {
		var s string
		if s, ok = m[p].(string); !ok {
			break
		}
		out = append(out, PathRewrite{Path: p, BackendPath: s})
	}
	if !ok {
		r.fail("%s must be a map of strings", r.name(k))
		return nil
	}
	return out
}

func (r *reader) integration(k string) *Integration {
	c := r.child(k, "base_uri", "connection_id", "integration_target",
		"paths", "ignored_paths", "preserve_forwarded_host")
	if c == nil {
		return nil
	}
	i := &Integration{
		ConnectionID:          c.str("connection_id", false),
		IntegrationTarget:     c.str("integration_target", false),
		Paths:                 c.strs("paths"),
		IgnoredPaths:          c.strs("ignored_paths"),
		PreserveForwardedHost: c.boolean("preserve_forwarded_host"),
	}
	if s := c.str("base_uri", true); s != nil {
		i.BaseURI = *s
	}
	return i
}

func (r *reader) authorizer(k string) *Authorizer {
	c := r.child(k, "security_scheme", "uri", "credentials", "result_ttl_in_seconds",
		"header", "identity_validation_expression", "request_headers")
	if c == nil {
		return nil
	}
	a := &Authorizer{
		Credentials:                  c.str("credentials", false),
		ResultTTLInSeconds:           c.number("result_ttl_in_seconds", 300),
		Header:                       "Authorization",
		IdentityValidationExpression: c.str("identity_validation_expression", false),
	}
	if s := c.str("security_scheme", true); s != nil {
		a.SecurityScheme = *s
	}
	if s := c.str("uri", true); s != nil {
		a.URI = *s
	}
	if s := c.str("header", false); s != nil {
		a.Header = *s
	}
	hs := c.m["request_headers"]
	if hs == nil {
		return a
	}
	l, ok := hs.([]any)
	if !ok {
		c.fail("%s must be a list of objects", c.name("request_headers"))
		return a
	}
	for i, h := range l {
		hr := newChild(c.err, fmt.Sprintf("%s[%d]", c.name("request_headers"), i), h, []string{"name", "value"})
		n, v := hr.str("name", true), hr.str("value", true)
		if n != nil && v != nil {
			a.RequestHeaders = append(a.RequestHeaders, Header{Name: *n, Value: *v})
		}
	}
	return a
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
