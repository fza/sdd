package secrets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const (
	DefaultGitHubAPI = "https://api.github.com"
	DefaultGitHubRaw = "https://raw.githubusercontent.com"

	// FetchTimeout bounds each request to GitHub, so the daily pass stays
	// well inside its run limit.
	FetchTimeout = 20 * time.Second

	gitleaksRepo     = "gitleaks/gitleaks"
	gitleaksRulePath = "config/gitleaks.toml"
	maxRuleFileSize  = 4 << 20
	maxAPIResponse   = 1 << 20
)

// GitHub reads gitleaks' releases: the API answers which release is the
// latest, the raw host serves the rule file at a release tag.
type GitHub struct {
	apiBase string
	rawBase string
	client  *http.Client
}

// NewGitHub returns a reader against the given bases; empty bases mean
// DefaultGitHubAPI and DefaultGitHubRaw, a nil client http.DefaultClient.
func NewGitHub(apiBase, rawBase string, client *http.Client) *GitHub {
	if apiBase == "" {
		apiBase = DefaultGitHubAPI
	}
	if rawBase == "" {
		rawBase = DefaultGitHubRaw
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &GitHub{apiBase: strings.TrimSuffix(apiBase, "/"), rawBase: strings.TrimSuffix(rawBase, "/"), client: client}
}

// LatestTag is the tag of gitleaks' latest release; a tag that is no plain
// semantic version, a pre-release among them, is refused.
func (g *GitHub) LatestTag(ctx context.Context) (string, error) {
	body, err := g.get(ctx, g.apiBase+"/repos/"+gitleaksRepo+"/releases/latest", "application/vnd.github+json", maxAPIResponse)
	if err != nil {
		return "", fmt.Errorf("asking for gitleaks' latest release: %w", err)
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	err = json.Unmarshal(body, &release)
	if err != nil {
		return "", fmt.Errorf("decoding gitleaks' latest release: %w", err)
	}
	if !releaseTag(release.TagName) {
		return "", fmt.Errorf("gitleaks' latest release has the tag %q, which is no release version", release.TagName)
	}
	return release.TagName, nil
}

// RuleFile downloads config/gitleaks.toml at a release tag.
func (g *GitHub) RuleFile(ctx context.Context, tag string) ([]byte, error) {
	if !releaseTag(tag) {
		return nil, fmt.Errorf("%q is no release version", tag)
	}
	data, err := g.get(ctx, g.rawBase+"/"+gitleaksRepo+"/"+tag+"/"+gitleaksRulePath, "", maxRuleFileSize)
	if err != nil {
		return nil, fmt.Errorf("downloading gitleaks' rule file at %s: %w", tag, err)
	}
	return data, nil
}

// TagCommit is the commit a release tag points at.
func (g *GitHub) TagCommit(ctx context.Context, tag string) (string, error) {
	if !releaseTag(tag) {
		return "", fmt.Errorf("%q is no release version", tag)
	}
	body, err := g.get(ctx, g.apiBase+"/repos/"+gitleaksRepo+"/commits/"+tag, "application/vnd.github.sha", maxAPIResponse)
	if err != nil {
		return "", fmt.Errorf("asking for the commit of gitleaks %s: %w", tag, err)
	}
	return strings.TrimSpace(string(body)), nil
}

func (g *GitHub) get(ctx context.Context, url, accept string, limit int64) (_ []byte, err error) {
	ctx, cancel := context.WithTimeout(ctx, FetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", url, err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%s answered more than %d bytes", url, limit)
	}
	return body, nil
}

func releaseTag(tag string) bool {
	return semver.IsValid(tag) && semver.Prerelease(tag) == "" && semver.Build(tag) == ""
}

// Updater fetches gitleaks' rule file of a release newer than the copy in
// use into its folder, where a Source reading that folder finds it.
type Updater struct {
	dir    string
	github *GitHub
	now    func() time.Time
}

// NewUpdater returns an updater for dir; now defaults to time.Now.
func NewUpdater(dir string, github *GitHub, now func() time.Time) *Updater {
	if now == nil {
		now = time.Now
	}
	return &Updater{dir: dir, github: github, now: now}
}

// Update asks for gitleaks' latest release and, when it is newer than the
// copy in use, downloads, validates and stores its rule file. A failure
// leaves the copy in use alone and is recorded in the fetch state before it
// is returned.
func (u *Updater) Update(ctx context.Context) error {
	state, err := readState(u.dir)
	if err != nil {
		return u.fail(fetchState{}, err)
	}
	builtin, err := BuiltinRuleFile()
	if err != nil {
		return err
	}
	inUse := builtin.Tag
	fetched, err := loadFetched(u.dir, state, builtin.Tag)
	if err == nil && fetched != nil {
		inUse = state.Tag
	}
	latest, err := u.github.LatestTag(ctx)
	if err != nil {
		return u.fail(state, err)
	}
	if semver.Compare(latest, inUse) <= 0 {
		state.CheckedAt = u.now()
		return writeState(u.dir, state)
	}
	data, err := u.github.RuleFile(ctx, latest)
	if err != nil {
		return u.fail(state, err)
	}
	_, err = ParseFetchedRules(data)
	if err != nil {
		return u.fail(state, fmt.Errorf("rejecting gitleaks' rule file at %s: %w", latest, err))
	}
	name := "gitleaks-" + latest + ".toml"
	err = writeFileAtomic(u.dir, name, data)
	if err != nil {
		return u.fail(state, err)
	}
	replaced := state.File
	sum := sha256.Sum256(data)
	now := u.now()
	state.Tag, state.File, state.SHA256 = latest, name, hex.EncodeToString(sum[:])
	state.AcceptedAt, state.CheckedAt = now, now
	err = writeState(u.dir, state)
	if err != nil {
		return err
	}
	return u.removeStale(name, replaced)
}

func (u *Updater) fail(state fetchState, err error) error {
	state.Failure, state.FailedAt = err.Error(), u.now()
	return errors.Join(err, writeState(u.dir, state))
}

// removeStale drops older copies and interrupted writes. The copy just
// replaced stays one more round, since a source may be reading it.
func (u *Updater) removeStale(keep ...string) error {
	entries, err := os.ReadDir(u.dir)
	if err != nil {
		return err
	}
	var errs []error
	for _, entry := range entries {
		name := entry.Name()
		stale := strings.HasPrefix(name, "gitleaks-") && strings.HasSuffix(name, ".toml")
		if matched, _ := filepath.Match(tempPattern, name); matched {
			stale = true
		}
		for _, kept := range keep {
			if name == kept {
				stale = false
			}
		}
		if stale {
			errs = append(errs, os.Remove(filepath.Join(u.dir, name)))
		}
	}
	return errors.Join(errs...)
}
