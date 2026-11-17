package model_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func agentEntry(id string, refs ...model.Ref) *model.Entry {
	e := personEntry(id, refs...)
	e.DecidedBy = model.DecidedByAgent
	return e
}

func personEntry(id string, refs ...model.Ref) *model.Entry {
	parts, err := model.ParseID(id)
	if err != nil {
		panic(err)
	}
	return &model.Entry{ID: id, Type: model.TypeFromAbbrev[parts.TypeCode], Layer: model.LayerFromAbbrev[parts.LayerCode], Kind: model.KindGap, Refs: refs}
}

func ref(id string, kind model.RefKind) model.Ref { return model.Ref{ID: id, Kind: kind} }

const approvalID = "20260102-090000-d-tac-apr"

func approval(approves ...string) *model.Entry {
	return &model.Entry{ID: approvalID, Type: model.TypeDecision, Layer: model.LayerTactical, Kind: model.KindDirective, Intent: model.IntentSettled, Approves: approves}
}

func TestAgentMarks(t *testing.T) {
	const (
		a  = "20260101-100000-s-tac-aaa"
		b  = "20260101-100100-s-tac-bbb"
		c  = "20260101-100200-s-tac-ccc"
		d  = "20260101-100300-s-tac-ddd"
		ap = approvalID
	)
	cases := []struct {
		name      string
		entries   []*model.Entry
		approvals []*model.Entry
		want      map[string]model.AgentMark
	}{
		{
			name: "three-hop chain traces back to its origin",
			entries: []*model.Entry{
				agentEntry(a),
				agentEntry(b, ref(a, model.RefKindGroundedIn)),
				agentEntry(c, ref(b, model.RefKindBuildsOn)),
				agentEntry(d, ref(c, model.RefKindDependsOn)),
			},
			want: map[string]model.AgentMark{
				a: {Marked: true, Roots: []string{a}, Hops: 0},
				b: {Marked: true, Roots: []string{a}, Hops: 1},
				c: {Marked: true, Roots: []string{a}, Hops: 2},
				d: {Marked: true, Roots: []string{a}, Hops: 3},
			},
		},
		{
			name: "a person's entry mid-chain is never marked and cuts the chain",
			entries: []*model.Entry{
				agentEntry(a),
				personEntry(b, ref(a, model.RefKindGroundedIn)),
				agentEntry(c, ref(b, model.RefKindRefines)),
			},
			want: map[string]model.AgentMark{
				a: {Marked: true, Roots: []string{a}, Hops: 0},
				c: {Marked: true, Roots: []string{c}, Hops: 0},
			},
		},
		{
			name: "approving a chain's middle clears it and starts a new chain after it",
			entries: []*model.Entry{
				agentEntry(a),
				agentEntry(b, ref(a, model.RefKindGroundedIn)),
				agentEntry(c, ref(b, model.RefKindGroundedIn)),
				agentEntry(d, ref(c, model.RefKindGroundedIn)),
			},
			approvals: []*model.Entry{approval(b)},
			want: map[string]model.AgentMark{
				a: {Marked: true, Roots: []string{a}, Hops: 0},
				c: {Marked: true, Roots: []string{c}, Hops: 0},
				d: {Marked: true, Roots: []string{c}, Hops: 1},
			},
		},
		{
			name: "a superseded approval clears nothing",
			entries: []*model.Entry{
				agentEntry(a),
				agentEntry(b, ref(a, model.RefKindGroundedIn)),
			},
			approvals: []*model.Entry{
				approval(b),
				{ID: "20260102-090100-d-tac-sup", Type: model.TypeDecision, Layer: model.LayerTactical, Kind: model.KindDirective, Intent: model.IntentSettled, Supersedes: []string{ap}},
			},
			want: map[string]model.AgentMark{
				a: {Marked: true, Roots: []string{a}, Hops: 0},
				b: {Marked: true, Roots: []string{a}, Hops: 1},
			},
		},
		{
			name: "an agent's approval clears nothing",
			entries: []*model.Entry{
				agentEntry(a),
			},
			approvals: []*model.Entry{
				func() *model.Entry { e := approval(a); e.DecidedBy = model.DecidedByAgent; return e }(),
			},
			want: map[string]model.AgentMark{
				a:  {Marked: true, Roots: []string{a}, Hops: 0},
				ap: {Marked: true, Roots: []string{ap}, Hops: 0},
			},
		},
		{
			name: "diamond with one clean and one marked parent takes the marked one",
			entries: []*model.Entry{
				personEntry(a),
				agentEntry(b),
				agentEntry(c, ref(a, model.RefKindGroundedIn), ref(b, model.RefKindGroundedIn)),
			},
			want: map[string]model.AgentMark{
				b: {Marked: true, Roots: []string{b}, Hops: 0},
				c: {Marked: true, Roots: []string{b}, Hops: 1},
			},
		},
		{
			name: "a related ref passes no mark",
			entries: []*model.Entry{
				agentEntry(a),
				agentEntry(b, ref(a, model.RefKindRelated)),
			},
			want: map[string]model.AgentMark{
				a: {Marked: true, Roots: []string{a}, Hops: 0},
				b: {Marked: true, Roots: []string{b}, Hops: 0},
			},
		},
		{
			name: "two origins at the same distance are both roots",
			entries: []*model.Entry{
				agentEntry(a),
				agentEntry(b),
				agentEntry(c, ref(b, model.RefKindGroundedIn), ref(a, model.RefKindDependsOn)),
			},
			want: map[string]model.AgentMark{
				a: {Marked: true, Roots: []string{a}, Hops: 0},
				b: {Marked: true, Roots: []string{b}, Hops: 0},
				c: {Marked: true, Roots: []string{a, b}, Hops: 1},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := model.NewGraph(append(tc.entries, tc.approvals...)).AgentMarks()
			if len(got) != len(tc.want) {
				t.Errorf("marked %d entries, want %d: %v", len(got), len(tc.want), got)
			}
			for id, want := range tc.want {
				mark := got[id]
				if mark.Marked != want.Marked || mark.Hops != want.Hops || !slices.Equal(mark.Roots, want.Roots) {
					t.Errorf("%s: mark = %+v, want %+v", model.ShortID(id), mark, want)
				}
			}
		})
	}
}

