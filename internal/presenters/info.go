package presenters

import (
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
// it.
func RenderSyncBlocked(w io.Writer, block *model.SyncBlocked) {
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
	fmt.Fprintln(w, block.Steps())
}
