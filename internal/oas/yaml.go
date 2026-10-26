package oas

import (
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// YAML 1.2 core schema, as resolved by the `yaml` npm package (v2).
// go.yaml.in/yaml/v3 resolves some forms differently (e.g. 017 as octal), so the
// provider resolves plain scalars itself.
var (
	reNull     = regexp.MustCompile(`^(?:~|[Nn]ull|NULL)?$`)
	reBool     = regexp.MustCompile(`^(?:[Tt]rue|TRUE|[Ff]alse|FALSE)$`)
	reIntOct   = regexp.MustCompile(`^0o[0-7]+$`)
	reInt      = regexp.MustCompile(`^[-+]?[0-9]+$`)
	reIntHex   = regexp.MustCompile(`^0x[0-9a-fA-F]+$`)
	reFloatNaN = regexp.MustCompile(`^(?:[-+]?\.(?:inf|Inf|INF)|\.nan|\.NaN|\.NAN)$`)
	reFloatExp = regexp.MustCompile(`^[-+]?(?:\.[0-9]+|[0-9]+(?:\.[0-9]*)?)[eE][-+]?[0-9]+$`)
	reFloat    = regexp.MustCompile(`^[-+]?(?:\.[0-9]+|[0-9]+\.[0-9]*)$`)

	reLineBreak = regexp.MustCompile(`\r\n|\r|\n`)
)

// document is a parsed YAML document and the source lines needed to spot the
// non-specific tag "!", which the parser does not report.
type document struct {
	root  *yaml.Node
	lines []string // nil when the source has no "!"
}

// toValue converts a parsed document into values; each call returns a fresh graph.
// Aliases share the anchored value, as in the `yaml` package.
func toValue(doc *document) (any, error) {
	c := &converter{seen: map[*yaml.Node]any{}, anchors: map[*yaml.Node]*anchor{}, lines: doc.lines}
	return c.node(doc.root)
}

// YAML 1.2 reads NEL, LS and PS as ordinary characters; go.yaml.in/yaml/v3 reads
// them as line breaks (YAML 1.1). They are swapped for private-use sentinels while
// parsing and restored afterwards.
var (
	breakChars = "\u0085\u2028\u2029"
	sentinels  = "\U0010FFF0\U0010FFF1\U0010FFF2"
	hideBreaks = strings.NewReplacer("\u0085", "\U0010FFF0", "\u2028", "\U0010FFF1", "\u2029", "\U0010FFF2")
	showBreaks = strings.NewReplacer("\U0010FFF0", "\u0085", "\U0010FFF1", "\u2028", "\U0010FFF2", "\u2029")
)

// parseDocument parses a single YAML document; it returns nil for an empty one.
func parseDocument(src string) (*document, error) {
	hide := strings.ContainsAny(src, breakChars)
	if hide && strings.ContainsAny(src, sentinels) {
		return nil, errors.New("parsing YAML: characters U+10FFF0 to U+10FFF2 are not supported")
	}
	parsed := src
	if hide {
		parsed = hideBreaks.Replace(src)
	}
	root, err := parseRaw(parsed)
	if err != nil || root == nil {
		return nil, err
	}
	if hide {
		restoreBreaks(root)
	}
	doc := &document{root: root}
	if strings.Contains(src, "!") {
		doc.lines = reLineBreak.Split(src, -1)
	}
	return doc, nil
}

func restoreBreaks(n *yaml.Node) {
	n.Value = showBreaks.Replace(n.Value)
	for _, c := range n.Content {
		restoreBreaks(c)
	}
}

func parseRaw(src string) (*yaml.Node, error) {
	dec := yaml.NewDecoder(strings.NewReader(src))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err == nil {
		return nil, errors.New("parsing YAML: source contains multiple documents")
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}
	return &doc, nil
}

type converter struct {
	seen    map[*yaml.Node]any
	anchors map[*yaml.Node]*anchor
	lines   []string
}

// maxAliasCount is the `yaml` package's default limit against alias expansion attacks.
const maxAliasCount = 100

type anchor struct{ count, aliasCount int }

// aliasCount is getAliasCount from the `yaml` package.
func (c *converter) aliasCount(n *yaml.Node) int {
	switch n.Kind {
	case yaml.AliasNode:
		if a := c.anchors[n.Alias]; a != nil {
			return a.count * a.aliasCount
		}
		return 0
	case yaml.MappingNode, yaml.SequenceNode:
		m := 0
		for _, it := range n.Content {
			m = max(m, c.aliasCount(it))
		}
		return m
	}
	return 1
}

// nonSpecificTag reports whether a scalar carries the "!" tag (a string in YAML 1.2).
// The node's column points at its properties: an optional anchor, then the tag.
func (c *converter) nonSpecificTag(n *yaml.Node) bool {
	if n.Style&yaml.TaggedStyle != 0 || c.lines == nil || n.Line < 1 || n.Line > len(c.lines) {
		return false
	}
	line := []rune(c.lines[n.Line-1])
	i := n.Column - 1
	if i < 0 || i >= len(line) {
		return false
	}
	if line[i] == '&' {
		for i < len(line) && line[i] != ' ' && line[i] != '\t' {
			i++
		}
		for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
			i++
		}
	}
	return i+1 < len(line) && line[i] == '!' && (line[i+1] == ' ' || line[i+1] == '\t')
}

