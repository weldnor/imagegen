package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/weldnor/imagegen/internal/auth"
	"github.com/weldnor/imagegen/internal/gallery"
	"github.com/weldnor/imagegen/internal/openrouter"
)

// Generator produces one image from a generation request. *openrouter.Client
// satisfies it; tests inject a fake.
type Generator interface {
	Generate(ctx context.Context, p openrouter.GenerateParams) (*openrouter.Image, error)
}

// GalleryStore is the subset of *gallery.Store the handlers use.
type GalleryStore interface {
	Save(ctx context.Context, userID string, data []byte, meta gallery.Metadata) (gallery.Image, error)
	List(ctx context.Context, userID string) ([]gallery.Image, error)
	Get(ctx context.Context, userID, imageID string) (gallery.Image, error)
	Delete(ctx context.Context, userID, imageID string) error
	ClearForUser(ctx context.Context, userID string) error
	AbsPath(gallery.Image) string
}

// API holds the collaborators for the data endpoints.
type API struct {
	Gen         Generator
	Gallery     GalleryStore
	Concurrency int
	MaxUpload   int64
}

// Bind installs the API's handlers onto d.
func (a *API) Bind(d *Deps) {
	d.Generate = a.Generate
	d.ListImages = a.ListImages
	d.GetImage = a.GetImage
	d.DeleteImage = a.DeleteImage
	d.ClearImages = a.ClearImages
	d.Models = a.Models
}

// ---- DTOs ----

type imageDTO struct {
	ID             string `json:"id"`
	URL            string `json:"url"`
	Prompt         string `json:"prompt"`
	Model          string `json:"model"`
	ModelName      string `json:"modelName"`
	ImageSize      string `json:"imageSize,omitempty"`
	AspectRatio    string `json:"aspectRatio"`
	ReferenceCount int    `json:"referenceCount"`
	ContentType    string `json:"contentType"`
	CreatedAt      string `json:"createdAt"`
}

func toDTO(img gallery.Image) imageDTO {
	return imageDTO{
		ID:             img.ID,
		URL:            "/api/images/" + img.ID,
		Prompt:         img.Prompt,
		Model:          img.Model,
		ModelName:      img.ModelName,
		ImageSize:      img.ImageSize,
		AspectRatio:    img.AspectRatio,
		ReferenceCount: img.ReferenceCount,
		ContentType:    img.ContentType,
		CreatedAt:      img.Created.UTC().Format(time.RFC3339),
	}
}

type generateRequest struct {
	Prompt      string   `json:"prompt"`
	Model       string   `json:"model"`
	ImageSize   string   `json:"imageSize"`
	AspectRatio string   `json:"aspectRatio"`
	Count       int      `json:"count"`
	References  []string `json:"references"`
}

type generateResponse struct {
	Images []imageDTO `json:"images"`
	Failed int        `json:"failed"`
}

// ---- POST /api/generate ----