func TestAgentLimit(t *testing.T) {
	const (
		person = "20260101-090000-d-tac-per"
		agent  = "20260101-090100-d-tac-agt"
	)
	prior := model.NewGraph([]*model.Entry{
		{ID: person, Type: model.TypeDecision, Layer: model.LayerTactical, Kind: model.KindPlan},
		{ID: agent, Type: model.TypeDecision, Layer: model.LayerTactical, Kind: model.KindPlan, DecidedBy: model.DecidedByAgent},
	})
	decision := func(layer model.Layer, kind model.Kind, intent model.Intent) *model.Entry {
		return &model.Entry{Type: model.TypeDecision, Layer: layer, Kind: kind, Intent: intent}
	}
	signal := func(kind model.Kind) *model.Entry {
		return &model.Entry{Type: model.TypeSignal, Layer: model.LayerStrategic, Kind: kind}
	}
	with := func(e *model.Entry, edit func(*model.Entry)) *model.Entry {
		edit(e)
		return e
	}
	cases := []struct {
		name  string
		entry *model.Entry
		rule  string // a fragment of the rule; empty when the agent may
	}{
		{"every signal kind, on any layer", signal(model.KindInsight), ""},
		{"a strategic gap", signal(model.KindGap), ""},
		{"a tactical plan", decision(model.LayerTactical, model.KindPlan, ""), ""},
		{"an operational activity", decision(model.LayerOperational, model.KindActivity, ""), ""},
		{"a tactical pending directive", decision(model.LayerTactical, model.KindDirective, model.IntentPending), ""},
		{"a strategic plan", decision(model.LayerStrategic, model.KindPlan, ""), "a strategic decision takes a person"},
		{"a conceptual pending directive", decision(model.LayerConceptual, model.KindDirective, model.IntentPending), "a conceptual decision"},
		{"a process activity", decision(model.LayerProcess, model.KindActivity, ""), "a process decision"},
		{"a guiding directive", decision(model.LayerTactical, model.KindDirective, model.IntentGuiding), "intent guiding"},
		{"a settled directive", decision(model.LayerOperational, model.KindDirective, model.IntentSettled), "intent settled"},
		{"a contract", decision(model.LayerTactical, model.KindContract, ""), "a decision of kind contract takes a person"},
		{"an aspiration", decision(model.LayerTactical, model.KindAspiration, ""), "an agent alone decides only plans"},
		{"a focus", decision(model.LayerOperational, model.KindFocus, ""), "kind focus takes"},
		{"superseding an agent's plan", with(decision(model.LayerTactical, model.KindPlan, ""), func(e *model.Entry) { e.Supersedes = []string{agent} }), ""},
		{"superseding a person's plan", with(decision(model.LayerTactical, model.KindPlan, ""), func(e *model.Entry) { e.Supersedes = []string{person} }), "superseding d-tac-per, which a person decided"},
		{"closing a person's plan", with(decision(model.LayerTactical, model.KindActivity, ""), func(e *model.Entry) { e.Closes = []string{person} }), "closing d-tac-per"},
		{"a gap closing a person's plan", with(signal(model.KindGap), func(e *model.Entry) { e.Closes = []string{person} }), "closing d-tac-per"},
		{"a done closing a person's plan", with(signal(model.KindDone), func(e *model.Entry) { e.Closes = []string{person} }), ""},
		{"a done superseding a person's entry", with(signal(model.KindDone), func(e *model.Entry) { e.Supersedes = []string{person} }), "superseding d-tac-per"},
		{"an approval", with(decision(model.LayerTactical, model.KindDirective, model.IntentSettled), func(e *model.Entry) { e.Approves = []string{agent} }), "an approval takes a person"},
		{"closing an entry outside the graph", with(signal(model.KindGap), func(e *model.Entry) { e.Closes = []string{"20250101-000000-s-tac-zzz"} }), "closing s-tac-zzz"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule := prior.AgentLimit(tc.entry)
			switch {
			case tc.rule == "" && rule != "":
				t.Errorf("an agent may write this alone, got rule %q", rule)
			case tc.rule != "" && !strings.Contains(rule, tc.rule):
				t.Errorf("rule = %q, want it to name %q", rule, tc.rule)
			case tc.rule != "" && !strings.HasPrefix(rule, "agent limit: "):
				t.Errorf("rule %q does not name the agent limit", rule)
			}
		})
	}
}

