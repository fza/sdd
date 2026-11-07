package gitgate

import (
	"context"
	"errors"
	"regexp"
	"strings"
)

type outcome string

const (
	outcomeOK          outcome = "ok"
	outcomeTransport   outcome = "transport"
	outcomeRateLimited outcome = "rate_limited"
	outcomeAuth        outcome = "auth"
	outcomeRejected    outcome = "rejected"
	outcomeOther       outcome = "other"
)

// Rate limit and auth come before transport: git reports them as "RPC failed;
// HTTP 429" or "HTTP 403", which the transport patterns would also match.
var outcomePatterns = []struct {
	outcome outcome
	pattern *regexp.Regexp
}{
	{outcomeRateLimited, regexp.MustCompile(`(?:http|error:) ?429\b|unable to handle this request due to load|rate limit`)},
	{outcomeAuth, regexp.MustCompile(`permission denied \(publickey|authentication failed|(?:http|error:) ?40[13]\b|could not read (?:username|password)`)},
	{outcomeRejected, regexp.MustCompile(`\[rejected\]|non-fast-forward|fetch first`)},
	{outcomeTransport, regexp.MustCompile(`timed out|timeout|connection reset|connection refused|could not resolve host|early eof|remote end hung up|rpc failed|(?:http|error:) ?5\d\d\b|ssh_exchange_identification|kex_exchange_identification|connection closed by remote host|context deadline exceeded`)},
}

func classify(err error) outcome {
	if err == nil {
		return outcomeOK
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return outcomeTransport
	}
	message := strings.ToLower(err.Error())
	for _, candidate := range outcomePatterns {
		if candidate.pattern.MatchString(message) {
			return candidate.outcome
		}
	}
	return outcomeOther
}
