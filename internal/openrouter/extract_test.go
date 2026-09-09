package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func parseResp(t *testing.T, s string) *chatResponse {
	t.Helper()
	var r chatResponse
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		t.Fatalf("bad test JSON: %v", err)
	}
	return &r
}

func TestExtractImageRef(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"images[].image_url.url",
			`{"choices":[{"message":{"images":[{"image_url":{"url":"data:image/png;base64,AAA"}}]}}]}`,
			"data:image/png;base64,AAA",
		},
		{
			"images[].url",
			`{"choices":[{"message":{"images":[{"url":"https://cdn.example/x.png"}]}}]}`,
			"https://cdn.example/x.png",
		},
		{
			"images[].b64_json",
			`{"choices":[{"message":{"images":[{"b64_json":"QUJD"}]}}]}`,
			"data:image/png;base64,QUJD",
		},
		{
			"content part image_url",
			`{"choices":[{"message":{"content":[{"type":"image_url","image_url":{"url":"data:image/webp;base64,WWW"}}]}}]}`,
			"data:image/webp;base64,WWW",
		},
		{
			"content part inlineData",
			`{"choices":[{"message":{"content":[{"inlineData":{"mimeType":"image/jpeg","data":"SkpK"}}]}}]}`,
			"data:image/jpeg;base64,SkpK",
		},
		{
			"content part inlineData default mime",
			`{"choices":[{"message":{"content":[{"inlineData":{"data":"SkpK"}}]}}]}`,
			"data:image/png;base64,SkpK",
		},
		{
			"content part generic image (raw base64)",
			`{"choices":[{"message":{"content":[{"type":"image","image":"Rk9P"}]}}]}`,
			"data:image/png;base64,Rk9P",
		},
		{
			"content string is a data URI",
			`{"choices":[{"message":{"content":"data:image/png;base64,ZZZ"}}]}`,
			"data:image/png;base64,ZZZ",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extractImageRef(parseResp(t, tc.body))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExtractImageRefNoImage(t *testing.T) {
	_, err := extractImageRef(parseResp(t, `{"choices":[{"message":{"content":"sorry, I can't"}}]}`))
	if !errors.Is(err, errNoImage) {
		t.Fatalf("err = %v, want errNoImage", err)
	}

	_, err = extractImageRef(parseResp(t, `{"choices":[]}`))
	if err == nil {
		t.Fatal("empty choices: want an error")
	}
}

func TestResolveImageDataURI(t *testing.T) {
	data, ct, err := resolveImage(context.Background(), "data:image/gif;base64,R0lGOA==", http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}
	if ct != "image/gif" {
		t.Errorf("content type = %q", ct)
	}
	if len(data) == 0 {
		t.Error("no bytes decoded")
	}
}

func TestExtensionForContentType(t *testing.T) {
	cases := map[string]string{
		"image/png":            "png",
		"image/jpeg":           "jpg",
		"image/jpg":            "jpg",
		"image/webp":           "webp",
		"image/gif":            "gif",
		"image/svg+xml":        "svg",
		"image/png; charset=x": "png",
		"application/weird":    "png",
	}
	for in, want := range cases {
		if got := ExtensionForContentType(in); got != want {
			t.Errorf("ExtensionForContentType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStripKey(t *testing.T) {
	got := stripKey("failed with key sk-or-123 via Bearer sk-or-123", "sk-or-123")
	if strings.Contains(got, "sk-or-123") {
		t.Errorf("key not stripped: %q", got)
	}
}
