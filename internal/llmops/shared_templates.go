package llmops

import "embed"

// Partials included by more than one prompt set. Static text only: every
// including block is a byte-stable cacheable prefix (20260604-164527-d-tac-fah).
//
//go:embed shared_templates/*.tmpl
var sharedPromptTemplates embed.FS
