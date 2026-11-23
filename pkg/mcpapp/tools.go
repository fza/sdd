package mcpapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/networkteam/sdd/internal/basefacts"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
	"github.com/networkteam/sdd/internal/query"
	"github.com/networkteam/sdd/internal/serveview"
	"github.com/networkteam/sdd/internal/textpatch"
	"github.com/networkteam/sdd/internal/truncate"
	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/application/types"
)

// defaultSearchHits caps search responses when the caller sets no limit —
// the CLI's `--limit 8` drill behavior, adopted as the MCP default
// (d-tac-dbk serve sizes).
const defaultSearchHits = 8

// --- the loop -------------------------------------------------------------

type StartSessionArgs struct {
	Project string `json:"project,omitempty" jsonschema:"project to open the session in, by ID. Optional: a composition serving one project infers it. With several accessible and none passed, the response lists them under projects and starts no session. Pick one and call again"`
	Shell   string `json:"shell,omitempty" jsonschema:"shell procedure to open the session with, by canonical; defaults to user-dialogue"`
	Label   string `json:"label,omitempty" jsonschema:"short single-line subject label for the session; set it early, update when the subject sharpens"`
	Link    string `json:"link,omitempty" jsonschema:"handle of an earlier session of this conversation whose served texts you still see. After a right answer a text identical to one served there comes short. Omit it in a fresh context"`
}

// ProjectResult is one accessible project, served by start_session when the
// caller must choose before a session can open.
type ProjectResult struct {
	ID          string `json:"id" jsonschema:"project ID; pass as start_session's project"`
	DisplayName string `json:"display_name,omitempty"`
	CanWrite    bool   `json:"can_write"`
}

// StatusProjectRequired is the ServeResult status start_session returns when
// the composition has several projects and none was passed: no session
// opened, the accessible projects are listed instead.
const StatusProjectRequired = "project-required"

// CallerMark tells a second caller on a session from the first, so each gets
// the session framing once (20261121-233256-d-tac-mm2).
type CallerMark struct {
	Caller string `json:"caller,omitempty" jsonschema:"set it only on a session another agent already works on, such as a subagent handed its parent's handle: a short name of your own, the same on every call. sdd sends each caller the session framing once and again when it changes; a call without caller is the session's first caller"`
}

type StartProcedureArgs struct {
	Answering
	CallerMark
	Session   string         `json:"session,omitempty" jsonschema:"attached session's handle, from start_session or resume_session; required. Retain it across context compaction"`
	Canonical string         `json:"canonical" jsonschema:"procedure to start, by its stable name (e.g. capture)"`
	Params    map[string]any `json:"params,omitempty" jsonschema:"typed start inputs per the procedure's declaration: declared params, plus any declared state field to seed at start (e.g. a known anchor)"`
	Label     string         `json:"label,omitempty" jsonschema:"short single-line subject label for the session (the dialogue); set it early, update when the subject sharpens"`
	Parent    string         `json:"parent,omitempty" jsonschema:"instance handle of the spawning instance, for a sub-move (e.g. a capture dispatched from an engage); records lineage in the session log"`
	Project   string         `json:"project,omitempty" jsonschema:"project the move works in, when not the session's home project. It must lie in the home project's declared dependency closure and be readable by the principal. Omitted: a foreign anchor selects its project, else the parent's, else the home project"`
}

type NextArgs struct {
	Answering
	CallerMark
	RetryRef  uint64 `json:"retry_ref,omitempty" jsonschema:"exact retry reference from a failed operation; mutually exclusive with report and cancel_ref"`
	CancelRef uint64 `json:"cancel_ref,omitempty" jsonschema:"exact cancellation reference from a failed operation; leaves effects in place and returns to the preceding interaction; mutually exclusive with report and retry_ref"`
	Session   string `json:"session,omitempty" jsonschema:"attached session's handle, from start_session or resume_session; required. Retain it across context compaction"`
	Instance  string `json:"instance" jsonschema:"instance handle from start_procedure"`
	// Report carries either state fields for the current step (per the served
	// report_schema) or a chooser answer {chooser, choice, userWords?, reason?, fields?}.
	Report map[string]any `json:"report,omitempty" jsonschema:"state fields per the served report_schema, or a chooser answer object {chooser, choice, userWords?, reason?, fields?}. Omit report, retry_ref and cancel_ref together to pick up a delivery a waiting step was served as awaiting"`
	Label  string         `json:"label,omitempty" jsonschema:"update the session's subject label when the dialogue's subject sharpens"`
}

type AbandonArgs struct {
	Answering
	CallerMark
	Instance string `json:"instance,omitempty" jsonschema:"instance handle of the move to abandon; pass session too, naming the session the move belongs to"`
	Session  string `json:"session,omitempty" jsonschema:"with instance: the attached session the move belongs to (required). Alone: a session handle to tear down directly; no resume, no framing"`
	Reason   string `json:"reason,omitempty" jsonschema:"why the instance or session is being abandoned"`
}

type AbandonResult struct {
	ResultNotices
	Abandoned bool   `json:"abandoned"`
	Session   string `json:"session,omitempty" jsonschema:"the torn-down session, on teardown by handle"`
	Label     string `json:"label,omitempty" jsonschema:"the torn-down session's subject label"`
	// DiscardedThreads names what went down with a session teardown — nothing
	// vanishes silently (d-tac-dbk).
	DiscardedThreads []string   `json:"discarded_threads,omitempty" jsonschema:"open moves discarded with the session (procedure at step)"`
	HeldMarkers      []string   `json:"held_markers,omitempty" jsonschema:"WIP markers held; left standing. Resume later or close via groom"`
	Instructions     string     `json:"instructions,omitempty"`
	Base             *BaseServe `json:"base_junction,omitempty" jsonschema:"the session shell's current serve: where the dialogue now stands"`
	ResultQuestion
}

type ParkArgs struct {
	Answering
	CallerMark
	Session  string `json:"session,omitempty" jsonschema:"attached session's handle, from start_session or resume_session; required. Retain it across context compaction"`
	Instance string `json:"instance" jsonschema:"running move instance to park"`
	Note     string `json:"note,omitempty" jsonschema:"one line on why the move is shelved; the session log carries it for whoever resumes it"`
}

type ParkResult struct {
	ResultNotices
	Parked       bool       `json:"parked"`
	Instance     string     `json:"instance"`
	Procedure    string     `json:"procedure"`
	Step         string     `json:"step"`
	Instructions string     `json:"instructions,omitempty"`
	Base         *BaseServe `json:"base_junction,omitempty" jsonschema:"the session shell's serve: where the dialogue lands"`
	ResultQuestion
}

type ChooserOptionResult struct {
	Choice  string   `json:"choice"`
	Collect []string `json:"collect,omitempty" jsonschema:"state fields this option carries (suffix ? = optional)"`
}

type ChooserResult struct {
	Chooser string                `json:"chooser" jsonschema:"the pending chooser's step id: the value for a chooser answer's chooser field"`
	Kind    string                `json:"kind" jsonschema:"who answers: agent (advisory judgment) or user (relay their answer verbatim)"`
	Options []ChooserOptionResult `json:"options"`
}

// PendingOperationResult is the unfinished recorded operation beside the
// pending-operation position's prose (d-tac-t6u).
type PendingOperationResult struct {
	Instance  string            `json:"instance"`
	RetryRef  uint64            `json:"retry_ref" jsonschema:"pass as retry_ref to next, with this instance and no report, to run the operation again on its recorded input"`
	CancelRef uint64            `json:"cancel_ref" jsonschema:"pass as cancel_ref to next, with this instance and no report, to leave its effects in place and return to the preceding interaction"`
	Command   string            `json:"command"`
	Values    map[string]string `json:"values,omitempty" jsonschema:"identifiers the operation recorded before it ran"`
	Error     string            `json:"error,omitempty" jsonschema:"why this attempt did not finish; present at the moment of failure only"`
	ErrorCode string            `json:"error_code,omitempty"`
}

// EffectResult is one thing a cancelled operation reports it left.
type EffectResult struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	State string `json:"state"`
}

// CancellationResult is a recorded cancellation: where the instance returned
// to and what the operation reports it left. Nothing was cleaned up.
type CancellationResult struct {
	Instance   string            `json:"instance"`
	CancelRef  uint64            `json:"cancel_ref"`
	Command    string            `json:"command"`
	Values     map[string]string `json:"values,omitempty"`
	ReturnStep string            `json:"return_step,omitempty"`
	Closed     bool              `json:"closed" jsonschema:"true when no interaction preceded the operation and the instance closed instead of returning"`
	Effects    []EffectResult    `json:"effects,omitempty"`
}

func pendingOperationResult(pending *sdd.WorkflowPendingOperation) *PendingOperationResult {
	if pending == nil {
		return nil
	}
	return &PendingOperationResult{
		Instance: pending.Instance, RetryRef: pending.RetryRef, CancelRef: pending.CancelRef, Command: pending.Command,
		Values: pending.Values, Error: pending.Error, ErrorCode: string(pending.ErrorCode),
	}
}

func cancellationResult(cancelled *sdd.WorkflowCancellation) *CancellationResult {
	if cancelled == nil {
		return nil
	}
	result := &CancellationResult{
		Instance: cancelled.Instance, CancelRef: cancelled.CancelRef, Command: cancelled.Command, Values: cancelled.Values,
		ReturnStep: cancelled.ReturnStep, Closed: cancelled.Closed,
	}
	for _, effect := range cancelled.Effects {
		result.Effects = append(result.Effects, EffectResult{Kind: effect.Kind, ID: effect.ID, State: effect.State})
	}
	return result
}

// ServeResult is the loop's uniform response shape: where the instance
// stands, what advances it, and the material to work with.
type ServeResult struct {
	ResultNotices
	Session          string                  `json:"session,omitempty" jsonschema:"session handle (the session ID); sessions survive restarts and resume via resume_session. Pass it to every other tool"`
	Project          string                  `json:"project,omitempty" jsonschema:"project this instance works in: the session's home project unless the move started in a dependency"`
	Projects         []ProjectResult         `json:"projects,omitempty" jsonschema:"start_session only: the principal's accessible projects, served with status project-required when several exist and none was passed; no session opened yet"`
	Branch           string                  `json:"branch,omitempty" jsonschema:"the session's declared branch binding"`
	Instance         string                  `json:"instance,omitempty"`
	Procedure        string                  `json:"procedure,omitempty"`
	Status           string                  `json:"status" jsonschema:"running, completed, or abandoned; or project-required from start_session, when a project must be chosen first"`
	Step             string                  `json:"step,omitempty"`
	Goal             string                  `json:"goal" jsonschema:"one line: what advances the instance from here"`
	Instructions     string                  `json:"instructions,omitempty"`
	Missing          []string                `json:"missing,omitempty" jsonschema:"required report fields not yet provided"`
	ReportSchema     map[string]any          `json:"report_schema,omitempty" jsonschema:"JSON Schema for the current step's report"`
	PendingChooser   *ChooserResult          `json:"pending_chooser,omitempty"`
	PendingOperation *PendingOperationResult `json:"pending_operation,omitempty" jsonschema:"unfinished recorded operation; only its retry_ref or cancel_ref advances this session"`
	Cancellation     *CancellationResult     `json:"cancellation,omitempty" jsonschema:"recorded cancellation: return position and what the operation reports it left"`
	Execution        string                  `json:"execution,omitempty" jsonschema:"execution hint for this instance; fork-preferred means the procedure is a task best run in a disposable forked context"`
	Produced         map[string]string       `json:"produced,omitempty" jsonschema:"engine-written results on completion (e.g. the created entry ID); a value past 200 characters is left out and named in instructions"`
	Framing          string                  `json:"framing,omitempty" jsonschema:"session framing (aspirations, directives, focus, participants): a block goes to each caller once and again when it changes, no answer needed; resume_session sends every block again"`
	Vocabulary       string                  `json:"vocabulary,omitempty" jsonschema:"translation table for non-English graphs: canonical tokens stay English, user-facing narration renders in the configured language; in full, left out while a right answer holds it"`
	OpenThreads      string                  `json:"open_threads,omitempty" jsonschema:"this dialogue's own open threads, carried on the session shell's serves. Other dialogues are never listed here"`
	Base             *BaseServe              `json:"base_junction,omitempty" jsonschema:"the session shell's current serve: where the dialogue lands now that this move has ended"`
	Collected        map[string]string       `json:"collected,omitempty" jsonschema:"resume_session only: the state this instance already collected, each value of up to 300 characters whole; values persist across handover. Do not re-derive them"`
	CollectedLarge   map[string]int          `json:"collected_large,omitempty" jsonschema:"resume_session only: the collected values past 300 characters, by name with their size in characters; read_value returns one whole"`
	Dispatches       []DispatchResult        `json:"dispatches,omitempty" jsonschema:"delegate handoffs this call owes: a delegate forked into its own child session, or sent its next input. Follow each one's instructions"`
	Parked           bool                    `json:"parked,omitempty" jsonschema:"set when the move is parked at step for a person; nothing was written"`
	Reason           string                  `json:"reason,omitempty" jsonschema:"why the move is parked"`
	Resume           string                  `json:"resume,omitempty" jsonschema:"the call a person resumes the parked move with"`
	ResultQuestion
}