func (a *API) Generate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, a.MaxUpload)

	var req generateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeErr(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.Prompt == "" {
		writeErr(w, http.StatusBadRequest, "prompt is required")
		return
	}
	modelCfg, known := openrouter.Model(req.Model)
	if !known {
		writeErr(w, http.StatusBadRequest, "unknown model: "+req.Model)
		return
	}
	if req.Count < 1 || req.Count > 8 {
		writeErr(w, http.StatusBadRequest, "count must be between 1 and 8")
		return
	}

	sess, _ := auth.SessionFromContext(r.Context())

	params := openrouter.GenerateParams{
		Prompt:      req.Prompt,
		Model:       req.Model,
		ImageSize:   req.ImageSize,
		AspectRatio: req.AspectRatio,
		References:  req.References,
	}

	// Reference count recorded in metadata reflects what the model actually
	// received (0 when the model does not support image input).
	refCount := 0
	if modelCfg.SupportsImageInput {
		refCount = len(req.References)
	}

	type outcome struct {
		img *openrouter.Image
		err error
	}
	outcomes := make([]outcome, req.Count)

	limit := a.Concurrency
	if limit < 1 {
		limit = 1
	}
	if limit > req.Count {
		limit = req.Count
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i := 0; i < req.Count; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()
			img, err := a.Gen.Generate(r.Context(), params)
			outcomes[idx] = outcome{img: img, err: err}
		}(i)
	}
	wg.Wait()

	resp := generateResponse{Images: make([]imageDTO, 0, req.Count)}
	var firstErrMsg string
	for _, o := range outcomes {
		if o.err != nil {
			resp.Failed++
			if firstErrMsg == "" {
				firstErrMsg = upstreamMessage(o.err)
			}
			continue
		}
		saved, err := a.Gallery.Save(r.Context(), sess.UserID, o.img.Data, gallery.Metadata{
			Prompt:         req.Prompt,
			Model:          req.Model,
			ModelName:      modelCfg.Name,
			ImageSize:      req.ImageSize,
			AspectRatio:    req.AspectRatio,
			ReferenceCount: refCount,
			ContentType:    o.img.ContentType,
		})
		if err != nil {
			resp.Failed++
			if firstErrMsg == "" {
				firstErrMsg = "failed to store a generated image"
			}
			continue
		}
		resp.Images = append(resp.Images, toDTO(saved))
	}

	if len(resp.Images) == 0 {
		if firstErrMsg == "" {
			firstErrMsg = "image generation failed"
		}
		writeErr(w, http.StatusBadGateway, firstErrMsg)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func upstreamMessage(err error) string {
	var ue *openrouter.UpstreamError
	if errors.As(err, &ue) {
		return ue.Message
	}
	return "image generation failed"
}

// ---- GET /api/images ----

func (a *API) ListImages(w http.ResponseWriter, r *http.Request) {
	sess, _ := auth.SessionFromContext(r.Context())
	imgs, err := a.Gallery.List(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not list images")
		return
	}
	out := make([]imageDTO, 0, len(imgs))
	for _, img := range imgs {
		out = append(out, toDTO(img))
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- GET /api/images/{id} ----

func (a *API) GetImage(w http.ResponseWriter, r *http.Request) {
	sess, _ := auth.SessionFromContext(r.Context())
	id := chi.URLParam(r, "id")

	img, err := a.Gallery.Get(r.Context(), sess.UserID, id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	f, err := os.Open(a.Gallery.AbsPath(img))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", img.ContentType)
	http.ServeContent(w, r, "", img.Created, f)
}

// ---- DELETE /api/images/{id} ----

func (a *API) DeleteImage(w http.ResponseWriter, r *http.Request) {
	sess, _ := auth.SessionFromContext(r.Context())
	id := chi.URLParam(r, "id")

	err := a.Gallery.Delete(r.Context(), sess.UserID, id)
	if errors.Is(err, gallery.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "could not delete image")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- DELETE /api/images ----

func (a *API) ClearImages(w http.ResponseWriter, r *http.Request) {
	sess, _ := auth.SessionFromContext(r.Context())
	if err := a.Gallery.ClearForUser(r.Context(), sess.UserID); err != nil {
		writeErr(w, http.StatusInternalServerError, "could not clear gallery")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- GET /api/models ----

type modelDTO struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	SupportsImageSize   bool   `json:"supportsImageSize"`
	SupportsAspectRatio bool   `json:"supportsAspectRatio"`
	SupportsImageInput  bool   `json:"supportsImageInput"`
	MaxReferences       int    `json:"maxReferences"`
}

func (a *API) Models(w http.ResponseWriter, r *http.Request) {
	ids := openrouter.KnownModels()
	out := make([]modelDTO, 0, len(ids))
	for _, id := range ids {
		m := openrouter.Models[id]
		out = append(out, modelDTO{
			ID:                  id,
			Name:                m.Name,
			SupportsImageSize:   m.SupportsImageSize,
			SupportsAspectRatio: m.SupportsAspectRatio,
			SupportsImageInput:  m.SupportsImageInput,
			MaxReferences:       m.MaxReferences,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	writeJSON(w, http.StatusOK, out)
}

// ---- shared helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
