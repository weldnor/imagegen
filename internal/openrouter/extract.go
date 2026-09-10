package openrouter

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// errNoImage is returned when a well-formed response carries no image.
var errNoImage = errors.New("no image in OpenRouter response")

// chatResponse is the subset of the OpenRouter response we read. Fields that can
// take several shapes are decoded as json.RawMessage / any and handled in
// extractImageRef, mirroring the fallback chain in src/app.js.
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content any    `json:"content"` // string or []part
			Refusal string `json:"refusal"`
			Images  []struct {
				ImageURL *struct {
					URL string `json:"url"`
				} `json:"image_url"`
				URL     string `json:"url"`
				B64JSON string `json:"b64_json"`
			} `json:"images"`
		} `json:"message"`
	} `json:"choices"`
}

// extractImageRef walks the same fallback chain as generateSingleImage() in
// src/app.js and returns a data URI or an https URL for the first image found.
func extractImageRef(r *chatResponse) (string, error) {
	if r == nil || len(r.Choices) == 0 {
		return "", errors.New("no response from model")
	}
	msg := r.Choices[0].Message

	// 1. message.images[0]
	if len(msg.Images) > 0 {
		img := msg.Images[0]
		if img.ImageURL != nil && img.ImageURL.URL != "" {
			return img.ImageURL.URL, nil
		}
		if img.URL != "" {
			return img.URL, nil
		}
		if img.B64JSON != "" {
			return "data:image/png;base64," + img.B64JSON, nil
		}
	}

	// 2. message.content parts
	if parts, ok := msg.Content.([]any); ok {
		for _, p := range parts {
			m, ok := p.(map[string]any)
			if !ok {
				continue
			}
			switch {
			case m["type"] == "image_url":
				if iu, ok := m["image_url"].(map[string]any); ok {
					if u, ok := iu["url"].(string); ok && u != "" {
						return u, nil
					}
				}
			case m["inlineData"] != nil:
				if id, ok := m["inlineData"].(map[string]any); ok {
					if data, ok := id["data"].(string); ok && data != "" {
						mime, _ := id["mimeType"].(string)
						if mime == "" {
							mime = "image/png"
						}
						return fmt.Sprintf("data:%s;base64,%s", mime, data), nil
					}
				}
			case m["type"] == "image":
				if img, ok := m["image"].(string); ok && img != "" {
					if strings.HasPrefix(img, "data:") {
						return img, nil
					}
					return "data:image/png;base64," + img, nil
				}
			}
		}
	}

	// 3. content is itself a data URI string
	if s, ok := msg.Content.(string); ok && strings.HasPrefix(s, "data:image") {
		return s, nil
	}

	// No image: the model answered with text instead — a content-policy
	// refusal, or it read the prompt as a question. Surface that text so the
	// caller learns why nothing was generated instead of a bare "no image".
	if text := firstNonEmpty(msg.Refusal, contentText(msg.Content)); text != "" {
		return "", fmt.Errorf("%w: %s", errNoImage, truncate(text, 400))
	}
	return "", errNoImage
}

// contentText pulls the plain text out of an OpenRouter message content, which
// is either a string or an array of parts.
func contentText(content any) string {
	switch c := content.(type) {
	case string:
		return strings.TrimSpace(c)
	case []any:
		var b strings.Builder
		for _, p := range c {
			m, ok := p.(map[string]any)
			if !ok {
				continue
			}
			if t, ok := m["text"].(string); ok && t != "" {
				if b.Len() > 0 {
					b.WriteByte(' ')
				}
				b.WriteString(t)
			}
		}
		return strings.TrimSpace(b.String())
	}
	return ""
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// truncate shortens s to at most n runes, appending an ellipsis when it cuts.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n])) + "…"
}

var dataURIRe = regexp.MustCompile(`^data:([^;,]+)?(;base64)?,(.*)$`)

// resolveImage turns a data URI or https URL into raw bytes plus a content type.
func resolveImage(ctx context.Context, ref string, httpClient *http.Client) (data []byte, contentType string, err error) {
	if strings.HasPrefix(ref, "data:") {
		m := dataURIRe.FindStringSubmatch(ref)
		if m == nil {
			return nil, "", errors.New("malformed data URI in response")
		}
		contentType = m[1]
		if contentType == "" {
			contentType = "image/png"
		}
		if m[2] == ";base64" {
			data, err = base64.StdEncoding.DecodeString(m[3])
			if err != nil {
				return nil, "", fmt.Errorf("decoding base64 image: %w", err)
			}
		} else {
			data = []byte(m[3])
		}
		return data, contentType, nil
	}

	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ref, nil)
		if err != nil {
			return nil, "", err
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, "", fmt.Errorf("fetching image URL: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, "", fmt.Errorf("fetching image URL: status %d", resp.StatusCode)
		}
		data, err = io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		if err != nil {
			return nil, "", err
		}
		contentType = resp.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "image/png"
		}
		return data, contentType, nil
	}

	return nil, "", fmt.Errorf("unsupported image reference (not a data URI or http(s) URL)")
}

// ExtensionForContentType maps an image content type to a file extension
// (without the dot), matching getImageExtension() in src/app.js.
func ExtensionForContentType(ct string) string {
	ct = strings.ToLower(strings.TrimSpace(ct))
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	switch ct {
	case "image/jpeg", "image/jpg":
		return "jpg"
	case "image/png":
		return "png"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	case "image/svg+xml":
		return "svg"
	}
	if strings.HasPrefix(ct, "image/") {
		return strings.TrimPrefix(ct, "image/")
	}
	return "png"
}
