package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/weldnor/imagegen/internal/openrouter"
)

const geminiModel = "google/gemini-2.5-flash-image"

func genBody(prompt, model string, count int) string {
	b, _ := json.Marshal(generateRequest{
		Prompt: prompt, Model: model, ImageSize: "1K", AspectRatio: "1:1", Count: count,
	})
	return string(b)
}

// ---- 7.1 validation ----

func TestGenerateValidation(t *testing.T) {
	env := newTestEnv(t, &fakeGen{fn: okImage})
	c := env.login(t, "alice", alicePass)

	cases := []struct {
		name string
		body string
		want int
	}{
		{"empty prompt", genBody("   ", geminiModel, 1), http.StatusBadRequest},
		{"unknown model", genBody("cat", "no/such", 1), http.StatusBadRequest},
		{"count zero", genBody("cat", geminiModel, 0), http.StatusBadRequest},
		{"count nine", genBody("cat", geminiModel, 9), http.StatusBadRequest},
		{"malformed json", "{nope", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := env.do(t, http.MethodPost, "/api/generate", c, tc.body)
			defer resp.Body.Close()
			if resp.StatusCode != tc.want {
				b, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, want %d (%s)", resp.StatusCode, tc.want, b)
			}
		})
	}
	if env.gen.calls.Load() != 0 {
		t.Errorf("generator was called %d times despite validation failures", env.gen.calls.Load())
	}
}

func TestGenerateBodyTooLarge(t *testing.T) {
	env := newTestEnvWith(t, &fakeGen{fn: okImage}, 2, 512)
	c := env.login(t, "alice", alicePass)

	big := `{"prompt":"` + strings.Repeat("x", 4000) + `","model":"` + geminiModel + `","count":1}`
	resp := env.do(t, http.MethodPost, "/api/generate", c, big)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", resp.StatusCode)
	}
}

// ---- 7.2 happy path ----

func TestGenerateHappyPathPersists(t *testing.T) {
	env := newTestEnv(t, &fakeGen{fn: okImage, delay: 5_000_000}) // 5ms to force overlap
	c := env.login(t, "alice", alicePass)

	resp := env.do(t, http.MethodPost, "/api/generate", c, genBody("a cat", geminiModel, 3))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d: %s", resp.StatusCode, b)
	}
	var gr generateResponse
	json.NewDecoder(resp.Body).Decode(&gr)
	if len(gr.Images) != 3 || gr.Failed != 0 {
		t.Fatalf("got %d images, failed %d; want 3/0", len(gr.Images), gr.Failed)
	}
	for _, img := range gr.Images {
		if img.URL != "/api/images/"+img.ID {
			t.Errorf("url = %q, want /api/images/%s", img.URL, img.ID)
		}
		if img.ModelName != "Gemini 2.5 Flash Image" || img.Prompt != "a cat" {
			t.Errorf("metadata not echoed: %+v", img)
		}
	}
	if env.gen.maxInFlt.Load() > 4 {
		t.Errorf("max concurrent generate calls = %d, want <= configured 4", env.gen.maxInFlt.Load())
	}

	// A subsequent gallery list shows them.
	lr := env.do(t, http.MethodGet, "/api/images", c, "")
	defer lr.Body.Close()
	var list []imageDTO
	json.NewDecoder(lr.Body).Decode(&list)
	if len(list) != 3 {
		t.Fatalf("gallery list has %d, want 3", len(list))
	}
}

func TestGenerateRespectsConcurrencyLimit(t *testing.T) {
	gen := &fakeGen{fn: okImage, delay: 10_000_000} // 10ms to force overlap
	env := newTestEnvWith(t, gen, 2, 8<<20)
	c := env.login(t, "alice", alicePass)

	resp := env.do(t, http.MethodPost, "/api/generate", c, genBody("cat", geminiModel, 8))
	resp.Body.Close()
	if got := gen.maxInFlt.Load(); got > 2 {
		t.Fatalf("max in-flight = %d, want <= 2", got)
	}
	if gen.calls.Load() != 8 {
		t.Fatalf("calls = %d, want 8", gen.calls.Load())
	}
}

// ---- 7.3 partial / total failure ----

func TestGeneratePartialFailure(t *testing.T) {
	gen := &fakeGen{fn: func(call int) (*openrouter.Image, error) {
		if call == 1 {
			return nil, &openrouter.UpstreamError{Status: 500, Message: "boom"}
		}
		return okImage(call)
	}}
	env := newTestEnv(t, gen)
	c := env.login(t, "alice", alicePass)

	resp := env.do(t, http.MethodPost, "/api/generate", c, genBody("cat", geminiModel, 3))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 for partial failure", resp.StatusCode)
	}
	var gr generateResponse
	json.NewDecoder(resp.Body).Decode(&gr)
	if len(gr.Images) != 2 || gr.Failed != 1 {
		t.Fatalf("got %d images / failed %d, want 2 / 1", len(gr.Images), gr.Failed)
	}
}

func TestGenerateTotalFailureIs502WithFirstMessage(t *testing.T) {
	gen := &fakeGen{fn: func(call int) (*openrouter.Image, error) {
		return nil, &openrouter.UpstreamError{Status: 503, Message: fmt.Sprintf("upstream down #%d", call)}
	}}
	env := newTestEnv(t, gen)
	c := env.login(t, "alice", alicePass)

	resp := env.do(t, http.MethodPost, "/api/generate", c, genBody("cat", geminiModel, 3))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if !strings.HasPrefix(body["error"], "upstream down #") {
		t.Errorf("error = %q, want the first upstream message", body["error"])
	}
}

