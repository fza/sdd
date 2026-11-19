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
// engine decides from them which piece serves as its pointer. Pointers to
// slices keep the argument types embedding it comparable.
type Proofs struct {
	Held   *[]string `json:"held,omitempty" jsonschema:"for each served piece whose text you still see: its tag and the word right after the tag in that text, as tag:word (e.g. \"… dialogue ⟦gn9p⟧ first …\" → gn9p:first), or, once its pointer names a next proof, the word that proof asks for, looked up in that text. List the tags from every earlier result, not only the last; the unproved field names those you have not proved since. A proved piece serves as a short pointer; a proof works once; a tag left out serves its whole text again. Never guess a word"`
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

// heldName names a held piece in a pointer by the tag it is held under, then
// its name: an agent read the name's first word as the proof word when the
// tag followed it. next is the word whose follower the next proof names.
func heldName(name, tag, next string) string {
	if tag == "" {
		return name
	}
	if proof := model.NextProof(tag, next); proof != "" {
		return model.TagName(tag) + " for " + name + " (" + proof + ")"
	}
	return model.TagName(tag) + " for " + name
}

const unprovedLead = "Earlier pieces you have not proved since, newest first. For each whose text you still see, list in held its tag and the word right after that tag in the full text you were served, never a word of this line, as tag:word; never guess a word. The pieces: "

// unprovedLine names the pieces of list, newest first, as many as fit room
// characters of a result, the instruction to prove them leading.
func unprovedLine(list []sdd.UnprovedPiece, room int) string {
	items := make([]string, 0, len(list))
	for _, piece := range list {
		items = append(items, heldName(piece.Name, piece.Tag, ""))
	}
	for ; len(items) > 0; items = items[:len(items)-1] {
		line := unprovedLead + strings.Join(items, "; ")
		if model.ServedChars(line) <= room {
			return line
		}
	}
	return ""
}

// proofNotice names the proofs of a call that failed, so the agent corrects
// or drops them.
func proofNotice(failed []sdd.FailedProof) string {
	names := make([]string, 0, len(failed))
	for _, f := range failed {
		reason := "no such tag"
		if f.Issued {
			reason = "not the word its proof asks for"
		}
		names = append(names, f.Proof+" ("+reason+")")
	}
	return fmt.Sprintf("these held proofs failed, so their pieces served in full: %s. A proof works once: send the word the tag's last next proof asked for, else the word right after the tag, in its full text, or leave the tag out", strings.Join(names, ", "))
}

// heldLine is the pointer to pieces the call holds and the way to have them
// again.
func heldLine(names []string) string {
	return fmt.Sprintf("(held: %s — prove each tag with the word its next proof asks for, else the word after the tag, in the full text you were served, never a word of this line; list a tag in reread to have its text again, or resume_session re-serves this position in full)", strings.Join(names, ", "))
}

// composeInstructions serves the step's first keep lanes: a lane the call
// holds becomes its pointer, the rest serve in full with their tags. With
// resent, a lane the result before sent becomes its pointer even where the
// call rereads it or proves it wrong, so lanes that wait take the room. A lane
// carrying an entry of pageFull, those the verify reading page served whole,
// holds that entry too. The lanes past keep wait, named with the call that
// serves them. It returns how many lanes it served in full.
func composeInstructions(ss *shellSession, serve *sdd.WorkflowServe, keep int, resent bool, pageFull []string) (string, int) {
	lanes := serve.InstructionLanes
	if len(lanes) == 0 {
		return serve.Instructions, 0
	}
	var fresh, held, waiting, pointed []string
	for i, lane := range lanes {
		name := "lane " + lane.Name
		if i >= keep {
			waiting = append(waiting, lane.Name)
			continue
		}
		// A playback recorded delivered holds the draft itself, never its pointer.
		if serve.PlaysBack && lane.Name == "draft" {
			fresh = append(fresh, lane.Text)
			continue
		}
		if resent {
			if tag, ok := ss.root.Resent(serve.Instance, name, lane.Text); ok {
				held = append(held, heldName(name, tag, ""))
				pointed = append(pointed, name)
				continue
			}
		}
		served := ss.root.ServeText(name, lane.Text, carried(pageFull, lane.Text))
		if served.Held {
			held = append(held, heldName(name, served.Tag, served.Next))
			continue
		}
		fresh = append(fresh, served.Text)
	}
	full := len(fresh)
	if len(fresh) == 0 && len(waiting) == 0 {
		fresh = append(fresh, fmt.Sprintf("(you hold the step %s instructions — follow them; goal: %s)", serve.Step, serve.Goal))
	}
	text := strings.Join(fresh, "\n\n")
	if len(held) > 0 {
		text += "\n\n" + heldLine(held)
	}
	if len(pointed) > 0 {
		text += fmt.Sprintf("\n\n(%s: sent in the result before, they serve as pointers so the lanes that waited fit, whatever this call proved or reread; once no lane waits, a reread has them in full)", strings.Join(pointed, ", "))
	}
	if len(waiting) > 0 {
		text += fmt.Sprintf("\n\n(%d more lanes of these instructions wait, as this result would run past its size bound: %s. Call next with this instance and no report to have them before you act; it serves the step again)", len(waiting), strings.Join(waiting, ", "))
	}
	return serve.ComposeInstructions(strings.TrimPrefix(text, "\n\n")), full
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
// out, as the agent named it itself, except for the next proof its pointer
// asks for (heldLine). A block holds the entries it carries in full; a
// block is never cut for them. Only the first keep blocks serve; the rest
// wait, as instruction lanes do past the result bound.
func composeFraming(ss *shellSession, blocks []types.ServeLane, keep int) framingServed {
	var (
		result       framingServed
		served, held []string
	)
	for _, block := range blocks {
		if block.Text == "" {
			continue
		}
		result.blocks++
		if result.blocks > keep {
			result.waiting = append(result.waiting, block.Name)
			continue
		}
		name := "framing " + block.Name
		piece := ss.root.ServeText(name, block.Text, ss.root.FramingFullIDs(block.Text))
		switch {
		case !piece.Held:
			served = append(served, piece.Text)
		case piece.Next != "":
			held = append(held, heldName(name, piece.Tag, piece.Next))
		}
	}
	result.fresh = len(served)
	if len(held) > 0 {
		served = append(served, heldLine(held))
	}
	result.text = strings.Join(served, "\n\n")
	return result
}

// framingServed is a composed framing: its text, the blocks that wait, how
// many blocks it served in full and how many the framing has.
type framingServed struct {
	text          string
	waiting       []string
	fresh, blocks int
}

// framingWaitLine names the framing blocks that wait; lanesWait leaves the
// call that serves them to the line naming the waiting lanes.
func framingWaitLine(waiting []string, lanesWait bool) string {
	if lanesWait {
		return fmt.Sprintf("(framing blocks wait too: %s)", strings.Join(waiting, ", "))
	}
	return fmt.Sprintf("(%d framing blocks wait, as this result would run past its size bound: %s. Call next with this instance and no report to have them before you act; it serves the step again)", len(waiting), strings.Join(waiting, ", "))
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
		served["held_properties"] = fmt.Sprintf("you hold %s — prove the tag with the word its next proof asks for, else the word after the tag, in the full schema you were served, never a word here; list the tag in reread to have them again, or resume_session re-serves them in full", heldName("these "+name, piece.Tag, piece.Next))
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

// serveVocabulary serves the graph's vocabulary table, reporting whether in
// full; while the call holds it, only the next proof its pointer asks for.
func serveVocabulary(ss *shellSession, vocabulary string) (string, bool) {
	if vocabulary == "" {
		return "", false
	}
	piece := ss.root.ServeText("vocabulary", vocabulary, nil)
	if !piece.Held {
		return piece.Text, true
	}
	if piece.Next == "" {
		return "", false
	}
	return heldLine([]string{heldName("vocabulary", piece.Tag, piece.Next)}), false
}
