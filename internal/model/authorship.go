package model

import (
	"fmt"
	"slices"
	"strings"
	"sync"
)

type agentMarkCache struct {
	once  sync.Once
	marks map[string]AgentMark
}

// AgentMark is the chain mark of the entry id (see AgentMarks), derived once
// per graph.
func (g *Graph) AgentMark(id string) AgentMark {
	if g.marks == nil {
		return g.AgentMarks()[id]
	}
	g.marks.once.Do(func() { g.marks.marks = g.AgentMarks() })
	return g.marks.marks[id]
}

// AgentChain is a chain of agent reasoning awaiting a person's review: its
// root, and the marked entries resting on it, oldest first.
type AgentChain struct {
	Root    *Entry
	Resting []*Entry
}

// AgentChains lists the chains whose root is live, neither closed nor
// superseded, newest root first.
func (g *Graph) AgentChains() []AgentChain {
	byRoot := map[string]*AgentChain{}
	var chains []*AgentChain
	for _, e := range g.Entries {
		for _, root := range g.AgentMark(e.ID).Roots {
			chain := byRoot[root]
			if chain == nil {
				r, ok := g.ByID[root]
				if !ok || len(g.ClosedBy[root]) > 0 || len(g.SupersededBy[root]) > 0 {
					continue
				}
				chain = &AgentChain{Root: r}
				byRoot[root] = chain
				chains = append(chains, chain)
			}
			if root != e.ID {
				chain.Resting = append(chain.Resting, e)
			}
		}
	}
	out := make([]AgentChain, 0, len(chains))
	for _, chain := range chains {
		slices.SortFunc(chain.Resting, func(a, b *Entry) int { return a.Time.Compare(b.Time) })
		out = append(out, *chain)
	}
	slices.SortFunc(out, func(a, b AgentChain) int { return b.Root.Time.Compare(a.Root.Time) })
	return out
}

// DecidedBy names who decided an entry. sdd writes DecidedByAgent on every
// entry an autonomous capture writes, with the agent's own reason on a
// decision (Entry.Reason) and the participant sdd ran as (Entry.RunBy). An
// entry without it reads as decided by a person.
type DecidedBy string

const DecidedByAgent DecidedBy = "agent"

// AgentDecided reports whether an agent decided e alone; an entry a person
// approved never reads as that, whatever else it carries.
func (e *Entry) AgentDecided() bool {
	return e.DecidedBy == DecidedByAgent && !e.Approved()
}

// DraftedByAgent is the reason-by and intent-by value of what the agent drafted.
const DraftedByAgent = "agent"

// Approved reports whether a person approved e at an assisted dialogue's
// final gate: the agent drafted it, and it is the person's. ApprovedBy names
// them; ReasonBy and IntentBy name who stated its reason and a directive's
// intent, the person or DraftedByAgent when they gave none.
func (e *Entry) Approved() bool {
	return e.ApprovedBy != ""
}

// ApprovalRule states what an approval is.
const ApprovalRule = "approves is only valid on a settled directive: an approval is a person's settled directive listing the agent-decided entries it vouches for"

// validateApproves checks that an approval vouches only for entries an agent
// decided.
func validateApproves(e *Entry, g *Graph) {
	for _, id := range e.Approves {
		if target, ok := g.ByID[id]; ok && !target.AgentDecided() {
			e.Warnings = append(e.Warnings, Warning{Field: "approves", Value: id, Message: fmt.Sprintf("approves %s, which a person decided: only an agent-decided entry is approved", id)})
		}
	}
}

// approvedEntries is the set of entries a live approval covers: a person's
// settled directive no later entry superseded.
func (g *Graph) approvedEntries() map[string]bool {
	approved := map[string]bool{}
	for _, e := range g.Entries {
		if len(e.Approves) == 0 || !e.IsSettled() || e.AgentDecided() || len(g.SupersededBy[e.ID]) > 0 {
			continue
		}
		for _, id := range e.Approves {
			approved[id] = true
		}
	}
	return approved
}

