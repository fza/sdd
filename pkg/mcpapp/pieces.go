package mcpapp

import (
	"fmt"
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
			held = append(held, heldName(name, served.Tag))
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

// serveReportSchema serves a step's report schema: its pointer when the call
// holds it, else the schema with its tag in one description.
func serveReportSchema(ss *shellSession, serve *sdd.WorkflowServe) (map[string]any, error) {
	name := fmt.Sprintf("schema %s/%s", serve.Procedure, serve.Step)
	piece, tagged, err := ss.root.ServeSchema(name, serve.ReportSchema)
	if err != nil {
		return nil, err
	}
	if !piece.Held {
		return tagged, nil
	}
	return map[string]any{
		"served_earlier": fmt.Sprintf("you hold the %s/%s report schema as %s — list the tag in reread to have it again, or resume_session re-serves it in full", serve.Procedure, serve.Step, model.AnchorMark(piece.Tag)),
	}, nil
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
