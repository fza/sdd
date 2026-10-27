package presenters

import (
	"fmt"
	"io"

	"github.com/networkteam/sdd/internal/query"
)

// RenderInfo writes the session-framing header — participant, language
// (when configured), and search capability. These lines are what skill
// session-start injections read for framing.
func RenderInfo(w io.Writer, result *query.InfoResult) {
	writeInfoHeader(w, result.LocalParticipant, result.Language, result.Search)
	if line := SchemaLine(result.GraphSchema, result.BinarySchema); line != "" {
		fmt.Fprintln(w, line)
	}
	if result.Contribution != "" {
		fmt.Fprintf(w, "Mode: contribution (%s)\n", result.Contribution)
	}
}

// SchemaLine names the graph's schema and, when the graph is older than the
// binary, that writes keep the graph's schema until `sdd migrate`. Empty when
// the graph's schema is unknown.
func SchemaLine(graph, binary int) string {
	if graph == 0 {
		return ""
	}
	if graph < binary {
		return fmt.Sprintf("Schema: graph %d, binary %d (writes keep schema %d until `sdd migrate`)", graph, binary, graph)
	}
	return fmt.Sprintf("Schema: %d", graph)
}

// writeInfoHeader emits the canonical session-header lines.
func writeInfoHeader(w io.Writer, localParticipant, language, search string) {
	if localParticipant != "" {
		fmt.Fprintf(w, "Local participant: %s\n", localParticipant)
	} else {
		fmt.Fprintln(w, "Local participant: (not configured — run sdd init)")
	}
	if language != "" {
		fmt.Fprintf(w, "Language: %s\n", language)
	}
	if search != "" {
		fmt.Fprintf(w, "Search: %s\n", search)
	}
}
