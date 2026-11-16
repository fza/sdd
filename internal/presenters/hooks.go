package presenters

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/networkteam/sdd/internal/model"
)

// RenderHooks writes the `sdd hooks` table: each hook with its state, its
// layer, event, command and the file declaring it.
func RenderHooks(w io.Writer, hooks []model.Hook) error {
	if len(hooks) == 0 {
		_, err := fmt.Fprintln(w, "no hooks declared")
		return err
	}
	table := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "STATE\tLAYER\tEVENT\tCOMMAND\tDECLARED IN")
	for _, hook := range hooks {
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s: hooks[%d]\n", hook.State, hook.Origin.Label(), hook.Event, hookCommandLine(hook.HookConfig), hook.Origin.ConfigPath, hook.Origin.Index)
	}
	if err := table.Flush(); err != nil {
		return err
	}
	for _, notice := range hookNotices(hooks) {
		if _, err := fmt.Fprintln(w, notice); err != nil {
			return err
		}
	}
	return nil
}

func hookNotices(hooks []model.Hook) []string {
	var notices []string
	for _, hook := range hooks {
		if hook.Notice != "" && !slices.Contains(notices, hook.Notice) {
			notices = append(notices, hook.Notice)
		}
	}
	return notices
}

// HookReviewText is what a person reads before allowing a repository's
// hooks, on the terminal and in an MCP client's prompt alike: every hook it
// declares, every file the allow covers with its digest, and the head of
// each command file.
func HookReviewText(review model.HookReview) string {
	var b strings.Builder
	repo := review.Checkout
	if review.Repo != "" {
		repo += " (" + review.Repo + " repository)"
	}
	fmt.Fprintf(&b, "%s hooks of %s\n", review.State, repo)
	for _, hook := range review.Hooks {
		fmt.Fprintf(&b, "  %s %s: %s, declared in %s: hooks[%d]", hook.Origin.Layer, hook.Event, hookCommandLine(hook.HookConfig), hook.Origin.ConfigPath, hook.Origin.Index)
		if hook.Findings {
			b.WriteString(", may report findings")
		}
		b.WriteString("\n")
	}
	b.WriteString("  files git tracks in the hooks folder, which the allow covers:\n")
	if len(review.Files) == 0 {
		b.WriteString("    none\n")
	}
	for _, file := range review.Files {
		fmt.Fprintf(&b, "    %s  %s\n", file.Digest, file.Path)
	}
	shown := map[string]bool{}
	for _, hook := range review.Hooks {
		if shown[hook.Path] {
			continue
		}
		shown[hook.Path] = true
		fmt.Fprintf(&b, "  head of %s:\n", hook.Command)
		for _, line := range review.Heads[hook.Path] {
			fmt.Fprintf(&b, "    | %s\n", line)
		}
	}
	return b.String()
}

func hookCommandLine(hook model.HookConfig) string {
	return strings.Join(append([]string{hook.Command}, hook.Args...), " ")
}
