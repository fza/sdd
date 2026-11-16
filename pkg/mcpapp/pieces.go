package mcpapp

import (
	"fmt"
	"maps"
	"strings"

	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// Proofs is what a call lists about the pieces served to it before; the
// engine decides from them which piece serves as its pointer.
type Proofs struct {
	Held   []string `json:"held,omitempty" jsonschema:"for each served piece whose text you still see, from every earlier result, not only the last: its tag and the word right after the tag, as tag:word (e.g. \"… dialogue ⟦gn9p⟧ first …\" → gn9p:first), or the word its last pointer asked for (next proof). A proved piece serves as a short pointer; a proof works once; never guess a word"`
	Reread []string `json:"reread,omitempty" jsonschema:"tags or piece names of served pieces you no longer see; they serve in full again"`
}

// heldName names a held piece in a pointer: its name, the tag it is held
// under and how the next call proves it.
func heldName(name, tag, next string) string {
	if tag == "" {
		return name
	}
	if proof := model.NextProof(tag, next); proof != "" {
		return name + " " + model.AnchorMark(tag) + " (" + proof + ")"
	}
	return name + " " + model.AnchorMark(tag)
}

// heldLine is the pointer to pieces the call holds and the way to have them
// again.
func heldLine(names []string) string {
	return fmt.Sprintf("(held: %s — list a tag in reread to have its text again, or resume_session re-serves this position in full)", strings.Join(names, ", "))
}

// composeInstructions serves the step's lanes: a lane the call holds becomes
// its pointer, the rest serve in full with their tags. A lane carrying an
// entry the verify reading page served whole holds that entry too.
func composeInstructions(ss *shellSession, serve *sdd.WorkflowServe) string {
	lanes := serve.InstructionLanes
	if len(lanes) == 0 {
		return serve.Instructions
	}
	pageFull := ss.root.TakeStepFullIDs()
	var fresh, held []string
	for _, lane := range lanes {
		name := "lane " + lane.Name
		served := ss.root.ServeText(name, lane.Text, carried(pageFull, lane.Text))
		if served.Held {
			held = append(held, heldName(name, served.Tag, served.Next))
			continue
		}
		fresh = append(fresh, served.Text)
	}
	if len(fresh) == 0 {
		fresh = append(fresh, fmt.Sprintf("(you hold the step %s instructions — follow them; goal: %s)", serve.Step, serve.Goal))
	}
	text := strings.Join(fresh, "\n\n")
	if len(held) > 0 {
		text += "\n\n" + heldLine(held)
	}
	return serve.ComposeInstructions(text)
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
// out, as the agent named it itself, except for the next proof it asks for.
// A block holds the entries it carries in full; a block is never cut for
// them.
func composeFraming(ss *shellSession, blocks []string) string {
	var served, held []string
	for i, block := range blocks {
		if block == "" {
			continue
		}
		name := fmt.Sprintf("framing block %d", i+1)
		piece := ss.root.ServeText(name, block, ss.root.FramingFullIDs(block))
		if !piece.Held {
			served = append(served, piece.Text)
			continue
		}
		held = append(held, heldName(name, piece.Tag, piece.Next))
	}
	if len(held) > 0 {
		served = append(served, heldLine(held))
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
		served["held_properties"] = fmt.Sprintf("you hold these %s as %s — list the tag in reread to have them again, or resume_session re-serves them in full", name, strings.TrimSpace(heldName("", piece.Tag, piece.Next)))
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

// serveVocabulary serves the graph's vocabulary table; while the call holds
// it, only its held line with the next proof.
func serveVocabulary(ss *shellSession, vocabulary string) string {
	if vocabulary == "" {
		return ""
	}
	piece := ss.root.ServeText("vocabulary", vocabulary, nil)
	if !piece.Held {
		return piece.Text
	}
	return heldLine([]string{heldName("vocabulary", piece.Tag, piece.Next)})
}
