package gollm

import (
	"maps"
	"regexp"
	"strings"

	"github.com/networkteam/sdd/internal/model"
)

// anthropicEffortModels are the Claude model families whose Messages API
// takes output_config.effort; every other model refuses it.
var anthropicEffortModels = []string{
	"claude-fable-5", "claude-mythos-5", "claude-mythos-preview",
	"claude-opus-5", "claude-sonnet-5", "claude-haiku-5-5",
	"claude-opus-4-8", "claude-opus-4-7", "claude-opus-4-6", "claude-sonnet-4-6", "claude-opus-4-5",
}

// openAIReasoningModel matches the OpenAI reasoning model families, the only
// ones Chat Completions takes reasoning_effort for: the o-series and GPT-5 on.
// openAIWithoutEffort are the names among them that refuse it: the chat
// snapshots and the first o1 previews.
var (
	openAIReasoningModel = regexp.MustCompile(`^(o[1-9]|gpt-([5-9]|[1-9][0-9]))([.-]|$)`)
	openAIWithoutEffort  = regexp.MustCompile(`chat|^o1-(mini|preview)`)
)

// effortParam is the request field provider takes modelName's reasoning
// effort in, or "" where the model's API refuses one. gollm maps the Anthropic
// field "effort" to output_config.effort; Ollama levels exist only for
// gpt-oss, other thinking models take a boolean.
func effortParam(provider, modelName string) string {
	name := strings.ToLower(modelName)
	base := name[strings.LastIndex(name, "/")+1:]
	switch provider {
	case "anthropic":
		for _, family := range anthropicEffortModels {
			if strings.Contains(name, family) {
				return "effort"
			}
		}
	case "openai":
		if openAIReasoningModel.MatchString(base) && !openAIWithoutEffort.MatchString(base) {
			return "reasoning_effort"
		}
	case "ollama":
		if strings.HasPrefix(base, "gpt-oss") {
			return "think"
		}
	}
	return ""
}

// requestParams are the fields every request carries: llm.params, and the
// resolved effort where the model takes one and llm.params does not set it.
func requestParams(cfg model.LLMConfig) map[string]string {
	params := maps.Clone(cfg.Params)
	key := effortParam(cfg.Provider, cfg.Model)
	effort := cfg.ResolvedEffort()
	if key == "" || effort == "" {
		return params
	}
	if _, set := params[key]; set {
		return params
	}
	if params == nil {
		params = map[string]string{}
	}
	params[key] = effort
	return params
}