// DispatchResult is one delegate handoff: the child handles a delegate
// receives, the prompts that carry them, and what to do this round. The
// dispatching session's own handle is never part of it, and a delegate sdd
// launched itself hands over neither handles nor prompts.
type DispatchResult struct {
	Session        string `json:"session,omitempty" jsonschema:"the delegate's child session handle; absent when sdd launched the delegate"`
	Instance       string `json:"instance,omitempty" jsonschema:"the delegate's task instance in its child session; absent when sdd launched the delegate"`
	Procedure      string `json:"procedure"`
	Prompt         string `json:"prompt,omitempty" jsonschema:"the whole prompt of a fresh-context delegate"`
	ContinuePrompt string `json:"continue_prompt,omitempty" jsonschema:"the prompt continuing a delegate that worked an earlier round"`
	Instructions   string `json:"instructions"`
	Agent          string `json:"agent,omitempty" jsonschema:"the subagent definition sdd init installs per harness to launch the delegate as"`
	Model          string `json:"model,omitempty" jsonschema:"the model the harness launches that subagent with; unset leaves the harness's own"`
	Effort         string `json:"effort,omitempty" jsonschema:"the effort the harness launches that subagent with; unset leaves the harness's own"`
	Runner         string `json:"runner,omitempty" jsonschema:"set when sdd launched the delegate itself: the runner its round runs with, in the background"`
}

func dispatchResults(dispatches []*sdd.WorkflowDispatch) []DispatchResult {
	var out []DispatchResult
	for _, dispatch := range dispatches {
		out = append(out, DispatchResult{
			Session: string(dispatch.Session), Instance: dispatch.Instance, Procedure: dispatch.Procedure,
			Prompt: dispatch.Prompt, ContinuePrompt: dispatch.ContinuePrompt, Instructions: dispatch.Instructions, Agent: dispatch.Agent, Model: dispatch.Model, Effort: dispatch.Effort, Runner: dispatch.Runner,
		})
	}
	return out
}

// BaseServe is the session shell's serve as nested into landing responses
// (move completion, abandon). Same shape as ServeResult minus the nesting
// field — the shell never lands on itself, and the omission keeps the JSON
// schema acyclic.
type BaseServe struct {
	Session        string         `json:"session"`
	Branch         string         `json:"branch,omitempty"`
	Instance       string         `json:"instance"`
	Procedure      string         `json:"procedure"`
	Status         string         `json:"status"`
	Step           string         `json:"step,omitempty"`
	Goal           string         `json:"goal"`
	Instructions   string         `json:"instructions,omitempty"`
	PendingChooser *ChooserResult `json:"pending_chooser,omitempty"`
	Framing        string         `json:"framing,omitempty"`
	OpenThreads    string         `json:"open_threads,omitempty" jsonschema:"this dialogue's own open threads; other dialogues are never listed here"`
}

// --- sessions & staging ----------------------------------------------------

type ResumeSessionArgs struct {
	CallerMark
	Session string `json:"session" jsonschema:"session handle (the session ID) whose current position to re-serve: its framing plus every running move at its current step, with the schema to continue it. Presenting the handle is the whole authorization. It also declares you need re-serving: every piece of the position serves in full, whatever the call holds"`
}

type ResumeSessionResult struct {
	ResultNotices
	Session          string                  `json:"session" jsonschema:"session handle (the session ID)"`
	Project          string                  `json:"project,omitempty" jsonschema:"the project this session is bound to"`
	Participant      string                  `json:"participant,omitempty"`
	Label            string                  `json:"label,omitempty" jsonschema:"the session's subject label, when one was recorded"`
	Branch           string                  `json:"branch,omitempty" jsonschema:"the session's declared branch binding"`
	PendingOperation *PendingOperationResult `json:"pending_operation,omitempty" jsonschema:"unfinished recorded operation; its instance's serve in open_instances carries the position"`
	Cancellation     *CancellationResult     `json:"cancellation,omitempty" jsonschema:"latest recorded cancellation, included even when its instance is closed or omitted"`
	LastEndedMove    string                  `json:"last_ended_move,omitempty" jsonschema:"one line naming the session's most recent ended move and how it ended: completed, abandoned or cancelled; absent while no move has ended. open_instances lists running moves only"`
	Open             []ServeResult           `json:"open_instances" jsonschema:"current serve for every running instance; the session shell's serve carries the open-threads block"`
	Framing          string                  `json:"framing,omitempty"`
	Instructions     string                  `json:"instructions,omitempty"`
	ResultQuestion
}

type BindBranchArgs struct {
	Session string `json:"session" jsonschema:"attached session's handle"`
	Branch  string `json:"branch,omitempty" jsonschema:"branch to bind this session to"`
	Clear   bool   `json:"clear,omitempty" jsonschema:"clear the current branch binding"`
}

type BindBranchResult struct {
	ResultNotices
	Branch string `json:"branch,omitempty" jsonschema:"the now-effective branch binding; empty after clear"`
	Status string `json:"status"`
}

// StageAttachmentArgs is a stage_attachment call. Name with Content, Path or
// Patches is the one-file shape, which marshals to arguments the server takes
// as a Files call of that one file; the tool listing advertises Files alone.
type StageAttachmentArgs struct {
	Session string             `json:"session,omitempty"`
	Files   []StageFileArg     `json:"files,omitempty"`
	Name    string             `json:"name,omitempty"`
	Content string             `json:"content,omitempty"`
	Path    string             `json:"path,omitempty"`
	Patches []SearchReplaceArg `json:"patches,omitempty"`
}

// stageAttachmentInput is the stage_attachment input schema the listing
// advertises.
type stageAttachmentInput struct {
	Session string         `json:"session,omitempty" jsonschema:"attached session's handle, from start_session or resume_session; required. Retain it across context compaction"`
	Files   []StageFileArg `json:"files" jsonschema:"files to stage, in order; a name appears at most once. Every file is checked before any is staged: one failing file refuses the call and nothing is staged"`
}

// StageFileArg is one file of a stage_attachment call.
type StageFileArg struct {
	Name    string             `json:"name" jsonschema:"target filename (plain name, no paths)"`
	Content string             `json:"content,omitempty" jsonschema:"file content to stage (UTF-8 text)"`
	Path    string             `json:"path,omitempty" jsonschema:"local file path to stage instead of inline content; read again before a playback confirms a body staged from it"`
	Patches []SearchReplaceArg `json:"patches,omitempty" jsonschema:"edit the file staged under name before this call in place: ordered exact search-replace pairs, applied atomically. Each old must match exactly once; a failing pair refuses the whole call. Pass instead of content/path"`
}

// SearchReplaceArg is one exact edit inside a patches list.
type SearchReplaceArg struct {
	Old string `json:"old" jsonschema:"exact text to replace; must match exactly once in the file as it stands when this pair applies"`
	New string `json:"new" jsonschema:"replacement text; empty deletes the old text"`
}

type StageAttachmentResult struct {
	ResultNotices
	// Handle answers a call in the one-file shape (name with content, path
	// or patches beside session), which the listing does not advertise.
	Handle string `json:"handle,omitempty" jsonschema:"the one file's handle, on a call naming name beside session instead of files"`
	// Files is a pointer so the result stays comparable; StagedFiles reads it.
	Files *[]StagedFileResult `json:"files" jsonschema:"one result per staged file, in call order"`
	// Dispatches is a pointer for the same reason; StageDispatches reads it.
	Dispatches *[]DispatchResult `json:"dispatches,omitempty" jsonschema:"delegate handoffs this call owes: a delegate a capture this staging re-advanced sent its next input. Follow each one's instructions"`
}

// StageDispatches returns the handoffs Dispatches points at.
func (r StageAttachmentResult) StageDispatches() []DispatchResult {
	if r.Dispatches == nil {
		return nil
	}
	return *r.Dispatches
}

// StagedFiles returns the per-file results Files points at.
func (r StageAttachmentResult) StagedFiles() []StagedFileResult {
	if r.Files == nil {
		return nil
	}
	return *r.Files
}

// StagedFileResult is one staged file of a stage_attachment call.
type StagedFileResult struct {
	Handle string   `json:"handle" jsonschema:"attachment handle; pass it in a report's attachments field, or as bodyHandle when the file holds an entry body"`
	SHA256 string   `json:"sha256" jsonschema:"sha256 of the staged file's raw bytes, as hex; a playback confirm binds to it when the file is a capture's bodyHandle"`
	Bound  []string `json:"bound,omitempty" jsonschema:"captures taking their body from this handle, whose body and playback this staging replaced"`
}

// --- free reads -------------------------------------------------------------

type SearchArgs struct {
	Rereads
	IncludesRevision  string       `json:"includes_revision,omitempty" jsonschema:"require a selected revision containing this successful write"`
	Session           string       `json:"session,omitempty" jsonschema:"attached session's handle, from start_session or resume_session; required. The read runs in that session's project and branch"`
	Project           string       `json:"project,omitempty" jsonschema:"project to read in; defaults to the session's home project. Another must lie in the home project's declared dependency closure and be readable by the principal"`
	Terms             []string     `json:"terms,omitempty" jsonschema:"text mode: regex terms combined with AND"`
	Query             string       `json:"query,omitempty" jsonschema:"vector mode: free-form phrase (requires a configured embedding provider); both together run hybrid"`
	Type              string       `json:"type,omitempty" jsonschema:"filter: s/signal or d/decision"`
	Layer             string       `json:"layer,omitempty" jsonschema:"filter: stg, cpt, tac, ops, prc"`
	Kind              string       `json:"kind,omitempty" jsonschema:"filter: entry kind"`
	IncludeSuperseded bool         `json:"include_superseded,omitempty"`
	Limit             int          `json:"limit,omitempty" jsonschema:"hit cap; default 8"`
	MaxCitations      *int         `json:"max_citations,omitempty" jsonschema:"citation snippet lines per entry; omitted = 1 (the strongest matching chunk, the match evidence), 0 = headers only"`
	Repos             []sdd.RepoID `json:"repos,omitempty" jsonschema:"also search these repos of the project's dependency closure by repo-id (additive to the local graph)"`
	AllRepos          bool         `json:"all_repos,omitempty" jsonschema:"also search every repo in the project's dependency closure"`
	Page              int          `json:"page,omitempty" jsonschema:"a later page of this tool's last result too large for one, as its page line names; it serves the held result and leaves the other arguments aside"`
}

type SearchResult struct {
	ResultNotices
	Coverage []sdd.SearchCoverage `json:"coverage,omitempty" jsonschema:"published entry coverage for each fixed search snapshot"`
	Notice   string               `json:"notice,omitempty" jsonschema:"readable incomplete-indexing notice"`
	Results  string               `json:"results" jsonschema:"matching entries with citations"`
}

type ViewArgs struct {
	Rereads
	Session  string       `json:"session,omitempty" jsonschema:"attached session's handle, from start_session or resume_session; required. The read runs in that session's project and branch"`
	Project  string       `json:"project,omitempty" jsonschema:"project to read in; defaults to the session's home project. Another must lie in the home project's declared dependency closure and be readable by the principal"`
	Layout   string       `json:"layout" jsonschema:"sdd view layout pipeline, e.g. 'active:as-counts' or 'top(15)'"`
	Repos    []sdd.RepoID `json:"repos,omitempty" jsonschema:"also render the layout over these repos of the project's dependency closure (additive to the local graph)"`
	AllRepos bool         `json:"all_repos,omitempty" jsonschema:"also render the layout over every repo in the project's dependency closure"`
	Page     int          `json:"page,omitempty" jsonschema:"a later page of this tool's last result too large for one, as its page line names; it serves the held result and leaves the other arguments aside"`
}

type ViewResult struct {
	ResultNotices
	Sections string `json:"sections"`
	Hint     string `json:"hint,omitempty" jsonschema:"one-line pointer to the view layout grammar, on the first view of a session and after resume_session"`
}

type ShowArgs struct {
	Rereads
	Session string   `json:"session,omitempty" jsonschema:"attached session's handle, from start_session or resume_session; required. The read runs in that session's project and branch"`
	Project string   `json:"project,omitempty" jsonschema:"project to read in; defaults to the session's home project. Another must lie in the home project's declared dependency closure and be readable by the principal"`
	IDs     []string `json:"ids" jsonschema:"entry IDs to show; accepts an unambiguous short ID ({type}-{layer}-{suffix}) and resolves it to the full entry"`
	Up      *int     `json:"up,omitempty" jsonschema:"upstream chain depth; omitted = default 2, 0 = no upstream"`
	Down    *int     `json:"down,omitempty" jsonschema:"downstream chain depth; omitted = default 1, 0 = no downstream"`
	Page    int      `json:"page,omitempty" jsonschema:"a later page of this tool's last result too large for one, as its page line names; it serves the held result and leaves the other arguments aside"`
}

type ShowResult struct {
	ResultNotices
	Entries string `json:"entries"`
}

type GatherArgs struct {
	Rereads
	Session  string   `json:"session,omitempty" jsonschema:"attached session's handle, from start_session or resume_session; required"`
	IDs      []string `json:"ids,omitempty" jsonschema:"entries to open in full; an entry the call already holds serves its pointer"`
	Instance string   `json:"instance,omitempty" jsonschema:"the running capture this reading grounds. Its reported draft is searched with its own text and its topics listed. Only reads made through gather inside it count for its grounding. Report the draft at assemble first"`
	Draft    string   `json:"draft,omitempty" jsonschema:"without instance: the draft text, searched by meaning as it stands; credits nothing"`
	Topics   []string `json:"topics,omitempty" jsonschema:"without instance: the draft's topic labels; with one: topics listed beside the draft's own. Each is listed in full"`
	Query    string   `json:"query,omitempty" jsonschema:"your search phrase, searched by meaning beside the draft's own text. Write it for one hypothesis about how the sought entry was written: who wrote it, what prompted it, and whether it frames the situation, the solution or the mechanism"`
	Terms    []string `json:"terms,omitempty" jsonschema:"your keyword terms, one search each, matched as whole words regardless of case. Each tests another hypothesis about the sought entry's wording, not a rephrasing of the same guess"`
	Wait     bool     `json:"wait,omitempty" jsonschema:"have the ranking judge score until the minimum is judged, without its time limit: what to pass, with the same searches, when a check judged too few and the person chose to wait"`
	Rejudge  bool     `json:"rejudge,omitempty" jsonschema:"have the ranking judge score every candidate against the draft as it stands, the judgements the capture keeps from earlier drafts included"`
	Page     int      `json:"page,omitempty" jsonschema:"a later page of this tool's last result too large for one, as its page line names, leaving the other arguments aside; with instance, the next page of that capture's graph check, built from the scores that arrived by then"`
}

