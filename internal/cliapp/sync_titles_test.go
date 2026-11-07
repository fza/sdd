package cliapp_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/test"
)

func titlesLLMConfig(t *testing.T) string {
	t.Helper()
	server := test.NewOpenAIServer(t, test.OpenAIBehaviour{Answer: `{"title": "backfilled title"}`, FinishReason: "stop"})
	return "llm:\n  provider: openai\n  model: glm-5.3-flash\n  endpoint: " + server.URL + "\n  api_keys:\n    openai: op-notarealkeyaaaaaaaaaa\n  retry:\n    max_attempts: 1\n"
}

const untitledEntryContent = "---\ntype: signal\nlayer: tactical\nkind: gap\nsummary: An untitled entry.\n---\n\nBody.\n"

// An explicit sync titles the untitled entries before it returns, in a graph
// clone and in the code repository (20261105-233807-d-tac-831).
func TestExplicitSyncBackfillsTitles(t *testing.T) {
	t.Run("code repository", func(t *testing.T) {
		root := syncedProject(t, "")
		writeFile(t, filepath.Join(root, ".sdd", "config.local.yaml"), "participant: Ada\n"+titlesLLMConfig(t))
		entry := filepath.Join(".sdd", "graph", "2026", "11", "01-100000-s-tac-cli.md")
		writeFile(t, filepath.Join(root, entry), untitledEntryContent)
		gitIn(t, root, "add", entry)
		gitIn(t, root, "commit", "-q", "-m", "sdd: signal tactical gap")

		out, err := runAppErr(t, root, "sync", "--pull")
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
	t.Run("graph clone", func(t *testing.T) {
		code, remote := graphRepoCode(t)
		entry := ".sdd/graph/2026/11/01-100000-s-tac-cli.md"
		remote.Commit(t, "sdd: signal tactical gap", map[string]string{entry: untitledEntryContent})
		writeFile(t, filepath.Join(code, "xdg-config", "sdd", "config.yaml"), "participant: Ada\n"+titlesLLMConfig(t))

		out, err := runAppErr(t, code, "sync")
		if err != nil {
			t.Fatalf("sync should succeed: %v\n%s", err, out)
		}
		if subject := strings.TrimSpace(gitIn(t, cloneDir(t), "log", "-1", "--format=%s")); subject != "sdd: summarize 1 entries" {
			t.Errorf("the sync should title the entry before it returns, got %q\n%s", subject, out)
		}
		if !strings.Contains(gitIn(t, cloneDir(t), "show", "HEAD:"+entry), "title: Backfilled title") {
			t.Error("the committed entry should carry its title")
		}
	})
}
