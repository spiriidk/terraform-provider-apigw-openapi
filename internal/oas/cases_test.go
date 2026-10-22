package oas

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestGolden checks testdata/cases.json. The expected bodies were produced by the
// reference implementation; see testdata/README.md.
func TestGolden(t *testing.T) {
	runCases(t, "testdata/cases.json")
}

// TestParity runs an external cases file, for comparing with another implementation
// on private specs. Skipped unless PARITY_CASES is set.
func TestParity(t *testing.T) {
	path := os.Getenv("PARITY_CASES")
	if path == "" {
		t.Skip("PARITY_CASES not set")
	}
	runCases(t, path)
}

// runCases reads a JSON list of
//
//	{"name": "...", "spec": "spec.yaml", "options": {...}, "expected": "body.json" | null}
//
// with paths relative to the file. "options" uses the function's attribute names;
// a null "expected" means rendering must fail.
func runCases(t *testing.T, path string) {
	t.Helper()
	dir := filepath.Dir(path)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name     string          `json:"name"`
		Spec     string          `json:"spec"`
		Options  json.RawMessage `json:"options"`
		Expected *string         `json:"expected"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			spec, err := os.ReadFile(rel(dir, c.Spec))
			if err != nil {
				t.Fatal(err)
			}
			var raw any
			if len(c.Options) > 0 {
				if err := json.Unmarshal(c.Options, &raw); err != nil {
					t.Fatal(err)
				}
			}
			opts, err := ParseOptions(raw)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Render(string(spec), opts)
			if c.Expected == nil {
				if err == nil {
					t.Fatalf("want an error, got none")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(rel(dir, *c.Expected))
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Fatalf("body differs at byte %d\n got: %s\nwant: %s", diffAt(got, string(want)), excerpt(got, string(want)), excerpt(string(want), got))
			}
		})
	}
}

func rel(dir, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(dir, p)
}

func diffAt(a, b string) int {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	return i
}

func excerpt(a, b string) string {
	i := diffAt(a, b)
	lo, hi := max(0, i-80), min(len(a), i+80)
	return a[lo:hi]
}