type GatherResult struct {
	ResultNotices
	Serve string `json:"serve" jsonschema:"the reads run, entries served in full, the entries to judge and the entries set aside; nothing is cut"`
}

type ReadAttachmentArgs struct {
	Session  string `json:"session,omitempty" jsonschema:"attached session's handle, from start_session or resume_session; required. The read runs in that session's project; a staged file is read from that session"`
	Project  string `json:"project,omitempty" jsonschema:"project to read the entry's attachment in; defaults to the session's home project. Another must lie in the home project's declared dependency closure and be readable by the principal"`
	ID       string `json:"id,omitempty" jsonschema:"full ID of the entry whose attachment to read; omit when reading a staged file by handle"`
	Name     string `json:"name,omitempty" jsonschema:"attachment filename; optional when the entry has exactly one"`
	Offset   int64  `json:"offset,omitempty" jsonschema:"byte position to continue from (next_offset of the previous page)"`
	MaxBytes int    `json:"max_bytes,omitempty" jsonschema:"page size cap; default 65536. A page stops short of it where the result would grow past its bound; next_offset continues"`
	Handle   string `json:"handle,omitempty" jsonschema:"read a file staged in this session (before any entry carries it) by its handle, in the same bounded pages; pass instead of id"`
}

type ReadValueArgs struct {
	Session  string `json:"session,omitempty" jsonschema:"attached session's handle; required"`
	Instance string `json:"instance" jsonschema:"the move whose value to read, as resume_session names it"`
	Name     string `json:"name" jsonschema:"the value's name, as collected_large lists it"`
	Page     int    `json:"page,omitempty" jsonschema:"a later page of this tool's last result too large for one, as its page line names; it serves the held result and leaves the other arguments aside"`
}

type ReadValueResult struct {
	ResultNotices
	Value string `json:"value" jsonschema:"the value whole, never cut; a value past one result comes in pages"`
}

type ReadGitArgs struct {
	Session string `json:"session,omitempty" jsonschema:"attached session's handle; required. The read runs in that session's project checkout"`
	Project string `json:"project,omitempty" jsonschema:"project to read in; defaults to the session's home project. Another must lie in the home project's declared dependency closure and be readable by the principal"`
	Op      string `json:"op,omitempty" jsonschema:"file: a file's content at rev; log: the commits from rev (default HEAD), not reachable from from when given, touching path when given; commit: one commit's message and changed files; diff: between from and to, for path when given"`
	Rev     string `json:"rev,omitempty" jsonschema:"revision a file, log or commit read starts from: a branch, tag or commit hash such as main or HEAD~2"`
	From    string `json:"from,omitempty" jsonschema:"diff: the older revision; log: commits reachable from it are left out"`
	To      string `json:"to,omitempty" jsonschema:"diff: the newer revision"`
	Path    string `json:"path,omitempty" jsonschema:"file: the file to read; log and diff: narrows them to this path. Relative to the checkout root"`
	Limit   int    `json:"limit,omitempty" jsonschema:"log: at most this many commits; default 20, at most 200"`
	Page    int    `json:"page,omitempty" jsonschema:"a later page of this tool's last result too large for one, as its page line names; it serves the held result and leaves the other arguments aside"`
}

type ReadGitResult struct {
	ResultNotices
	Text string `json:"text" jsonschema:"what git returned for the read, never cut; a text past one result comes in pages"`
}

type ReadAttachmentResult struct {
	ResultNotices
	Name       string   `json:"name"`
	Content    string   `json:"content"`
	Offset     int64    `json:"offset"`
	NextOffset int64    `json:"next_offset,omitempty"`
	TotalBytes int64    `json:"total_bytes"`
	More       bool     `json:"more"`
	Available  []string `json:"available" jsonschema:"the entry's attachment filenames"`
	Path       string   `json:"path,omitempty" jsonschema:"absolute filesystem path; present only for local (stdio) clients, which may read the file directly instead of paging"`
}

// ResultNotices carries the blocked-sync notice while automatic sync of the
// graph repository cannot proceed, once per change of its text; once the
// notice of a failed background cache pruning run; once the config keys
// that changed since the previous call.
type ResultNotices struct {
	SyncNotice  string `json:"sync_notice,omitempty" jsonschema:"set when automatic sync of the graph repository becomes blocked, the background title backfill fails, or a connected repository's last sync round fails, or a refresh the call started still runs past its wait, and whenever that notice changes. One line each: which, and since when, why or how old the clone it read is, plus the fix steps when the attached session owns the fix. While it stands unchanged, later results leave it out; resume_session serves it again. Plain status, not a tool error; act on it as it says"`
	CacheNotice string `json:"cache_notice,omitempty" jsonschema:"set once after sdd's background cache pruning failed: the error and where to look. Plain status, not a tool error; pass it on to the user"`
	HostNotice  string `json:"host_notice,omitempty" jsonschema:"set once after the host's config changed: the changed keys with their old and new values (a secret names only its key). Plain status, not a tool error; carry on under the new values"`
}

func (f *ResultNotices) setSyncNotice(notice string)  { f.SyncNotice = notice }
func (f *ResultNotices) setCacheNotice(notice string) { f.CacheNotice = notice }
func (f *ResultNotices) setHostNotice(notice string)  { f.HostNotice = notice }

// ResultQuestion ends a response that starts, resumes or advances a session
// with the questions the next call that advances it answers
// (20261121-230157-d-tac-ym8). Embedded last, it is the result's last field.
type ResultQuestion struct {
	Question string `json:"question,omitempty" jsonschema:"two questions about texts sdd served you, one per line, each as q <text>: …? | lost. Answer both in answer on your next call that advances this session, in their order and parted by ; (reach sdd ; 280): the words each asks for, looked up in its text, or lost"`
}

func (f *ResultQuestion) setQuestion(line string) { f.Question = line }

// SyncBlockedResult is the blocked-sync record info shows.
type SyncBlockedResult struct {
	Kind        string   `json:"kind"`
	Since       string   `json:"since"`
	Upstream    string   `json:"upstream,omitempty"`
	Detail      string   `json:"detail,omitempty"`
	Source      string   `json:"source,omitempty" jsonschema:"for a conflict: the upstream commit that would not merge"`
	Destination string   `json:"destination,omitempty" jsonschema:"for a conflict: the local commit it was to merge into"`
	Paths       []string `json:"paths,omitempty"`
	Owner       string   `json:"owner,omitempty" jsonschema:"session owning the fix"`
}

type InfoArgs struct {
	Session string `json:"session,omitempty" jsonschema:"attached session's handle, from start_session or resume_session; required. The header describes that session's project"`
	Project string `json:"project,omitempty" jsonschema:"project to describe; defaults to the session's home project. Another must lie in the home project's declared dependency closure and be readable by the principal"`
}

type InfoResult struct {
	ResultNotices
	SyncBlocked  *SyncBlockedResult `json:"sync_blocked,omitempty" jsonschema:"what blocks automatic sync of the graph repository; absent while sync proceeds"`
	Project      string             `json:"project,omitempty" jsonschema:"the session's project ID"`
	Participant  string             `json:"participant,omitempty" jsonschema:"configured local participant (canonical name)"`
	Language     string             `json:"language,omitempty" jsonschema:"configured graph language; empty = English"`
	Search       string             `json:"search" jsonschema:"available retrieval modes: text or vector,text"`
	Version      string             `json:"version,omitempty"`
	GraphSchema  int                `json:"graphSchema,omitempty" jsonschema:"schema version the graph records; entries are written at it"`
	BinarySchema int                `json:"binarySchema,omitempty" jsonschema:"schema version this build writes by default"`
	SecretRules  SecretRulesResult  `json:"secretRules" jsonschema:"the gitleaks rule file credential scans use"`
}

type SecretRulesResult struct {
	Source       string             `json:"source" jsonschema:"built-in or fetched"`
	Tag          string             `json:"tag" jsonschema:"the gitleaks release the rule file came from"`
	Update       string             `json:"update" jsonschema:"secrets.rules_update: daily or off"`
	Unused       string             `json:"unused,omitempty" jsonschema:"why a fetched copy on disk is not in use"`
	LastAccepted string             `json:"lastAccepted,omitempty" jsonschema:"when the fetched copy in use was accepted"`
	LastChecked  string             `json:"lastChecked,omitempty" jsonschema:"when the daily pass last found no newer release"`
	LastFailure  *SecretRulesFailed `json:"lastFailure,omitempty" jsonschema:"the last failed or rejected fetch"`
}

type SecretRulesFailed struct {
	At     string `json:"at"`
	Reason string `json:"reason"`
}

type RegistryArgs struct {
	Session string `json:"session,omitempty" jsonschema:"attached session's handle, from start_session or resume_session; required"`
	Class   string `json:"class,omitempty" jsonschema:"filter: predicate, query, or command"`
	Page    int    `json:"page,omitempty" jsonschema:"the page of the contracts to serve, from 1; next_page names the one after it"`
}

type RegistryFuncResult struct {
	Name   string   `json:"name"`
	Class  string   `json:"class"`
	Doc    string   `json:"doc"`
	Reads  []string `json:"reads,omitempty"`
	Writes []string `json:"writes,omitempty"`
}

type RegistryResult struct {
	ResultNotices
	Functions []RegistryFuncResult `json:"functions"`
	NextPage  int                  `json:"next_page,omitempty" jsonschema:"set while more contracts follow: pass it as page for them"`
}

func (s *toolSet) register(srv *mcp.Server) error {
	stageInput, err := jsonschema.For[stageAttachmentInput](&jsonschema.ForOptions{})
	if err != nil {
		return fmt.Errorf("stage_attachment input schema: %w", err)
	}
	srv.AddReceivingMiddleware(stageOneFile)
	addTool(srv, s, &mcp.Tool{
		Name: "start_session",
		Description: "Open a fresh dialogue session. One of the two doors, and the only tool that takes a " +
			"project. Auto-starts the session shell (user-dialogue by default). Returns its opening serve: " +
			"your orientation, the available moves, and the session handle. The handle is the dialogue's " +
			"identity and binds the session to one project. Retain it across context compaction. Pass it " +
			"to every other tool, reads included. Pass project when the composition serves several. " +
			"Without project, a sole accessible project is inferred. With several, the response lists them " +
			"(status project-required) and opens no session. Every call opens a new dialogue under a new " +
			"handle. To re-serve an existing one, present its handle to resume_session.",
	}, s.startSession)

	addTool(srv, s, &mcp.Tool{
		Name: "start_procedure",
		Description: "Start a procedure instance (a playbook move such as capture); session required. " +
			"Returns the current step's instructions, the report schema to answer with, and the goal " +
			"that advances it. The only path to graph writes: writes happen inside procedure " +
			"transitions, never through a direct tool.",
	}, s.startProcedure)

	addTool(srv, s, &mcp.Tool{
		Name: "next",
		Description: "Advance a procedure instance; session required. Send state fields per the served " +
			"report_schema, or answer a pending chooser with {chooser, choice, userWords?, reason?, " +
			"fields?}. User choosers must carry the user's answer, relayed verbatim, in userWords. " +
			"Exception: the served schema offers reason for a choice you decide yourself. Unfinished " +
			"operation: omit report and pass its exact retry_ref to continue, or cancel_ref to leave its " +
			"effects in place and return to the preceding interaction. Other transitions stay blocked " +
			"until the operation is retried or cancelled. When a move ends, the response carries the " +
			"session shell's serve: where the dialogue lands.",
	}, s.next)

	addTool(srv, s, &mcp.Tool{
		Name: "abandon",
		Description: "Abandon a running move instance (instance, plus session naming the session it belongs " +
			"to). Or tear down a session directly by handle (session alone): one call, no resume, no " +
			"framing. The response names the label and discarded threads. Nothing is cleaned up " +
			"implicitly: held WIP markers are surfaced and left standing for resume or grooming. The " +
			"session shell concludes through its own junction, never through abandon.",
	}, s.abandon)

	addTool(srv, s, &mcp.Tool{
		Name: "park",
		Description: "Park a running move back to the session junction; session required. State and step " +
			"position keep. The move lists as an open thread (at junctions and on conclude), and next " +
			"resumes it. Use it when the user shelves work mid-dialogue. A seeded draft then parks as a " +
			"graph-visible thread, not as an agent promise living in conversation memory.",
	}, s.park)

	addTool(srv, s, &mcp.Tool{
		Name: "resume_session",
		Description: "The second door, and the compaction escape. Re-serves the current position of the " +
			"session whose handle you present: its framing plus every running move at its current step, " +
			"each with the report schema to continue it. Presenting the handle is the whole authorization. " +
			"A handle reaches you only by issuance or by the user handing it over. Presenting it also " +
			"declares you need re-serving: every piece of the position serves in full. " +
			"Only recorded session state resumes (step position, collected fields, staged files), " +
			"never another conversation's context. A lost handle is the user's to recover, not yours to guess.",
	}, s.resumeSession)

	addTool(srv, s, &mcp.Tool{
		Name: "bind_branch",
		Description: "Declare or clear the attached session's durable branch binding. Pass exactly one of " +
			"branch or clear:true. Setting validates the live registered checkout before changing the " +
			"session; clearing needs no branch capability.",
	}, s.bindBranch)

	addTool(srv, s, &mcp.Tool{
		Name: "stage_attachment",
		Description: "Stage one or more files into the session scratch in one call; session required. Each " +
			"files item takes a name and exactly one of content, path and patches. Returns, per file in call " +
			"order, a handle and the sha256 of the staged bytes. Pass a handle in a report's attachments field, " +
			"or as a capture's bodyHandle when the file holds the entry body. Or amend what is already staged: " +
			"with patches, the file staged under name is edited in place through atomic search-replace pairs, " +
			"not re-staged whole. A path is read now and again before a playback confirms a body staged from it, never written. " +
			"Never a graph write: the write gate materializes " +
			"staged files with the entry.",
		InputSchema: stageInput,
	}, s.stageAttachment)

	addTool(srv, s, &mcp.Tool{
		Name: "search",
		Description: "Search graph entries: terms (text/regex), query (semantic phrase), or both (hybrid). " +
			"A free read; session required. No move needed, never blocked by procedure state. Runs in " +
			"that session's project and branch.",
	}, s.search)

	addTool(srv, s, &mcp.Tool{
		Name: "view",
		Description: "Run an sdd view layout pipeline over the graph: overview sections, topic counts, " +
			"ranked lists. A free read; session required.",
	}, s.view)

	addTool(srv, s, &mcp.Tool{
		Name: "show",
		Description: "Read entries in full with their upstream and downstream reference chains; session " +
			"required. Use whenever the dialogue touches a specific entry: summaries are pointers, not facts.",
	}, s.show)

	addTool(srv, s, &mcp.Tool{
		Name: "gather",
		Description: "Ground a draft before its capture, in one call; session required. Runs a keyword search " +
			"per term, the live listing of every draft topic, and a search by meaning where an embedding model " +
			"is set, with the draft's own text and your query; the ranking judge orders what they found. Name the running capture as instance: only " +
			"reads made through gather inside it ground the capture. Pass ids to open entries in full. " +
			"An entry already served in full in the same result prints as a pointer with its brief.",
	}, s.gather)

	addTool(srv, s, &mcp.Tool{
		Name: "read_attachment",
		Description: "Read an attachment's content, paged; session required. Either an entry's, by ID and " +
			"filename, or a file staged to that session before any entry carries it, by handle. Never " +
			"derive storage paths.",
	}, s.readAttachment)

	addTool(srv, s, &mcp.Tool{
		Name: "read_git",
		Description: "Read the project checkout's git history, read-only; session required. op file returns a " +
			"file at a revision, log the commits of a revision or range, commit one commit's message and changed " +
			"files, diff the change between two revisions. sdd builds the git command itself: no raw arguments.",
	}, s.readGit)

	addTool(srv, s, &mcp.Tool{
		Name: "read_value",
		Description: "Read one value a move of the session collected, whole, in pages where it runs past one " +
			"result. resume_session carries the small values and names the larger ones under collected_large; " +
			"this returns one of those.",
	}, s.readValue)

	addTool(srv, s, &mcp.Tool{
		Name:        "info",
		Description: "Session framing header; session required. Project, local participant, configured language, available search modes.",
	}, s.info)

	addTool(srv, s, &mcp.Tool{
		Name:        "registry",
		Description: "Engine function contracts (predicates, queries, commands) for procedure spec authors. Carries the session handle like every other tool.",
	}, s.registryDocs)

	addTool(srv, s, &mcp.Tool{
		Name: "sync_repos",
		Description: "Run the sync round of the connected repositories; session required. Each repository's " +
			"sdd-owned clone fetches, merges and publishes as far as the git gate admits it. Pass repos to " +
			"name them. Without repos, every connected repository syncs. Returns one outcome per repository.",
	}, s.syncRepos)
	return nil
}