// ---- 7.4 list ----

func TestListImagesEmpty(t *testing.T) {
	env := newTestEnv(t, &fakeGen{fn: okImage})
	c := env.login(t, "bob", bobPass)
	resp := env.do(t, http.MethodGet, "/api/images", c, "")
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if strings.TrimSpace(string(b)) != "[]" {
		t.Fatalf("empty gallery body = %q, want []", b)
	}
}

// ---- 7.5 fetch bytes / cross-user ----

func TestGetImageBytesAndCrossUser(t *testing.T) {
	env := newTestEnv(t, &fakeGen{fn: okImage})
	alice := env.login(t, "alice", alicePass)
	bob := env.login(t, "bob", bobPass)

	resp := env.do(t, http.MethodPost, "/api/generate", alice, genBody("cat", geminiModel, 1))
	var gr generateResponse
	json.NewDecoder(resp.Body).Decode(&gr)
	resp.Body.Close()
	id := gr.Images[0].ID

	// Owner fetch.
	r1 := env.do(t, http.MethodGet, "/api/images/"+id, alice, "")
	got, _ := io.ReadAll(r1.Body)
	r1.Body.Close()
	if r1.StatusCode != http.StatusOK || string(got) != "PNGDATA" {
		t.Fatalf("owner fetch = %d %q", r1.StatusCode, got)
	}
	if ct := r1.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("content-type = %q", ct)
	}

	// Cross-user -> 404.
	r2 := env.do(t, http.MethodGet, "/api/images/"+id, bob, "")
	r2.Body.Close()
	if r2.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-user fetch = %d, want 404", r2.StatusCode)
	}

	// Unknown id -> 404.
	r3 := env.do(t, http.MethodGet, "/api/images/00000000-0000-4000-8000-000000000000", alice, "")
	r3.Body.Close()
	if r3.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown id fetch = %d, want 404", r3.StatusCode)
	}
}

// ---- 7.6 delete / clear ----

func TestDeleteAndClear(t *testing.T) {
	env := newTestEnv(t, &fakeGen{fn: okImage})
	alice := env.login(t, "alice", alicePass)
	bob := env.login(t, "bob", bobPass)

	resp := env.do(t, http.MethodPost, "/api/generate", alice, genBody("cat", geminiModel, 3))
	var gr generateResponse
	json.NewDecoder(resp.Body).Decode(&gr)
	resp.Body.Close()

	// Non-owner delete -> 404.
	r := env.do(t, http.MethodDelete, "/api/images/"+gr.Images[0].ID, bob, "")
	r.Body.Close()
	if r.StatusCode != http.StatusNotFound {
		t.Fatalf("non-owner delete = %d, want 404", r.StatusCode)
	}

	// Owner delete -> 204, then gone from the list.
	r = env.do(t, http.MethodDelete, "/api/images/"+gr.Images[0].ID, alice, "")
	r.Body.Close()
	if r.StatusCode != http.StatusNoContent {
		t.Fatalf("owner delete = %d, want 204", r.StatusCode)
	}
	list := env.do(t, http.MethodGet, "/api/images", alice, "")
	var after []imageDTO
	json.NewDecoder(list.Body).Decode(&after)
	list.Body.Close()
	if len(after) != 2 {
		t.Fatalf("after delete list has %d, want 2", len(after))
	}

	// Clear -> 204, list empty.
	r = env.do(t, http.MethodDelete, "/api/images", alice, "")
	r.Body.Close()
	if r.StatusCode != http.StatusNoContent {
		t.Fatalf("clear = %d, want 204", r.StatusCode)
	}
	list = env.do(t, http.MethodGet, "/api/images", alice, "")
	b, _ := io.ReadAll(list.Body)
	list.Body.Close()
	if strings.TrimSpace(string(b)) != "[]" {
		t.Fatalf("after clear list = %q, want []", b)
	}
}

// ---- 7.7 models / healthz ----

func TestModelsAndHealthz(t *testing.T) {
	env := newTestEnv(t, &fakeGen{fn: okImage})

	// /healthz is public and body-less.
	hz, err := http.Get(env.srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(hz.Body)
	hz.Body.Close()
	if hz.StatusCode != http.StatusOK || len(b) != 0 {
		t.Fatalf("/healthz = %d body %q, want 200 empty", hz.StatusCode, b)
	}

	// /api/models needs a session.
	if r := env.do(t, http.MethodGet, "/api/models", nil, ""); r.StatusCode != http.StatusUnauthorized {
		r.Body.Close()
		t.Fatalf("/api/models without session = %d, want 401", r.StatusCode)
	}

	c := env.login(t, "alice", alicePass)
	r := env.do(t, http.MethodGet, "/api/models", c, "")
	defer r.Body.Close()
	var models []modelDTO
	json.NewDecoder(r.Body).Decode(&models)
	if len(models) != len(openrouter.Models) {
		t.Fatalf("got %d models, want %d", len(models), len(openrouter.Models))
	}
	var found *modelDTO
	for i := range models {
		if models[i].ID == geminiModel {
			found = &models[i]
		}
	}
	if found == nil || !found.SupportsImageInput || found.MaxReferences != 3 {
		t.Fatalf("gemini model DTO wrong: %+v", found)
	}
}
