package mcpapp

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	sdd "github.com/networkteam/sdd/pkg/application"
)

type SyncReposArgs struct {
	Session string   `json:"session,omitempty" jsonschema:"attached session's handle, from start_session or resume_session; required. The sync runs for that session's home project"`
	Repos   []string `json:"repos,omitempty" jsonschema:"connected repositories to sync, by repo ID. Omitted: every repository connected on this machine"`
}

// RepoSyncResult is what the sync round did for one connected repository.
type RepoSyncResult struct {
	Repo   string `json:"repo"`
	State  string `json:"state" jsonschema:"synced, pushed (local commits published), skipped (the git gate holds it back; next says when it is due), failed (reason says why; next says when it is retried) or refused (reason says why, e.g. not connected on this machine)"`
	Next   string `json:"next,omitempty" jsonschema:"RFC 3339 time a skipped repository is due or a failed one is retried"`
	Reason string `json:"reason,omitempty"`
}

type SyncReposResult struct {
	ResultNotices
	Repos []RepoSyncResult `json:"repos"`
}

func (s *Server) syncRepos(ctx context.Context, req *mcp.CallToolRequest, args SyncReposArgs) (*mcp.CallToolResult, SyncReposResult, error) {
	ss, err := s.attachedSession(ctx, req, args.Session, Proofs{})
	if err != nil {
		return nil, SyncReposResult{}, err
	}
	project, _, _, err := s.readScope(ctx, req, ss, "")
	ss.mu.Unlock()
	if err != nil {
		return nil, SyncReposResult{}, err
	}
	request := sdd.SyncReposRequest{}
	for _, repo := range args.Repos {
		request.Repos = append(request.Repos, sdd.RepoID(repo))
	}
	synced, err := s.app.SyncRepos(ctx, s.requestIdentity(req), project, request)
	if err != nil {
		return nil, SyncReposResult{}, err
	}
	result := SyncReposResult{Repos: make([]RepoSyncResult, 0, len(synced.Outcomes))}
	for _, outcome := range synced.Outcomes {
		repo := RepoSyncResult{Repo: string(outcome.Repo), State: string(outcome.State), Reason: outcome.Reason}
		if !outcome.Next.IsZero() {
			repo.Next = outcome.Next.Format(time.RFC3339)
		}
		result.Repos = append(result.Repos, repo)
	}
	return nil, result, nil
}
