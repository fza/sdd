package cliapp

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/model"
	sdd "github.com/networkteam/sdd/pkg/application"
)

// sessionNewUnsupported are the sdd new flags the capture procedure has no
// report field for, or takes through its own steps.
var sessionNewUnsupported = []string{"topic", "actors", "when", "involvement", "attach", "summary", "title", "dry-run", "skip-preflight", "preflight-verified"}

// maxSessionNewSteps bounds the steps one sdd new --session answers.
const maxSessionNewSteps = 8

// newInSession writes ncmd through the session's capture, so the draft passes
// the gate the engine capture passes (20261109-004506-d-cpt-udz, D3): it is
// reported at assemble, the command stands for the user's confirm at
// playback, and the generated summary is printed for its user to check. A
// step the CLI cannot answer stops the run with its diagnostics; the session
// continues it.
func newInSession(ctx context.Context, cmd *cli.Command, ncmd *command.NewEntryCmd) error {
	for _, name := range sessionNewUnsupported {
		if cmd.IsSet(name) {
			return fmt.Errorf("--%s does not apply with --session: the capture takes no such field from sdd new", name)
		}
	}
	draft, err := sessionDraft(ncmd, cmd.String("widen-report"))
	if err != nil {
		return err
	}
	session, err := joinSession(ctx, cmd, "preflight-timeout")
	if err != nil {
		return err
	}
	instance, err := session.capture(cmd, true)
	if err != nil {
		return err
	}
	serve, err := session.workflow.Advance(ctx, session.identity, sdd.WorkflowAdvanceRequest{Instance: instance, Report: draft})
	if err != nil {
		return err
	}
	for range maxSessionNewSteps {
		var report map[string]any
		switch {
		case serve.Status != "running":
			return printSessionEntry(cmd, instance, serve)
		case serve.Step == "guideReview":
			printDiagnostics(cmd.ErrWriter, "writing guide", serve.Diagnostics)
			report = map[string]any{"chooser": "guideReview", "choice": "proceed"}
		case serve.Step == "playback":
			report = map[string]any{"chooser": "playback", "choice": "confirm", "userWords": "sdd new --session " + string(session.workflow.ID())}
		case serve.Step == "verifySummary":
			report = map[string]any{"chooser": "verifySummary", "choice": "faithful", "fields": map[string]any{"fidelityNote": "not reviewed by an agent: sdd new prints the summary for its user to check"}}
		default:
			return sessionStopped(cmd, instance, session, serve)
		}
		serve, err = session.workflow.Advance(ctx, session.identity, sdd.WorkflowAdvanceRequest{Instance: instance, Report: report})
		if err != nil {
			return err
		}
	}
	return fmt.Errorf("capture %s did not end within %d steps: continue it in session %s", instance, maxSessionNewSteps, session.workflow.ID())
}

// sessionDraft is ncmd as the capture's assemble report.
func sessionDraft(ncmd *command.NewEntryCmd, widenReport string) (map[string]any, error) {
	kind := ncmd.Kind
	if kind == "" {
		kind = model.DefaultKindForType(ncmd.Type)
	}
	if !model.IsValidKindForType(ncmd.Type, kind) {
		return nil, fmt.Errorf("kind %s is not a %s kind", kind, ncmd.Type)
	}
	report := map[string]any{"body": ncmd.Description, "entryKind": string(kind), "layer": string(ncmd.Layer), "confidence": ncmd.Confidence}
	if len(ncmd.Refs) > 0 {
		refs := make([]any, len(ncmd.Refs))
		for i, ref := range ncmd.Refs {
			refs[i] = map[string]any{"id": ref.ID, "kind": string(ref.Kind), "desc": ref.Desc}
		}
		report["refs"] = refs
	}
	lists := map[string][]string{"supersedes": ncmd.Supersedes, "closes": ncmd.Closes, "topics": ncmd.TopicLabels, "participants": ncmd.Participants, "aliases": ncmd.Aliases}
	for field, values := range lists {
		if len(values) == 0 {
			continue
		}
		list := make([]any, len(values))
		for i, value := range values {
			list[i] = value
		}
		report[field] = list
	}
	texts := map[string]string{"intent": ncmd.Intent, "canonical": ncmd.Canonical, "actorKind": ncmd.ActorKind, "class": ncmd.Class, "roleActor": ncmd.Actor, "widenReport": strings.TrimSpace(widenReport)}
	for field, value := range texts {
		if value != "" {
			report[field] = value
		}
	}
	if ncmd.Index != nil {
		report["index"] = map[string]any{"title": ncmd.Index.Title, "topic": ncmd.Index.Topic.String()}
	}
	if ncmd.SkipGrounding {
		report["skipGrounding"] = true
	}
	return report, nil
}

// printSessionEntry prints the written entry as plain sdd new does.
func printSessionEntry(cmd *cli.Command, instance string, serve *sdd.WorkflowServe) error {
	entryID, _ := serve.Produced["entryId"].(string)
	if serve.Status != "completed" || entryID == "" {
		return fmt.Errorf("capture %s ended %s without an entry", instance, serve.Status)
	}
	fmt.Fprintln(cmd.Writer, entryID+".md")
	dir, err := resolveGraphDir(cmd)
	if err != nil {
		return err
	}
	if rel, err := model.IDToRelPath(entryID); err == nil {
		fmt.Fprintf(cmd.Writer, "  → %s\n", filepath.Join(dir, rel))
	}
	g, err := loadGraph(cmd)
	if err != nil {
		return err
	}
	if entry := g.ByID[entryID]; entry != nil && entry.Summary != "" {
		fmt.Fprintf(cmd.Writer, "  Summary: %s\n", entry.Summary)
	}
	return nil
}

// sessionStopped reports the step the CLI does not answer and leaves the
// capture there for the session to continue.
func sessionStopped(cmd *cli.Command, instance string, session *cliSession, serve *sdd.WorkflowServe) error {
	printDiagnostics(cmd.ErrWriter, "capture "+instance, serve.Diagnostics)
	if len(serve.Missing) > 0 {
		fmt.Fprintf(cmd.ErrWriter, "capture %s: missing %s\n", instance, strings.Join(serve.Missing, ", "))
	}
	return fmt.Errorf("capture %s stopped at step %s: continue it in session %s", instance, serve.Step, session.workflow.ID())
}

func printDiagnostics(w io.Writer, source string, diagnostics []string) {
	for _, diagnostic := range diagnostics {
		fmt.Fprintf(w, "%s: %s\n", source, strings.TrimSpace(diagnostic))
	}
}
