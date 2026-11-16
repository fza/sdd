package gitgate

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// TargetFromURL parses a git remote URL: git@host:path(.git),
// ssh://[user@]host[:port]/path, https://host/path(.git), http:// (counted as
// HTTPS), file:///path and absolute or relative local paths.
func TargetFromURL(rawURL string) (Target, error) {
	if rawURL == "" {
		return Target{}, errors.New("parsing git remote URL: empty")
	}
	if scheme, rest, found := strings.Cut(rawURL, "://"); found {
		return targetFromSchemeURL(rawURL, strings.ToLower(scheme), rest)
	}
	if host, path, ok := scpLike(rawURL); ok {
		return remoteTarget(rawURL, host, path, SSH)
	}
	return Target{Repo: rawURL, Protocol: Local}, nil
}

func targetFromSchemeURL(rawURL, scheme, rest string) (Target, error) {
	var protocol Protocol
	switch scheme {
	case "file":
		return Target{Repo: rest, Protocol: Local}, nil
	case "ssh", "git+ssh", "ssh+git":
		protocol = SSH
	case "https", "http":
		protocol = HTTPS
	default:
		return Target{}, fmt.Errorf("parsing git remote URL %q: unsupported scheme %q", rawURL, scheme)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return Target{}, fmt.Errorf("parsing git remote URL %q: %w", rawURL, err)
	}
	return remoteTarget(rawURL, parsed.Hostname(), parsed.Path, protocol)
}

// scpLike follows git's rule: a colon before any slash makes [user@]host:path,
// except a single drive letter.
func scpLike(rawURL string) (host, path string, ok bool) {
	colon := strings.Index(rawURL, ":")
	if colon < 0 {
		return "", "", false
	}
	slash := strings.Index(rawURL, "/")
	if slash >= 0 && slash < colon {
		return "", "", false
	}
	host = rawURL[:colon]
	if at := strings.LastIndex(host, "@"); at >= 0 {
		host = host[at+1:]
	}
	if len(host) <= 1 {
		return "", "", false
	}
	return host, rawURL[colon+1:], true
}

func remoteTarget(rawURL, host, path string, protocol Protocol) (Target, error) {
	host = strings.ToLower(host)
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	path = strings.TrimSuffix(path, "/")
	if host == "" || path == "" {
		return Target{}, fmt.Errorf("parsing git remote URL %q: host or path missing", rawURL)
	}
	return Target{Host: host, Repo: host + "/" + path, Protocol: protocol}, nil
}
