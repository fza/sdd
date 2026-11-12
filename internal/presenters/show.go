package presenters

import (
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/networkteam/sdd/internal/mdcompose"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/pkg/application/types"
)

// bodyHeadingLevel is where an embedded entry body starts: one level below the
// `# body` section heading that contains it.
const bodyHeadingLevel = 2

// ShowOptions controls optional segments of the show rendering.
type ShowOptions struct {
	// WithSummary includes the primary's stored summary in the envelope.
	// Off by default — the body renders right after the envelope, and the
	// summary is only needed for human drift-review.
	WithSummary bool
	// Repeats applies the no-repeat rule; nil serves everything in full.
	Repeats *Repeats
	// Width is the target wrap width for the styled renderer's glamour body
	// (terminal columns). Zero falls back to a sensible default. Ignored by
	// the plain renderer, which never reflows the body.
	Width int
}

// RenderShow writes the plain-markdown show output for the shown entries
// (query.ShowResult.ShownEntries). Each entry renders as a YAML frontmatter
// envelope + raw markdown body, followed by compact markdown trees for the
// upstream and downstream neighborhoods (each present to the depth the query
// set; an empty direction is omitted). Entries are separated by a blank line —
// each is a self-contained frontmatter+body markdown document.
//
// This is the agent / pipe / `--format text` renderer; the styled terminal
// renderer (RenderShowStyled) reads the same data.
func RenderShow(w io.Writer, entries []types.ShownEntry, opts ShowOptions) error {
	for i, entry := range entries {
		if i > 0 {
			fmt.Fprintln(w)
		}
		if err := renderShowEntry(w, entry, opts); err != nil {
			return err
		}
	}
	opts.Repeats.WriteWayBack(w)
	return nil
}

// ShowText is the show text of the application and the MCP server: RenderShow
// with default options under r's no-repeat rule, without the trailing newline.
func ShowText(entries []types.ShownEntry, r *Repeats) (string, error) {
	var rendered strings.Builder
	if err := RenderShow(&rendered, entries, ShowOptions{Repeats: r}); err != nil {
		return "", err
	}
	return strings.TrimRight(rendered.String(), "\n"), nil
}

func renderShowEntry(w io.Writer, entry types.ShownEntry, opts ShowOptions) error {
	if pointer, held := opts.Repeats.pointer(entry.ID, entry.Brief); held {
		fmt.Fprintln(w, pointer)
		renderShowChains(w, entry, opts)
		return nil
	}
	if err := writeEnvelope(w, entry, opts); err != nil {
		return err
	}
	fmt.Fprintln(w)
	// Top-level `# body` heading with the body demoted beneath it, so an
	// embedded heading never sits level with the section containing it
	// (d-cpt-5wv).
	fmt.Fprintln(w, "# body")
	fmt.Fprintln(w)
	fmt.Fprintln(w, opts.Repeats.anchor(entry.ID, mdcompose.DemoteTo(entry.Body, bodyHeadingLevel)))
	renderShowChains(w, entry, opts)
	return nil
}

func renderShowChains(w io.Writer, entry types.ShownEntry, opts ShowOptions) {
	if len(entry.Upstream) > 0 || len(entry.UpstreamTruncated) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "# upstream")
		fmt.Fprintln(w)
		for _, item := range entry.Upstream {
			renderTreeItem(w, item, entry.ID, opts.Repeats)
		}
		renderDirectionCut(w, entry.UpstreamTruncated)
	}

	if len(entry.Downstream) > 0 || len(entry.DownstreamTruncated) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "# downstream")
		fmt.Fprintln(w)
		for _, item := range entry.Downstream {
			renderTreeItem(w, item, entry.ID, opts.Repeats)
		}
		renderDirectionCut(w, entry.DownstreamTruncated)
	}
}

// renderDirectionCut names the primary's own children a chain budget kept out
// of the direction entirely — the direction-level honest frontier.
func renderDirectionCut(w io.Writer, ids []string) {
	if len(ids) == 0 {
		return
	}
	fmt.Fprintf(w, "+%d more entries truncated (chain budget): %s\n", len(ids), strings.Join(ids, ", "))
}

