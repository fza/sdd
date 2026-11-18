package application

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/networkteam/sdd/internal/finders"
	"github.com/networkteam/sdd/internal/llmops"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/internal/secrets"
	"github.com/networkteam/sdd/pkg/application/types"
	"github.com/networkteam/sdd/pkg/llm"
)

// PreflightEntry checks a draft without publishing it or opening staged bytes.
// The procedure persists its findings and invalidates them when checked input changes.
func (a *Application) PreflightEntry(ctx context.Context, identity RequestIdentity, project ProjectID, binding SessionBinding, draft EntryDraft) (PreflightEntryResult, error) {
	return a.preflightEntry(ctx, identity, project, draft, nil, false)
}

// preflightEntry checks the draft, as the light verifier over reading where
// the project's verifier is on. Where deepJudges,
// the deep verifier judges in pre-flight's place: only the mechanical checks
// run, and the result carries the rubric the verifier takes over. A model
// answer cut at the token cap, or a call past its bound, returns the
// mechanical findings with an ErrorAnswerCut or ErrorTimedOut error: the
// model did not judge.
func (a *Application) preflightEntry(ctx context.Context, identity RequestIdentity, project ProjectID, draft EntryDraft, reading *captureReading, deepJudges bool) (PreflightEntryResult, error) {
	principal, runtime, err := a.resolve(ctx, identity, project, AccessWrite)
	if err != nil {
		return PreflightEntryResult{}, err
	}
	draft.Target, err = resolveMutationTarget(ctx, runtime, draft.Target)
	if err != nil {
		return PreflightEntryResult{}, err
	}
	result := PreflightEntryResult{Target: draft.Target}
	if draft.EntryID == "" {
		entryType, err := entryTypeForKind(model.Kind(draft.Kind))
		if err != nil {
			return result, err
		}
		draft.EntryID = model.GenerateIDAt(entryType, draftLayer(draft.Layer), "000", a.now())
	}
	target, effective, err := captureTargetSnapshot(ctx, runtime, draft.Target)
	if err != nil {
		return result, err
	}
	entry, snapshot, _, err := a.prepareCaptureEntry(ctx, identity, principal, effective, target, draft)
	if err != nil {
		return result, err
	}
	registry, err := ProcedureRegistry()
	if err != nil {
		return result, err
	}
	finder := finders.New(finders.Options{
		PreflightRunner: effective.options.LLM, ProcedureRegistry: registry,
		Config: &model.PerRepoConfig{Language: effective.options.Language, Dependencies: effective.options.Dependencies},
	})
	if deepJudges {
		result.Findings, err = mechanicalFindings(finder, snapshot.graph, entry)
		if err != nil {
			return result, err
		}
		result.Rubric, err = llmops.PreflightRubric(entry, snapshot.graph, effective.options.Language)
		if err != nil {
			return result, fmt.Errorf("pre-flight: %w", err)
		}
		return result, nil
	}
	light, err := effective.lightPreflight(ctx, reading, draft.Kind)
	if err != nil {
		return result, fmt.Errorf("pre-flight: %w", err)
	}
	preflight, err := finder.Preflight(ctx, snapshot.graph, query.PreflightQuery{Entry: entry, Light: light, KindFit: effective.kindFit(draft.Kind)})
	var unjudged ErrorCode
	switch {
	case errors.Is(err, llm.ErrAnswerCut):
		unjudged = ErrorAnswerCut
	case errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil:
		unjudged = ErrorTimedOut
	}
	if unjudged != "" {
		coded := &ApplicationError{Code: unjudged, Message: fmt.Sprintf("pre-flight: %v", err), Cause: err}
		result.Findings, err = mechanicalFindings(finder, snapshot.graph, entry)
		if err != nil {
			return result, err
		}
		return result, coded
	}
	if err != nil {
		return result, fmt.Errorf("pre-flight: %w", err)
	}
	result.Findings = publicFindings(preflight.Findings)
	result.Calls, result.Excerpted = preflight.Calls, preflight.Excerpted
	return result, nil
}

func mechanicalFindings(finder *finders.Finder, graph *model.Graph, entry *model.Entry) ([]Finding, error) {
	mechanical, err := finder.MechanicalPreflight(graph, entry)
	if err != nil {
		return nil, fmt.Errorf("pre-flight: %w", err)
	}
	return publicFindings(mechanical), nil
}