// AgentLimit returns the rule that keeps an agent from writing e alone, or ""
// when an agent may. An agent alone writes every signal kind, and plans,
// activities and pending directives on the tactical and operational layers;
// it never approves, and never supersedes or closes an entry a person
// decided, except that a done may close anything. g resolves the closed and
// superseded entries.
func (g *Graph) AgentLimit(e *Entry) string {
	if len(e.Approves) > 0 {
		return "agent limit: an approval takes a person"
	}
	if e.Type == TypeDecision {
		switch {
		case e.Layer != LayerTactical && e.Layer != LayerOperational:
			return fmt.Sprintf("agent limit: a %s decision takes a person — an agent alone decides only on the tactical and operational layers", e.Layer)
		case e.Kind == KindDirective && e.Intent != IntentPending:
			return fmt.Sprintf("agent limit: a directive with intent %s takes a person — an agent alone sets only pending directives", intentOrUnset(e.Intent))
		case e.Kind != KindDirective && e.Kind != KindPlan && e.Kind != KindActivity:
			return fmt.Sprintf("agent limit: a decision of kind %s takes a person — an agent alone decides only plans, activities and pending directives", e.Kind)
		}
	}
	for _, id := range e.Supersedes {
		if !g.agentDecided(id) {
			return fmt.Sprintf("agent limit: superseding %s, which a person decided, takes a person", ShortID(id))
		}
	}
	if e.Type == TypeSignal && e.Kind == KindDone {
		return ""
	}
	for _, id := range e.Closes {
		if !g.agentDecided(id) {
			return fmt.Sprintf("agent limit: closing %s, which a person decided, takes a person", ShortID(id))
		}
	}
	return ""
}

func intentOrUnset(intent Intent) string {
	if intent == "" {
		return "unset"
	}
	return string(intent)
}

func (g *Graph) agentDecided(id string) bool {
	if g == nil {
		return false
	}
	target, ok := g.ByID[id]
	return ok && target.AgentDecided()
}

// AgentMark is an entry's chain mark: whether it rests on agent reasoning no
// person approved, the nearest chain origins that reasoning starts at, and
// how many steps away they are. A marked entry with no marked basis is its
// own root, zero steps away.
type AgentMark struct {
	Marked bool
	Roots  []string
	Hops   int
}

// chainRefKinds are the ref kinds a mark passes along: those that build an
// entry on its target.
var chainRefKinds = []RefKind{RefKindGroundedIn, RefKindBuildsOn, RefKindRefines, RefKindDependsOn}

// AgentMarks derives the chain mark of every marked entry, keyed by ID. An
// agent-decided entry no live approval covers is marked; the mark passes to
// the marked entries built on it (chainRefKinds), so their roots trace back
// to where the agent reasoning starts. A person's entry is never marked and
// passes nothing; neither does an approved one.
func (g *Graph) AgentMarks() map[string]AgentMark {
	approved := g.approvedEntries()
	marks := map[string]AgentMark{}
	seen := map[string]bool{}
	var visit func(e *Entry) AgentMark
	visit = func(e *Entry) AgentMark {
		if seen[e.ID] {
			return marks[e.ID]
		}
		seen[e.ID] = true
		if !e.AgentDecided() || approved[e.ID] {
			return AgentMark{}
		}
		mark := AgentMark{Marked: true, Hops: -1}
		for _, ref := range e.Refs {
			parent, ok := g.ByID[ref.ID]
			if !ok || !slices.Contains(chainRefKinds, ref.Kind) {
				continue
			}
			up := visit(parent)
			switch {
			case !up.Marked:
			case mark.Hops < 0 || up.Hops+1 < mark.Hops:
				mark.Hops = up.Hops + 1
				mark.Roots = append([]string(nil), up.Roots...)
			case up.Hops+1 == mark.Hops:
				mark.Roots = append(mark.Roots, up.Roots...)
			}
		}
		if mark.Hops < 0 {
			mark.Hops = 0
			mark.Roots = []string{e.ID}
		}
		slices.Sort(mark.Roots)
		mark.Roots = slices.Compact(mark.Roots)
		marks[e.ID] = mark
		return mark
	}
	for _, e := range g.Entries {
		visit(e)
	}
	return marks
}

// ClientActor names the actor an MCP client stands for: the canonical of the
// active actor whose canonical or alias matches the client's name, ignoring
// case, separators and an "mcp client" suffix (claude-code matches the alias
// Claude Code), else the client name itself.
func (g *Graph) ClientActor(client string) string {
	want := clientKey(client)
	if want == "" {
		return ""
	}
	for _, actor := range g.ActiveActorHeads() {
		if clientKey(actor.Canonical) == want || slices.ContainsFunc(actor.Aliases, func(alias string) bool { return clientKey(alias) == want }) {
			return actor.Canonical
		}
	}
	return strings.TrimSpace(client)
}

func clientKey(name string) string {
	key := strings.Join(strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return r == '-' || r == '_' || r == ' ' || r == '.'
	}), " ")
	return strings.TrimSuffix(key, " mcp client")
}
