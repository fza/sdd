package cliapp

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	sdd "github.com/networkteam/sdd/pkg/application"
)

// sessionFlags join a CLI read or write to an open dialogue session
// (20261109-004506-d-cpt-udz, D3): its reads log into the session, and gather,
// dismiss and new act on its capture.
func sessionFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "session", Usage: "Session ID of an open dialogue: reads log into it, and gather, dismiss and new act on its capture"},
		&cli.StringFlag{Name: "instance", Usage: "With --session: the capture instance to act on; default the session's one running capture"},
	}
}

// cliSessionClient names the CLI as the session's attached client.
const cliSessionClient = "sdd-cli"

// cliSession is a dialogue session the CLI joined.
type cliSession struct {
	workflow *sdd.WorkflowSession
	identity sdd.RequestIdentity
}

// joinSession resumes the session --session names over the working project's
// application, its LLM runner bounded by timeoutFlag; nil without the flag.
func joinSession(ctx context.Context, cmd *cli.Command, timeoutFlag string) (*cliSession, error) {
	if !sessionRequested(cmd) {
		return nil, instanceWithoutSession(cmd)
	}
	application, _, identity, err := workingApplicationTimed(ctx, cmd, timeoutFlag)
	if err != nil {
		return nil, err
	}
	return joinSessionOn(ctx, cmd, application, identity)
}

// joinSessionOn is joinSession over an application the command already built.
func joinSessionOn(ctx context.Context, cmd *cli.Command, application *sdd.Application, identity sdd.RequestIdentity) (*cliSession, error) {
	if !sessionRequested(cmd) {
		return nil, instanceWithoutSession(cmd)
	}
	id := sdd.SessionID(strings.TrimSpace(cmd.String("session")))
	workflow, _, err := application.ResumeWorkflow(ctx, identity, sdd.WorkflowResumeRequest{SessionID: id, ClientName: cliSessionClient, ClientVersion: cmd.Root().Version})
	if err != nil {
		return nil, fmt.Errorf("joining session %s: %w", id, err)
	}
	return &cliSession{workflow: workflow, identity: identity}, nil
}

func sessionRequested(cmd *cli.Command) bool {
	return strings.TrimSpace(cmd.String("session")) != ""
}

func instanceWithoutSession(cmd *cli.Command) error {
	if cmd.String("instance") != "" {
		return fmt.Errorf("--instance needs --session")
	}
	return nil
}

// logRead records a CLI read into the session, as the MCP read tools do. It
// credits no capture: only gather reads count for grounding (D31).
func (s *cliSession) logRead(ctx context.Context, tool string, full, summary []string) error {
	if s == nil {
		return nil
	}
	return s.workflow.LogRead(ctx, s.identity, tool, full, summary)
}

// capture returns the capture instance --instance names, else the session's
// one running capture. Without a running capture it returns "" unless
// required.
func (s *cliSession) capture(cmd *cli.Command, required bool) (string, error) {
	var captures []string
	for _, open := range s.workflow.OpenInstances() {
		if open.Procedure == "capture" {
			captures = append(captures, open.Instance)
		}
	}
	if named := strings.TrimSpace(cmd.String("instance")); named != "" {
		for _, instance := range captures {
			if instance == named {
				return named, nil
			}
		}
		return "", fmt.Errorf("session %s runs no capture %s (running: %s)", s.workflow.ID(), named, listOrNone(captures))
	}
	switch len(captures) {
	case 0:
		if !required {
			return "", nil
		}
		return "", fmt.Errorf("session %s runs no capture: start one (start_procedure capture) first", s.workflow.ID())
	case 1:
		return captures[0], nil
	}
	return "", fmt.Errorf("session %s runs %d captures (%s): name one with --instance", s.workflow.ID(), len(captures), strings.Join(captures, ", "))
}

func listOrNone(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}