func TestClientActor(t *testing.T) {
	g := model.NewGraph([]*model.Entry{
		{ID: "20260101-090000-s-prc-cla", Type: model.TypeSignal, Layer: model.LayerProcess, Kind: model.KindActor, Canonical: "Claude", Aliases: []string{"Claude Code"}},
		{ID: "20260101-090100-s-prc-cdx", Type: model.TypeSignal, Layer: model.LayerProcess, Kind: model.KindActor, Canonical: "Codex"},
	})
	cases := map[string]string{
		"claude-code":      "Claude",
		"Claude":           "Claude",
		"codex-mcp-client": "Codex",
		"cursor-vscode":    "",
		"":                 "",
	}
	for client, want := range cases {
		if got := g.ClientActor(client); got != want {
			t.Errorf("ClientActor(%q) = %q, want %q", client, got, want)
		}
	}
}

func TestClientParticipant(t *testing.T) {
	g := model.NewGraph([]*model.Entry{
		{ID: "20260101-090000-s-prc-cla", Type: model.TypeSignal, Layer: model.LayerProcess, Kind: model.KindActor, Canonical: "Claude", Aliases: []string{"Claude Code"}},
	})
	cursor := &model.Entry{Type: model.TypeSignal, Layer: model.LayerProcess, Kind: model.KindActor, Canonical: "Cursor", Aliases: []string{"cursor-vscode"}}
	gap := &model.Entry{Type: model.TypeSignal, Layer: model.LayerTactical, Kind: model.KindGap}
	cases := []struct {
		name   string
		entry  *model.Entry
		client string
		want   string
	}{
		{name: "a known client", entry: gap, client: "claude-code", want: "Claude"},
		{name: "an unknown client", entry: gap, client: "cursor-vscode", want: ""},
		{name: "the actor naming the client", entry: cursor, client: "cursor-vscode", want: "Cursor"},
		{name: "an actor naming another client", entry: cursor, client: "zed", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := g.ClientParticipant(tc.entry, tc.client); got != tc.want {
				t.Errorf("ClientParticipant(%q) = %q, want %q", tc.client, got, tc.want)
			}
		})
	}
}

