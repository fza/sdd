package model

import (
	"fmt"
	"slices"
	"strings"
)

// Grounding defaults.
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

// EntryBrief is an entry's brief: the first line of its summary, else of its
// body. Never cut.
func EntryBrief(e *Entry) string {
	brief := strings.TrimSpace(e.Summary)
	if i := strings.IndexAny(brief, "\n"); i >= 0 {
		brief = brief[:i]
	}
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

// LLMPrefilterConfig is the `llm_prefilter` block: the relevance judge, with
// the keys of `llm` that apply to it. Its provider is a field of its own,
// since it takes the dedicated judges beside the chat providers.
type LLMPrefilterConfig struct {
	// Provider picks the judge: a chat provider runs the generate judge on
	// its model; a dedicated provider rates over its own protocol.
	Provider       PrefilterProvider `yaml:"provider,omitempty"`
	Model          string            `yaml:"model,omitempty"`
	Timeout        string            `yaml:"timeout,omitempty"`
	Params         map[string]string `yaml:"params,omitempty"`
	Concurrency    int               `yaml:"concurrency,omitempty"`
	Endpoint       string            `yaml:"endpoint,omitempty"`
	OllamaEndpoint string            `yaml:"ollama_endpoint,omitempty"`
	APIKeys        map[string]string `yaml:"api_keys,omitempty" sdd:"secret"`
	RateLimitRPS   float64           `yaml:"rate_limit_rps,omitempty"`
	Retry          RetryConfig       `yaml:"retry,omitempty"`
}

// PrefilterProvider is an llm_prefilter.provider value.
type PrefilterProvider string

// The dedicated judges, each named after its wire protocol.
const (
	PrefilterSystemOne PrefilterProvider = "systemone"
	PrefilterCohere    PrefilterProvider = "cohere"
)

// AllowedValues lists the chat providers, then the dedicated judges.
func (PrefilterProvider) AllowedValues() []string {
	return append(LLMProviders(), string(PrefilterSystemOne), string(PrefilterCohere))
}

// Dedicated reports a judge that rates over its own protocol, not a chat
// model.
func (p PrefilterProvider) Dedicated() bool {
	return p == PrefilterSystemOne || p == PrefilterCohere
}

// CheckPrefilterProvider refuses an llm_prefilter.provider outside its
// allowed values; unset passes.
func CheckPrefilterProvider(provider PrefilterProvider) error {
	if provider == "" {
		return nil
	}
	return checkClosedValue("llm_prefilter.provider", string(provider), provider.AllowedValues())
}

// CheckLLMProviderIsNoJudge refuses an llm.provider naming a dedicated
// judge: only llm_prefilter.provider names one.
func CheckLLMProviderIsNoJudge(provider string) error {
	if !PrefilterProvider(provider).Dedicated() {
		return nil
	}
	return fmt.Errorf("llm.provider %s is a prefilter judge, not a chat model: set it as llm_prefilter.provider", provider)
}

// Chat is the block as the llm settings the generate judge's chat model runs
// on.
func (c LLMPrefilterConfig) Chat() LLMConfig {
	return LLMConfig{
		Provider: string(c.Provider), Model: c.Model, Timeout: c.Timeout, Params: c.Params,
		Concurrency: c.Concurrency, Endpoint: c.Endpoint, OllamaEndpoint: c.OllamaEndpoint,
		APIKeys: c.APIKeys, RateLimitRPS: c.RateLimitRPS, Retry: c.Retry,
	}
}

func prefilterFromChat(c LLMConfig) LLMPrefilterConfig {
	return LLMPrefilterConfig{
		Provider: PrefilterProvider(c.Provider), Model: c.Model, Timeout: c.Timeout, Params: c.Params,
		Concurrency: c.Concurrency, Endpoint: c.Endpoint, OllamaEndpoint: c.OllamaEndpoint,
		APIKeys: c.APIKeys, RateLimitRPS: c.RateLimitRPS, Retry: c.Retry,
	}
}

// ResolvedPrefilter is the prefilter block as it runs. A dedicated provider
// takes the block as set; otherwise the llm block's provider, model and
// connection, each value llm_prefilter sets winning. A dedicated provider
// reached through llm.provider is refused.
func (b BaseConfig) ResolvedPrefilter() (LLMPrefilterConfig, error) {
	if b.LLMPrefilter.Provider.Dedicated() {
		return b.LLMPrefilter, nil
	}
	base := LLMConfig{
		Provider: b.LLM.Provider, Model: b.LLM.Model,
		Endpoint: b.LLM.Endpoint, OllamaEndpoint: b.LLM.OllamaEndpoint,
		APIKeys: b.LLM.APIKeys,
	}
	resolved := prefilterFromChat(mergeLLMConfig(base, b.LLMPrefilter.Chat()))
	err := CheckLLMProviderIsNoJudge(string(resolved.Provider))
	if err != nil {
		return LLMPrefilterConfig{}, err
	}
	return resolved, nil
}

func mergeLLMPrefilterConfig(base, overlay LLMPrefilterConfig) LLMPrefilterConfig {
	return prefilterFromChat(mergeLLMConfig(base.Chat(), overlay.Chat()))
}

func boolOr(v *bool, fallback bool) bool {
	if v == nil {
		return fallback
	}
	return *v
}
