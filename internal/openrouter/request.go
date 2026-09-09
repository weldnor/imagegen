package openrouter

import "strings"

// GenerateParams is one image-generation request, already validated by the
// caller (prompt non-empty, model known).
type GenerateParams struct {
	Prompt      string
	Model       string
	ImageSize   string // quality token, e.g. "1K"/"2K"/"4K"; "" when N/A
	AspectRatio string
	References  []string // reference images as data URIs
}

// contentPart is one element of a multi-part message content array.
type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type imageConfig struct {
	ImageSize   string `json:"image_size"`
	AspectRatio string `json:"aspect_ratio"`
}

type chatMessage struct {
	Role string `json:"role"`
	// Content is a plain string when there are no reference parts, otherwise a
	// []contentPart — matching generateSingleImage() in src/app.js.
	Content any `json:"content"`
}

// chatRequest is the OpenRouter chat/completions body.
type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	ImageConfig *imageConfig  `json:"image_config,omitempty"`
	AspectRatio string        `json:"aspect_ratio,omitempty"`
}

// isGemini reports whether the model id is a Gemini model, matching the
// frontend's `state.selectedModel.includes('gemini')` check.
func isGemini(model string) bool {
	return strings.Contains(model, "gemini")
}

// buildRequest constructs the OpenRouter request body for p, mirroring
// generateSingleImage() in src/app.js:
//   - reference images become image_url content parts only when the model
//     supports image input;
//   - the text prompt is always the final content part;
//   - when there are no reference parts, content is the bare prompt string;
//   - Gemini models that support image size get an image_config (size + aspect
//     ratio); non-Gemini models that support aspect ratio get a top-level
//     aspect_ratio.
func buildRequest(p GenerateParams, cfg ModelConfig) chatRequest {
	var parts []contentPart
	if cfg.SupportsImageInput {
		for _, ref := range p.References {
			if ref == "" {
				continue
			}
			parts = append(parts, contentPart{
				Type:     "image_url",
				ImageURL: &imageURL{URL: ref, Detail: "high"},
			})
		}
	}
	parts = append(parts, contentPart{Type: "text", Text: p.Prompt})

	var content any
	if len(parts) == 1 {
		content = p.Prompt
	} else {
		content = parts
	}

	req := chatRequest{
		Model:    p.Model,
		Messages: []chatMessage{{Role: "user", Content: content}},
	}

	if cfg.SupportsImageSize && isGemini(p.Model) {
		req.ImageConfig = &imageConfig{
			ImageSize:   strings.ToLower(p.ImageSize),
			AspectRatio: p.AspectRatio,
		}
	}
	if cfg.SupportsAspectRatio && !isGemini(p.Model) {
		req.AspectRatio = p.AspectRatio
	}

	return req
}