func publicFindings(findings []query.Finding) []Finding {
	var out []Finding
	for _, finding := range findings {
		out = append(out, Finding{Severity: string(finding.Severity), Category: finding.Category, Observation: finding.Observation, Basis: finding.Basis})
	}
	return out
}

// entryPublicationExists reports whether a committed publication exists under
// the key, reading the target's store without changing it.
func (a *Application) entryPublicationExists(ctx context.Context, identity RequestIdentity, target MutationTarget, key PublicationKey, entryID string) (exists bool, err error) {
	_, runtime, err := a.resolve(ctx, identity, target.Project, AccessRead)
	if err != nil {
		return false, err
	}
	target, err = resolveMutationTarget(ctx, runtime, target)
	if err != nil {
		return false, err
	}
	acquired, err := runtime.acquire(ctx, target)
	if err != nil {
		return false, err
	}
	defer func() { err = errors.Join(err, acquired.Release()) }()
	publisher, err := publicationStoreOf(acquired, target.Project)
	if err != nil {
		return false, err
	}
	_, exists, err = publisher.LookupEntryPublication(ctx, key, entryID)
	return exists, err
}

// CreateEntry requires the EntryID, Publication and attachment references recorded
// for this invocation. It returns a prior publication before preparing again;
// summary generation may repeat until publication succeeds. Preflight acceptance
// belongs to the calling procedure. Required finalizers run on every attempt.
func (a *Application) CreateEntry(ctx context.Context, identity RequestIdentity, project ProjectID, binding SessionBinding, draft EntryDraft) (result CreateEntryResult, err error) {
	principal, runtime, err := a.resolve(ctx, identity, project, AccessWrite)
	if err != nil {
		return result, err
	}
	result = CreateEntryResult{Project: runtime.options.Project, Binding: binding, EntryID: draft.EntryID}
	if binding.Subject != principal.Subject {
		return result, &ApplicationError{Code: ErrorSessionOwnership, Message: "entry publication ownership mismatch"}
	}
	stored, err := a.sessions.Load(ctx, binding.SessionID)
	if err != nil {
		return result, err
	}
	if err := verifyBinding(stored, binding); err != nil {
		return result, err
	}
	if err := draft.Publication.Validate(); err != nil {
		return result, err
	}
	if draft.Publication.Session != binding.SessionID {
		return result, fmt.Errorf("publication belongs to another session")
	}
	if _, err := model.IDToRelPath(draft.EntryID); err != nil {
		return result, fmt.Errorf("recorded entry ID: %w", err)
	}
	draft.Target, err = resolveMutationTarget(ctx, runtime, draft.Target)
	if err != nil {
		return result, err
	}
	acquired, err := runtime.acquire(ctx, draft.Target)
	if err != nil {
		return result, err
	}
	defer func() {
		if acquired != nil {
			err = errors.Join(err, acquired.Release())
		}
	}()
	publisher, err := publicationStoreOf(acquired, project)
	if err != nil {
		return result, err
	}
	publication, exists, err := publisher.LookupEntryPublication(ctx, draft.Publication, draft.EntryID)
	if err != nil {
		return result, err
	}
	if !exists {
		err = checkGraphSchema(ctx, acquired.Graph)
		if err != nil {
			return result, err
		}
		readRuntime := *runtime
		readRuntime.options.Graph = acquired.Graph
		target, effective, err := readMaterializedSnapshot(ctx, &readRuntime, "")
		if err != nil {
			return result, err
		}
		releaseErr := acquired.Release()
		acquired = nil
		if releaseErr != nil {
			return result, releaseErr
		}
		entry, snapshot, attachments, err := a.prepareCaptureEntry(ctx, identity, principal, effective, target, draft)
		if err != nil {
			return result, err
		}
		if draft.SkipPreflight {
			entry.Preflight = "skipped"
		}
		titles, err := effective.similarTitles(ctx, snapshot, entry)
		if err != nil {
			return result, fmt.Errorf("generating summary: %w", err)
		}
		described, err := llmops.Describe(ctx, effective.options.LLM, entry, snapshot.graph, effective.options.Language, false, titles)
		if err != nil {
			return result, fmt.Errorf("generating summary: %w", err)
		}
		if described.Summary != "" {
			entry.Summary = described.Summary
		}
		entry.Title = described.Title
		err = secrets.CheckEntry(entry)
		if err != nil {
			return result, err
		}
		logicalPath, err := model.IDToRelPath(draft.EntryID)
		if err != nil {
			return result, err
		}
		canonical := []byte(model.FormatFrontmatter(entry) + "\n" + entry.Content + "\n")
		document, err := ParseEntryDocument(logicalPath, canonical)
		if err != nil {
			return result, err
		}
		document.Attachments = append([]string(nil), entry.Attachments...)
		batch := MutationBatch{
			ID:          draft.Publication.String(),
			Changes:     []DocumentChange{{LogicalPath: logicalPath, Document: &document, CanonicalBytes: canonical}},
			Attachments: attachments,
			Message:     entry.CaptureCommitMessage(),
		}
		acquired, err = runtime.acquire(ctx, draft.Target)
		if err != nil {
			return result, err
		}
		publisher, err = publicationStoreOf(acquired, project)
		if err != nil {
			return result, err
		}
		publication, err = publisher.PublishEntry(ctx, draft.Publication, batch, ownedBlobReader{store: a.blobs, ref: SessionRef{Subject: principal.Subject, Session: binding.SessionID}})
		if err != nil {
			return result, err
		}
	}
	id, err := model.RelPathToID(publication.Document.LogicalPath)
	if err != nil {
		return result, err
	}
	if id != draft.EntryID {
		return result, fmt.Errorf("publication returned entry %s instead of %s", id, draft.EntryID)
	}
	batch, err := entryPublicationBatch(draft.Publication, publication)
	if err != nil {
		return result, err
	}
	for _, finalizer := range acquired.Finalizers {
		if err := finalizer.Finalize(ctx, AppliedMutation{Project: project, BatchID: batch.ID, Revision: publication.Revision, Batch: batch}); err != nil {
			return result, fmt.Errorf("completing entry publication (%s): %w", finalizer.Name(), err)
		}
	}
	result.EntryID = id
	result.Summary, _ = publication.Document.Frontmatter["summary"].(string)
	return result, nil
}

