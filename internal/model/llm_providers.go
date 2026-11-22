package model

// LLMProviders lists the values llm.provider takes.
func LLMProviders() []string {
	return []string{DefaultLLMProvider, "anthropic", "openai", "ollama"}
}
