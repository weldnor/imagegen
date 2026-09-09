package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Default client identification headers sent to OpenRouter.
const (
	defaultReferer = "https://github.com/weldnor/imagegen"
	defaultTitle   = "Imagen"
)

// Client calls OpenRouter's chat/completions API with a server-held key.
type Client struct {
	apiKey     string
	baseURL    string
	referer    string
	title      string
	timeout    time.Duration
	httpClient *http.Client
}

// Config configures a Client.
type Config struct {
	APIKey  string
	BaseURL string        // e.g. https://openrouter.ai/api/v1
	Timeout time.Duration // per-request upstream timeout
	Referer string        // optional; defaults to the project URL
	Title   string        // optional; defaults to "Imagen"

	// HTTPClient is optional; a sensible default is used when nil.
	HTTPClient *http.Client
}

// New builds a Client.
func New(cfg Config) *Client {
	c := &Client{
		apiKey:     cfg.APIKey,
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		referer:    cfg.Referer,
		title:      cfg.Title,
		timeout:    cfg.Timeout,
		httpClient: cfg.HTTPClient,
	}
	if c.referer == "" {
		c.referer = defaultReferer
	}
	if c.title == "" {
		c.title = defaultTitle
	}
	if c.timeout <= 0 {
		c.timeout = 120 * time.Second
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{Timeout: c.timeout + 30*time.Second}
	}
	return c
}

// Image is one generated image.
type Image struct {
	Data        []byte
	ContentType string
}

// Generate performs a single image generation: it builds the request body,
// calls OpenRouter, and extracts the image bytes. Errors from the upstream call
// are returned as *UpstreamError with an appropriate HTTP status and a
// key-stripped message.
func (c *Client) Generate(ctx context.Context, p GenerateParams) (*Image, error) {
	cfg, ok := Model(p.Model)
	if !ok {
		return nil, &UpstreamError{Status: http.StatusBadRequest, Message: "unknown model: " + p.Model}
	}

	body, err := json.Marshal(buildRequest(p, cfg))
	if err != nil {
		return nil, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", c.referer)
	req.Header.Set("X-Title", c.title)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
			return nil, &UpstreamError{Status: http.StatusGatewayTimeout, Message: "the image provider timed out"}
		}
		return nil, &UpstreamError{Status: http.StatusBadGateway, Message: "could not reach the image provider"}
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 96<<20))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, c.upstreamError(resp.StatusCode, raw)
	}

	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, &UpstreamError{Status: http.StatusBadGateway, Message: "unreadable response from the image provider"}
	}

	ref, err := extractImageRef(&parsed)
	if err != nil {
		return nil, &UpstreamError{Status: http.StatusBadGateway, Message: err.Error()}
	}

	data, ct, err := resolveImage(ctx, ref, c.httpClient)
	if err != nil {
		return nil, &UpstreamError{Status: http.StatusBadGateway, Message: stripKey(err.Error(), c.apiKey)}
	}
	return &Image{Data: data, ContentType: ct}, nil
}

// upstreamError maps a non-2xx OpenRouter response to an *UpstreamError,
// preserving the status class and forwarding the upstream message with the key
// stripped.
func (c *Client) upstreamError(status int, raw []byte) *UpstreamError {
	msg := fmt.Sprintf("image provider error (status %d)", status)
	var errBody struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &errBody) == nil && strings.TrimSpace(errBody.Error.Message) != "" {
		msg = errBody.Error.Message
	}
	msg = stripKey(msg, c.apiKey)

	out := status
	if status < 400 || status > 599 {
		out = http.StatusBadGateway
	}
	return &UpstreamError{Status: out, Message: msg}
}

func isTimeout(err error) bool {
	type timeout interface{ Timeout() bool }
	var t timeout
	return errors.As(err, &t) && t.Timeout()
}