// attachedSession returns the named session locked and replayed from its
// ledger, its call opened with the pieces it rereads and no answer.
func (s *toolSet) attachedSession(ctx context.Context, req *mcp.CallToolRequest, session string, reread []string) (*shellSession, error) {
	return s.attach(ctx, req, session, "", reread)
}

// advancingSession is attachedSession for a call that advances the session:
// it must carry an answer, and its response ends with a question.
func (s *toolSet) advancingSession(ctx context.Context, req *mcp.CallToolRequest, session string, args Answering) (*shellSession, error) {
	if strings.TrimSpace(args.Answer) == "" {
		return nil, toolError("answer is required: the words each question the last response ended with asks for, in their order and parted by ; (reach sdd ; 280), or lost when you no longer see a text or saw no question")
	}
	ss, err := s.attach(ctx, req, session, args.Answer, rereadList(args.Reread))
	if err != nil {
		return nil, err
	}
	askQuestion(ctx)
	return ss, nil
}

func (s *toolSet) attach(ctx context.Context, req *mcp.CallToolRequest, session, answer string, reread []string) (*shellSession, error) {
	id := sdd.SessionID(strings.TrimSpace(session))
	if id == "" {
		return nil, noHandleError()
	}
	ss := s.sessions.get(id)
	var fresh bool
	if ss == nil {
		workflow, err := s.app.RefreshWorkflowAllowingHooks(ctx, s.requestIdentity(req), id, newElicitingAllower(req))
		if err != nil {
			return nil, err
		}
		workflow.CutRepeats()
		candidate := &shellSession{root: workflow}
		candidate.mu.Lock()
		ss = s.sessions.put(candidate)
		fresh = ss == candidate
		if !fresh {
			candidate.mu.Unlock()
		}
	}
	if !fresh {
		ss.mu.Lock()
		workflow, err := s.app.RefreshWorkflowAllowingHooks(ctx, s.requestIdentity(req), id, newElicitingAllower(req))
		if err != nil {
			ss.mu.Unlock()
			return nil, err
		}
		workflow.CutRepeats()
		ss.root = workflow
	}
	ss.playbacks = nil
	ss.call = callKey(req)
	ss.repeatsPaged = ss.call != "" && ss.call == ss.pagedCall
	ss.pagedCall = ""
	s.noteSyncSession(ctx, req, ss)
	if ss.root.Finished() {
		ss.mu.Unlock()
		s.sessions.evict(id)
		return nil, toolError("session %s has ended — %s", session, sdd.NewSessionNote)
	}
	ss.root.BeginCall(answer, reread)
	return ss, nil
}

func mcpClientName(session *mcp.ServerSession) string {
	if session != nil {
		if params := session.InitializeParams(); params != nil && params.ClientInfo != nil {
			return params.ClientInfo.Name
		}
	}
	return ""
}

func mcpClientVersion(session *mcp.ServerSession) string {
	if session != nil {
		if params := session.InitializeParams(); params != nil && params.ClientInfo != nil {
			return params.ClientInfo.Version
		}
	}
	return ""
}

// noHandleError is the typed rejection for a tool called without a session
// handle. It names both doors and nothing else: handles are issued, never
// published, so no listing rides a rejection (d-cpt-aen).
func noHandleError() error {
	return toolError("no session handle — every tool carries one: start_session opens a fresh session and returns its handle, resume_session re-serves the session whose handle you present. A handle you no longer hold is the user's to recover.")
}

// startSession opens a fresh dialogue: a new durable session under a new
// handle, every call. Nothing is derived from the connection.
func (s *toolSet) startSession(ctx context.Context, req *mcp.CallToolRequest, args StartSessionArgs) (*mcp.CallToolResult, ServeResult, error) {
	identity := s.requestIdentity(req)
	// The door is the one tool that takes a project. Left empty, the
	// application infers a sole accessible project or reports the ambiguity,
	// which becomes the project listing (d-tac-1z6).
	project := sdd.ProjectID(strings.TrimSpace(args.Project))
	var (
		workflow *sdd.WorkflowSession
		serve    *sdd.WorkflowServe
	)
	err := s.app.RefuseOpen(ctx, identity, project, mcpClientName(req.Session))
	if err == nil {
		err = s.allowHooks(ctx, req, project)
	}
	if err == nil {
		workflow, serve, err = s.app.OpenWorkflow(ctx, identity, project, sdd.WorkflowOpenRequest{
			ClientName: mcpClientName(req.Session), ClientVersion: mcpClientVersion(req.Session),
			Shell: args.Shell, Label: args.Label, Link: sdd.SessionID(strings.TrimSpace(args.Link)),
		})
	}
	if err != nil {
		var appErr *sdd.ApplicationError
		if project == "" && errors.As(err, &appErr) && appErr.Code == sdd.ErrorProjectRequired {
			result, err := s.serveProjects(ctx, identity)
			return nil, result, err
		}
		return nil, ServeResult{}, err
	}
	workflow.CutRepeats()
	ss := s.sessions.put(&shellSession{root: workflow})
	s.noteSyncSession(ctx, req, ss)
	askQuestion(ctx)
	ss.mu.Lock()
	defer ss.mu.Unlock()
	lead := questionGuide + "\n\n"
	if notice := workflow.LinkNotice(); notice != "" {
		lead += notice + "\n\n"
	}
	guide, err := resultChars(lead)
	if err != nil {
		return nil, ServeResult{}, err
	}
	result, err := s.toRootServeResultWithin(ctx, req, ss, serve, serveBudget(ctx)-guide)
	if err != nil {
		return nil, ServeResult{}, err
	}
	result.Instructions = lead + result.Instructions
	return nil, result, nil
}

// serveProjects is start_session's answer when the principal reaches several
// projects and named none: no session opens, the projects are listed and the
// caller picks one.
func (s *toolSet) serveProjects(ctx context.Context, identity sdd.RequestIdentity) (ServeResult, error) {
	list, err := s.app.Projects(ctx, identity)
	if err != nil {
		return ServeResult{}, err
	}
	result := ServeResult{
		Status: StatusProjectRequired,
		Goal:   "choose a project, then call start_session again with project set",
		Instructions: "This composition serves several projects and none was named, so no session opened. " +
			"Ask the user which project the dialogue is about when it is not evident, then call " +
			"start_session with project set to its ID.",
	}
	for _, candidate := range list.Projects {
		if candidate.State != sdd.ProjectReady || !candidate.CanRead {
			continue
		}
		result.Projects = append(result.Projects, ProjectResult{ID: string(candidate.ID), DisplayName: candidate.DisplayName, CanWrite: candidate.CanWrite})
	}
	return result, nil
}

func (s *toolSet) startProcedure(ctx context.Context, req *mcp.CallToolRequest, args StartProcedureArgs) (*mcp.CallToolResult, ServeResult, error) {
	if strings.TrimSpace(args.Canonical) == "" {
		return nil, ServeResult{}, toolError("canonical is required")
	}
	ss, err := s.advancingSession(ctx, req, args.Session, args.Answering)
	if err != nil {
		return nil, ServeResult{}, err
	}
	defer ss.mu.Unlock()
	ss.root.SetCaller(args.Caller)
	identity := s.requestIdentity(req)
	serve, err := ss.root.Start(ctx, identity, sdd.WorkflowStartRequest{
		Canonical: args.Canonical, Params: args.Params, Label: args.Label, Parent: args.Parent,
		Project: sdd.ProjectID(strings.TrimSpace(args.Project)), ClientName: mcpClientName(req.Session),
	})
	if err != nil {
		return nil, ServeResult{}, err
	}
	result, err := s.toRootServeResult(ctx, req, ss, serve)
	return nil, result, err
}

func (s *toolSet) next(ctx context.Context, req *mcp.CallToolRequest, args NextArgs) (*mcp.CallToolResult, ServeResult, error) {
	ss, err := s.advancingSession(ctx, req, args.Session, args.Answering)
	if err != nil {
		return nil, ServeResult{}, err
	}
	defer ss.mu.Unlock()
	ss.root.SetCaller(args.Caller)
	if args.Instance == "" {
		return nil, ServeResult{}, toolError("instance is required")
	}
	identity := s.requestIdentity(req)
	ss.root.FollowDeferral(args.Instance)
	serve, err := ss.root.Advance(ctx, identity, sdd.WorkflowAdvanceRequest{Instance: args.Instance, Report: args.Report, Label: args.Label, RetryRef: args.RetryRef, CancelRef: args.CancelRef})
	if err != nil {
		return nil, ServeResult{}, err
	}
	if ss.root.Finished() {
		// The conclude serve is the dialogue's last; a finished session is
		// never re-served, so its replay leaves the cache with it.
		defer s.sessions.evict(ss.root.ID())
	}
	result, err := s.toRootServeResult(ctx, req, ss, serve)
	return nil, result, err
}

func (s *toolSet) abandon(ctx context.Context, req *mcp.CallToolRequest, args AbandonArgs) (*mcp.CallToolResult, AbandonResult, error) {
	if args.Instance == "" {
		if args.Session == "" {
			return nil, AbandonResult{}, toolError("pass instance (a move to abandon, with its session) or session alone (tear down a session by handle)")
		}
		return s.abandonSession(ctx, req, args)
	}
	ss, err := s.advancingSession(ctx, req, args.Session, args.Answering)
	if err != nil {
		return nil, AbandonResult{}, err
	}
	defer ss.mu.Unlock()
	ss.root.SetCaller(args.Caller)
	identity := s.requestIdentity(req)
	result, err := ss.root.Abandon(ctx, identity, args.Instance, args.Reason)
	if err != nil {
		return nil, AbandonResult{}, err
	}
	out := AbandonResult{Abandoned: result.Abandoned, HeldMarkers: result.HeldMarkers}
	if len(out.HeldMarkers) > 0 {
		out.Instructions = "The instance holds WIP markers, left standing by design. Tell the user: resume the work later or close the markers through grooming."
	}
	if result.Base != nil {
		around, err := resultChars(out)
		if err != nil {
			return nil, AbandonResult{}, err
		}
		base, err := s.toBaseServeResult(ctx, req, ss, result.Base, serveBudget(ctx)-around)
		if err != nil {
			return nil, AbandonResult{}, err
		}
		out.Base = base
	}
	return nil, out, nil
}

