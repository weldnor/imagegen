package openrouter

import (
	"fmt"
	"regexp"
	"strings"
)

// UpstreamError describes a failed OpenRouter call in terms the HTTP layer can
// return directly. Status is the status code to send to the client; Message is
// safe to expose (never contains the API key).
type UpstreamError struct {
	Status  int
	Message string
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("openrouter: %d: %s", e.Status, e.Message)
}

var bearerRe = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._\-]+`)

// stripKey removes the API key and any "Bearer <token>" sequence from s so an
// upstream error message can be forwarded without leaking credentials.
func stripKey(s, apiKey string) string {
	if apiKey != "" {
		s = strings.ReplaceAll(s, apiKey, "[redacted]")
	}
	return bearerRe.ReplaceAllString(s, "Bearer [redacted]")
}
