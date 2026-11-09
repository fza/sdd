package model

import (
	"slices"
	"strings"
)

// Grounding defaults (20261109-004506-d-cpt-udz, D25, D27, D31;
// 20261109-222220-d-tac-4ev for the prefilter).
const (
	DefaultGroundingDepthDown     = 2
	DefaultGroundingDepthUp       = 1
	DefaultGroundingBriefAllBelow = 100
	DefaultGroundingPrefilterFrom = 30
	DefaultGroundingPrefilter     = true
)

// GroundingDepthConfig is the grounding depth: how far up and down a
// candidate's chain the reading scopes candidates.
type GroundingDepthConfig struct {
	Down *int `yaml:"down,omitempty"`
	Up   *int `yaml:"up,omitempty"`
}

// GroundingPolicy is the resolved grounding configuration of one project.
type GroundingPolicy struct {
	Enforce       bool
	DepthDown     int
	DepthUp       int
	BriefAllBelow int
	PrefilterFrom int
	Prefilter     bool
}

// GroundingPolicy resolves the block's grounding keys against the defaults;
// every mode but off enforces grounding.
func (c VerifyConfig) GroundingPolicy() GroundingPolicy {
	enforce, _ := c.ResolvedMode().Settings()
	return GroundingPolicy{
		Enforce:       enforce,
		DepthDown:     intOr(c.Depth.Down, DefaultGroundingDepthDown),
		DepthUp:       intOr(c.Depth.Up, DefaultGroundingDepthUp),
		BriefAllBelow: intOr(c.BriefAllBelow, DefaultGroundingBriefAllBelow),
		PrefilterFrom: intOr(c.PrefilterFrom, DefaultGroundingPrefilterFrom),
		Prefilter:     boolOr(c.Prefilter, DefaultGroundingPrefilter),
	}
}

func intOr(v *int, fallback int) int {
	if v == nil {
		return fallback
	}
	return *v
}

// GroundingExemptKind reports whether drafts of kind skip the grounding gate
// and entries of kind never become candidates: only records of who takes
// part and in which role cannot contradict, duplicate or outdate a claim
// (D38).
func GroundingExemptKind(kind Kind) bool {
	return kind == KindActor || kind == KindRole
}

// GroundingPopulation returns the live entries a reading judges, in graph
// order: the open population (superseded and closed entries excluded, D12)
// without the exempt kinds.
func GroundingPopulation(g *Graph) []*Entry {
	var live []*Entry
	for _, e := range g.Filter(GraphFilter{OpenOnly: true}) {
		if !GroundingExemptKind(e.Kind) {
			live = append(live, e)
		}
	}
	return live
}

// EntryBrief is an entry's brief: its title, else the first sentence of its
// summary. Never cut (D29).
func EntryBrief(e *Entry) string {
	brief := e.ListingTitle()
	if brief == "" {
		brief = strings.TrimSpace(e.Content)
		if i := strings.IndexAny(brief, "\n"); i >= 0 {
			brief = brief[:i]
		}
	}
	return brief
}

// TopicListing returns the live entries whose effective topics carry label
// as a component-wise prefix, the listing a reading runs per draft topic.
func TopicListing(g *Graph, live []*Entry, label TopicPath) []*Entry {
	if label.IsZero() {
		return nil
	}
	var listed []*Entry
	for _, e := range live {
		for _, topic := range g.EffectiveTopics(e) {
			if topic.HasPrefix(label) {
				listed = append(listed, e)
				break
			}
		}
	}
	return listed
}

// GatherCandidate is one entry a reading's reads listed, tagged by the reads
// that found it.
type GatherCandidate struct {
	Entry   *Entry
	FoundBy []string
	// Score is the prefilter's score, nil when it rated none.
	Score *float64
}

// GatherSetAside is a group of listed entries a reading does not ask to judge,
// with the reason.
type GatherSetAside struct {
	Reason string
	IDs    []string
}

// Set-aside reasons.
const (
	SetAsideAnswered = "answered in this capture (refs or verdicts)"
	SetAsideExempt   = "actor and role records, never candidates"
	SetAsideRetired  = "not live (closed, superseded or retired)"
	SetAsideJudged   = "judged not relevant before (relevance ledger)"
	SetAsideFiltered = "set aside by the prefilter (score below its calibrated threshold)"
	SetAsideCosine   = "set aside by the cosine stage (similarity below its calibrated floor)"
)

