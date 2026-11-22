package cliapp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	sdd "github.com/networkteam/sdd/pkg/application"
)

// sessionsCmd is the authenticated listing the MCP surface deliberately lacks:
// handles are issued there, never published, so a lost handle is recovered
// here, where the OS user is the principal (d-cpt-aen).
func sessionsCmd() *cli.Command {
	return &cli.Command{
		Name:  "sessions",
		Usage: "List this project's dialogue sessions with open work; abandon stale ones",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "format", Usage: "Output format: text (default) or json", Value: "text"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			format := cmd.String("format")
			if format != "text" && format != "json" {
				return fmt.Errorf("--format: unsupported value %q (text or json)", format)
			}
			application, project, identity, err := buildLocalStoreApplication(ctx, cmd)
			if err != nil {
				return err
			}
			sessions, err := application.ListWorkflowSessions(ctx, identity, project)
			if err != nil {
				return err
			}
			if format == "json" {
				return renderSessionsJSON(cmd.Writer, sessions)
			}
			renderSessions(cmd.Writer, sessions, time.Now())
			return nil
		},
		Commands: []*cli.Command{sessionsAbandonCmd()},
	}
}

func sessionsAbandonCmd() *cli.Command {
	return &cli.Command{
		Name:      "abandon",
		Usage:     "Tear down a session by its ID: its open moves are discarded, held WIP markers are left standing",
		ArgsUsage: "<session-id>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "reason", Usage: "Reason recorded with the abandon"},
		},
		Action: withWriteGate(func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 1 {
				return fmt.Errorf("pass exactly one session ID (see `sdd sessions`)")
			}
			application, _, identity, err := buildLocalStoreApplication(ctx, cmd)
			if err != nil {
				return err
			}
			result, err := application.AbandonWorkflowSession(ctx, identity, sdd.WorkflowResumeRequest{
				SessionID: sdd.SessionID(strings.TrimSpace(cmd.Args().First())), ClientName: "sdd sessions abandon", Person: true,
			}, cmd.String("reason"))
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.Writer, "Abandoned %s", result.Session)
			if result.Label != "" {
				fmt.Fprintf(cmd.Writer, " %q", result.Label)
			}
			fmt.Fprintln(cmd.Writer)
			for _, instance := range result.Discarded {
				fmt.Fprintf(cmd.Writer, "  discarded: %s at %s\n", instance.Procedure, instance.Step)
			}
			for _, marker := range result.HeldMarkers {
				fmt.Fprintf(cmd.Writer, "  WIP marker left standing: %s\n", marker)
			}
			return nil
		}),
	}
}

// openSession is one open move of `sdd sessions --format json`.
type openSession struct {
	Session   string    `json:"session"`
	Procedure string    `json:"procedure"`
	Parked    bool      `json:"parked"`
	Step      string    `json:"step"`
	Reason    string    `json:"reason,omitempty"`
	Since     time.Time `json:"since"`
	// LateCatches is the session's sdd.WorkflowSessionSummary.LateCatches.
	LateCatches int `json:"lateCatches,omitempty"`
}

func renderSessionsJSON(writer io.Writer, sessions []sdd.WorkflowSessionSummary) error {
	open := []openSession{}
	for _, session := range sessions {
		for _, instance := range session.Open {
			item := openSession{Session: string(session.Session), Procedure: instance.Procedure, Step: instance.Step, Since: session.LastActivity, LateCatches: session.LateCatches}
			if parked := instance.Parked; parked != nil {
				item.Parked, item.Reason, item.Since = true, parked.Reason, parked.Since
			}
			open = append(open, item)
		}
	}
	enc := json.NewEncoder(writer)
	enc.SetIndent("", "  ")
	return enc.Encode(open)
}

func ago(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func renderSessions(writer io.Writer, sessions []sdd.WorkflowSessionSummary, now time.Time) {
	shown := 0
	for _, session := range sessions {
		if len(session.Open) == 0 {
			continue
		}
		shown++
		activity := "idle"
		if session.Active {
			activity = "active"
		}
		line := fmt.Sprintf("%s · %s · last %s", session.Session, activity, session.LastActivity.Local().Format(time.RFC3339))
		if session.Attachment != nil && session.Attachment.ClientName != "" {
			line += " · " + session.Attachment.ClientName
		}
		if session.Branch != "" {
			line += " · branch " + session.Branch
		}
		switch session.LateCatches {
		case 0:
		case 1:
			line += " · 1 compaction caught one call late"
		default:
			line += fmt.Sprintf(" · %d compactions caught one call late", session.LateCatches)
		}
		fmt.Fprintln(writer, line)
		if session.Label != "" {
			fmt.Fprintf(writer, "  %s\n", session.Label)
		}
		for _, instance := range session.Open {
			if parked := instance.Parked; parked != nil {
				fmt.Fprintf(writer, "  - %s: %s parked at %s · %s · %s\n", instance.Instance, instance.Procedure, parked.Step, ago(now.Sub(parked.Since)), parked.Reason)
				continue
			}
			fmt.Fprintf(writer, "  - %s: %s at %s\n", instance.Instance, instance.Procedure, instance.Step)
		}
	}
	if shown == 0 {
		fmt.Fprintln(writer, "No sessions with open work.")
	}
}
