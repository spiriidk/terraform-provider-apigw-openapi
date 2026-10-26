package oas

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// methods are the operations that get an integration (OpenAPI keys, any case).
var methods = map[string]bool{"get": true, "post": true, "patch": true, "put": true, "head": true, "delete": true}

// catchAllMethods get the 404 MOCK on /{proxy+} unless the document defines them.
var catchAllMethods = []string{"get", "post", "put", "patch", "delete", "head", "options"}

// Render returns the REST API body for an OpenAPI document (YAML or JSON).
func Render(spec string, o Options) (string, error) {
	doc, err := parseDocument(spec)
	if err != nil {
		return "", err
	}
	// Two independent graphs, as in the reference: operations are read from `parsed`
	// and written to `out`. strip_prefix moves `parsed` path items into `out`.
	parsed, err := load(doc)
	if err != nil {
		return "", err
	}
	out, err := load(doc)
	if err != nil {
		return "", err
	}
	if !truthy(get(out, "paths")) {
		out.set("paths", newObject())
	}
	paths, ok := get(out, "paths").(*object)
	if !ok {
		return "", fmt.Errorf("paths must be an object")
	}
	parsedPaths := get(parsed, "paths")

	for _, p := range o.ExcludedPaths {
		if !truthy(get(paths, p)) && !truthy(get(parsedPaths, p)) {
			return "", fmt.Errorf("excluded path %s is not in the spec", p)
		}
		paths.del(p)
		if pp, ok := parsedPaths.(*object); ok {
			pp.del(p)
		}
	}

	if o.Authorizer != nil {
		if err := addSecurityScheme(out, o.Authorizer); err != nil {
			return "", err
		}
	}

	if parsedPaths != nil {
		pp, ok := parsedPaths.(*object)
		if !ok {
			return "", fmt.Errorf("paths must be an object")
		}
		for _, p := range pp.ownKeys() {
			if err := renderPath(p, pp, paths, o); err != nil {
				return "", err
			}
		}
	}

	for _, rw := range o.PathRewrites {
		if err := rewritePath(paths, rw); err != nil {
			return "", err
		}
	}

	if o.CatchAllNotFound {
		proxy, err := ensureObject(paths, "/{proxy+}")
		if err != nil {
			return "", err
		}
		for _, m := range catchAllMethods {
			if !truthy(get(proxy, m)) {
				proxy.set(m, notFound())
			}
		}
	}

	return toJSON(out)
}

func load(doc *document) (*object, error) {
	var v any
	if doc != nil {
		var err error
		if v, err = toValue(doc); err != nil {
			return nil, err
		}
	}
	v, err := resolveRefs(v)
	if err != nil {
		return nil, err
	}
	o, ok := v.(*object)
	if !ok {
		return nil, fmt.Errorf("spec must be a YAML or JSON object")
	}
	return o, nil
}

func addSecurityScheme(doc *object, a *Authorizer) error {
	components, err := ensureObject(doc, "components")
	if err != nil {
		return err
	}
	schemes, err := ensureObject(components, "securitySchemes")
	if err != nil {
		return err
	}
	auth := newObject()
	auth.set("type", "token")
	auth.set("authorizerUri", a.URI)
	if a.Credentials != nil {
		auth.set("authorizerCredentials", *a.Credentials)
	}
	auth.set("authorizerResultTtlInSeconds", a.ResultTTLInSeconds)
	auth.set("identitySource", "method.request.header."+a.Header)
	if a.IdentityValidationExpression != nil {
		auth.set("identityValidationExpression", *a.IdentityValidationExpression)
	}
	scheme := newObject()
	scheme.set("type", "apiKey")
	scheme.set("name", a.Header)
	scheme.set("in", "header")
	scheme.set("x-amazon-apigateway-authtype", "custom")
	scheme.set("x-amazon-apigateway-authorizer", auth)
	schemes.set(a.SecurityScheme, scheme)
	return nil
}

