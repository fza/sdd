package test

import (
	"embed"
	"io/fs"
)

//go:embed fixtures/llmwindow
var llmWindowFixtures embed.FS

// LLMWindowFixtures holds provider answers that report a model's context
// window: the Anthropic Models API, an OpenRouter and a vLLM models listing,
// and Ollama's /api/show with and without a num_ctx parameter.
func LLMWindowFixtures() fs.FS {
	sub, err := fs.Sub(llmWindowFixtures, "fixtures/llmwindow")
	if err != nil {
		panic(err)
	}
	return sub
}
