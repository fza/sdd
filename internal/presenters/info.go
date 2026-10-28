package presenters

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/networkteam/sdd/internal/model"
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

type infoJSON struct {
	Participant  string                    `json:"participant"`
	Language     string                    `json:"language"`
	Search       string                    `json:"search"`
	GraphSchema  int                       `json:"graph_schema,omitempty"`
	BinarySchema int                       `json:"binary_schema"`
	Capabilities *model.SchemaCapabilities `json:"capabilities"`
	Contribution string                    `json:"contribution"`
}

// RenderInfoJSON writes the session framing as one JSON object for tools
// that drive sdd (20261027-122627-d-tac-2cx).
func RenderInfoJSON(w io.Writer, result *query.InfoResult) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	err := encoder.Encode(infoJSON{
		Participant:  result.LocalParticipant,
		Language:     result.Language,
		Search:       result.Search,
		GraphSchema:  result.GraphSchema,
		BinarySchema: result.BinarySchema,
		Capabilities: result.Capabilities,
		Contribution: result.Contribution,
	})
	if err != nil {
		return fmt.Errorf("encoding info as JSON: %w", err)
	}
	return nil
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

// RenderSyncBlocked emits the blocked-sync record and the steps that clear
// it, or, while an agent session's lease on the fix is live at now, who owns
// it.
func RenderSyncBlocked(w io.Writer, block *model.SyncBlocked, now time.Time) {
	if block == nil {
		return
	}
	fmt.Fprintf(w, "Sync blocked: %s since %s\n", block.Kind, block.Since.Local().Format(time.RFC3339))
	paths := make([]string, 0, len(block.Paths))
	for _, path := range block.Paths {
		paths = append(paths, strconv.Quote(path))
	}
	detail := ""
	if block.Detail != "" {
		detail = strconv.Quote(block.Detail)
	}
	for _, field := range []struct{ label, value string }{
		{"Upstream", block.Upstream}, {"Source", block.Source}, {"Destination", block.Destination},
		{"Paths", strings.Join(paths, ", ")}, {"Detail", detail}, {"Owner", block.Owner},
	} {
		if field.value != "" {
			fmt.Fprintf(w, "  %s: %s\n", field.label, field.value)
		}
	}
	if owner := block.LiveOwner(now); owner != "" {
		fmt.Fprintf(w, "Agent session %s owns the fix; leave it to that agent.\n", owner)
		return
	}
	fmt.Fprintln(w, block.Steps())
}
