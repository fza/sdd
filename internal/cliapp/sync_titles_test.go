package cliapp_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/test"
)

// An explicit sync titles the untitled entries before it returns, under
// either sync mode (20261105-233807-d-tac-831).
func TestExplicitSyncBackfillsTitles(t *testing.T) {
	for _, tc := range []struct {
		mode string
		args []string
	}{
		{mode: "auto", args: []string{"sync"}},
		{mode: "manual", args: []string{"sync", "--pull"}},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			root := syncedProject(t, tc.mode)
			server := test.NewOpenAIServer(t, test.OpenAIBehaviour{Answer: `{"title": "backfilled title"}`, FinishReason: "stop"})
			writeFile(t, filepath.Join(root, ".sdd", "config.local.yaml"), "participant: Ada\nsync:\n  cooldown: 1ns\n  mode: "+tc.mode+
				"\nllm:\n  provider: openai\n  model: glm-5.3-flash\n  endpoint: "+server.URL+"\n  api_keys:\n    openai: op-notarealkeyaaaaaaaaaa\n  retry:\n    max_attempts: 1\n")
			entry := filepath.Join(".sdd", "graph", "2026", "11", "01-100000-s-tac-cli.md")
			writeFile(t, filepath.Join(root, entry), "---\ntype: signal\nlayer: tactical\nkind: gap\nsummary: An untitled entry.\n---\n\nBody.\n")
			gitIn(t, root, "add", entry)
			gitIn(t, root, "commit", "-q", "-m", "sdd: signal tactical gap")

			out, err := runAppErr(t, root, tc.args...)
			if err != nil {
				t.Fatalf("sync should succeed: %v\n%s", err, out)
			}
			if subject := strings.TrimSpace(gitIn(t, root, "log", "-1", "--format=%s")); subject != "sdd: summarize 1 entries" {
				t.Errorf("the sync should title the entry before it returns, got %q\n%s", subject, out)
			}
			if !strings.Contains(gitIn(t, root, "show", "HEAD:"+filepath.ToSlash(entry)), "title: Backfilled title") {
				t.Error("the committed entry should carry its title")
			}
		})
	}
}