func captureTargetSnapshot(ctx context.Context, runtime *ProjectRuntime, target MutationTarget) (_ *Snapshot, _ *ProjectRuntime, err error) {
	acquired, err := runtime.acquire(ctx, target)
	if err != nil {
		return nil, nil, err
	}
	defer func() { err = errors.Join(err, acquired.Release()) }()
	selected := *runtime
	selected.options.Graph = acquired.Graph
	return readMaterializedSnapshot(ctx, &selected, "")
}

func entryPublicationBatch(key PublicationKey, publication EntryPublication) (MutationBatch, error) {
	id, err := model.RelPathToID(publication.Document.LogicalPath)
	if err != nil {
		return MutationBatch{}, err
	}
	attachmentDir, err := model.AttachDirRelPath(id)
	if err != nil {
		return MutationBatch{}, err
	}
	parts, err := model.ParseID(id)
	if err != nil {
		return MutationBatch{}, err
	}
	// Built from the published document, so a publication replayed after a
	// failed commit is committed under the same message as a fresh one.
	captured := model.Entry{ID: id, Type: model.TypeFromAbbrev[parts.TypeCode], Layer: model.LayerFromAbbrev[parts.LayerCode], Content: publication.Document.Body}
	captured.Title, _ = publication.Document.Frontmatter["title"].(string)
	batch := MutationBatch{ID: key.String(), Message: captured.CaptureCommitMessage(), Changes: []DocumentChange{{LogicalPath: publication.Document.LogicalPath, Document: &publication.Document}}}
	for _, name := range publication.Document.Attachments {
		batch.Attachments = append(batch.Attachments, AttachmentMaterialization{SourceName: name, LogicalPath: path.Join(attachmentDir, name)})
	}
	return batch, nil
}

