package local_test

import (
	"path/filepath"
	"testing"

	"github.com/networkteam/sdd/pkg/local"
)

// A record is written in the repository's .sdd folder and committed alone under
// its own subject; a key never written reads as absent.
func TestGitTeamProofs_CommitsEachRecordOnItsOwn(t *testing.T) {
	repo := newGitRepository(t)
	repo.write(".sdd/graph/2026/01/01-000000-s-tac-aaa.md", "entry\n")
	repo.write("unrelated.txt", "staged elsewhere\n")
	repo.git("add", "unrelated.txt")
	proofs, err := local.NewGitTeamProofs(filepath.Join(repo.root, ".sdd"), filepath.Join(repo.root, ".sdd", "graph"))
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := proofs.ReadTeamProof(t.Context(), "chat-m"); err != nil || found {
		t.Fatalf("found %v, %v: no record is written yet", found, err)
	}
	if err := proofs.WriteTeamProof(t.Context(), "chat-m", []byte("{\"judge\":\"chat:m\"}\n")); err != nil {
		t.Fatal(err)
	}
	if got := repo.git("log", "-1", "--format=%s"); got != "sdd: proof chat-m" {
		t.Fatalf("subject %q: the record should be committed under its own subject", got)
	}
	if got := repo.git("show", "--name-only", "--format=", "HEAD"); got != ".sdd/proofs/chat-m.json" {
		t.Fatalf("commit holds %q: only the record, in the .sdd folder", got)
	}
	data, found, err := proofs.ReadTeamProof(t.Context(), "chat-m")
	if err != nil || !found || string(data) != "{\"judge\":\"chat:m\"}\n" {
		t.Fatalf("read %q, %v, %v: the written record should read back", data, found, err)
	}
	if err := proofs.WriteTeamProof(t.Context(), "chat-m", []byte("{\"judge\":\"chat:m\",\"graph_size\":2}\n")); err != nil {
		t.Fatal(err)
	}
	if got := repo.git("rev-list", "--count", "HEAD"); got != "3" {
		t.Fatalf("%s commits: each write is a commit of its own", got)
	}
}