// park shelves a running move back to the shell junction: nothing about the
// instance changes — state, step, and evidence keep — but the shelving is
// logged (legible to a resuming agent) and the response lands the dialogue
// on the shell, with the move now listed among the open threads
// (d-tac-dbk). Seeded drafts ride the normal dispatch path; park adds no
// side channel.
func (s *toolSet) park(ctx context.Context, req *mcp.CallToolRequest, args ParkArgs) (*mcp.CallToolResult, ParkResult, error) {
	ss, err := s.advancingSession(ctx, req, args.Session, args.Answering)
	if err != nil {
		return nil, ParkResult{}, err
	}
	defer ss.mu.Unlock()
	ss.root.SetCaller(args.Caller)
	if args.Instance == "" {
		return nil, ParkResult{}, toolError("instance is required")
	}
	identity := s.requestIdentity(req)
	result, err := ss.root.Park(ctx, identity, args.Instance, args.Note)
	if err != nil {
		return nil, ParkResult{}, err
	}
	out := ParkResult{
		Parked: true, Instance: result.Instance, Procedure: result.Procedure, Step: result.Step,
		Instructions: "The move is parked with its state kept — it stays listed as an open thread and resumes through next. Tell the user it is recorded as open work, not forgotten.",
	}
	if result.Base != nil {
		around, err := resultChars(out)
		if err != nil {
			return nil, ParkResult{}, err
		}
		base, err := s.toBaseServeResult(ctx, req, ss, result.Base, serveBudget(ctx)-around)
		if err != nil {
			return nil, ParkResult{}, err
		}
		out.Base = base
	}
	return nil, out, nil
}

// abandonSession tears down a session by handle: no resume, no framing, one
// call (d-tac-dbk). The response names the label and every discarded thread —
// nothing vanishes silently. Holding the handle is the authorization
// (d-cpt-aen); a session this server holds replayed leaves the cache with it.
func (s *toolSet) abandonSession(ctx context.Context, req *mcp.CallToolRequest, args AbandonArgs) (*mcp.CallToolResult, AbandonResult, error) {
	identity := s.requestIdentity(req)
	id := sdd.SessionID(strings.TrimSpace(args.Session))
	if id == "" {
		return nil, AbandonResult{}, noHandleError()
	}
	if cached := s.sessions.get(id); cached != nil {
		cached.mu.Lock()
		defer cached.mu.Unlock()
		s.sessions.evict(id)
	}
	result, err := s.app.AbandonWorkflowSessionAsAgent(ctx, identity, sdd.WorkflowResumeRequest{
		SessionID: id, ClientName: mcpClientName(req.Session), ClientVersion: mcpClientVersion(req.Session),
	}, args.Reason)
	if err != nil {
		return nil, AbandonResult{}, err
	}
	out := AbandonResult{Abandoned: true, Session: string(result.Session), Label: result.Label, HeldMarkers: boundList(result.HeldMarkers, "held markers")}
	out.SyncNotice = result.SyncNotice
	for _, instance := range result.Discarded {
		out.DiscardedThreads = append(out.DiscardedThreads, instance.Procedure+" at "+instance.Step)
	}
	out.DiscardedThreads = boundList(out.DiscardedThreads, "discarded threads")
	if len(out.HeldMarkers) > 0 {
		out.Instructions = "The discarded threads hold WIP markers, left standing by design. Tell the user: resume the work later or close the markers through grooming."
	}
	return nil, out, nil
}

// resumeSession re-serves the current position of the session whose handle the
// caller presents — the reorientation. Presenting the handle declares the
// caller needs re-serving, so its call serves every piece in full (d-cpt-aen).
func (s *toolSet) resumeSession(ctx context.Context, req *mcp.CallToolRequest, args ResumeSessionArgs) (*mcp.CallToolResult, ResumeSessionResult, error) {
	identity := s.requestIdentity(req)
	if id := sdd.SessionID(strings.TrimSpace(args.Session)); id != "" {
		err := s.app.RefuseClient(ctx, identity, id, mcpClientName(req.Session))
		if err != nil {
			return nil, ResumeSessionResult{}, err
		}
	}
	ss, err := s.attachedSession(ctx, req, args.Session, nil)
	if err != nil {
		return nil, ResumeSessionResult{}, err
	}
	defer ss.mu.Unlock()
	ss.root.SetCaller(args.Caller)
	askQuestion(ctx)
	if err := ss.root.Reorient(ctx, identity); err != nil {
		return nil, ResumeSessionResult{}, err
	}
	var moves resumeMoves
	source, err := ss.root.ServeAllWithin(ctx, identity, func(head sdd.WorkflowResumeResult, serve sdd.WorkflowServe) (string, error) {
		return s.mapResumeMove(ctx, req, ss, &moves, head, serve)
	})
	if err != nil {
		return nil, ResumeSessionResult{}, err
	}
	mapped, err := s.mapRootResume(ctx, req, ss, source, moves)
	return nil, mapped, err
}

func (s *toolSet) bindBranch(ctx context.Context, req *mcp.CallToolRequest, args BindBranchArgs) (*mcp.CallToolResult, BindBranchResult, error) {
	if args.Branch != strings.TrimSpace(args.Branch) {
		return nil, BindBranchResult{}, toolError("branch must not have leading or trailing whitespace")
	}
	if (args.Branch != "") == args.Clear {
		return nil, BindBranchResult{}, toolError("pass exactly one of a nonblank branch or clear=true")
	}
	ss, err := s.attachedSession(ctx, req, args.Session, nil)
	if err != nil {
		return nil, BindBranchResult{}, err
	}
	defer ss.mu.Unlock()
	identity := s.requestIdentity(req)
	if err := ss.root.BindBranch(ctx, identity, args.Branch, args.Clear); err != nil {
		return nil, BindBranchResult{}, err
	}
	status := "bound"
	if args.Clear {
		status = "cleared"
	}
	return nil, BindBranchResult{Branch: ss.root.Branch(), Status: status}, nil
}

// ensureShellInstance re-derives the session's shell instance after replay
// or rebind, auto-starting the default shell when none is running — a
// resumed pre-shell session gains a base to land on (d-tac-bfc).

func (s *toolSet) stageAttachment(ctx context.Context, req *mcp.CallToolRequest, args StageAttachmentArgs) (*mcp.CallToolResult, StageAttachmentResult, error) {
	ss, err := s.attachedSession(ctx, req, args.Session, nil)
	if err != nil {
		return nil, StageAttachmentResult{}, err
	}
	defer ss.mu.Unlock()
	oneFile, _ := ctx.Value(oneFileCallKey{}).(bool)
	items := make([]sdd.StageItem, 0, len(args.Files))
	for _, file := range args.Files {
		item := sdd.StageItem{Name: file.Name, Path: file.Path}
		if file.Content != "" {
			item.Content = []byte(file.Content)
		}
		for _, p := range file.Patches {
			item.Patches = append(item.Patches, textpatch.Pair{Old: p.Old, New: p.New})
		}
		// A one-file call without a source stages an empty file; a files
		// item keeps the exactly-one-source rule the listing states.
		if oneFile && item.Path == "" && len(item.Patches) == 0 && item.Content == nil {
			item.Content = []byte{}
		}
		items = append(items, item)
	}
	staged, err := ss.root.StageAttachments(ctx, s.requestIdentity(req), items)
	if err != nil {
		return nil, StageAttachmentResult{}, err
	}
	files := make([]StagedFileResult, 0, len(staged.Files))
	for _, file := range staged.Files {
		files = append(files, StagedFileResult{Handle: file.Handle, SHA256: file.SHA256, Bound: file.Bound})
	}
	result := StageAttachmentResult{Files: &files}
	if dispatches := dispatchResults(staged.Dispatches); len(dispatches) > 0 {
		result.Dispatches = &dispatches
	}
	if oneFile && len(files) == 1 {
		result.Handle = files[0].Handle
	}
	return nil, result, nil
}

type oneFileCallKey struct{}

// stageOneFile takes a stage_attachment call in the one-file shape, name with
// content, path or patches beside session, as a files call of that one file.
// It runs ahead of the schema check, which knows files alone.
func stageOneFile(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		call, ok := req.(*mcp.CallToolRequest)
		if !ok || call.Params == nil || call.Params.Name != "stage_attachment" {
			return next(ctx, method, req)
		}
		var args map[string]json.RawMessage
		err := json.Unmarshal(call.Params.Arguments, &args)
		if err != nil {
			return next(ctx, method, req)
		}
		file := map[string]json.RawMessage{}
		for _, field := range []string{"name", "content", "path", "patches"} {
			value, present := args[field]
			if present {
				file[field] = value
				delete(args, field)
			}
		}
		if len(file) == 0 {
			return next(ctx, method, req)
		}
		if _, present := args["files"]; present {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "stage_attachment takes files: [{name, content | path | patches}, ...], " +
				"or one file's name with content, path or patches beside session, not both"}}}, nil
		}
		args["files"], err = json.Marshal([]map[string]json.RawMessage{file})
		if err != nil {
			return nil, err
		}
		call.Params.Arguments, err = json.Marshal(args)
		if err != nil {
			return nil, err
		}
		return next(context.WithValue(ctx, oneFileCallKey{}, true), method, req)
	}
}

// readScope is what a free read takes from its session: the project it runs
// in — the home project with the session's branch binding, or a project the
// caller names that the session may work in (d-tac-1z6, d-cpt-yjc).
func (s *toolSet) readScope(ctx context.Context, req *mcp.CallToolRequest, ss *shellSession, project string) (sdd.ProjectID, string, bool, error) {
	return ss.root.ReadScope(ctx, s.requestIdentity(req), sdd.ProjectID(strings.TrimSpace(project)))
}

// viewHint is the single producer of the view tool's Hint field: a pointer
// to the view-grammar fact, served once per session, as a read carries no
// answer to hold it by (s-prc-3kh). It never gates the read (s-cpt-1dz,
// d-cpt-h99).
func viewHint(ss *shellSession) string {
	return ss.root.ServeNotice("view hint", "view layout grammar: show "+basefacts.ViewGrammarFactID+
		" for the full filter/rank/macro vocabulary and the quoting rules")
}

func (s *toolSet) logRootRead(ctx context.Context, req *mcp.CallToolRequest, ss *shellSession, tool string, full, summary []string) error {
	return ss.root.LogRead(ctx, s.requestIdentity(req), tool, full, summary)
}

func (s *toolSet) search(ctx context.Context, req *mcp.CallToolRequest, args SearchArgs) (*mcp.CallToolResult, SearchResult, error) {
	if len(args.Terms) == 0 && args.Query == "" && args.Page < 2 {
		return nil, SearchResult{}, toolError("pass terms (text mode), query (vector mode), or both (hybrid)")
	}
	ss, err := s.attachedSession(ctx, req, args.Session, rereadList(args.Reread))
	if err != nil {
		return nil, SearchResult{}, err
	}
	defer ss.mu.Unlock()
	if args.Page > 1 {
		results, err := heldPage(ss, "search", args.Page)
		return nil, SearchResult{Results: results}, err
	}
	limit := args.Limit
	if limit == 0 {
		limit = defaultSearchHits
	}
	// Omitted means the shared default, so both surfaces show match evidence;
	// only an explicit 0 is headers-only (s-tac-rst).
	maxCitations := query.DefaultMaxCitationsPerEntry
	if args.MaxCitations != nil {
		maxCitations = *args.MaxCitations
	}
	project, branch, branchFromSession, err := s.readScope(ctx, req, ss, args.Project)
	if err != nil {
		return nil, SearchResult{}, err
	}
	result, err := s.app.Search(ctx, s.requestIdentity(req), project, args.request(s.searchSyncMode, limit, maxCitations, branch, branchFromSession))
	if err != nil {
		return nil, SearchResult{}, toolError("searching: %v", err)
	}
	if err := s.logRootRead(ctx, req, ss, "search", nil, result.EntryIDs); err != nil {
		return nil, SearchResult{}, err
	}
	out := presenters.SearchText(result.Hits, result.Notices, ss.repeats())
	if strings.TrimSpace(out) == "" {
		out = "(no entries matched — try another phrasing, or proceed if the topic is genuinely new)"
	}
	return nil, SearchResult{Results: pagedText(ctx, ss, "search", out), Coverage: result.Coverage, Notice: result.Notice}, nil

}

// request is the search tool's read. The read tools render what they serve
// with the session's repeats, so their requests are DataOnly.
func (a SearchArgs) request(mode sdd.SearchSyncMode, limit, maxCitations int, branch string, branchFromSession bool) sdd.SearchRequest {
	return sdd.SearchRequest{
		SyncMode: mode, IncludesRevision: a.IncludesRevision,
		Terms: a.Terms, Phrase: a.Query, Type: a.Type, Layer: a.Layer, Kind: a.Kind,
		IncludeSuperseded: a.IncludeSuperseded, Limit: limit, MaxCitations: maxCitations,
		Branch: branch, BranchFromSession: branchFromSession, Repos: a.Repos, AllRepos: a.AllRepos,
		DataOnly: true,
	}
}

func (a ViewArgs) request(branch string, branchFromSession bool) sdd.ViewRequest {
	return sdd.ViewRequest{
		Layout: a.Layout, Branch: branch, BranchFromSession: branchFromSession, Repos: a.Repos, AllRepos: a.AllRepos,
		DataOnly: true,
	}
}

func (a ShowArgs) request(up, down int, branch string, branchFromSession bool) sdd.ShowRequest {
	return sdd.ShowRequest{
		IDs: a.IDs, UpDepth: up, DownDepth: down, Branch: branch, BranchFromSession: branchFromSession,
		DataOnly: true,
	}
}