func (a *Application) prepareCaptureEntry(ctx context.Context, identity RequestIdentity, principal Principal, runtime *ProjectRuntime, target *Snapshot, draft EntryDraft) (*model.Entry, *Snapshot, []AttachmentMaterialization, error) {
	refIDs := make([]string, 0, len(draft.Refs))
	for _, ref := range draft.Refs {
		refIDs = append(refIDs, ref.ID)
	}
	for _, repoID := range model.CrossRepoIDs(refIDs) {
		if dependency, err := a.dependency(ctx, identity, runtime, repoID); err == nil {
			a.demand(ctx, dependency, "")
		}
	}
	snapshot, err := a.snapshotWithDependencyPolicy(ctx, identity, runtime, target, false)
	if err != nil {
		return nil, nil, nil, err
	}
	entry, findings := entryFromDraft(draft, draft.EntryID, a.now())
	if len(findings) > 0 {
		return nil, nil, nil, captureValidationError(findings)
	}
	participant, err := a.participantFor(ctx, principal, runtime)
	if err != nil {
		return nil, nil, nil, err
	}
	switch {
	case draft.agent != nil:
		if len(draft.Participants) > 0 {
			return nil, nil, nil, captureValidationError([]model.Finding{{Field: "participants", Message: agentParticipantsRule}})
		}
		actor := snapshot.graph.ClientParticipant(entry, draft.agent.client)
		if actor == "" {
			return nil, nil, nil, captureValidationError([]model.Finding{{Field: "participants", Message: unknownClientRule(draft.agent.client)}})
		}
		entry.DecidedBy, entry.RunBy, entry.Participants = model.DecidedByAgent, participant, []string{actor}
	case draft.approval != nil:
		markApproved(entry, participant, *draft.approval)
	case len(entry.Participants) == 0 && participant != "":
		entry.Participants = []string{participant}
	}
	attachmentDir, err := model.AttachDirRelPath(draft.EntryID)
	if err != nil {
		return nil, nil, nil, err
	}
	attachments := make([]AttachmentMaterialization, 0, len(draft.Attachments))
	for _, attachment := range draft.Attachments {
		if attachment.Filename == "" || attachment.Filename == "." || path.Base(attachment.Filename) != attachment.Filename || strings.ContainsAny(attachment.Filename, "\\\x00") {
			return nil, nil, nil, fmt.Errorf("invalid staged attachment filename %q", attachment.Filename)
		}
		entry.Attachments = append(entry.Attachments, attachment.Filename)
		attachments = append(attachments, AttachmentMaterialization{BlobID: attachment.BlobID, SourceName: attachment.Filename, LogicalPath: path.Join(attachmentDir, attachment.Filename)})
	}
	entry.Content = model.ResolveAttachmentLinks(entry.Content, draft.EntryID)
	if entry.Refs, err = snapshot.graph.ResolveRefIDs(entry.Refs); err != nil {
		return nil, nil, nil, fmt.Errorf("resolving refs: %w", err)
	}
	if entry.Closes, err = snapshot.graph.ResolveIDs(entry.Closes); err != nil {
		return nil, nil, nil, fmt.Errorf("resolving closes: %w", err)
	}
	if entry.Supersedes, err = snapshot.graph.ResolveIDs(entry.Supersedes); err != nil {
		return nil, nil, nil, fmt.Errorf("resolving supersedes: %w", err)
	}
	if entry.Approves, err = snapshot.graph.ResolveIDs(entry.Approves); err != nil {
		return nil, nil, nil, fmt.Errorf("resolving approves: %w", err)
	}
	if rule := snapshot.graph.AgentLimit(entry); draft.agent != nil && rule != "" {
		return nil, nil, nil, captureValidationError([]model.Finding{{Field: "decided-by", Message: rule}})
	}
	construction, findings := model.ConstructFromEntry(entry)
	validated, writeFindings := construction.ValidateForWrite(snapshot.graph)
	findings = append(findings, writeFindings...)
	if len(findings) > 0 {
		return nil, nil, nil, captureValidationError(findings)
	}
	// Pre-flight and summary send the entry to an LLM provider; staged
	// attachments were scanned when they were staged.
	err = secrets.CheckEntry(validated)
	if err != nil {
		return nil, nil, nil, err
	}
	return validated, snapshot, attachments, nil
}

// markApproved writes entry as approved by person. Their approval adopts the
// drafted reason and a directive's intent as theirs; the checks before their
// answer read both as the agent's draft.
func markApproved(entry *model.Entry, person string, approval approvalAuthor) {
	entry.ApprovedBy = person
	if len(entry.Participants) == 0 {
		entry.Participants = []string{person}
	}
	by := model.DraftedByAgent
	if approval.given {
		by = person
	}
	if entry.Type == model.TypeDecision && strings.TrimSpace(entry.Reason) != "" {
		entry.ReasonBy = by
	}
	if entry.Kind == model.KindDirective {
		entry.IntentBy = by
	}
}

func captureValidationError(findings []model.Finding) error {
	warnings := make([]types.Warning, 0, len(findings))
	for _, finding := range findings {
		warnings = append(warnings, finding.Warning())
	}
	return &ValidationError{Warnings: warnings}
}