// renderPath adds security and integrations to the operations of one path.
func renderPath(path string, parsedPaths, paths *object, o Options) error {
	var ops *object
	switch x := get(parsedPaths, path).(type) {
	case nil:
		return fmt.Errorf("path %s: path item is null", path)
	case *object:
		ops = x
	default:
		return nil
	}
	for _, method := range ops.ownKeys() {
		if !methods[strings.ToLower(method)] {
			continue
		}
		op := get(ops, method)
		if op == nil {
			return fmt.Errorf("%s %s: operation is null", method, path)
		}

		publicPath := path
		if o.StripPrefix != nil {
			publicPath = strings.Replace(path, *o.StripPrefix, "", 1)
			paths.del(path)
			paths.set(publicPath, get(parsedPaths, path))
		}
		// The operation to write to; it may differ from op (see Render).
		target := func() (*object, error) {
			t, ok := get(get(paths, publicPath), method).(*object)
			if !ok {
				return nil, fmt.Errorf("%s %s: operation must be an object", method, path)
			}
			return t, nil
		}

		security := get(op, "security")
		if a := o.Authorizer; a != nil && !truthy(security) {
			t, err := target()
			if err != nil {
				return err
			}
			req := newObject()
			req.set(a.SecurityScheme, &array{})
			t.set("security", &array{items: []any{req}})
		}
		authorized, err := usesAuthorizer(security, o.Authorizer)
		if err != nil {
			return fmt.Errorf("%s %s: %w", method, path, err)
		}

		integ := backend(path, o)
		if integ == nil || truthy(get(op, "x-amazon-apigateway-integration")) {
			continue
		}
		t, err := target()
		if err != nil {
			return err
		}
		in, err := integration(method, path, op, integ, o.Authorizer, authorized)
		if err != nil {
			return err
		}
		t.set("x-amazon-apigateway-integration", in)
	}
	return nil
}

// backend picks the integration for a path: the secondary where it matches, else the primary.
func backend(path string, o Options) *Integration {
	if routes(path, o.SecondaryIntegration, nil) {
		return o.SecondaryIntegration
	}
	if routes(path, o.Integration, []string{"/"}) {
		return o.Integration
	}
	return nil
}

// usesAuthorizer reports whether the authorizer runs for an operation: it has no
// security of its own, or a requirement names the authorizer's scheme. Operations
// secured otherwise must not get the authorizer's (empty) context headers.
func usesAuthorizer(security any, a *Authorizer) (bool, error) {
	if a == nil {
		return false, nil
	}
	if !truthy(security) {
		return true, nil
	}
	reqs, ok := security.(*array)
	if !ok {
		return false, fmt.Errorf("security must be a list")
	}
	for _, r := range reqs.items {
		switch r.(type) {
		case *object, *array:
		default:
			return false, fmt.Errorf("security requirements must be objects")
		}
		if has(r, a.SecurityScheme) {
			return true, nil
		}
	}
	return false, nil
}

func integration(method, path string, op any, integ *Integration, a *Authorizer, authorized bool) (*object, error) {
	in := newObject()
	in.set("type", "HTTP_PROXY")
	in.set("httpMethod", method)
	if integ.ConnectionID != nil {
		in.set("connectionType", "VPC_LINK")
		in.set("connectionId", *integ.ConnectionID)
	}
	in.set("uri", integ.BaseURI+path)
	if integ.IntegrationTarget != nil && *integ.IntegrationTarget != "" {
		in.set("integrationTarget", *integ.IntegrationTarget)
	}

	rp := newObject()
	if authorized {
		for _, h := range a.RequestHeaders {
			rp.set("integration.request.header."+h.Name, h.Value)
		}
	}
	if integ.PreserveForwardedHost {
		rp.set("integration.request.header.X-Forwarded-Host", "context.domainName")
	}
	var params []any
	switch x := get(op, "parameters").(type) {
	case *array:
		params = x.items
	case string: // iterable in JS, but holds no parameter objects
	default:
		if truthy(x) {
			return nil, fmt.Errorf("%s %s: parameters must be a list", method, path)
		}
	}
	for _, p := range params {
		if truthy(p) && get(p, "in") == "path" && truthy(get(p, "name")) {
			name := toString(get(p, "name"))
			rp.set("integration.request.path."+name, "method.request.path."+name)
		}
	}
	if len(rp.keys) > 0 {
		in.set("requestParameters", rp)
	}
	return in, nil
}

// routes reports whether integ serves path: it matches one of integ's paths (or
// defaults when it has none) and none of its ignored paths.
func routes(path string, integ *Integration, defaults []string) bool {
	if integ == nil {
		return false
	}
	paths := integ.Paths
	if len(paths) == 0 {
		paths = defaults
	}
	match := func(prefixes []string) bool {
		for _, p := range prefixes {
			if matchesPrefix(path, p) {
				return true
			}
		}
		return false
	}
	return match(paths) && !match(integ.IgnoredPaths)
}

// matchesPrefix is a segment-aware prefix match: /a matches /a and /a/b, not /ab.
func matchesPrefix(path, prefix string) bool {
	if prefix != "/" {
		prefix = strings.TrimSuffix(prefix, "/")
	}
	return prefix == "/" || path == prefix || strings.HasPrefix(path, prefix+"/")
}

