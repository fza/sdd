package mcpapp

import (
	"fmt"
	"strings"

	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// Proofs is what a call lists about the pieces served to it before; the
// engine decides from them which piece serves as its pointer. Pointers to
// slices keep the argument types embedding it comparable.
type Proofs struct {
	Held   *[]string `json:"held,omitempty" jsonschema:"for each served piece whose text you still see, from every earlier result, not only the last: its tag and the word right after the tag, as tag:word (e.g. \"… dialogue ⟦gn9p⟧ first …\" → gn9p:first), or the word its last pointer asked for (next proof). A proved piece serves as a short pointer; a proof works once; never guess a word"`
	Reread *[]string `json:"reread,omitempty" jsonschema:"tags or piece names of served pieces you no longer see; they serve in full again"`
}

func (p Proofs) lists() (held, reread []string) {
	if p.Held != nil {
		held = *p.Held
	}
	if p.Reread != nil {
		reread = *p.Reread
	}
	return held, reread
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
	texts := make([]string, len(lanes))
	for i, lane := range lanes {
		texts[i] = lane.Text
	}
	field := strings.Join(texts, "\n\n")
	var fresh, held []string
	for _, lane := range lanes {
		name := "lane " + lane.Name
		served := ss.root.ServeTextIn(name, lane.Text, field, carried(pageFull, lane.Text))
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
	field := strings.Join(blocks, "\n\n")
	var served, held []string
	for i, block := range blocks {
		if block == "" {
			continue
		}
		name := fmt.Sprintf("framing block %d", i+1)
		piece := ss.root.ServeTextIn(name, block, field, ss.root.FramingFullIDs(block))
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
		"served_earlier": fmt.Sprintf("you hold the %s/%s report schema as %s — list the tag in reread to have it again, or resume_session re-serves it in full", serve.Procedure, serve.Step, strings.TrimSpace(heldName("", piece.Tag, piece.Next))),
	}, nil
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
