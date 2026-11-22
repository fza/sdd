package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/networkteam/sdd/internal/engine"
)

// sessionLink is what a new session carries from the earlier session it
// links (20261122-105026-d-tac-wl5): the texts that session holds, or, for
// a log with no record of served texts, the line saying the link went
// unused.
type sessionLink struct {
	session SessionID
	texts   []engine.CarriedText
	unused  string
}

// resolveLink reads the served record of the session id names, which the
// caller must be authorized to continue. A handle the store does not find,
// never issued or removed by the sweep, is a caller error.
func (a *Application) resolveLink(ctx context.Context, identity RequestIdentity, id SessionID) (*sessionLink, error) {
	if id == "" {
		return nil, nil
	}
	principal, err := a.resolvePrincipal(ctx, identity)
	if err != nil {
		return nil, err
	}
	stored, err := a.sessions.Load(ctx, id)
	if errors.Is(err, ErrSessionNotFound) {
		return nil, &ApplicationError{Code: ErrorInvalidArgument, Message: fmt.Sprintf(
			"link: session %s is unknown or was removed; start_session works without link", id)}
	}
	if err != nil {
		return nil, err
	}
	err = a.access.AuthorizeSession(ctx, SessionAccessRequest{
		Actor: principal, Owner: Principal{Subject: stored.Metadata.Subject},
		Session: id, Project: stored.Metadata.Project,
	})
	if err != nil {
		return nil, err
	}
	err = validateStoredSession(stored)
	if err != nil {
		return nil, err
	}
	events, err := decodeWorkflowEvents(stored.Events)
	if err != nil {
		return nil, err
	}
	texts, recorded := engine.HeldTexts(events)
	if !recorded {
		return &sessionLink{session: id, unused: fmt.Sprintf(
			"(link %s not used: its log holds no record of served texts, as stock sdd or an earlier sdd build writes it; every text comes in full)", id)}, nil
	}
	return &sessionLink{session: id, texts: texts}, nil
}

// apply records the link on the session just opened, before its first serve.
func (l *sessionLink) apply(w *WorkflowSession) {
	if l == nil {
		return
	}
	if l.unused != "" {
		w.linkNotice = l.unused
		return
	}
	w.session.Link(string(l.session), l.texts)
}

// LinkNotice is the line the opening response carries when the link went
// unused, empty otherwise.
func (w *WorkflowSession) LinkNotice() string { return w.linkNotice }