func TestAuthorshipMarkConstruction(t *testing.T) {
	g := model.NewGraph(nil)
	base := func(typ model.EntryType, kind model.Kind) *model.Entry {
		e := &model.Entry{ID: "20260101-100000-d-tac-new", Type: typ, Layer: model.LayerTactical, Kind: kind, Confidence: "high", Content: "Body."}
		if kind == model.KindDirective {
			e.Intent = model.IntentPending
		}
		return e
	}
	cases := []struct {
		name    string
		entry   func() *model.Entry
		finding string // the field a finding names; empty when none
	}{
		{"an agent's decision with its reason", func() *model.Entry {
			e := base(model.TypeDecision, model.KindDirective)
			e.DecidedBy, e.Reason = model.DecidedByAgent, "The tests need it."
			return e
		}, ""},
		{"an agent's decision without a reason", func() *model.Entry {
			e := base(model.TypeDecision, model.KindDirective)
			e.DecidedBy = model.DecidedByAgent
			return e
		}, "reason"},
		{"an agent's signal with a reason", func() *model.Entry {
			e := base(model.TypeSignal, model.KindGap)
			e.DecidedBy, e.Reason = model.DecidedByAgent, "Why."
			return e
		}, "reason"},
		{"a person's decision with a reason", func() *model.Entry {
			e := base(model.TypeDecision, model.KindDirective)
			e.Reason = "Why."
			return e
		}, "reason"},
		{"an unknown decided-by", func() *model.Entry {
			e := base(model.TypeSignal, model.KindGap)
			e.DecidedBy = "person"
			return e
		}, "decided-by"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			construction, findings := model.ConstructFromEntry(tc.entry())
			_, writeFindings := construction.ValidateForWrite(g)
			findings = append(findings, writeFindings...)
			var fields []string
			for _, f := range findings {
				fields = append(fields, f.Field)
			}
			if tc.finding == "" && len(fields) > 0 {
				t.Errorf("findings on %v, want none: %v", fields, findings)
			}
			if tc.finding != "" && !slices.Contains(fields, tc.finding) {
				t.Errorf("findings on %v, want one on %s", fields, tc.finding)
			}
		})
	}
}

func TestAuthorshipMarkRoundTrip(t *testing.T) {
	e := &model.Entry{
		ID: "20260101-100000-d-tac-new", Type: model.TypeDecision, Layer: model.LayerTactical, Kind: model.KindPlan,
		Participants: []string{"Claude"}, DecidedBy: model.DecidedByAgent, RunBy: "Ada", Reason: "The build needs it.", Content: "Body.",
	}
	text := model.FormatFrontmatter(e) + "\n" + e.Content + "\n"
	for _, line := range []string{"decided-by: agent", "run-by: Ada", "reason: The build needs it."} {
		if !strings.Contains(text, line) {
			t.Errorf("frontmatter lacks %q:\n%s", line, text)
		}
	}
	parsed, err := model.ParseEntry("20260101-100000-d-tac-new.md", text)
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.AgentDecided() || parsed.RunBy != "Ada" || parsed.Reason != "The build needs it." {
		t.Errorf("parsed mark = %q, %q, %q", parsed.DecidedBy, parsed.RunBy, parsed.Reason)
	}
	construction, _ := model.ConstructFromEntry(parsed)
	if back := construction.Entry(); back.DecidedBy != e.DecidedBy || back.RunBy != e.RunBy || back.Reason != e.Reason {
		t.Errorf("construction drops the mark: %q, %q, %q", back.DecidedBy, back.RunBy, back.Reason)
	}
	person, err := model.ParseEntry("20260101-100000-d-tac-old.md", "---\ntype: decision\nlayer: tac\nkind: plan\n---\nBody.\n")
	if err != nil {
		t.Fatal(err)
	}
	if person.AgentDecided() {
		t.Error("an entry without decided-by reads as an agent's")
	}
}