func (s *toolSet) view(ctx context.Context, req *mcp.CallToolRequest, args ViewArgs) (*mcp.CallToolResult, ViewResult, error) {
	if strings.TrimSpace(args.Layout) == "" && args.Page < 2 {
		return nil, ViewResult{}, toolError("layout is required")
	}
	ss, err := s.attachedSession(ctx, req, args.Session, rereadList(args.Reread))
	if err != nil {
		return nil, ViewResult{}, err
	}
	defer ss.mu.Unlock()
	if args.Page > 1 {
		sections, err := s.heldPageRead(ctx, req, ss, "view", args.Page)
		return nil, ViewResult{Sections: sections}, err
	}
	project, branch, branchFromSession, err := s.readScope(ctx, req, ss, args.Project)
	if err != nil {
		return nil, ViewResult{}, err
	}
	result, err := s.app.View(ctx, s.requestIdentity(req), project, args.request(branch, branchFromSession))
	if err != nil {
		return nil, ViewResult{}, toolError("viewing: %v", err)
	}
	hint := viewHint(ss)
	sections := presenters.ViewText(result.ViewSections, ss.repeats())
	if err := ss.flushServed(ctx, s.requestIdentity(req)); err != nil {
		return nil, ViewResult{}, err
	}
	// An empty result must never reach an agent as a blank string — it cannot
	// tell "matched nothing" from a broken call. Say so, and when a participant
	// filter came up empty (exact-match miss), name the participants the graph
	// knows. Any rendered content is kept below.
	if result.MatchedCount == 0 {
		msg := emptyViewMessage(result.KnownParticipants)
		if strings.TrimSpace(sections) == "" {
			sections = msg
		} else {
			sections = msg + "\n\n" + sections
		}
	}
	sections, err = s.pagedRead(ctx, req, ss, "view", sections, result.FullIDs, result.SummaryIDs)
	if err != nil {
		return nil, ViewResult{}, err
	}
	return nil, ViewResult{Sections: sections, Hint: hint}, nil

}

// emptyViewMessage is the explicit stand-in for an empty view result. It names
// the graph's known participants when a participant filter matched nothing so
// an exact-match miss reads as a wrong spelling, not absent data.
func emptyViewMessage(knownParticipants []string) string {
	msg := "0 entries matched the layout."
	if len(knownParticipants) > 0 {
		msg += " Known participants: " + strings.Join(knownParticipants, ", ") + "."
	}
	return msg
}

// maxLineListBytes caps the whole-line lists MCP responses carry (open
// threads, discarded threads, session rejections); cuts keep whole lines and
// name the remainder.
var maxLineListBytes = serveview.Default().Cap(serveview.PartLineList).MaxBytes

// maxResumeOpenInstances caps how many open instances a resume projection
// serves in full; the rest are named, each reachable through next.
const maxResumeOpenInstances = 8

// maxEndedValueChars bounds a produced value an ended move serves: the
// results a caller acts on, such as the entry written, fit; the move's
// working state does not.
const maxEndedValueChars = 200

// endedValues renders the values an ended move produced that fit
// maxEndedValueChars, and names the rest in order.
func endedValues(values map[string]any) (map[string]string, []string) {
	var out map[string]string
	var omitted []string
	for _, name := range slices.Sorted(maps.Keys(values)) {
		value := sdd.CollectedText(values[name])
		if len(value) > maxEndedValueChars {
			omitted = append(omitted, name)
			continue
		}
		if out == nil {
			out = map[string]string{}
		}
		out[name] = value
	}
	return out, omitted
}

func (s *toolSet) show(ctx context.Context, req *mcp.CallToolRequest, args ShowArgs) (*mcp.CallToolResult, ShowResult, error) {
	if len(args.IDs) == 0 && args.Page < 2 {
		return nil, ShowResult{}, toolError("ids is required")
	}
	ss, err := s.attachedSession(ctx, req, args.Session, rereadList(args.Reread))
	if err != nil {
		return nil, ShowResult{}, err
	}
	defer ss.mu.Unlock()
	if args.Page > 1 {
		entries, err := s.heldPageRead(ctx, req, ss, "show", args.Page)
		return nil, ShowResult{Entries: entries}, err
	}
	up := sdd.DefaultShowUpDepth
	if args.Up != nil {
		up = *args.Up
	}
	down := sdd.DefaultShowDownDepth
	if args.Down != nil {
		down = *args.Down
	}
	project, branch, branchFromSession, err := s.readScope(ctx, req, ss, args.Project)
	if err != nil {
		return nil, ShowResult{}, err
	}
	result, err := s.app.Show(ctx, s.requestIdentity(req), project, args.request(up, down, branch, branchFromSession))
	if err != nil {
		return nil, ShowResult{}, err
	}
	entries, err := presenters.ShowText(result.Items, ss.repeats())
	if err != nil {
		return nil, ShowResult{}, err
	}
	if err := ss.flushServed(ctx, s.requestIdentity(req)); err != nil {
		return nil, ShowResult{}, err
	}
	entries, err = s.pagedRead(ctx, req, ss, "show", entries, result.FullIDs, result.SummaryIDs)
	if err != nil {
		return nil, ShowResult{}, err
	}
	return nil, ShowResult{Entries: entries}, nil
}

func (s *toolSet) gather(ctx context.Context, req *mcp.CallToolRequest, args GatherArgs) (*mcp.CallToolResult, GatherResult, error) {
	ss, err := s.attachedSession(ctx, req, args.Session, rereadList(args.Reread))
	if err != nil {
		return nil, GatherResult{}, err
	}
	defer ss.mu.Unlock()
	instance := strings.TrimSpace(args.Instance)
	if args.Page > 1 && instance == "" {
		serve, err := heldPage(ss, "gather", args.Page)
		return nil, GatherResult{Serve: serve}, err
	}
	result, err := ss.root.Gather(ctx, s.requestIdentity(req), sdd.WorkflowGatherRequest{
		Instance: instance, IDs: args.IDs, Draft: args.Draft, Topics: args.Topics,
		Query: args.Query, Terms: args.Terms, Wait: args.Wait, Rejudge: args.Rejudge, Paged: true, Page: args.Page, SyncMode: s.searchSyncMode,
	})
	if err != nil {
		return nil, GatherResult{}, err
	}
	if err := ss.flushServed(ctx, s.requestIdentity(req)); err != nil {
		return nil, GatherResult{}, err
	}
	return nil, GatherResult{Serve: pagedText(ctx, ss, "gather", result.Text)}, nil
}

func (s *toolSet) readValue(ctx context.Context, req *mcp.CallToolRequest, args ReadValueArgs) (*mcp.CallToolResult, ReadValueResult, error) {
	ss, err := s.attachedSession(ctx, req, args.Session, nil)
	if err != nil {
		return nil, ReadValueResult{}, err
	}
	defer ss.mu.Unlock()
	if args.Page > 1 {
		value, err := heldPage(ss, "read_value", args.Page)
		return nil, ReadValueResult{Value: value}, err
	}
	value, err := ss.root.CollectedValue(strings.TrimSpace(args.Instance), strings.TrimSpace(args.Name))
	if err != nil {
		return nil, ReadValueResult{}, err
	}
	return nil, ReadValueResult{Value: pagedText(ctx, ss, "read_value", value)}, nil
}

func (s *toolSet) readGit(ctx context.Context, req *mcp.CallToolRequest, args ReadGitArgs) (*mcp.CallToolResult, ReadGitResult, error) {
	ss, err := s.attachedSession(ctx, req, args.Session, nil)
	if err != nil {
		return nil, ReadGitResult{}, err
	}
	defer ss.mu.Unlock()
	if args.Page > 1 {
		text, err := heldPage(ss, "read_git", args.Page)
		return nil, ReadGitResult{Text: text}, err
	}
	project, _, _, err := s.readScope(ctx, req, ss, args.Project)
	if err != nil {
		return nil, ReadGitResult{}, err
	}
	result, err := s.app.ReadGit(ctx, s.requestIdentity(req), project, sdd.GitReadRequest{
		Op: strings.TrimSpace(args.Op), Rev: args.Rev, From: args.From, To: args.To, Path: args.Path, Limit: args.Limit,
	})
	if err != nil {
		return nil, ReadGitResult{}, err
	}
	return nil, ReadGitResult{Text: pagedText(ctx, ss, "read_git", result.Text)}, nil
}

func (s *toolSet) readAttachment(ctx context.Context, req *mcp.CallToolRequest, args ReadAttachmentArgs) (*mcp.CallToolResult, ReadAttachmentResult, error) {
	maxBytes := args.MaxBytes
	if maxBytes == 0 {
		maxBytes = 65536
	}
	if args.Handle != "" && args.ID != "" {
		return nil, ReadAttachmentResult{}, toolError("pass id or handle, not both")
	}
	if args.Handle == "" && args.ID == "" {
		return nil, ReadAttachmentResult{}, toolError("id is required (or handle, for a staged file)")
	}
	ss, err := s.attachedSession(ctx, req, args.Session, nil)
	if err != nil {
		return nil, ReadAttachmentResult{}, err
	}
	defer ss.mu.Unlock()
	if args.Handle != "" {
		page, staged, err := ss.root.ReadStagedAttachment(ctx, s.requestIdentity(req), args.Handle, args.Offset, maxBytes)
		if err != nil {
			return nil, ReadAttachmentResult{}, err
		}
		return nil, boundedPage(ctx, ReadAttachmentResult{
			Name: page.Filename, Content: string(page.Content), Offset: page.Offset, NextOffset: page.NextOffset,
			TotalBytes: page.TotalSize, More: page.More, Available: staged,
		}), nil
	}
	project, branch, branchFromSession, err := s.readScope(ctx, req, ss, args.Project)
	if err != nil {
		return nil, ReadAttachmentResult{}, err
	}
	result, err := s.app.ReadAttachment(ctx, s.requestIdentity(req), project, sdd.ReadAttachmentRequest{Branch: branch, BranchFromSession: branchFromSession, EntryID: args.ID, Filename: args.Name, Offset: args.Offset, MaxBytes: maxBytes})
	if err != nil {
		return nil, ReadAttachmentResult{}, err
	}
	if err := s.logRootRead(ctx, req, ss, "read_attachment", nil, []string{args.ID}); err != nil {
		return nil, ReadAttachmentResult{}, err
	}
	page := result.Page
	output := ReadAttachmentResult{
		Name: page.Filename, Content: string(page.Content), Offset: page.Offset, NextOffset: page.NextOffset,
		TotalBytes: page.TotalSize, More: page.More, Available: result.Available,
	}
	if s.local {
		output.Path = page.LocalPath
	}

	return nil, boundedPage(ctx, output), nil

}

func (s *toolSet) info(ctx context.Context, req *mcp.CallToolRequest, args InfoArgs) (*mcp.CallToolResult, InfoResult, error) {
	ss, err := s.attachedSession(ctx, req, args.Session, nil)
	if err != nil {
		return nil, InfoResult{}, err
	}
	defer ss.mu.Unlock()
	project, branch, branchFromSession, err := s.readScope(ctx, req, ss, args.Project)
	if err != nil {
		return nil, InfoResult{}, err
	}
	info, err := s.app.Info(ctx, s.requestIdentity(req), project, sdd.InfoRequest{Branch: branch, BranchFromSession: branchFromSession})
	if err != nil {
		return nil, InfoResult{}, err
	}
	return nil, InfoResult{
		Project: string(info.Project.ID), Participant: info.Participant, Language: info.Language, Search: info.Search,
		Version: s.version, GraphSchema: info.GraphSchema, BinarySchema: info.BinarySchema,
		SyncBlocked: syncBlockedResult(info.SyncBlocked), SecretRules: secretRulesResult(info.SecretRules),
	}, nil

}

func secretRulesResult(rules sdd.SecretRules) SecretRulesResult {
	stamp := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.Format(time.RFC3339)
	}
	result := SecretRulesResult{
		Source: rules.Source, Tag: rules.Tag, Update: rules.Update, Unused: rules.Unused,
		LastAccepted: stamp(rules.LastAccepted), LastChecked: stamp(rules.LastChecked),
	}
	if !rules.LastFailureAt.IsZero() {
		result.LastFailure = &SecretRulesFailed{At: stamp(rules.LastFailureAt), Reason: rules.LastFailure}
	}
	return result
}

func (s *toolSet) registryDocs(ctx context.Context, req *mcp.CallToolRequest, args RegistryArgs) (*mcp.CallToolResult, RegistryResult, error) {
	ss, err := s.attachedSession(ctx, req, args.Session, nil)
	if err != nil {
		return nil, RegistryResult{}, err
	}
	ss.mu.Unlock()
	docs, err := sdd.WorkflowRegistryDocs(args.Class)
	if err != nil {
		return nil, RegistryResult{}, err
	}
	functions := make([]RegistryFuncResult, 0, len(docs))
	for _, doc := range docs {
		functions = append(functions, RegistryFuncResult{Name: doc.Name, Class: doc.Class, Doc: doc.Doc, Reads: doc.Reads, Writes: doc.Writes})
	}
	pages, err := pagedItems(ctx, functions)
	if err != nil {
		return nil, RegistryResult{}, err
	}
	page := max(args.Page, 1)
	if page > len(pages) {
		return nil, RegistryResult{}, toolError("page %d does not exist: the contracts fill %d", page, len(pages))
	}
	result := RegistryResult{Functions: pages[page-1]}
	if page < len(pages) {
		result.NextPage = page + 1
	}
	return nil, result, nil
}