func (c *converter) node(n *yaml.Node) (any, error) {
	if v, ok := c.seen[n]; ok {
		return v, nil
	}
	if n.Anchor != "" && n.Kind != yaml.AliasNode {
		c.anchors[n] = &anchor{count: 1}
	}
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil, nil
		}
		return c.node(n.Content[0])
	case yaml.AliasNode:
		v, ok := c.seen[n.Alias]
		a := c.anchors[n.Alias]
		if !ok || a == nil {
			return nil, fmt.Errorf("line %d: alias *%s used before its anchor", n.Line, n.Value)
		}
		a.count++
		if a.aliasCount == 0 {
			a.aliasCount = c.aliasCount(n.Alias)
		}
		if a.count*a.aliasCount > maxAliasCount {
			return nil, fmt.Errorf("line %d: excessive alias count", n.Line)
		}
		return v, nil
	case yaml.ScalarNode:
		v, err := c.scalar(n)
		if err != nil {
			return nil, err
		}
		c.seen[n] = v
		return v, nil
	case yaml.SequenceNode:
		a := &array{}
		c.seen[n] = a
		for _, it := range n.Content {
			v, err := c.node(it)
			if err != nil {
				return nil, err
			}
			a.items = append(a.items, v)
		}
		return a, nil
	case yaml.MappingNode:
		o := newObject()
		c.seen[n] = o
		keys := map[any]bool{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			kn, vn := n.Content[i], n.Content[i+1]
			kv, err := c.node(kn)
			if err != nil {
				return nil, err
			}
			// Scalar keys compare by value (NaN never matches itself, as in JS).
			switch kv.(type) {
			case *object, *array:
			default:
				if keys[kv] {
					return nil, fmt.Errorf("line %d: map keys must be unique", kn.Line)
				}
				keys[kv] = true
			}
			ks, err := keyString(kv, kn)
			if err != nil {
				return nil, err
			}
			v, err := c.node(vn)
			if err != nil {
				return nil, err
			}
			o.set(ks, v)
		}
		return o, nil
	}
	return nil, fmt.Errorf("line %d: unsupported YAML node", n.Line)
}

// keyString is how the `yaml` package turns a key into a JS property name.
func keyString(v any, n *yaml.Node) (string, error) {
	switch x := v.(type) {
	case nil:
		return "", nil
	case string:
		return x, nil
	case bool:
		return strconv.FormatBool(x), nil
	case float64:
		return numberToString(x), nil
	}
	return "", fmt.Errorf("line %d: collection map keys are not supported", n.Line)
}

// scalar resolves a scalar by the YAML 1.2 core schema.
func (c *converter) scalar(n *yaml.Node) (any, error) {
	s := n.Value
	if c.nonSpecificTag(n) {
		return s, nil
	}
	if n.Style&yaml.TaggedStyle != 0 {
		switch n.Tag {
		case "!!null":
			if reNull.MatchString(s) {
				return nil, nil
			}
		case "!!bool":
			if reBool.MatchString(s) {
				return s[0] == 't' || s[0] == 'T', nil
			}
		case "!!int":
			if v, ok := resolveInt(s); ok {
				return v, nil
			}
		case "!!float":
			if v, ok := resolveFloat(s); ok {
				return v, nil
			}
		case "!!map", "!!seq":
			return nil, fmt.Errorf("line %d: tag %s on a scalar", n.Line, n.Tag)
		case "!!binary", "!!timestamp", "!!set", "!!omap", "!!pairs":
			return nil, fmt.Errorf("line %d: tag %s is not supported", n.Line, n.Tag)
		}
		return s, nil
	}
	if n.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle|yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		return s, nil
	}
	// Fast path: core-schema null, bool and number forms all start with one of these.
	if s != "" && !strings.ContainsRune("~nNtTfF+-.0123456789", rune(s[0])) {
		return s, nil
	}
	switch {
	case reNull.MatchString(s):
		return nil, nil
	case reBool.MatchString(s):
		return s[0] == 't' || s[0] == 'T', nil
	}
	if v, ok := resolveInt(s); ok {
		return v, nil
	}
	if v, ok := resolveFloat(s); ok {
		return v, nil
	}
	return s, nil
}

func resolveInt(s string) (float64, bool) {
	switch {
	case reIntOct.MatchString(s):
		return parseRadix(s[2:], 8), true
	case reInt.MatchString(s):
		f, _ := strconv.ParseFloat(s, 64)
		return f, true
	case reIntHex.MatchString(s):
		return parseRadix(s[2:], 16), true
	}
	return 0, false
}

func resolveFloat(s string) (float64, bool) {
	switch {
	case reFloatNaN.MatchString(s):
		if strings.ToLower(s[len(s)-3:]) == "nan" {
			return math.NaN(), true
		}
		if s[0] == '-' {
			return math.Inf(-1), true
		}
		return math.Inf(1), true
	case reFloatExp.MatchString(s), reFloat.MatchString(s):
		// ParseFloat returns ±Inf with a range error on overflow, like parseFloat.
		f, _ := strconv.ParseFloat(s, 64)
		return f, true
	}
	return 0, false
}

// parseRadix is parseInt(s, radix) for valid digits.
func parseRadix(s string, radix int) float64 {
	if u, err := strconv.ParseUint(s, radix, 64); err == nil {
		return float64(u)
	}
	f := 0.0
	for _, c := range s {
		d, _ := strconv.ParseUint(string(c), radix, 8)
		f = f*float64(radix) + float64(d)
	}
	return f
}
