package model

// ResultBound is how many characters the text content of one MCP result may
// take, as ServedChars counts them, for the host the result is served to.
// Every other bound on a result derives from it.
type ResultBound int

const (
	// DefaultResultBound holds for every host but Claude Code: Codex prints
	// a result's JSON within a 10,000-token output budget and drops the middle
	// of a larger one, past about 19,000 characters of text.
	DefaultResultBound ResultBound = 18000
	// ClaudeCodeResultBound stays below the 50,000 characters past which
	// Claude Code saves a tool's result to a file (20261122-103723-d-tac-npg).
	ClaudeCodeResultBound ResultBound = 45000
	// ClaudeCodeClient is the MCP client name Claude Code reports.
	ClaudeCodeClient = "claude-code"
	// charsPerOutputToken turns Claude Code's MCP token limit into
	// characters, a quarter below the 2.67 measured on sdd results.
	charsPerOutputToken = 2
)

// ResultBoundFor is the bound of a result served to the MCP client named
// clientName. Only the exact name claude-code gets ClaudeCodeResultBound,
// lowered to two characters per token of mcpOutputTokens, Claude Code's
// MAX_MCP_OUTPUT_TOKENS as sdd's environment holds it (0 when unset), but
// never below DefaultResultBound, the least bound every page is sized for.
func ResultBoundFor(clientName string, mcpOutputTokens int) ResultBound {
	if clientName != ClaudeCodeClient {
		return DefaultResultBound
	}
	bound := ClaudeCodeResultBound
	if mcpOutputTokens > 0 {
		bound = min(bound, ResultBound(mcpOutputTokens*charsPerOutputToken))
	}
	return max(bound, DefaultResultBound)
}

// Serve is what one serve may take of a result, leaving room for the notices
// that join it.
func (b ResultBound) Serve() int { return int(b) - 2000 }

// Read is what the one large text field of a read may take.
func (b ResultBound) Read() int { return int(b) - 3000 }

// DeepPage bounds what one page of the deep verifier's reading costs in the
// result that serves it, leaving the rest of Serve to the serve around the
// page: the resume serving page 1 with the question guide, the verifier's
// rules lane and the report schema.
func (b ResultBound) DeepPage() int { return b.Serve() - 8000 }