// addTool registers handler behind toolBoundary. Unless outputSchema is set,
// the output type is erased to any so the SDK declares no outputSchema; the
// structured content is still marshalled from the returned value.
func addTool[In, Out any](srv *mcp.Server, s *toolSet, tool *mcp.Tool, handler func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error)) {
	bounded := toolBoundary(s, handler)
	if s.outputSchema {
		mcp.AddTool(srv, tool, bounded)
		return
	}
	mcp.AddTool(srv, tool, func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		return bounded(ctx, req, in)
	})
}

// toolBoundary is where an error meets the agent: a coded application error
// is rendered with its code leading the text, so a refusal is identified by
// its code rather than its prose (d-cpt-s2i). Uncoded errors pass unchanged.
func toolBoundary[In, Out any](s *toolSet, handler func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error)) func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		call, hostNotice, err := s.beforeCall(ctx)
		if err != nil {
			var none Out
			return nil, none, err
		}
		bound := model.ResultBoundFor(mcpClientName(req.Session), s.mcpOutputTokens)
		call = sdd.WithResultBound(call, int(bound))
		slot := &syncNoticeSlot{}
		res, out, err := handler(context.WithValue(call, syncNoticeKey{}, slot), req, in)
		if asked, ok := inputRequired(err); ok {
			var none Out
			return asked, none, nil
		}
		var coded *sdd.ApplicationError
		if err != nil && errors.As(err, &coded) && coded.Code != "" {
			err = fmt.Errorf("%s: %w", coded.Code, err)
		}
		var syncNotice, cacheNotice string
		if slot.claim != nil {
			var claimErr error
			syncNotice, claimErr = slot.claim(call, err != nil)
			err = errors.Join(err, claimErr)
		}
		if s.cacheNotice != nil {
			cacheNotice = s.cacheNotice(call)
		}
		if err != nil {
			for _, notice := range []string{syncNotice, cacheNotice, hostNotice} {
				if notice != "" {
					err = fmt.Errorf("%w\n\n%s", err, notice)
				}
			}
			return res, out, err
		}
		if carrier, ok := any(&out).(interface{ setSyncNotice(string) }); ok && slot.claim != nil {
			carrier.setSyncNotice(syncNotice)
		}
		if carrier, ok := any(&out).(interface{ setCacheNotice(string) }); ok {
			carrier.setCacheNotice(cacheNotice)
		}
		if carrier, ok := any(&out).(interface{ setHostNotice(string) }); ok {
			carrier.setHostNotice(hostNotice)
		}
		if carrier, ok := any(&out).(interface{ setQuestion(string) }); ok && slot.question != "" {
			carrier.setQuestion(slot.question)
		}
		return res, out, err
	}
}

func (s *toolSet) beforeCall(ctx context.Context) (context.Context, string, error) {
	if s.before == nil {
		return ctx, "", nil
	}
	call, notice, err := s.before(ctx)
	if call == nil {
		call = ctx
	}
	return call, notice, err
}

type syncNoticeKey struct{}

const questionsStandNotice = "This call answered no question: the questions the last response ended with still stand. Answer them again on your next call."

// syncNoticeSlot lets the call's session, once attached, name how its
// blocked-sync notice is claimed after the handler ran, so the notice
// reflects what the call itself recorded. With ask set, the claim of a call
// that succeeded leaves in question the line its response ends with. A
// failed call answers no question.
type syncNoticeSlot struct {
	claim    func(ctx context.Context, failed bool) (string, error)
	ask      bool
	question string
}

// askQuestion marks the call in flight as one whose response ends with a
// question: it starts, resumes or advances its session.
func askQuestion(ctx context.Context) {
	if slot, ok := ctx.Value(syncNoticeKey{}).(*syncNoticeSlot); ok {
		slot.ask = true
	}
}

func (s *toolSet) noteSyncSession(ctx context.Context, req *mcp.CallToolRequest, ss *shellSession) {
	slot, ok := ctx.Value(syncNoticeKey{}).(*syncNoticeSlot)
	if !ok {
		return
	}
	identity := s.requestIdentity(req)
	slot.claim = func(ctx context.Context, failed bool) (string, error) {
		ss.mu.Lock()
		defer ss.mu.Unlock()
		notice := ss.root.ServeNotice("sync notice", ss.root.SyncNotice(ctx, identity))
		if failed {
			if ss.root.QuestionsStand() {
				notice = strings.TrimPrefix(notice+"\n\n"+questionsStandNotice, "\n\n")
			}
			ss.root.DropAnswer()
		}
		err := ss.root.RecordPieces(ctx, identity)
		if err != nil || failed || !slot.ask {
			return notice, err
		}
		slot.question, err = ss.root.Question(ctx, identity)
		return notice, err
	}
}

func syncBlockedResult(block *sdd.SyncBlocked) *SyncBlockedResult {
	if block == nil {
		return nil
	}
	return &SyncBlockedResult{
		Kind: block.Kind, Since: block.Since.Format(time.RFC3339), Upstream: block.Upstream, Detail: block.Detail,
		Source: block.Source, Destination: block.Destination, Paths: block.Paths, Owner: block.Owner,
	}
}

// --- serve conversion --------------------------------------------------------

// serveResultBody converts the serve fields every landing shares —
// instructions with per-lane dedup, the first keep lanes of them, chooser,
// the first framing blocks of the framing, open threads; framing below zero
// composes none, resent points at lanes sent before (composeInstructions).
// Schema and vocabulary dedup live only in composeServe: a base mapping drops
// those fields, and recording bytes never delivered would stub them for a
// later serve that does deliver. It returns what it served.
func (s *toolSet) serveResultBody(ctx context.Context, req *mcp.CallToolRequest, ss *shellSession, serve *sdd.WorkflowServe, keep, framing int, resent bool, pageFull []string) (ServeResult, servedCount, error) {
	res := ServeResult{
		Session: string(serve.Session), Project: string(serve.Project),
		Branch: serve.Branch, Instance: serve.Instance, Procedure: serve.Procedure, Status: serve.Status,
		Step: serve.Step, Goal: serve.Goal, Missing: serve.Missing,
		ReportSchema: serve.ReportSchema, Execution: serve.Execution,
		PendingOperation: pendingOperationResult(serve.PendingOperation), Cancellation: cancellationResult(serve.Cancellation),
	}
	res.Collected, res.CollectedLarge = sdd.SplitCollected(serve.Collected)
	if parked := serve.Parked; parked != nil {
		res.Parked, res.Reason, res.Resume = true, parked.Reason, parked.Resume
	}
	var count servedCount
	res.Instructions, count.lanes = composeInstructions(ss, serve, keep, resent, pageFull)
	var omitted []string
	res.Produced, omitted = endedValues(serve.Produced)
	if len(omitted) > 0 {
		res.Instructions = strings.TrimPrefix(res.Instructions+"\n\n"+fmt.Sprintf("(produced leaves out the move's working state, kept in the session store: %s)", strings.Join(omitted, ", ")), "\n\n")
	}
	if serve.PendingChooser != nil {
		chooser := &ChooserResult{Chooser: serve.PendingChooser.Chooser, Kind: string(serve.PendingChooser.Kind)}
		for _, option := range serve.PendingChooser.Options {
			chooser.Options = append(chooser.Options, ChooserOptionResult{Choice: option.Choice, Collect: option.Collect})
		}
		res.PendingChooser = chooser
	}
	res.Dispatches = dispatchResults(serve.Dispatches)
	if serve.Waiting {
		return res, count, nil
	}
	if framing >= 0 {
		blocks, err := ss.root.FramingBlocksFor(ctx, s.requestIdentity(req), serve.Framing)
		if err != nil {
			return ServeResult{}, servedCount{}, err
		}
		composed := composeFraming(ss, blocks, framing)
		res.Framing, count.framing, count.blocks = composed.text, composed.fresh, composed.blocks
		if len(composed.waiting) > 0 {
			res.Instructions = strings.TrimPrefix(res.Instructions+"\n\n"+framingWaitLine(composed.waiting, keep < len(serve.InstructionLanes)), "\n\n")
		}
	}
	if ss.root.IsShell(serve.Instance) {
		res.OpenThreads = openThreadsRoot(ss)
	}
	return res, count, nil
}

// servedCount is what a composed serve holds: the lanes and framing blocks
// it served in full, the framing blocks its framing has, and whether its
// vocabulary table waits.
type servedCount struct {
	lanes, framing, blocks int
	vocabularyWaits        bool
}

// toRootServeResult converts an engine serve into the tool response, within
// the result bound, and records what it served in full into the session
// ledger. The shell's open-work block lists this dialogue's own threads only;
// other dialogues never appear.
func (s *toolSet) toRootServeResult(ctx context.Context, req *mcp.CallToolRequest, ss *shellSession, serve *sdd.WorkflowServe) (ServeResult, error) {
	return s.toRootServeResultWithin(ctx, req, ss, serve, serveBudget(ctx))
}

// toRootServeResultWithin is toRootServeResult within budget, for a result
// that carries more than the serve, as the opening carries the question guide.
func (s *toolSet) toRootServeResultWithin(ctx context.Context, req *mcp.CallToolRequest, ss *shellSession, serve *sdd.WorkflowServe, budget int) (ServeResult, error) {
	res, held, err := s.boundedServe(ctx, req, ss, serve, budget, true)
	if err != nil {
		return ServeResult{}, err
	}
	if err := recordDeferral(ss, serve, held, res); err != nil {
		return ServeResult{}, err
	}
	ss.notePlayback(serve, held.keep)
	ss.notePlayback(serve.Base, held.base)
	if err := ss.flushServed(ctx, s.requestIdentity(req)); err != nil {
		return ServeResult{}, err
	}
	return res, nil
}

// heldLanes is how much a bounded result holds: lanes of its serve and of the
// session shell's serve it lands in, framing blocks of its serve, of the
// blocks its framing has, whether its vocabulary table waits and whether
// lanes the result before sent serve as pointers whatever the call answers.
type heldLanes struct {
	keep, base, framing, blocks int
	vocabularyWaits, resent     bool
}

const (
	framingAll  = math.MaxInt
	framingNone = -1
)

// boundedServe composes serve's result to cost at most budget:
// while the whole would cost more, the last
// lanes of the session shell's serve it lands in wait first, then the lanes
// the result before sent serve as pointers whatever the call answers or
// rereads, then the step's own last lanes wait, then its last framing blocks,
// each named with the call that serves
// them, and what the larger composition served is withdrawn. With progress,
// a result leaving anything waiting serves at least one lane or framing block
// in full: where none fits beside the rest, the first not yet held serves
// alone past the bound, so a piece larger than one result never waits
// forever. A resume serves without it and without framing, which it carries
// once for every move, as a move whose lanes all wait resumes through next.
// It returns what the result holds; the caller records what it served and,
// through recordDeferral, what waits. The leading draft lane never waits
// (leadingLanes).
func (s *toolSet) boundedServe(ctx context.Context, req *mcp.CallToolRequest, ss *shellSession, serve *sdd.WorkflowServe, budget int, progress bool) (ServeResult, heldLanes, error) {
	ss.root.EndDeferral(serve.Instance)
	pageFull := ss.root.TakeStepFullIDs()
	mark := ss.root.ServeMark()
	compose := func(held heldLanes) (ServeResult, heldLanes, servedCount, bool, error) {
		res, count, err := s.composeServe(ctx, req, ss, serve, held, pageFull)
		if err != nil {
			return ServeResult{}, heldLanes{}, servedCount{}, false, err
		}
		cost, err := resultChars(res)
		if err != nil {
			return ServeResult{}, heldLanes{}, servedCount{}, false, err
		}
		held.blocks, held.vocabularyWaits = count.blocks, count.vocabularyWaits
		return res, held, count, cost <= budget, nil
	}
	lanes := len(serve.InstructionLanes)
	held := heldLanes{keep: lanes, framing: framingAll}
	if !progress {
		held.framing = framingNone
	}
	if serve.Base != nil {
		for held.base = len(serve.Base.InstructionLanes); held.base > leadingLanes(serve.Base); held.base-- {
			res, kept, _, fits, err := compose(held)
			if err != nil || fits {
				return res, kept, err
			}
			ss.root.WithdrawServedSince(mark)
		}
	}
	if res, kept, _, fits, err := compose(held); err != nil || fits {
		return res, kept, err
	}
	ss.root.WithdrawServedSince(mark)
	held.resent = true
	floor, alone := leadingLanes(serve), -1
	for held.keep = lanes; ; held.keep-- {
		res, kept, count, fits, err := compose(held)
		if err != nil {
			return ServeResult{}, heldLanes{}, err
		}
		if count.lanes > 0 {
			alone = held.keep
		}
		if fits && (held.keep == lanes || count.lanes > 0 || !progress) {
			return res, kept, nil
		}
		if held.keep == floor && !progress {
			return res, kept, nil
		}
		ss.root.WithdrawServedSince(mark)
		if held.keep == floor {
			break
		}
	}
	// No lanes fit beside the whole framing: the fewest lanes that serve one
	// in full, or all where every lane is held, stay, the framing's last
	// blocks wait and, where none fits beside the vocabulary, it waits too.
	held.keep = lanes
	if alone >= 0 {
		held.keep = alone
	}
	blocks := 0
	fitFraming := func() (ServeResult, heldLanes, bool, error) {
		for held.framing = framingAll; ; held.framing-- {
			res, kept, count, fits, err := compose(held)
			if err != nil {
				return ServeResult{}, heldLanes{}, false, err
			}
			if held.framing == framingAll {
				blocks = kept.blocks
				held.framing = blocks
			}
			if fits && (count.lanes > 0 || count.framing > 0) {
				return res, kept, true, nil
			}
			ss.root.WithdrawServedSince(mark)
			if held.framing == 0 {
				return ServeResult{}, heldLanes{}, false, nil
			}
		}
	}
	for _, vocabularyWaits := range []bool{false, true} {
		held.vocabularyWaits = vocabularyWaits
		res, kept, fits, err := fitFraming()
		if err != nil || fits {
			return res, kept, err
		}
	}
	if alone >= 0 {
		held.framing = 0
		res, kept, _, _, err := compose(held)
		return res, kept, err
	}
	for held.framing = 1; ; held.framing++ {
		res, kept, count, _, err := compose(held)
		if err != nil || count.framing > 0 || held.framing >= blocks {
			return res, kept, err
		}
		ss.root.WithdrawServedSince(mark)
	}
}