// uriPath matches a URI from "://host" to the end; like JS `.`, the path part
// does not cross line terminators.
var uriPath = regexp.MustCompile(`(://[^/]+)/[^\n\r\x{2028}\x{2029}]*$`)

// rewritePath replaces the path part of every integration URI on a public path.
func rewritePath(paths *object, rw PathRewrite) error {
	ops := get(paths, rw.Path)
	if !truthy(ops) {
		return fmt.Errorf("path rewrite %s is not in the spec", rw.Path)
	}
	rewritten := false
	if ops, ok := ops.(*object); ok {
		for _, k := range ops.ownKeys() {
			in, ok := get(get(ops, k), "x-amazon-apigateway-integration").(*object)
			if !ok || !truthy(get(in, "uri")) {
				continue
			}
			uri, ok := get(in, "uri").(string)
			if !ok {
				return fmt.Errorf("path rewrite %s: integration uri must be a string", rw.Path)
			}
			in.set("uri", replaceFirst(uriPath, uri, "$1"+rw.BackendPath))
			rewritten = true
		}
	}
	if !rewritten {
		return fmt.Errorf("path rewrite %s has no operation with an integration uri", rw.Path)
	}
	return nil
}

// replaceFirst is JS s.replace(re, repl) for a non-global regexp.
func replaceFirst(re *regexp.Regexp, s, repl string) string {
	m := re.FindStringSubmatchIndex(s)
	if m == nil {
		return s
	}
	return s[:m[0]] + expand(s, m, repl, re.NumSubexp()) + s[m[1]:]
}

// expand is ECMAScript GetSubstitution ($$, $&, $`, $', $n, $nn) without named groups.
func expand(s string, m []int, repl string, groups int) string {
	var b strings.Builder
	group := func(n int) string {
		if m[2*n] < 0 {
			return ""
		}
		return s[m[2*n]:m[2*n+1]]
	}
	isDigit := func(i int) bool { return i < len(repl) && repl[i] >= '0' && repl[i] <= '9' }
	for i := 0; i < len(repl); i++ {
		if repl[i] != '$' || i+1 == len(repl) {
			b.WriteByte(repl[i])
			continue
		}
		switch next := repl[i+1]; {
		case next == '$':
			b.WriteByte('$')
		case next == '&':
			b.WriteString(s[m[0]:m[1]])
		case next == '`':
			b.WriteString(s[:m[0]])
		case next == '\'':
			b.WriteString(s[m[1]:])
		case isDigit(i + 1):
			if isDigit(i + 2) {
				if n, _ := strconv.Atoi(repl[i+1 : i+3]); n >= 1 && n <= groups {
					b.WriteString(group(n))
					i += 2
					continue
				}
			}
			if n := int(next - '0'); n >= 1 && n <= groups {
				b.WriteString(group(n))
			} else {
				b.WriteString(repl[i : i+2])
			}
		default:
			b.WriteString(repl[i : i+2])
		}
		i++
	}
	return b.String()
}

func ensureObject(o *object, k string) (*object, error) {
	v := get(o, k)
	if !truthy(v) {
		n := newObject()
		o.set(k, n)
		return n, nil
	}
	x, ok := v.(*object)
	if !ok {
		return nil, fmt.Errorf("%s must be an object", k)
	}
	return x, nil
}

// notFoundJSON is the catch-all operation, as the reference writes it.
const notFoundJSON = `{"responses":{"404":{"description":"Not Found - Path not defined in API specification","headers":{"Content-Type":{"schema":{"type":"string"}}},"content":{"application/json":{"schema":{"type":"object","properties":{"message":{"type":"string","example":"Not Found"},"path":{"type":"string","example":"/requested/path"},"error":{"type":"string","example":"The requested path is not defined in the API specification"}}}}}}},"x-amazon-apigateway-integration":{"type":"MOCK","requestTemplates":{"application/json":"{\"statusCode\": 404}"},"responses":{"404":{"statusCode":"404","responseParameters":{"method.response.header.Content-Type":"'application/json'"},"responseTemplates":{"application/json":"{\n  \"message\": \"Not Found\",\n  \"path\": \"$context.path\",\n  \"error\": \"The requested path is not defined in the API specification\"\n}"}}}}}`

func notFound() *object {
	doc, err := parseDocument(notFoundJSON)
	if err != nil {
		panic(err)
	}
	v, err := toValue(doc)
	if err != nil {
		panic(err)
	}
	return v.(*object)
}
