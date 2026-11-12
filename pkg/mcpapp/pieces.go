package mcpapp

import (
	"fmt"
	"maps"
	"strings"

	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/application/types"
)

// Proofs is what a call lists about the pieces served to it before; the
// engine decides from them which piece serves as its pointer
// (20261112-075604-d-tac-t4r).
type Proofs struct {
	Held   []string `json:"held,omitempty" jsonschema:"for each served piece whose text you still see: its tag and the word right after the tag, as tag:word (e.g. k3f9:grounding). A proved piece serves as a short pointer; never guess a word"`
	Reread []string `json:"reread,omitempty" jsonschema:"tags or piece names of served pieces you no longer see; they serve in full again"`
}

// heldName names a held piece in a pointer: its name and the tag it is held
// under.
func heldName(name, tag string) string {
	if tag == "" {
		return name
	}
	return name + " " + model.AnchorMark(tag)
}

// heldLine is the pointer to pieces the call holds and the way to have them
// again.
func heldLine(names []string) string {
	return fmt.Sprintf("(held: %s — list a tag in reread to have its text again, or resume_session re-serves this position in full)", strings.Join(names, ", "))
}

// composeInstructions serves the step's first keep lanes: a lane the call
// holds becomes its pointer, the rest serve in full with their tags. A lane
// carrying an entry of pageFull, those the verify reading page served whole,
// holds that entry too. The lanes past keep wait, named with the call that
// serves them.
func composeInstructions(ss *shellSession, serve *sdd.WorkflowServe, keep int, pageFull []string) string {
	lanes := serve.InstructionLanes
	if len(lanes) == 0 {
		return serve.Instructions
	}
	var fresh, held, waiting []string
	for i, lane := range lanes {
		name := "lane " + lane.Name
		if i >= keep {
			waiting = append(waiting, lane.Name)
			continue
		}
		served := ss.root.ServeText(name, lane.Text, carried(pageFull, lane.Text))
		if served.Held {
			held = append(held, heldName(name, served.Tag))
			continue
		}
		fresh = append(fresh, served.Text)
	}
	if len(fresh) == 0 && len(waiting) == 0 {
		fresh = append(fresh, fmt.Sprintf("(you hold the step %s instructions — follow them; goal: %s)", serve.Step, serve.Goal))
	}
	text := strings.Join(fresh, "\n\n")
	if len(held) > 0 {
		text += "\n\n" + heldLine(held)
	}
	if len(waiting) > 0 {
		text += fmt.Sprintf("\n\n(%d more lanes of these instructions wait, as this result would run past its size bound: %s. Call next with this instance and no report to have them before you act; it serves the step again)", len(waiting), strings.Join(waiting, ", "))
	}
	return serve.ComposeInstructions(strings.TrimPrefix(text, "\n\n"))
}

func carried(ids []string, text string) []string {
	var in []string
	for _, id := range ids {
		if strings.Contains(text, id) {
			in = append(in, id)
		}
	}
	return in
}

// composeFraming serves the framing blocks per block: a graph write that
// changes one block (the recent-moves lane) re-serves that block alone, never
// the stable aspirations or directives (I6). A block the call holds is left
// out, as the agent named it itself; a pointer on every serve would be the
// repeat it saves. A block holds the entries it carries in full (R14); a
// block is never cut for them.
func composeFraming(ss *shellSession, blocks []types.ServeLane) string {
	var served []string
	for _, block := range blocks {
		if block.Text == "" {
			continue
		}
		piece := ss.root.ServeText("framing "+block.Name, block.Text, ss.root.FramingFullIDs(block.Text))
		if !piece.Held {
			served = append(served, piece.Text)
		}
	}
	return strings.Join(served, "\n\n")
}

// serveReportSchema serves a step's report schema: the required list and any
// chooser envelope inline, the properties block of the state fields it takes
// as a piece of its own (20260827-224111-s-tac-vz9), so steps that differ
// only in what they require share it. The block serves as its pointer when
// the call holds it, else with its tag in one description.
func serveReportSchema(ss *shellSession, serve *sdd.WorkflowServe) (map[string]any, error) {
	path, ok := fieldsAt(serve.ReportSchema)
	if !ok {
		return serve.ReportSchema, nil
	}
	fields := schemaAt(serve.ReportSchema, path)
	name := serve.Procedure + " report fields"
	piece, tagged, err := ss.root.ServeSchema(name, fields["properties"].(map[string]any))
	if err != nil {
		return nil, err
	}
	served := maps.Clone(fields)
	if piece.Held {
		delete(served, "properties")
		served["held_properties"] = fmt.Sprintf("you hold these %s as %s — list the tag in reread to have them again, or resume_session re-serves them in full", name, model.AnchorMark(piece.Tag))
	} else {
		served["properties"] = tagged
	}
	return withSchemaAt(serve.ReportSchema, path, served), nil
}

// fieldsAt is the path to the node whose properties are a schema's state
// fields: the schema itself for a report, its fields object for a chooser
// answer, none for an answer collecting no field.
func fieldsAt(schema map[string]any) ([]string, bool) {
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		return nil, false
	}
	if _, answer := props["chooser"]; !answer {
		return nil, true
	}
	fields, _ := props["fields"].(map[string]any)
	_, ok = fields["properties"].(map[string]any)
	return []string{"properties", "fields"}, ok
}

func schemaAt(schema map[string]any, path []string) map[string]any {
	for _, key := range path {
		schema, _ = schema[key].(map[string]any)
	}
	return schema
}

// withSchemaAt returns a copy of schema whose node at path is node; the maps
// on the path are copied, the rest is shared.
func withSchemaAt(schema map[string]any, path []string, node map[string]any) map[string]any {
	if len(path) == 0 {
		return node
	}
	out := maps.Clone(schema)
	child, _ := schema[path[0]].(map[string]any)
	out[path[0]] = withSchemaAt(child, path[1:], node)
	return out
}

// serveVocabulary serves the graph's vocabulary table, left out while the
// call holds it.
func serveVocabulary(ss *shellSession, vocabulary string) string {
	if vocabulary == "" {
		return ""
	}
	piece := ss.root.ServeText("vocabulary", vocabulary, nil)
	if piece.Held {
		return ""
	}
	return piece.Text
}
