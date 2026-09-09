// Package openrouter builds requests to OpenRouter's chat/completions API,
// performs the call with the server-held API key, and extracts image bytes from
// the response.
package openrouter

import "sort"

// ModelConfig mirrors one entry of MODEL_CONFIGS in src/app.js. The set below is
// kept identical to the frontend's (verified by TestModelsMatchAppJS).
type ModelConfig struct {
	Name                string
	SupportsImageSize   bool
	SupportsAspectRatio bool
	SupportsImageInput  bool
	MaxReferences       int
}

// Models is the known model list. Ported verbatim from src/app.js MODEL_CONFIGS.
var Models = map[string]ModelConfig{
	"google/gemini-2.5-flash-image": {
		Name: "Gemini 2.5 Flash Image", SupportsImageSize: true, SupportsAspectRatio: true, SupportsImageInput: true, MaxReferences: 3,
	},
	"google/gemini-2.5-flash-image-preview": {
		Name: "Gemini 2.5 Flash Image (Preview)", SupportsImageSize: true, SupportsAspectRatio: true, SupportsImageInput: true, MaxReferences: 3,
	},
	"google/gemini-3.1-flash-image-preview": {
		Name: "Gemini 3.1 Flash Image (Preview)", SupportsImageSize: true, SupportsAspectRatio: true, SupportsImageInput: true, MaxReferences: 3,
	},
	"google/gemini-3-pro-image-preview": {
		Name: "Gemini 3 Pro Image (Preview)", SupportsImageSize: true, SupportsAspectRatio: true, SupportsImageInput: true, MaxReferences: 14,
	},
	"openai/gpt-5-image": {
		Name: "GPT-5 Image", SupportsImageSize: false, SupportsAspectRatio: true, SupportsImageInput: true, MaxReferences: 1,
	},
	"openai/gpt-5-image-mini": {
		Name: "GPT-5 Image Mini", SupportsImageSize: false, SupportsAspectRatio: true, SupportsImageInput: true, MaxReferences: 1,
	},
	"black-forest-labs/flux.2-pro": {
		Name: "Flux 2 Pro", SupportsImageSize: false, SupportsAspectRatio: true, SupportsImageInput: false, MaxReferences: 0,
	},
	"black-forest-labs/flux.2-max": {
		Name: "Flux 2 Max", SupportsImageSize: false, SupportsAspectRatio: true, SupportsImageInput: false, MaxReferences: 0,
	},
	"black-forest-labs/flux.2-flex": {
		Name: "Flux 2 Flex", SupportsImageSize: false, SupportsAspectRatio: true, SupportsImageInput: false, MaxReferences: 0,
	},
	"black-forest-labs/flux.2-klein-4b": {
		Name: "Flux 2 Klein 4B", SupportsImageSize: false, SupportsAspectRatio: true, SupportsImageInput: false, MaxReferences: 0,
	},
	"bytedance-seed/seedream-4.5": {
		Name: "Seedream 4.5", SupportsImageSize: false, SupportsAspectRatio: true, SupportsImageInput: false, MaxReferences: 0,
	},
	"sourceful/riverflow-v2-fast-preview": {
		Name: "Riverflow V2 Fast", SupportsImageSize: false, SupportsAspectRatio: true, SupportsImageInput: false, MaxReferences: 0,
	},
	"sourceful/riverflow-v2-standard-preview": {
		Name: "Riverflow V2 Standard", SupportsImageSize: false, SupportsAspectRatio: true, SupportsImageInput: false, MaxReferences: 0,
	},
	"sourceful/riverflow-v2-max-preview": {
		Name: "Riverflow V2 Max", SupportsImageSize: false, SupportsAspectRatio: true, SupportsImageInput: false, MaxReferences: 0,
	},
}

// Model returns the config for id and whether it is known.
func Model(id string) (ModelConfig, bool) {
	m, ok := Models[id]
	return m, ok
}

// KnownModels returns the model ids, sorted.
func KnownModels() []string {
	ids := make([]string, 0, len(Models))
	for id := range Models {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
