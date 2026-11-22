package cliapp

import (
	"context"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/networkteam/sdd/internal/llm/factory"
	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"
	pkgllm "github.com/networkteam/sdd/pkg/llm"
	pkgrelevance "github.com/networkteam/sdd/pkg/llm/relevance"
	localadapter "github.com/networkteam/sdd/pkg/local"
)

// relevanceLedgerDir is the folder of a project's relevance verdict ledger:
// machine state under stateRoot, keyed like the search index, and nowhere in
// the repository.
func relevanceLedgerDir(stateRoot, repoKey string) string {
	return filepath.Join(stateRoot, model.RelevanceDirName, filepath.FromSlash(repoKey))
}

const leftoverRelevanceNotice = "ignoring relevance segments in the repository: the ledger lives only in machine state"

func leftoverRelevanceAdvice(ledgerDir string) string {
	return "move them into " + ledgerDir + ", then commit their removal from the repository"
}

// projectRelevanceStore opens the project's relevance verdict ledger and
// stamps the project as used.
func projectRelevanceStore(stateRoot, repoKey string) (usedRelevanceStore, error) {
	store, err := localadapter.NewFilesystemRelevanceStore(relevanceLedgerDir(stateRoot, repoKey))
	if err != nil {
		return usedRelevanceStore{}, err
	}
	used := usedRelevanceStore{FilesystemRelevanceStore: store, stateRoot: stateRoot, repoKey: repoKey, now: time.Now}
	if err := used.mark(); err != nil {
		return usedRelevanceStore{}, err
	}
	return used, nil
}

// usedRelevanceStore stamps its project as used on every read and write, so
// the daily sweep keeps a project a long-running server works in.
type usedRelevanceStore struct {
	*localadapter.FilesystemRelevanceStore
	stateRoot, repoKey string
	now                func() time.Time
}

func (s usedRelevanceStore) mark() error {
	return localadapter.MarkProjectUsed(s.stateRoot, s.repoKey, s.now())
}

func (s usedRelevanceStore) Append(ctx context.Context, segment []byte) error {
	if err := s.mark(); err != nil {
		return err
	}
	return s.FilesystemRelevanceStore.Append(ctx, segment)
}

func (s usedRelevanceStore) Segments(ctx context.Context) ([][]byte, error) {
	if err := s.mark(); err != nil {
		return nil, err
	}
	return s.FilesystemRelevanceStore.Segments(ctx)
}

// chatBatchSizes opens the batch sizes chat judges start at and the sizes
// that broke: machine state beside the relevance ledgers, one file each for
// every project, since a size is the model's.
func chatBatchSizes(stateRoot string) (*localadapter.FilesystemBatchSizes, error) {
	dir := filepath.Join(stateRoot, model.RelevanceDirName)
	return localadapter.NewFilesystemBatchSizes(filepath.Join(dir, "chat-batch-sizes.json"), filepath.Join(dir, "chat-batch-failures.json"))
}

// projectPrefilter composes the prefilter verify.prefilter switches on;
// the zero value while it is off. Its calls are recorded into sink.
func projectPrefilter(cfg *model.PerRepoConfig, sink pkgllm.StatsSink, sizes pkgrelevance.BatchSizes) (sdd.Prefilter, error) {
	var base model.BaseConfig
	if cfg != nil {
		base = cfg.BaseConfig
	}
	if !base.Verify.GroundingPolicy().Prefilter {
		return sdd.Prefilter{}, nil
	}
	prefilter, err := factory.NewPrefilter(base.ResolvedPrefilter(), sink, sizes)
	if err != nil {
		return sdd.Prefilter{}, err
	}
	return appPrefilter(prefilter), nil
}

// projectRanking composes the ranking both readings share: the Claude host's
// judge while verify.prefilter is on and llm_prefilter is unset
// (model.BaseConfig.ClaudeHostRanking), whether its claude CLI is on PATH,
// and verify.ranking_timeout. Its calls are recorded into sink.
func projectRanking(cfg *model.PerRepoConfig, sink pkgllm.StatsSink, sizes pkgrelevance.BatchSizes) (sdd.Ranking, error) {
	var base model.BaseConfig
	if cfg != nil {
		base = cfg.BaseConfig
	}
	ranking := sdd.Ranking{Timeout: base.Verify.ResolvedRankingTimeout()}
	host, ok := base.ClaudeHostRanking()
	if !base.Verify.GroundingPolicy().Prefilter || !ok {
		return ranking, nil
	}
	judge, err := factory.NewPrefilter(host, sink, sizes)
	if err != nil {
		return sdd.Ranking{}, err
	}
	ranking.ClaudeHost = appPrefilter(judge)
	_, err = exec.LookPath("claude")
	ranking.ClaudeCLI = err == nil
	return ranking, nil
}

// appPrefilter is a composed judge as the application takes it; a judge
// without a scan leaves Scan nil.
func appPrefilter(judge factory.Prefilter) sdd.Prefilter {
	out := sdd.Prefilter{Scorer: judge.Scorer, Judge: judge.Judge, Revision: judge.Revision}
	if judge.Scan != nil {
		out.Scan = judge.Scan
	}
	return out
}

// proofStore opens the proof records of a project's hiding rules: machine
// state inside the project's relevance ledger folder, so the daily sweep
// treats both alike.
func proofStore(stateRoot, repoKey string) (*localadapter.FilesystemProofStore, error) {
	return localadapter.NewFilesystemProofStore(filepath.Join(relevanceLedgerDir(stateRoot, repoKey), "proofs"))
}