// leadingLanes counts the first lanes of serve that serve whatever the bound:
// its draft lane (types.DraftLane), served past the bound where it fills a
// result alone.
func leadingLanes(serve *sdd.WorkflowServe) int {
	if len(serve.InstructionLanes) > 0 && serve.InstructionLanes[0].Name == types.DraftLane {
		return 1
	}
	return 0
}

// recordDeferral records a serve, or the session shell's serve it lands in,
// that holds only its first lanes or framing blocks, so the next call on that
// instance serves what waits and points at what res, the result delivered,
// sent: lanes, framing, schema and entries alike, as any of them re-sent to
// an agent that answers lost would leave what waits no room.
func recordDeferral(ss *shellSession, serve *sdd.WorkflowServe, held heldLanes, res any) error {
	return recordDeferrals(ss, deferredInstances(serve, held), res)
}

// deferredInstances names the instances of serve whose serve, as held, left
// something waiting.
func deferredInstances(serve *sdd.WorkflowServe, held heldLanes) []string {
	var waiting []string
	if held.keep < len(serve.InstructionLanes) || held.framing >= 0 && held.framing < held.blocks || held.vocabularyWaits {
		waiting = append(waiting, serve.Instance)
	}
	if serve.Base != nil && held.base < len(serve.Base.InstructionLanes) {
		waiting = append(waiting, serve.Base.Instance)
	}
	return waiting
}

// recordDeferrals records a deferral of each instance against res, the
// result delivered.
func recordDeferrals(ss *shellSession, instances []string, res any) error {
	if len(instances) == 0 {
		return nil
	}
	delivered, err := json.Marshal(res)
	if err != nil {
		return fmt.Errorf("recording a deferral: %w", err)
	}
	for _, instance := range instances {
		ss.root.RecordDeferral(instance, string(delivered))
	}
	return nil
}

// composeServe composes serve's whole result with what held names, and
// returns what it served of serve's own.
func (s *toolSet) composeServe(ctx context.Context, req *mcp.CallToolRequest, ss *shellSession, serve *sdd.WorkflowServe, held heldLanes, pageFull []string) (ServeResult, servedCount, error) {
	res, count, err := s.serveResultBody(ctx, req, ss, serve, held.keep, held.framing, held.resent, pageFull)
	if err != nil {
		return ServeResult{}, servedCount{}, err
	}
	if len(serve.ReportSchema) > 0 {
		res.ReportSchema, err = serveReportSchema(ss, serve)
		if err != nil {
			return ServeResult{}, servedCount{}, err
		}
	}
	if serve.Waiting {
		return res, count, nil
	}
	vocabulary, err := s.app.Vocabulary(ctx, s.requestIdentity(req), ss.root.Project())
	if err != nil {
		return ServeResult{}, servedCount{}, err
	}
	vocabularyMark := ss.root.ServeMark()
	var vocabularyFull bool
	res.Vocabulary, vocabularyFull = serveVocabulary(ss, vocabulary)
	if held.vocabularyWaits && vocabularyFull {
		ss.root.WithdrawServedSince(vocabularyMark)
		res.Vocabulary, count.vocabularyWaits = "", true
		res.Instructions += "\n\n(the vocabulary table waits, as this result would run past its size bound. Call next with this instance and no report to have it before you act; it serves the step again)"
	}
	if serve.Base != nil {
		body, _, err := s.serveResultBody(ctx, req, ss, serve.Base, held.base, framingAll, false, nil)
		if err != nil {
			return ServeResult{}, servedCount{}, err
		}
		res.Base = rootBaseServe(body)
	}
	return res, count, nil
}

// toBaseServeResult maps a serve onto the base_junction landing, which
// carries no schema or vocabulary — so neither is recorded as served here.
// Its last lanes wait while it would cost more than budget, what a serve may
// take less the result around it, as boundedServe has them.
func (s *toolSet) toBaseServeResult(ctx context.Context, req *mcp.CallToolRequest, ss *shellSession, serve *sdd.WorkflowServe, budget int) (*BaseServe, error) {
	ss.root.EndDeferral(serve.Instance)
	pageFull := ss.root.TakeStepFullIDs()
	mark := ss.root.ServeMark()
	var base *BaseServe
	keep := len(serve.InstructionLanes)
	for ; ; keep-- {
		body, _, err := s.serveResultBody(ctx, req, ss, serve, keep, framingAll, false, pageFull)
		if err != nil {
			return nil, err
		}
		base = rootBaseServe(body)
		cost, err := resultChars(base)
		if err != nil {
			return nil, err
		}
		if cost <= budget || keep == leadingLanes(serve) {
			break
		}
		ss.root.WithdrawServedSince(mark)
	}
	if keep < len(serve.InstructionLanes) {
		if err := recordDeferrals(ss, []string{serve.Instance}, base); err != nil {
			return nil, err
		}
	}
	ss.notePlayback(serve, keep)
	if err := ss.flushServed(ctx, s.requestIdentity(req)); err != nil {
		return nil, err
	}
	return base, nil
}

func rootBaseServe(result ServeResult) *BaseServe {
	return &BaseServe{
		Session: result.Session, Branch: result.Branch, Instance: result.Instance, Procedure: result.Procedure, Status: result.Status,
		Step: result.Step, Goal: result.Goal, Instructions: result.Instructions, PendingChooser: result.PendingChooser,
		Framing: result.Framing, OpenThreads: result.OpenThreads,
	}
}

// resumeMoves is what a resume's moves mapped to as they rendered.
type resumeMoves struct {
	open              []ServeResult
	started           bool
	spent             int
	shell             string
	omitted, deferred []string
}

func resumeHeadResult(ss *shellSession, source sdd.WorkflowResumeResult) ResumeSessionResult {
	result := ResumeSessionResult{
		Session: string(source.Session), Project: string(ss.root.Project()),
		Participant: source.Participant, Label: source.Label, Branch: source.Branch,
		Instructions: resumeInstructionsFor(ss), PendingOperation: pendingOperationResult(source.PendingOperation), Cancellation: cancellationResult(source.Cancellation),
	}
	if ended := source.LastEnded; ended != nil {
		result.LastEndedMove = fmt.Sprintf("Last ended move: %s %s (%s)", ended.Procedure, ended.Instance, ended.Outcome)
	}
	if source.Instructions != "" {
		result.Instructions += "\n\n" + source.Instructions
	}
	return result
}

// mapResumeMove maps one move of a resume as it renders, the session shell
// last, which takes what room is left: its lanes wait sooner than a move is
// omitted. Whole-instance granularity is the only legitimate cut on a resume
// projection (d-tac-qwc): an instance past the count or the result bound is
// named instead. It returns the text the result carries of the move, empty
// for one omitted; ServeAllWithin withdraws what that text does not carry.
func (s *toolSet) mapResumeMove(ctx context.Context, req *mcp.CallToolRequest, ss *shellSession, moves *resumeMoves, head sdd.WorkflowResumeResult, serve sdd.WorkflowServe) (string, error) {
	if !moves.started {
		spent, err := resultChars(resumeHeadResult(ss, head))
		if err != nil {
			return "", err
		}
		moves.started, moves.spent = true, spent
	}
	if ss.root.IsShell(serve.Instance) {
		moves.shell = serve.Instance
	}
	// The instance holding an unfinished operation is the position the
	// resume exists to re-serve; it is never the one cut for size.
	pending := head.PendingOperation != nil && head.PendingOperation.Instance == serve.Instance
	name := fmt.Sprintf("%s (%s at %s)", serve.Instance, serve.Procedure, serve.Step)
	if !pending && len(moves.open) >= maxResumeOpenInstances {
		moves.omitted = append(moves.omitted, name)
		return "", nil
	}
	mapped, held, err := s.boundedServe(ctx, req, ss, &serve, serveBudget(ctx)-moves.spent, false)
	if err != nil {
		return "", err
	}
	mapped.Framing = ""
	cost, err := resultChars(mapped)
	if err != nil {
		return "", err
	}
	if !pending && len(moves.open) > 0 && moves.spent+cost > serveBudget(ctx) {
		moves.omitted = append(moves.omitted, name)
		return "", nil
	}
	moves.deferred = append(moves.deferred, deferredInstances(&serve, held)...)
	ss.notePlayback(&serve, held.keep)
	ss.notePlayback(serve.Base, held.base)
	moves.spent += cost
	moves.open = append(moves.open, mapped)
	delivered, err := json.Marshal(mapped)
	if err != nil {
		return "", fmt.Errorf("measuring a resumed move: %w", err)
	}
	return string(delivered), nil
}

func (s *toolSet) mapRootResume(ctx context.Context, req *mcp.CallToolRequest, ss *shellSession, source sdd.WorkflowResumeResult, moves resumeMoves) (ResumeSessionResult, error) {
	result := resumeHeadResult(ss, source)
	framing, err := ss.root.FramingBlocks(ctx, s.requestIdentity(req))
	if err != nil {
		return ResumeSessionResult{}, err
	}
	result.Open = moves.open
	omitted, deferred := moves.omitted, moves.deferred
	if len(omitted) > 0 {
		result.Instructions += fmt.Sprintf("\n\n%d open moves omitted here for size — each resumes at its current step through next: %s", len(omitted), strings.Join(omitted, "; "))
	}
	shell, err := resumeFraming(ctx, ss, moves.shell, framing, &result)
	if err != nil {
		return ResumeSessionResult{}, err
	}
	if shell != "" && !slices.Contains(deferred, shell) {
		deferred = append(deferred, shell)
	}
	if err := recordDeferrals(ss, deferred, result); err != nil {
		return ResumeSessionResult{}, err
	}
	if err := ss.flushServed(ctx, s.requestIdentity(req)); err != nil {
		return ResumeSessionResult{}, err
	}
	return result, nil
}

// resumeFraming composes the resume's framing into the room its moves leave:
// the last blocks that do not fit wait, named with the session shell's next
// call, which serves them. It returns the shell's instance where blocks wait.
func resumeFraming(ctx context.Context, ss *shellSession, shell string, framing []types.ServeLane, result *ResumeSessionResult) (string, error) {
	instructions := result.Instructions
	mark := ss.root.ServeMark()
	for keep := len(framing); ; keep-- {
		composed := composeFraming(ss, framing, keep)
		result.Framing, result.Instructions = composed.text, instructions
		if len(composed.waiting) > 0 {
			result.Instructions += fmt.Sprintf("\n\n(%d framing blocks wait, as this result would run past its size bound: %s. Call next with instance %s and no report to have them before you act)", len(composed.waiting), strings.Join(composed.waiting, ", "), shell)
		}
		cost, err := resultChars(result)
		if err != nil {
			return "", err
		}
		if cost <= serveBudget(ctx) || keep == 0 || shell == "" {
			if len(composed.waiting) == 0 {
				return "", nil
			}
			return shell, nil
		}
		ss.root.WithdrawServedSince(mark)
	}
}

// openThreadsRoot renders the session shell's open-work block: this dialogue's
// own open threads only. Other dialogues are never pushed onto any serve —
// handles are issued, never published (d-cpt-aen).
func openThreadsRoot(ss *shellSession) string {
	var lines []string
	for _, instance := range ss.root.OpenInstances() {
		lines = append(lines, fmt.Sprintf("- %s: %s at %s", instance.Instance, instance.Procedure, instance.Step))
	}
	if len(lines) == 0 {
		return ""
	}
	body := boundLines(lines, "open threads")
	// The one serve that reports dropped work states it in full — never stubbed by
	// a pointer, and never framed as continuations it no longer offers.
	if ss.root.Finished() {
		if ss.root.Autonomous() {
			return autonomousConcludedThreadsIntro + "\n" + body
		}
		return concludedThreadsIntro + "\n" + body
	}
	intro := openThreadsIntro
	if ss.root.Autonomous() {
		intro = autonomousOpenThreadsIntro
	}
	piece := ss.root.ServeText("open threads", intro, nil)
	if piece.Held {
		return openThreadsReminder + " " + heldLine([]string{"open threads"}) + "\n" + body
	}
	return piece.Text + "\n" + body
}

// resumeInstructionsFor leaves out briefing the user when the session is
// autonomous.
func resumeInstructionsFor(ss *shellSession) string {
	if ss.root.Autonomous() {
		return autonomousResumeInstructions + "\n\n" + questionGuide
	}
	return resumeInstructions + "\n\n" + questionGuide
}

// boundList cuts a whole-item string list at the line-list cap, appending a
// remainder item naming the drop.
func boundList(items []string, what string) []string {
	bounded := truncate.Items(items, func(s string) int { return len(s) + 1 }, maxLineListBytes, "")
	if bounded.Cut.Dropped > 0 {
		return append(bounded.Items, fmt.Sprintf("+%d more %s", bounded.Cut.Dropped, what))
	}
	return items
}

// boundLines cuts a whole-line list at the line-list cap and names the
// remainder — the shared shape for every line list an MCP response carries.
func boundLines(lines []string, remainder string) string {
	bounded := truncate.Items(lines, func(s string) int { return len(s) + 1 }, maxLineListBytes, "")
	body := strings.Join(bounded.Items, "\n")
	if bounded.Cut.Dropped > 0 {
		body += fmt.Sprintf("\n(+%d more %s)", bounded.Cut.Dropped, remainder)
	}
	return body
}
