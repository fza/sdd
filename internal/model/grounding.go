package model

import (
	"reflect"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Grounding defaults.
const (
	DefaultGroundingDepthDown     = 2
	DefaultGroundingDepthUp       = 1
	DefaultGroundingPrefilterFrom = 30
	DefaultGroundingPrefilter     = true
	DefaultGroundingAutoCalibrate = true
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
	PrefilterFrom int
	Prefilter     bool
	AutoCalibrate bool
	// JudgeTimeout and JudgeMinimum bound the ranking judge of a graph check.
	JudgeTimeout time.Duration
	JudgeMinimum int
}

// GroundingPolicy resolves the block's grounding keys against the defaults;
// every mode but off enforces grounding.
func (c VerifyConfig) GroundingPolicy() GroundingPolicy {
	enforce, _ := c.ResolvedMode().Settings()
	return GroundingPolicy{
		Enforce:       enforce,
		DepthDown:     intOr(c.Depth.Down, DefaultGroundingDepthDown),
		DepthUp:       intOr(c.Depth.Up, DefaultGroundingDepthUp),
		PrefilterFrom: intOr(c.PrefilterFrom, DefaultGroundingPrefilterFrom),
		Prefilter:     boolOr(c.Prefilter, DefaultGroundingPrefilter),
		AutoCalibrate: boolOr(c.AutoCalibrate, DefaultGroundingAutoCalibrate),
		JudgeTimeout:  c.ResolvedJudgeTimeout(),
		JudgeMinimum:  c.ResolvedJudgeMinimum(),
	}
}

func intOr(v *int, fallback int) int {
	if v == nil {
		return fallback
	}
	return *v
}

// GroundingExemptKind reports whether drafts of kind skip the grounding gate
// and entries of kind never become candidates: only records of who takes part
// and in which role cannot contradict, duplicate or outdate a claim.
func GroundingExemptKind(kind Kind) bool {
	return kind == KindActor || kind == KindRole
}

// GroundingPopulation returns the live entries a reading judges, in graph
// order: the open population (superseded and closed entries excluded) without
// the exempt kinds.
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
// summary. Never cut.
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
	SetAsideFloor    = "set aside by the far floor (rated no higher than entries far from the draft in the same call)"
)

// GatherCandidates folds the reads into the candidates to judge: each live
// entry once, tagged by every read that found it, in the order the reads
// first found them (OrderGatherCandidates places them). Opened entries,
// answered entries, exempt kinds and entries outside the live population
// are set aside with their reason.
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
		if read.Text == "" {
			return "semantic(draft text)"
		}
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

// PrefilterProtocol is the protocol that rates relevance.
type PrefilterProtocol string

const (
	PrefilterSystemOne PrefilterProtocol = "systemone"
	PrefilterCohere    PrefilterProtocol = "cohere"
	// PrefilterChat has a chat model score a batch of candidates against the
	// draft per call, halving the batch where an answer breaks.
	PrefilterChat PrefilterProtocol = "chat"
)

func (PrefilterProtocol) AllowedValues() []string {
	return closedValues(PrefilterSystemOne, PrefilterCohere, PrefilterChat)
}

// legacyProtocols are the protocol names a former sdd wrote, by the one each
// reads as now.
var legacyProtocols = map[string]PrefilterProtocol{
	"cohere-rerank": PrefilterCohere,
	"generate":      PrefilterChat,
	"listwise":      PrefilterChat,
}

// UnmarshalYAML reads a legacy protocol name as the protocol it became.
func (p *PrefilterProtocol) UnmarshalYAML(node *yaml.Node) error {
	var value string
	if err := node.Decode(&value); err != nil {
		return err
	}
	*p = PrefilterProtocol(value)
	if current, ok := legacyProtocols[value]; ok {
		*p = current
	}
	return nil
}

// CanonicalJudge is the name a stored judge reads as: a judge named by a
// legacy protocol is the same model under the protocol it became, so its
// scores and drift steps keep counting.
func CanonicalJudge(judge string) string {
	protocol, model, ok := strings.Cut(judge, ":")
	if current, legacy := legacyProtocols[protocol]; ok && legacy {
		return string(current) + ":" + model
	}
	return judge
}

// The ranking judge on a Claude host while llm_prefilter is unset: Haiku
// through the claude CLI, at the lowest effort it takes, since its thinking
// cannot be switched off.
const (
	DefaultRankingModel  = "claude-haiku-5-5"
	DefaultRankingEffort = "low"
)

// ClaudeHostRanking is the prefilter block both readings rank with on a
// Claude host (ClaudeClient), on the llm block's claude CLI credentials.
// False once llm_prefilter sets any value: then the configured prefilter
// ranks, whatever the host.
func (b BaseConfig) ClaudeHostRanking() (LLMPrefilterConfig, bool) {
	if !reflect.ValueOf(b.LLMPrefilter).IsZero() {
		return LLMPrefilterConfig{}, false
	}
	return LLMPrefilterConfig{Protocol: PrefilterChat, LLMConfig: LLMConfig{
		Provider: DefaultLLMProvider, Model: DefaultRankingModel, Effort: DefaultRankingEffort, ClaudeCLI: b.LLM.ClaudeCLI,
	}}, true
}

// LLMPrefilterConfig is the `llm_prefilter` block: the shape of `llm` plus
// the protocol that rates relevance. For systemone and cohere, Provider
// names the API key's entry in api_keys (default opper); for chat, Provider
// and Model select the chat model as in `llm`.
type LLMPrefilterConfig struct {
	Protocol  PrefilterProtocol `yaml:"protocol,omitempty"`
	LLMConfig `yaml:",inline"`
}

// DefaultPrefilterEffort is the effort a prefilter on the llm provider runs
// with: judging relevance needs no reasoning.
const DefaultPrefilterEffort = "low"

// ResolvedPrefilter is the prefilter block as it runs: without a protocol,
// chat; under chat, the llm block's provider, model and connection at
// DefaultPrefilterEffort, each value llm_prefilter sets winning. The other
// protocols take the block as set.
func (b BaseConfig) ResolvedPrefilter() LLMPrefilterConfig {
	out := b.LLMPrefilter
	if out.Protocol == "" {
		out.Protocol = PrefilterChat
	}
	if out.Protocol != PrefilterChat {
		return out
	}
	base := LLMConfig{
		Provider: b.LLM.Provider, Model: b.LLM.Model, Effort: DefaultPrefilterEffort,
		Endpoint: b.LLM.Endpoint, OllamaEndpoint: b.LLM.OllamaEndpoint,
		APIKeys: b.LLM.APIKeys, StructuredOutput: b.LLM.StructuredOutput,
		ClaudeCLI: b.LLM.ClaudeCLI,
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
