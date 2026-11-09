package llmops

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	"github.com/networkteam/sdd/pkg/llm"
)

//go:embed gatherquery_templates/*.tmpl
var gatherQueryTemplates embed.FS

var gatherQueryTemplate = template.Must(template.New("gather_query").ParseFS(gatherQueryTemplates, "gatherquery_templates/*.tmpl"))

// GatherQueryDraft is what the query writer reads: the draft's kind, topics
// and text.
type GatherQueryDraft struct {
	Kind   string
	Topics []string
	Text   string
}

// GatherQueries is what the query writer wrote: one semantic phrase and the
// keyword terms, one text search each.
type GatherQueries struct {
	Query string
	Terms []string
}

// GatherQuery writes the searches a grounding reading runs for a draft
// (20261109-004506-d-cpt-udz, D24): the queries must bear on what the draft
// commits to, which no fixed rule over the draft's words guarantees.
func GatherQuery(ctx context.Context, runner llm.Runner, draft GatherQueryDraft) (GatherQueries, error) {
	if strings.TrimSpace(draft.Text) == "" {
		return GatherQueries{}, fmt.Errorf("the query writer needs the draft's text")
	}
	var sys, user strings.Builder
	data := struct{ Kind, Topics, Draft string }{Kind: draft.Kind, Topics: strings.Join(draft.Topics, ", "), Draft: draft.Text}
	if err := gatherQueryTemplate.ExecuteTemplate(&sys, "gather_query_system", data); err != nil {
		return GatherQueries{}, fmt.Errorf("rendering the gather-query prompt: %w", err)
	}
	if err := gatherQueryTemplate.ExecuteTemplate(&user, "gather_query_user", data); err != nil {
		return GatherQueries{}, fmt.Errorf("rendering the gather-query prompt: %w", err)
	}
	result, err := runner.Run(ctx, llm.Request{
		Purpose:        llm.PurposeGatherQuery,
		SystemPrompt:   strings.TrimSpace(sys.String()),
		UserPrompt:     strings.TrimSpace(user.String()),
		ResponseSchema: gatherQuerySchema,
	})
	if err != nil {
		return GatherQueries{}, fmt.Errorf("writing the gather queries: %w", err)
	}
	return parseGatherQueries(result.Text)
}

func parseGatherQueries(output string) (GatherQueries, error) {
	text, err := extractJSONObject(output)
	if err != nil {
		return GatherQueries{}, fmt.Errorf("parsing the gather queries: %w", err)
	}
	var resp struct {
		Query string   `json:"query"`
		Terms []string `json:"terms"`
	}
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		return GatherQueries{}, fmt.Errorf("parsing the gather queries: %w", err)
	}
	queries := GatherQueries{Query: strings.TrimSpace(resp.Query)}
	if queries.Query == "" {
		return GatherQueries{}, fmt.Errorf("parsing the gather queries: the query is empty")
	}
	seen := map[string]bool{}
	for _, term := range resp.Terms {
		term = strings.TrimSpace(term)
		if term == "" || seen[strings.ToLower(term)] {
			continue
		}
		seen[strings.ToLower(term)] = true
		queries.Terms = append(queries.Terms, term)
	}
	if len(queries.Terms) == 0 {
		return GatherQueries{}, fmt.Errorf("parsing the gather queries: no keyword term")
	}
	return queries, nil
}
