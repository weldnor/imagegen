package openrouter

import "strings"

// AspectRatios and ImageSizes are the values the upstream image API accepts;
// anything else is rejected there with an "invalid option" error, so both the
// HTTP API and the bot validate against these lists before sending a request.
var (
	AspectRatios = []string{
		"1:1", "1:4", "1:8", "2:3", "3:2", "3:4", "4:1",
		"4:3", "4:5", "5:4", "8:1", "9:16", "16:9", "21:9",
	}
	ImageSizes = []string{"0.5K", "1K", "2K", "4K"}
)

// legacySizes maps the pixel dimensions earlier versions stored (and offered
// as bot buttons) onto the quality tokens the API expects, so regenerating an
// old gallery image still works.
var legacySizes = map[string]string{
	"512x512":   "0.5K",
	"1024x1024": "1K",
	"2048x2048": "2K",
	"4096x4096": "4K",
}

// NormalizeAspectRatio canonicalises a user-supplied aspect ratio, reporting
// whether it is one the API accepts. The empty string normalises to itself
// and is valid: it means "let the model decide".
func NormalizeAspectRatio(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", true
	}
	for _, r := range AspectRatios {
		if s == r {
			return r, true
		}
	}
	return "", false
}

// NormalizeImageSize canonicalises a user-supplied image size (case-insensitive,
// legacy pixel dimensions included), reporting whether it is one the API
// accepts. The empty string is valid and means "model default".
func NormalizeImageSize(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", true
	}
	if token, ok := legacySizes[strings.ToLower(s)]; ok {
		return token, true
	}
	for _, size := range ImageSizes {
		if strings.EqualFold(s, size) {
			return size, true
		}
	}
	return "", false
}