// showEnvelope is the YAML frontmatter envelope for the primary entry. It
// mirrors the on-disk frontmatter (reusing model.Ref's object-form marshaling)
// and adds the filename-derived id, the entry time, discovered attachments,
// and the derived status and effective topics. Field order here is the YAML
// output order. A procedure's machine part (params/state/steps/framing)
// renders after the scannable tail, and Summary last (and only with
// --with-summary), so neither pushes the scannable fields down.
type showEnvelope struct {
	ID           string             `yaml:"id"`
	Type         string             `yaml:"type"`
	Kind         string             `yaml:"kind,omitempty"`
	Layer        string             `yaml:"layer"`
	Title        string             `yaml:"title,omitempty"`
	Confidence   string             `yaml:"confidence,omitempty"`
	Intent       string             `yaml:"intent,omitempty"`
	Participants []string           `yaml:"participants,omitempty"`
	Canonical    string             `yaml:"canonical,omitempty"`
	Aliases      []string           `yaml:"aliases,omitempty"`
	ActorKind    string             `yaml:"actor_kind,omitempty"`
	Class        string             `yaml:"class,omitempty"`
	Actor        string             `yaml:"actor,omitempty"`
	Topics       []string           `yaml:"topics,omitempty"`
	Index        *envelopeFactIndex `yaml:"index,omitempty"`
	Refs         []model.Ref        `yaml:"refs,omitempty"`
	Closes       []string           `yaml:"closes,omitempty"`
	Supersedes   []string           `yaml:"supersedes,omitempty"`
	Attachments  []string           `yaml:"attachments,omitempty"`
	Preflight    string             `yaml:"preflight,omitempty"`
	Grounding    string             `yaml:"grounding,omitempty"`
	Verification string             `yaml:"verification,omitempty"`
	Status       string             `yaml:"status,omitempty"`
	Time         string             `yaml:"time"`
	Params       yaml.Node          `yaml:"params,omitempty"`
	State        yaml.Node          `yaml:"state,omitempty"`
	Steps        yaml.Node          `yaml:"steps,omitempty"`
	Framing      yaml.Node          `yaml:"framing,omitempty"`
	Summary      string             `yaml:"summary,omitempty"`
}

// envelopeFactIndex is the fact index in model.FactIndex's frontmatter shape.
type envelopeFactIndex struct {
	Title string `yaml:"title"`
	Topic string `yaml:"topic"`
}

func writeEnvelope(w io.Writer, entry types.ShownEntry, opts ShowOptions) error {
	env := showEnvelope{
		ID:           entry.ID,
		Type:         entry.Type,
		Layer:        entry.Layer,
		Kind:         entry.Kind,
		Title:        entry.Title,
		Confidence:   entry.Confidence,
		Intent:       entry.Intent,
		Participants: entry.Participants,
		Canonical:    entry.Canonical,
		Aliases:      entry.Aliases,
		ActorKind:    entry.ActorKind,
		Class:        entry.Class,
		Actor:        entry.Actor,
		Topics:       entry.Topics,
		Refs:         envelopeRefs(entry.Refs),
		Closes:       entry.Closes,
		Supersedes:   entry.Supersedes,
		Attachments:  entry.Attachments,
		Preflight:    entry.Preflight,
		Grounding:    entry.Grounding,
		Verification: entry.Verification,
		Status:       formatStatusTrailValue(entry.DerivedStatus),
		Time:         entry.Time.Format("2006-01-02 15:04:05"),
	}
	if entry.Index != nil {
		env.Index = &envelopeFactIndex{Title: entry.Index.Title, Topic: entry.Index.Topic}
	}
	if spec := entry.Procedure; spec != nil {
		for _, section := range []struct {
			name   string
			text   string
			target *yaml.Node
		}{
			{"params", spec.Params, &env.Params},
			{"state", spec.State, &env.State},
			{"steps", spec.Steps, &env.Steps},
			{"framing", spec.Framing, &env.Framing},
		} {
			node, err := procedureSection(section.text)
			if err != nil {
				return fmt.Errorf("procedure %s of %s: %w", section.name, entry.ID, err)
			}
			*section.target = node
		}
	}
	if opts.WithSummary {
		env.Summary = entry.Summary
	}

	data, err := yaml.Marshal(&env)
	if err != nil {
		return fmt.Errorf("encoding the envelope of %s: %w", entry.ID, err)
	}
	fmt.Fprint(w, "---\n")
	fmt.Fprint(w, string(data))
	fmt.Fprint(w, "---\n")
	return nil
}

func envelopeRefs(refs []types.ShownRef) []model.Ref {
	var out []model.Ref
	for _, ref := range refs {
		out = append(out, model.Ref{ID: ref.ID, Kind: model.RefKind(ref.Kind), Desc: ref.Desc})
	}
	return out
}

