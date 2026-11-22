package mcpapp

import (
	"fmt"
	"maps"
	"strings"

	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/application/types"
)

// Answering is what a call that advances a session carries: the answer to
// the question the response before ended with, required
// (20261121-230157-d-tac-ym8), and the pieces it wants in full again. A
// pointer to the reread slice keeps the argument types embedding it
// comparable.
type Answering struct {
	Answer string    `json:"answer" jsonschema:"required: the answers to the questions the last response of this session ended with (q <text>: …? | lost, one per line), in their order and parted by ; (reach sdd ; 280): the words each asks for, looked up in its text, or lost when you no longer see that text or saw no question. Never guess. Both right serve repeated text short; lost or a wrong answer to either serves it in full"`
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

// composeInstructions serves the step's first keep lanes: a lane the call
// holds becomes its pointer, the rest serve in full. With resent, a lane the
// result before sent becomes its pointer even where the call rereads it, so
// lanes that wait take the room. A lane carrying an entry of pageFull, those
// the verify reading page served whole, holds that entry too. The lanes past
// keep wait, named with the call that serves them. It returns how many lanes
// it served in full.
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
		if serve.PlaysBack && lane.Name == types.DraftLane {
			fresh = append(fresh, lane.Text)
			continue
		}
		if resent && ss.root.Resent(serve.Instance, name, lane.Text) {
			held = append(held, name)
			pointed = append(pointed, name)
			continue
		}
		var served sdd.ServedPiece
		if lane.Authored {
			served = ss.root.ServeAuthoredText(name, lane.Text)
		} else {
			served = ss.root.ServeText(name, lane.Text, carried(pageFull, lane.Text))
		}
		if served.Held {
			held = append(held, name)
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
		text += fmt.Sprintf("\n\n(%s: sent in the result before, they serve as pointers so the lanes that waited fit, whatever this call rereads; once no lane waits, a reread has them in full)", strings.Join(pointed, ", "))
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

// composeFraming serves each framing block to the call's caller once per
// change of its content, with no proof asked (20261121-233256-d-tac-mm2): a
// graph write that changes one block (the recent-moves lane) re-serves that
// block alone, never the stable aspirations or directives (I6), and a caller
// the session has not served gets every block. A block is never cut for the
// entries it carries. Of the blocks due, the first keep serve; the rest
// wait, as instruction lanes do past the result bound.
func composeFraming(ss *shellSession, blocks []types.ServeLane, keep int) framingServed {
	var (
		result framingServed
		served []string
	)
	for _, block := range blocks {
		name := "framing " + block.Name
		if block.Text == "" || !ss.root.OncePerChangeDue(name, block.Text) {
			continue
		}
		result.blocks++
		if result.blocks > keep {
			// A block a resume owed in full that waits serves on the next
			// call, even where the caller was sent it before.
			ss.root.ForgetOncePerChange(name)
			result.waiting = append(result.waiting, block.Name)
			continue
		}
		ss.root.ServeOncePerChange(name, block.Text)
		served = append(served, block.Text)
	}
	result.fresh = len(served)
	result.text = strings.Join(served, "\n\n")
	return result
}

// framingServed is a composed framing: its text, the blocks that wait, how
// many blocks it served in full and how many it had to serve.
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
// the call holds it.
func serveReportSchema(ss *shellSession, serve *sdd.WorkflowServe) (map[string]any, error) {
	path, ok := fieldsAt(serve.ReportSchema)
	if !ok {
		return serve.ReportSchema, nil
	}
	fields := schemaAt(serve.ReportSchema, path)
	name := serve.Procedure + " report fields"
	held, err := ss.root.ServeSchema(name, fields["properties"].(map[string]any))
	if err != nil {
		return nil, err
	}
	if !held {
		return serve.ReportSchema, nil
	}
	served := maps.Clone(fields)
	delete(served, "properties")
	served["held_properties"] = fmt.Sprintf("you hold the %s, served to you before; list %q in reread to have them again, or resume_session re-serves them in full", name, name)
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
// full; while the call holds it, nothing.
func serveVocabulary(ss *shellSession, vocabulary string) (string, bool) {
	if vocabulary == "" {
		return "", false
	}
	piece := ss.root.ServeText("vocabulary", vocabulary, nil)
	if piece.Held {
		return "", false
	}
	return piece.Text, true
}
