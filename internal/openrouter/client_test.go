package openrouter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testAPIKey = "sk-or-secret-KEYVALUE-do-not-leak"

func newTestClient(baseURL string) *Client {
	return New(Config{APIKey: testAPIKey, BaseURL: baseURL, Timeout: 2 * time.Second})
}

// pngDataURI is a 1x1 transparent PNG as a data URI.
func pngDataURI() string {
	raw := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	return "data:image/png;base64," + raw
}

func TestGenerateSendsHeadersAndBody(t *testing.T) {
	var gotAuth, gotReferer, gotTitle, gotCT string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotReferer = r.Header.Get("HTTP-Referer")
		gotTitle = r.Header.Get("X-Title")
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &gotBody)

		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q, want /chat/completions", r.URL.Path)
		}
		resp := map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{
					"images": []any{map[string]any{
						"image_url": map[string]any{"url": pngDataURI()},
					}},
				},
			}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	img, err := c.Generate(context.Background(), GenerateParams{
		Prompt: "hello", Model: "google/gemini-2.5-flash-image", ImageSize: "1K", AspectRatio: "1:1",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if gotAuth != "Bearer "+testAPIKey {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotReferer == "" || gotTitle == "" {
		t.Errorf("missing HTTP-Referer (%q) or X-Title (%q)", gotReferer, gotTitle)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q", gotCT)
	}
	if gotBody["model"] != "google/gemini-2.5-flash-image" {
		t.Errorf("body model = %v", gotBody["model"])
	}
	if _, ok := gotBody["image_config"]; !ok {
		t.Errorf("gemini body missing image_config: %v", gotBody)
	}
	if img.ContentType != "image/png" || len(img.Data) == 0 {
		t.Errorf("image = %+v", img)
	}
	wantBytes, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(pngDataURI(), "data:image/png;base64,"))
	if string(img.Data) != string(wantBytes) {
		t.Errorf("decoded image bytes do not match the data URI payload")
	}
}

func TestGenerateUpstream4xxKeepsStatusAndStripsKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		// Upstream error message that (pathologically) echoes the key.
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "rate limited for Bearer " + testAPIKey},
		})
	}))
	defer srv.Close()

	_, err := newTestClient(srv.URL).Generate(context.Background(), GenerateParams{
		Prompt: "x", Model: "google/gemini-2.5-flash-image",
	})
	ue, ok := err.(*UpstreamError)
	if !ok {
		t.Fatalf("error type = %T, want *UpstreamError", err)
	}
	if ue.Status != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", ue.Status)
	}
	if strings.Contains(ue.Error(), testAPIKey) {
		t.Errorf("error leaks the API key: %s", ue.Error())
	}
	if !strings.Contains(ue.Message, "rate limited") {
		t.Errorf("message not forwarded: %q", ue.Message)
	}
}

func TestGenerateUpstream5xxMapsToSameClass(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"upstream boom"}}`, http.StatusBadGateway)
	}))
	defer srv.Close()

	_, err := newTestClient(srv.URL).Generate(context.Background(), GenerateParams{
		Prompt: "x", Model: "google/gemini-2.5-flash-image",
	})
	ue, _ := err.(*UpstreamError)
	if ue == nil || ue.Status != http.StatusBadGateway {
		t.Fatalf("err = %v, want *UpstreamError status 502", err)
	}
}

func TestGenerateTimeoutIs504(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
	}))
	defer srv.Close()

	c := New(Config{APIKey: testAPIKey, BaseURL: srv.URL, Timeout: 50 * time.Millisecond})
	_, err := c.Generate(context.Background(), GenerateParams{Prompt: "x", Model: "google/gemini-2.5-flash-image"})
	ue, _ := err.(*UpstreamError)
	if ue == nil || ue.Status != http.StatusGatewayTimeout {
		t.Fatalf("err = %v, want *UpstreamError status 504", err)
	}
	if strings.Contains(ue.Error(), testAPIKey) {
		t.Errorf("timeout error leaks key: %s", ue.Error())
	}
}

func TestGenerateConnectionRefusedIs502(t *testing.T) {
	// Nothing listening on this port.
	c := New(Config{APIKey: testAPIKey, BaseURL: "http://127.0.0.1:1", Timeout: time.Second})
	_, err := c.Generate(context.Background(), GenerateParams{Prompt: "x", Model: "google/gemini-2.5-flash-image"})
	ue, _ := err.(*UpstreamError)
	if ue == nil || ue.Status != http.StatusBadGateway {
		t.Fatalf("err = %v, want *UpstreamError status 502", err)
	}
}

func TestGenerateUnknownModel(t *testing.T) {
	c := New(Config{APIKey: testAPIKey, BaseURL: "http://unused", Timeout: time.Second})
	_, err := c.Generate(context.Background(), GenerateParams{Prompt: "x", Model: "no/such-model"})
	ue, _ := err.(*UpstreamError)
	if ue == nil || ue.Status != http.StatusBadRequest {
		t.Fatalf("err = %v, want *UpstreamError status 400", err)
	}
}