func TestApprovalValidation(t *testing.T) {
	const (
		person = "20260101-090000-d-tac-per"
		agent  = "20260101-090100-d-tac-agt"
	)
	g := model.NewGraph([]*model.Entry{
		{ID: person, Type: model.TypeDecision, Layer: model.LayerTactical, Kind: model.KindPlan},
		{ID: agent, Type: model.TypeDecision, Layer: model.LayerTactical, Kind: model.KindPlan, DecidedBy: model.DecidedByAgent, Reason: "Why."},
	})
	cases := []struct {
		name    string
		edit    func(*model.Entry)
		finding string // a fragment of the finding on approves; empty when none
	}{
		{"a person approving an agent's entry", func(*model.Entry) {}, ""},
		{"a pending directive approving", func(e *model.Entry) { e.Intent = model.IntentPending }, "only valid on a settled directive"},
		{"a plan approving", func(e *model.Entry) { e.Kind, e.Intent = model.KindPlan, "" }, "only valid on a settled directive"},
		{"an agent approving", func(e *model.Entry) { e.DecidedBy, e.Reason = model.DecidedByAgent, "Why." }, "an agent does not approve"},
		{"approving a person's entry", func(e *model.Entry) { e.Approves = []string{person} }, "which a person decided"},
		{"approving an entry outside the graph", func(e *model.Entry) { e.Approves = []string{"20250101-000000-d-tac-zzz"} }, "dangling ref in approves"},
		{"approving across repos", func(e *model.Entry) { e.Approves = []string{"github.com/x/y:20250101-000000-d-tac-zzz"} }, "cross-repo ID in approves"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := approval(agent)
			e.Confidence, e.Content = "high", "Body."
			tc.edit(e)
			construction, findings := model.ConstructFromEntry(e)
			_, writeFindings := construction.ValidateForWrite(g)
			var messages []string
			for _, f := range append(findings, writeFindings...) {
				if f.Field == "approves" {
					messages = append(messages, f.Message)
				}
			}
			joined := strings.Join(messages, "; ")
			if tc.finding == "" && joined != "" {
				t.Errorf("findings on approves, want none: %s", joined)
			}
			if tc.finding != "" && !strings.Contains(joined, tc.finding) {
				t.Errorf("findings on approves %q, want one naming %q", joined, tc.finding)
			}
		})
	}
	text := model.FormatFrontmatter(approval(agent))
	if !strings.Contains(text, "approves:\n    - "+agent) {
		t.Errorf("frontmatter lacks the approves list:\n%s", text)
	}
	parsed, err := model.ParseEntry("20260102-090000-d-tac-apr.md", text+"\nBody.\n")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(parsed.Approves, []string{agent}) {
		t.Errorf("parsed approves = %v", parsed.Approves)
	}
}

func TestAgentChains(t *testing.T) {
	const (
		a = "20260101-100000-s-tac-aaa"
		b = "20260101-100100-s-tac-bbb"
		c = "20260101-100200-s-tac-ccc"
		d = "20260101-100300-s-tac-ddd"
		e = "20260101-100400-s-tac-eee"
	)
	g := model.NewGraph([]*model.Entry{
		agentEntry(a),
		agentEntry(b, ref(a, model.RefKindGroundedIn)),
		agentEntry(c, ref(b, model.RefKindBuildsOn)),
		agentEntry(d),
		personEntry(e),
		{ID: "20260101-100500-s-tac-cls", Type: model.TypeSignal, Layer: model.LayerTactical, Kind: model.KindDone, Closes: []string{d}},
	})
	chains := g.AgentChains()
	if len(chains) != 1 || chains[0].Root.ID != a {
		t.Fatalf("one live chain rooted at a should await review, the dismissed d none, got %v", chains)
	}
	var resting []string
	for _, entry := range chains[0].Resting {
		resting = append(resting, entry.ID)
	}
	if !slices.Equal(resting, []string{b, c}) {
		t.Errorf("the chain should list the entries resting on its root, oldest first, got %v", resting)
	}
	if mark := g.AgentMark(c); !mark.Marked || mark.Hops != 2 || !slices.Equal(mark.Roots, []string{a}) {
		t.Errorf("AgentMark should read the derived mark, got %+v", mark)
	}
	if mark := g.AgentMark(e); mark.Marked {
		t.Errorf("a person's entry should carry no mark, got %+v", mark)
	}
}