// GatherCandidates folds the reads into the candidates to judge: each live
// entry once, tagged by every read that found it, sorted by how many reads
// found it, then by ID. Opened entries, answered entries, exempt kinds and
// entries outside the live population are set aside with their reason.
func GatherCandidates(g *Graph, live []*Entry, reads []GroundingRead, opened, answered []string) ([]GatherCandidate, []GatherSetAside) {
	liveSet := make(map[string]bool, len(live))
	for _, e := range live {
		liveSet[e.ID] = true
	}
	skip := map[string]string{}
	for _, id := range answered {
		skip[id] = SetAsideAnswered
	}
	for _, id := range opened {
		skip[id] = ""
	}
	byID := map[string]*GatherCandidate{}
	var order []string
	setAside := map[string][]string{}
	for _, read := range reads {
		if read.Mode == GroundingOpened {
			continue
		}
		tag := readTag(read)
		for _, id := range read.IDs {
			if reason, skipped := skip[id]; skipped {
				if reason != "" && !slices.Contains(setAside[reason], id) {
					setAside[reason] = append(setAside[reason], id)
				}
				continue
			}
			entry := g.ByID[id]
			if entry == nil {
				continue
			}
			if !liveSet[id] {
				reason := SetAsideRetired
				if GroundingExemptKind(entry.Kind) {
					reason = SetAsideExempt
				}
				if !slices.Contains(setAside[reason], id) {
					setAside[reason] = append(setAside[reason], id)
				}
				continue
			}
			candidate, ok := byID[id]
			if !ok {
				candidate = &GatherCandidate{Entry: entry}
				byID[id] = candidate
				order = append(order, id)
			}
			if !slices.Contains(candidate.FoundBy, tag) {
				candidate.FoundBy = append(candidate.FoundBy, tag)
			}
		}
	}
	candidates := make([]GatherCandidate, 0, len(order))
	for _, id := range order {
		candidates = append(candidates, *byID[id])
	}
	slices.SortStableFunc(candidates, func(a, b GatherCandidate) int {
		if len(a.FoundBy) != len(b.FoundBy) {
			return len(b.FoundBy) - len(a.FoundBy)
		}
		return strings.Compare(a.Entry.ID, b.Entry.ID)
	})
	var groups []GatherSetAside
	for _, reason := range []string{SetAsideAnswered, SetAsideExempt, SetAsideRetired} {
		if ids := setAside[reason]; len(ids) > 0 {
			slices.Sort(ids)
			groups = append(groups, GatherSetAside{Reason: reason, IDs: ids})
		}
	}
	return candidates, groups
}

func readTag(read GroundingRead) string {
	switch read.Mode {
	case GroundingSemantic:
		return "semantic"
	case GroundingKeyword:
		return "keyword(" + read.Text + ")"
	case GroundingListing:
		return "topic(" + read.Text + ")"
	case GroundingBriefs:
		return "all live"
	case GroundingNew:
		return "new since the reading began"
	case GroundingLate:
		return "landed after the assemble check"
	}
	return string(read.Mode)
}

// Prefilter protocols (D23).
const (
	PrefilterSystemOne    = "systemone"
	PrefilterCohereRerank = "cohere-rerank"
	PrefilterGenerate     = "generate"
)

// LLMPrefilterConfig is the `llm_prefilter` block: the shape of `llm` plus
// the protocol that rates relevance. For systemone and cohere-rerank,
// Provider names the API key's entry in api_keys (default opper); for
// generate, Provider and Model select the chat model as in `llm`.
type LLMPrefilterConfig struct {
	Protocol  string `yaml:"protocol,omitempty"`
	LLMConfig `yaml:",inline"`
}

// DefaultPrefilterEffort is the effort a prefilter on the llm provider runs
// with: judging relevance needs no reasoning.
const DefaultPrefilterEffort = "low"

// ResolvedPrefilter is the prefilter block as it runs
// (20261109-222220-d-tac-4ev): without a protocol, generate; under generate,
// the llm block's provider, model and connection at DefaultPrefilterEffort,
// each value llm_prefilter sets winning. The other protocols take the block as
// set.
func (b BaseConfig) ResolvedPrefilter() LLMPrefilterConfig {
	out := b.LLMPrefilter
	if out.Protocol == "" {
		out.Protocol = PrefilterGenerate
	}
	if out.Protocol != PrefilterGenerate {
		return out
	}
	base := LLMConfig{
		Provider: b.LLM.Provider, Model: b.LLM.Model, Effort: DefaultPrefilterEffort,
		Endpoint: b.LLM.Endpoint, OllamaEndpoint: b.LLM.OllamaEndpoint,
		APIKeys: b.LLM.APIKeys, StructuredOutput: b.LLM.StructuredOutput,
	}
	out.LLMConfig = mergeLLMConfig(base, out.LLMConfig)
	return out
}

func mergeLLMPrefilterConfig(base, overlay LLMPrefilterConfig) LLMPrefilterConfig {
	out := LLMPrefilterConfig{Protocol: base.Protocol, LLMConfig: mergeLLMConfig(base.LLMConfig, overlay.LLMConfig)}
	if overlay.Protocol != "" {
		out.Protocol = overlay.Protocol
	}
	return out
}

func boolOr(v *bool, fallback bool) bool {
	if v == nil {
		return fallback
	}
	return *v
}
