package presenters

import (
	"fmt"
	"io"
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
	return table.Flush()
}

// HookReviewText is what a person reads before confirming a hook, on the
// terminal and in an MCP client's prompt alike: the hook, every file its
// hash covers with its digest, and the head of its command file.
func HookReviewText(review model.HookReview) string {
	hook := review.Hook
	var b strings.Builder
	fmt.Fprintf(&b, "%s hook declared in %s (hooks[%d], %s layer)\n", hook.State, hook.Origin.ConfigPath, hook.Origin.Index, hook.Origin.Label())
	fmt.Fprintf(&b, "  event:    %s\n", hook.Event)
	fmt.Fprintf(&b, "  command:  %s\n", hookCommandLine(hook.HookConfig))
	fmt.Fprintf(&b, "  runs in:  %s\n", hook.Origin.Dir)
	if hook.Findings {
		b.WriteString("  findings: may report findings\n")
	}
	b.WriteString("  hashed files:\n")
	for _, file := range review.Files {
		fmt.Fprintf(&b, "    %s  %s (%s)\n", file.Digest, file.Path, file.Role)
	}
	fmt.Fprintf(&b, "  head of %s:\n", hook.Command)
	for _, line := range review.Head {
		fmt.Fprintf(&b, "    | %s\n", line)
	}
	return b.String()
}

func hookCommandLine(hook model.HookConfig) string {
	return strings.Join(append([]string{hook.Command}, hook.Args...), " ")
}
