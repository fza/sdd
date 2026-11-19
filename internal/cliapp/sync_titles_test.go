package cliapp_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/networkteam/sdd/test"
)

func titlesLLM(t *testing.T) (config string, server *test.OpenAIServer) {
	t.Helper()
	server = test.NewOpenAIServer(t, test.OpenAIBehaviour{Answer: `{"title": "backfilled title"}`, FinishReason: "stop"})
	return "llm:\n  provider: openai\n  model: glm-5.3-flash\n  endpoint: " + server.URL + "\n  api_keys:\n    openai: op-notarealkeyaaaaaaaaaa\n  retry:\n    max_attempts: 1\n", server
}

const untitledEntryContent = "---\ntype: signal\nlayer: tactical\nkind: gap\nsummary: An untitled entry.\n---\n\nBody.\n"

const titlesBackfillOn = "titles:\n  backfill: true\n"

// An explicit sync titles the untitled entries before it returns where
// titles.backfill is on, in a graph clone and in the code repository;
// elsewhere it calls no model and commits nothing.
func TestExplicitSyncBackfillsTitlesOnlyWhereSwitchedOn(t *testing.T) {
	const entryCommit = "sdd: signal tactical gap"
	for _, tc := range []struct {
		name     string
		switched string
		subject  string
	}{
		{name: "switched on", switched: titlesBackfillOn, subject: "sdd: summarize 1 entries"},
		{name: "not switched on", subject: entryCommit},
	} {
		check := func(t *testing.T, repo, entry, out string, server *test.OpenAIServer) {
			t.Helper()
			titled := tc.switched != ""
			subject := strings.TrimSpace(gitIn(t, repo, "log", "-1", "--format=%s"))
			if subject != tc.subject {
				t.Errorf("only a sync with the switch on should commit titles, head is %q\n%s", subject, out)
			}
			if strings.Contains(gitIn(t, repo, "show", "HEAD:"+entry), "title: Backfilled title") != titled {
				t.Error("the committed entry should carry a title only with the switch on")
			}
			if (len(server.Requests()) > 0) != titled {
				t.Errorf("only a sync with the switch on should call the model, got %d calls", len(server.Requests()))
			}
		}
		t.Run(tc.name+" in the local config, code repository", func(t *testing.T) {
			root := syncedProject(t, "")
			llm, server := titlesLLM(t)
			writeFile(t, filepath.Join(root, ".sdd", "config.local.yaml"), "participant: Ada\n"+llm+tc.switched)
			entry := ".sdd/graph/2026/11/01-100000-s-tac-cli.md"
			writeFile(t, filepath.Join(root, filepath.FromSlash(entry)), untitledEntryContent)
			gitIn(t, root, "add", entry)
			gitIn(t, root, "commit", "-q", "-m", entryCommit)

			out, err := runAppErr(t, root, "sync", "--pull")
			if err != nil {
				t.Fatalf("sync should succeed: %v\n%s", err, out)
			}
			check(t, root, entry, out, server)
		})
		t.Run(tc.name+" in the committed config, graph clone", func(t *testing.T) {
			code, remote := graphRepoCode(t)
			entry := ".sdd/graph/2026/11/01-100000-s-tac-cli.md"
			remote.Commit(t, entryCommit, map[string]string{
				entry:              untitledEntryContent,
				".sdd/config.yaml": "graph_dir: .sdd/graph\nrepo_id: " + string(graphCloneRepoID) + "\ndefault_branch: main\nlanguage: en\nverify:\n  mode: off\n" + tc.switched,
			})
			llm, server := titlesLLM(t)
			writeFile(t, filepath.Join(code, "xdg-config", "sdd", "config.yaml"), "participant: Ada\n"+llm)

			out, err := runAppErr(t, code, "sync")
			if err != nil {
				t.Fatalf("sync should succeed: %v\n%s", err, out)
			}
			check(t, cloneDir(t), entry, out, server)
		})
	}
}