// procedureSection decodes a procedure section's YAML text back into the node
// the envelope nests; an absent section stays the zero node omitempty drops.
func procedureSection(text string) (yaml.Node, error) {
	if text == "" {
		return yaml.Node{}, nil
	}
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(text), &document); err != nil {
		return yaml.Node{}, err
	}
	if len(document.Content) == 0 {
		return yaml.Node{}, nil
	}
	return *document.Content[0], nil
}

// renderTreeItem renders one neighborhood node as a markdown bullet. Indent
// encodes depth (depth 1 sits flush-left); the line shape is
// `- <verb> <full-id> (<kind>, <status>) — <first-sentence>`. An `↳` why
// sub-line follows when the ref carries a desc, and unexpanded children at the
// depth boundary render as an indented child-level truncation line — never
// inline on this node.
func renderTreeItem(w io.Writer, item types.ShownChainItem, primaryID string, r *Repeats) {
	bulletIndent := strings.Repeat("  ", item.Depth-1)
	subIndent := strings.Repeat("  ", item.Depth)

	var line strings.Builder
	line.WriteString(bulletIndent)
	line.WriteString("- ")
	line.WriteString(treeVerb(item))
	line.WriteString(" ")
	pointer, held := "", false
	if !item.ShownAbove && !item.ShownBelow {
		pointer, held = r.pointer(item.ID, item.Brief)
	}
	switch {
	case held:
		line.WriteString(pointer)
	default:
		line.WriteString(item.ID)
		if qual := treeQualifier(item); qual != "" {
			line.WriteString(" ")
			line.WriteString(qual)
		}
		if sentence := treeSentence(item, primaryID); sentence != "" {
			line.WriteString(" — ")
			line.WriteString(sentence)
		}
	}
	fmt.Fprintln(w, line.String())

	if item.Desc != "" {
		fmt.Fprintf(w, "%s↳ %s\n", subIndent, item.Desc)
	}

	if len(item.Truncated) > 0 {
		fmt.Fprintf(w, "%s%s\n", subIndent, truncationLine(item))
	}
}

func truncationLine(item types.ShownChainItem) string {
	reason := fmt.Sprintf("depth %d", item.Depth)
	if item.TruncatedReason != "" {
		reason = item.TruncatedReason
	}
	return fmt.Sprintf("+%d more refs truncated (%s): %s", len(item.Truncated), reason, strings.Join(item.Truncated, ", "))
}

// treeQualifier is the parenthesized `(<kind>, <status>)` slot for a tree
// node. Empty parts collapse so a node with no derived status renders
// `(<kind>)` and a legacy entry with neither renders no parens at all —
// never a bare `()` or a leading-comma `(, …)`. An unresolved cross-repo
// node takes the bracketed unresolved marker in this slot instead.
func treeQualifier(item types.ShownChainItem) string {
	if item.UnresolvedRepo != "" {
		return "[unresolved: repo " + item.UnresolvedRepo + "]"
	}
	kind := item.KindLabel
	status := formatStatusTrailValue(item.DerivedStatus)
	switch {
	case kind != "" && status != "":
		return "(" + kind + ", " + status + ")"
	case kind != "":
		return "(" + kind + ")"
	case status != "":
		return "(" + status + ")"
	default:
		return ""
	}
}

// treeSentence is the trailing micro-summary for a node. Dedup markers take the
// slot when the node was already shown (or is a later primary); otherwise it's
// the entry's first sentence — from the stored summary when present, else the
// body. An unresolved cross-repo node has none.
func treeSentence(item types.ShownChainItem, primaryID string) string {
	switch {
	case item.ShownAbove && item.ID == primaryID:
		return "(this entry)"
	case item.ShownAbove:
		return "(see above)"
	case item.ShownBelow:
		return "(see below)"
	}
	return item.FirstSentence
}

// treeVerb is the leading relation word for a tree node. A refs/refd-by edge
// carrying a kind renders as that kind (grounded-in, addresses, …) — the
// meaningful relationship; legacy refs without a kind stay "refs"/"refd-by".
// closes/supersedes (and their downstream inverses) render verbatim — they
// carry uniform meaning and never accept per-edge kinds. Combined relations
// join with a comma.
func treeVerb(item types.ShownChainItem) string {
	parts := make([]string, len(item.Relations))
	for i, r := range item.Relations {
		if (r == "refs" || r == "refd-by") && item.RefKind != "" && item.RefKind != string(model.RefKindUnknown) {
			parts[i] = item.RefKind
		} else {
			parts[i] = r
		}
	}
	return strings.Join(parts, ",")
}
