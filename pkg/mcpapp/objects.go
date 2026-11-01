package mcpapp

import (
	"strings"

	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/application/types"
)

// Object results replace the text fields when Options.ServeInFull is set
// (20261101-165616-d-tac-qtk): a client that collapses repeats itself splits
// them per entry, hit, lane and block without parsing sdd's text.

type NamedText struct {
	Name string `json:"name" jsonschema:"stable name of the lane or block across calls"`
	Text string `json:"text"`
}

type ShowItemResult struct {
	ID         string            `json:"id"`
	Title      string            `json:"title,omitempty"`
	Type       string            `json:"type"`
	Kind       string            `json:"kind,omitempty"`
	Layer      string            `json:"layer"`
	Status     string            `json:"status,omitempty"`
	Summary    string            `json:"summary,omitempty"`
	Body       string            `json:"body" jsonschema:"the entry's full text"`
	Upstream   []ChainItemResult `json:"upstream,omitempty" jsonschema:"upstream chain in preorder: each item followed by its descendants, depth 1 linking to this entry"`
	Downstream []ChainItemResult `json:"downstream,omitempty" jsonschema:"downstream chain in preorder, as upstream"`

	UpstreamTruncated   []string `json:"upstream_truncated,omitempty" jsonschema:"IDs cut from the upstream chain by its size budget; show them to expand"`
	DownstreamTruncated []string `json:"downstream_truncated,omitempty" jsonschema:"IDs cut from the downstream chain by its size budget"`
}

type ChainItemResult struct {
	Ref            string   `json:"ref,omitempty" jsonschema:"kind of the refs edge, empty when the edge has none"`
	Relation       string   `json:"relation,omitempty" jsonschema:"the edge: refs, closes, supersedes, refd-by and their inverses"`
	ID             string   `json:"id"`
	Title          string   `json:"title,omitempty"`
	Kind           string   `json:"kind,omitempty"`
	Status         string   `json:"status,omitempty"`
	Summary        string   `json:"summary,omitempty"`
	Desc           string   `json:"desc,omitempty"`
	Depth          int      `json:"depth" jsonschema:"distance from the shown entry: 1 links to it directly, 2 to a depth-1 item above it in the list"`
	SeenInResponse bool     `json:"seen_in_response,omitempty" jsonschema:"the entry already appears elsewhere in this response"`
	Truncated      []string `json:"truncated,omitempty" jsonschema:"children of this item hidden at the depth or fan-out frontier; show them to expand"`
}

type SearchHitResult struct {
	ID      string  `json:"id"`
	Title   string  `json:"title,omitempty"`
	Type    string  `json:"type"`
	Kind    string  `json:"kind,omitempty"`
	Layer   string  `json:"layer"`
	Status  string  `json:"status,omitempty"`
	Summary string  `json:"summary,omitempty"`
	Score   float32 `json:"score"`
}

func showItems(entries []sdd.ShownEntry) []ShowItemResult {
	items := make([]ShowItemResult, 0, len(entries))
	for _, entry := range entries {
		items = append(items, ShowItemResult{
			ID: entry.ID, Title: entry.Title, Type: entry.Type, Kind: entry.Kind, Layer: entry.Layer, Status: entry.Status,
			Summary: entry.Summary, Body: entry.Body, Upstream: chainItems(entry.Upstream), Downstream: chainItems(entry.Downstream),
			UpstreamTruncated: entry.UpstreamTruncated, DownstreamTruncated: entry.DownstreamTruncated,
		})
	}
	return items
}

func chainItems(chain []sdd.ShownChainItem) []ChainItemResult {
	var items []ChainItemResult
	for _, item := range chain {
		items = append(items, ChainItemResult{
			Ref: item.RefKind, Relation: item.Relation, ID: item.ID, Title: item.Title, Kind: item.Kind, Status: item.Status,
			Summary: item.Summary, Desc: item.Desc, Depth: item.Depth, SeenInResponse: item.SeenInResponse, Truncated: item.Truncated,
		})
	}
	return items
}

func searchHitItems(hits []sdd.SearchHit) []SearchHitResult {
	items := make([]SearchHitResult, 0, len(hits))
	for _, hit := range hits {
		items = append(items, SearchHitResult{
			ID: hit.ID, Title: hit.Title, Type: hit.Type, Kind: hit.Kind, Layer: hit.Layer, Status: hit.Status, Summary: hit.Summary, Score: hit.Score,
		})
	}
	return items
}

// instructionLanes is composeInstructions for an in-full session: each lane
// as its own object, a lane repeated within this response sent once, the
// gate's diagnostics as a final diagnostics lane.
func instructionLanes(ss *shellSession, serve *sdd.WorkflowServe) []NamedText {
	if len(serve.InstructionLanes) == 0 {
		if serve.Instructions == "" {
			return []NamedText{}
		}
		return []NamedText{{Name: "instructions", Text: serve.Instructions}}
	}
	lanes := []NamedText{}
	for _, lane := range serve.InstructionLanes {
		if ss.servedBefore(lane.Text) {
			continue
		}
		lanes = append(lanes, NamedText{Name: lane.Name, Text: lane.Text})
	}
	if diagnostics := strings.TrimSpace(serve.ComposeInstructions("")); diagnostics != "" {
		lanes = append(lanes, NamedText{Name: "diagnostics", Text: diagnostics})
	}
	return lanes
}

// framingFields fills either the framing text or, in full, the named blocks.
func framingFields(ss *shellSession, blocks []types.ServeLane) (string, *[]NamedText) {
	if ss.inFull {
		named := framingBlocks(ss, blocks)
		return "", &named
	}
	texts := make([]string, len(blocks))
	for i, block := range blocks {
		texts[i] = block.Text
	}
	return composeFraming(ss, texts), nil
}

func framingBlocks(ss *shellSession, blocks []types.ServeLane) []NamedText {
	named := []NamedText{}
	for _, block := range blocks {
		if block.Text == "" || ss.servedBefore(block.Text) {
			continue
		}
		named = append(named, NamedText{Name: block.Name, Text: block.Text})
	}
	return named
}
