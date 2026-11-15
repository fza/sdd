package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const (
	gitHubAPI        = "https://api.github.com"
	gitHubRaw        = "https://raw.githubusercontent.com"
	fetchTimeout     = 20 * time.Second
	gitleaksRepo     = "gitleaks/gitleaks"
	gitleaksRulePath = "config/gitleaks.toml"
	maxRuleFileSize  = 4 << 20
	maxAPIResponse   = 1 << 20
)

// gitHub reads gitleaks' releases: the API answers which release is the
// latest, the raw host serves the rule file at a release tag.
type gitHub struct {
	client *http.Client
}

// LatestTag is the tag of gitleaks' latest release; a tag that is no plain
// semantic version, a pre-release among them, is refused.
func (g *gitHub) LatestTag(ctx context.Context) (string, error) {
	body, err := g.get(ctx, gitHubAPI+"/repos/"+gitleaksRepo+"/releases/latest", "application/vnd.github+json", maxAPIResponse)
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
func (g *gitHub) RuleFile(ctx context.Context, tag string) ([]byte, error) {
	if !releaseTag(tag) {
		return nil, fmt.Errorf("%q is no release version", tag)
	}
	data, err := g.get(ctx, gitHubRaw+"/"+gitleaksRepo+"/"+tag+"/"+gitleaksRulePath, "", maxRuleFileSize)
	if err != nil {
		return nil, fmt.Errorf("downloading gitleaks' rule file at %s: %w", tag, err)
	}
	return data, nil
}

// TagCommit is the commit a release tag points at.
func (g *gitHub) TagCommit(ctx context.Context, tag string) (string, error) {
	if !releaseTag(tag) {
		return "", fmt.Errorf("%q is no release version", tag)
	}
	body, err := g.get(ctx, gitHubAPI+"/repos/"+gitleaksRepo+"/commits/"+tag, "application/vnd.github.sha", maxAPIResponse)
	if err != nil {
		return "", fmt.Errorf("asking for the commit of gitleaks %s: %w", tag, err)
	}
	return strings.TrimSpace(string(body)), nil
}

func (g *gitHub) get(ctx context.Context, url, accept string, limit int64) (_ []byte, err error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
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
