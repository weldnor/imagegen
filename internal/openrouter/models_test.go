package openrouter

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

// TestModelsMatchAppJS asserts the ported Models map is identical to
// MODEL_CONFIGS in src/app.js — same ids, names, capability flags, and
// maxReferences.
func TestModelsMatchAppJS(t *testing.T) {
	src, err := os.ReadFile("../../src/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}

	entry := regexp.MustCompile(`'([^']+)':\s*\{\s*` +
		`name:\s*'([^']*)',\s*` +
		`supportsImageSize:\s*(true|false),\s*` +
		`supportsAspectRatio:\s*(true|false),\s*` +
		`supportsImageInput:\s*(true|false),\s*` +
		`maxReferences:\s*(\d+)\s*\}`)

	matches := entry.FindAllStringSubmatch(string(src), -1)
	if len(matches) == 0 {
		t.Fatal("no MODEL_CONFIGS entries parsed from app.js")
	}

	fromJS := map[string]ModelConfig{}
	for _, m := range matches {
		refs, _ := strconv.Atoi(m[6])
		fromJS[m[1]] = ModelConfig{
			Name:                m[2],
			SupportsImageSize:   m[3] == "true",
			SupportsAspectRatio: m[4] == "true",
			SupportsImageInput:  m[5] == "true",
			MaxReferences:       refs,
		}
	}

	if len(fromJS) != len(Models) {
		t.Errorf("count mismatch: app.js has %d models, Go has %d", len(fromJS), len(Models))
	}
	for id, jsCfg := range fromJS {
		goCfg, ok := Models[id]
		if !ok {
			t.Errorf("model %q in app.js is missing from Go Models", id)
			continue
		}
		if goCfg != jsCfg {
			t.Errorf("model %q mismatch:\n  app.js: %+v\n  go:     %+v", id, jsCfg, goCfg)
		}
	}
	for id := range Models {
		if _, ok := fromJS[id]; !ok {
			t.Errorf("model %q in Go Models is not in app.js", id)
		}
	}
}
