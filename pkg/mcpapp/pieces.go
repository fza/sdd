package mcpapp

import (
	"fmt"
	"strings"

	sdd "github.com/networkteam/sdd/pkg/application"
)

// Answering is what a call that advances a session carries: the answer to
// the question the response before ended with, required
// (20261121-230157-d-tac-ym8), and the pieces it wants in full again. A
// pointer to the reread slice keeps the argument types embedding it
// comparable.
type Answering struct {
	Answer string    `json:"answer" jsonschema:"required: the answer to the question the last response of this session ended with (q <text>: …? | lost): the words it asks for, looked up in that text, or lost when you no longer see that text or saw no question. Never guess. A right answer serves repeated text short; lost or a wrong answer serves it in full"`
	Reread *[]string `json:"reread,omitempty" jsonschema:"names of served pieces you no longer see; they serve in full again"`
}

// Rereads is what a read carries about the pieces served to it before.
type Rereads struct {
	Reread *[]string `json:"reread,omitempty" jsonschema:"names of served pieces you no longer see; they serve in full again"`
}

func rereadList(reread *[]string) []string {
	if reread == nil {
		return nil
	}
	return *reread
}

// heldLine is the pointer to pieces the call holds and the way to have them
// again.
func heldLine(names []string) string {
	return fmt.Sprintf("(held: %s — served to you before; list a name in reread to have its text again, or resume_session re-serves this position in full)", strings.Join(names, ", "))
}

// composeInstructions serves the step's lanes: a lane the call holds becomes
// its pointer, the rest serve in full. A lane carrying an entry the verify
// reading page served whole holds that entry too.
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
			held = append(held, name)
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
// out. A block holds the entries it carries in full; a block is never cut for
// them.
func composeFraming(ss *shellSession, blocks []string) string {
	var served []string
	for i, block := range blocks {
		if block == "" {
			continue
		}
		piece := ss.root.ServeText(fmt.Sprintf("framing block %d", i+1), block, ss.root.FramingFullIDs(block))
		if !piece.Held {
			served = append(served, piece.Text)
		}
	}
	return strings.Join(served, "\n\n")
}

// serveReportSchema serves a step's report schema: its pointer when the call
// holds it.
func serveReportSchema(ss *shellSession, serve *sdd.WorkflowServe) (map[string]any, error) {
	name := fmt.Sprintf("schema %s/%s", serve.Procedure, serve.Step)
	held, err := ss.root.ServeSchema(name, serve.ReportSchema)
	if err != nil {
		return nil, err
	}
	if !held {
		return serve.ReportSchema, nil
	}
	return map[string]any{
		"served_earlier": fmt.Sprintf("you hold the %s/%s report schema, served to you before; list %q in reread to have it again, or resume_session re-serves it in full", serve.Procedure, serve.Step, name),
	}, nil
}

// serveVocabulary serves the graph's vocabulary table; while the call holds
// it, nothing.
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
