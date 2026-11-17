package engine_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/engine"
	"github.com/networkteam/sdd/internal/model"
)

// A choice the person answers goes through the question tool the session
// names, in the goal and in the unit text through questionTool; a choice the
// agent takes names none.
func TestPersonsChoiceGoesThroughTheQuestionTool(t *testing.T) {
	const tool = "the AskUserQuestion tool"
	t.Run("guided", func(t *testing.T) {
		_, sv := dialogueSession(t, nopSink{}, engine.WithQuestionTool(tool))
		if !strings.Contains(sv.Goal, "through "+tool) {
			t.Errorf("a user chooser's goal should name the question tool, got %q", sv.Goal)
		}
	})
	t.Run("assisted gate", func(t *testing.T) {
		session, sv := assistedSession(t, nopSink{}, assisted(), engine.WithQuestionTool(tool))
		if strings.Contains(sv.Goal, "through") {
			t.Errorf("a choice the agent takes should name no question tool, got %q", sv.Goal)
		}
		sv = toGate(t, session, sv.Instance)
		if !strings.Contains(sv.Goal, "through "+tool) {
			t.Errorf("the approval gate's goal should name the question tool, got %q", sv.Goal)
		}
	})
	t.Run("unnamed", func(t *testing.T) {
		_, sv := dialogueSession(t, nopSink{})
		if !strings.Contains(sv.Goal, "through "+model.QuestionTool("")) {
			t.Errorf("a session naming no tool should say so neutrally, got %q", sv.Goal)
		}
	})
	t.Run("unit text", func(t *testing.T) {
		spec := mustParseLanes(t, dialogueMachine, "## unit: pick\n\nAsk through {{.questionTool}}.\n\n## unit: keep\n\nAsk.\n")
		eng := engine.New(engine.NewRegistry(), engine.StaticGraphs{Graph: model.NewGraph(nil)})
		sv, err := eng.NewSession("s_tool", "tester", nopSink{}, engine.WithQuestionTool(tool)).Start(spec, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		if sv.UnitText != "Ask through "+tool+"." {
			t.Errorf("questionTool should render the session's tool, got %q", sv.UnitText)
		}
	})
}
