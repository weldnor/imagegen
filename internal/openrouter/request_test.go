package openrouter

import (
	"encoding/json"
	"testing"
)

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestBuildRequestGemini(t *testing.T) {
	cfg := Models["google/gemini-2.5-flash-image"]
	got := buildRequest(GenerateParams{
		Prompt:      "a red cube",
		Model:       "google/gemini-2.5-flash-image",
		ImageSize:   "2K",
		AspectRatio: "16:9",
		References:  []string{"data:image/png;base64,AAAA"},
	}, cfg)

	want := `{"model":"google/gemini-2.5-flash-image","messages":[{"role":"user","content":[` +
		`{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA","detail":"high"}},` +
		`{"type":"text","text":"a red cube"}]}],` +
		`"image_config":{"image_size":"2K","aspect_ratio":"16:9"}}`

	if g := mustJSON(t, got); g != want {
		t.Errorf("gemini request mismatch:\n got: %s\nwant: %s", g, want)
	}
}

func TestBuildRequestGeminiNoReferences(t *testing.T) {
	cfg := Models["google/gemini-2.5-flash-image"]
	got := buildRequest(GenerateParams{
		Prompt:      "just text",
		Model:       "google/gemini-2.5-flash-image",
		ImageSize:   "1K",
		AspectRatio: "1:1",
	}, cfg)

	// Single content part -> content is the bare prompt string.
	want := `{"model":"google/gemini-2.5-flash-image","messages":[{"role":"user","content":"just text"}],` +
		`"image_config":{"image_size":"1K","aspect_ratio":"1:1"}}`
	if g := mustJSON(t, got); g != want {
		t.Errorf("gemini (no refs) mismatch:\n got: %s\nwant: %s", g, want)
	}
}

func TestBuildRequestNonGemini(t *testing.T) {
	cfg := Models["black-forest-labs/flux.2-pro"] // supportsImageInput=false, supportsAspectRatio=true
	got := buildRequest(GenerateParams{
		Prompt:      "a blue sphere",
		Model:       "black-forest-labs/flux.2-pro",
		AspectRatio: "3:2",
		References:  []string{"data:image/png;base64,ZZZZ"}, // must be dropped
	}, cfg)

	want := `{"model":"black-forest-labs/flux.2-pro","messages":[{"role":"user","content":"a blue sphere"}],` +
		`"aspect_ratio":"3:2"}`
	if g := mustJSON(t, got); g != want {
		t.Errorf("non-gemini mismatch:\n got: %s\nwant: %s", g, want)
	}
}

func TestBuildRequestNonGeminiWithImageInput(t *testing.T) {
	cfg := Models["openai/gpt-5-image"] // supportsImageInput=true, not gemini
	got := buildRequest(GenerateParams{
		Prompt:      "combine these",
		Model:       "openai/gpt-5-image",
		AspectRatio: "1:1",
		References:  []string{"data:image/png;base64,AAAA", "data:image/png;base64,BBBB"},
	}, cfg)

	want := `{"model":"openai/gpt-5-image","messages":[{"role":"user","content":[` +
		`{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA","detail":"high"}},` +
		`{"type":"image_url","image_url":{"url":"data:image/png;base64,BBBB","detail":"high"}},` +
		`{"type":"text","text":"combine these"}]}],` +
		`"aspect_ratio":"1:1"}`
	if g := mustJSON(t, got); g != want {
		t.Errorf("non-gemini w/ image input mismatch:\n got: %s\nwant: %s", g, want)
	}
}

// The API rejects an empty string as an invalid option, so unset options must
// be left out of the body entirely rather than sent blank.
func TestBuildRequestOmitsUnsetOptions(t *testing.T) {
	t.Run("gemini", func(t *testing.T) {
		got := buildRequest(GenerateParams{
			Prompt: "just text",
			Model:  "google/gemini-2.5-flash-image",
		}, Models["google/gemini-2.5-flash-image"])

		want := `{"model":"google/gemini-2.5-flash-image","messages":[{"role":"user","content":"just text"}]}`
		if g := mustJSON(t, got); g != want {
			t.Errorf("gemini (no options) mismatch:\n got: %s\nwant: %s", g, want)
		}
	})

	t.Run("gemini size only", func(t *testing.T) {
		got := buildRequest(GenerateParams{
			Prompt:    "just text",
			Model:     "google/gemini-2.5-flash-image",
			ImageSize: "4K",
		}, Models["google/gemini-2.5-flash-image"])

		want := `{"model":"google/gemini-2.5-flash-image","messages":[{"role":"user","content":"just text"}],` +
			`"image_config":{"image_size":"4K"}}`
		if g := mustJSON(t, got); g != want {
			t.Errorf("gemini (size only) mismatch:\n got: %s\nwant: %s", g, want)
		}
	})

	t.Run("non-gemini", func(t *testing.T) {
		got := buildRequest(GenerateParams{
			Prompt: "a blue sphere",
			Model:  "black-forest-labs/flux.2-pro",
		}, Models["black-forest-labs/flux.2-pro"])

		want := `{"model":"black-forest-labs/flux.2-pro","messages":[{"role":"user","content":"a blue sphere"}]}`
		if g := mustJSON(t, got); g != want {
			t.Errorf("non-gemini (no options) mismatch:\n got: %s\nwant: %s", g, want)
		}
	})
}

// Gallery rows written before sizes became quality tokens still hold pixel
// dimensions; regenerating one must send a token the API accepts.
func TestBuildRequestNormalizesLegacySize(t *testing.T) {
	got := buildRequest(GenerateParams{
		Prompt:      "just text",
		Model:       "google/gemini-2.5-flash-image",
		ImageSize:   "1024x1024",
		AspectRatio: "1:1",
	}, Models["google/gemini-2.5-flash-image"])

	want := `{"model":"google/gemini-2.5-flash-image","messages":[{"role":"user","content":"just text"}],` +
		`"image_config":{"image_size":"1K","aspect_ratio":"1:1"}}`
	if g := mustJSON(t, got); g != want {
		t.Errorf("legacy size mismatch:\n got: %s\nwant: %s", g, want)
	}
}
