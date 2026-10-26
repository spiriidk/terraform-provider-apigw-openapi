package oas

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzRender checks that no input makes Render panic.
func FuzzRender(f *testing.F) {
	specs, _ := filepath.Glob("testdata/specs/*.yaml")
	for _, p := range specs {
		b, err := os.ReadFile(p)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(string(b), "/v2")
	}
	f.Add("a: &x [*x]", "")
	f.Add("paths: {/a: {get: null}}", "/a")
	link := "vpclink"
	f.Fuzz(func(t *testing.T, spec, prefix string) {
		_, _ = Render(spec, Options{
			Integration:      &Integration{BaseURI: "http://h", ConnectionID: &link, PreserveForwardedHost: true},
			Authorizer:       &Authorizer{SecurityScheme: "s", URI: "u", Header: "Authorization", RequestHeaders: []Header{{"h", "v"}}},
			StripPrefix:      &prefix,
			PathRewrites:     []PathRewrite{{Path: "/a", BackendPath: "/$1$$"}},
			CatchAllNotFound: true,
		})
	})
}
