package model_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestRepoIDsOverlap(t *testing.T) {
	tests := []struct {
		name string
		a, b model.RepoID
		want bool
	}{
		{"same id", "github.com/org/repo", "github.com/org/repo", true},
		{"case variant", "github.com/ORG/repo", "github.com/org/repo", true},
		{"parent of a clone", "github.com/org", "github.com/org/repo", true},
		{"inside a clone", "github.com/org/repo/.git", "github.com/org/repo", true},
		{"through a clone's file", "evil.example/a/l/x", "evil.example/a", true},
		{"shared name prefix only", "github.com/org/repository", "github.com/org/repo", false},
		{"sibling", "github.com/other/repo", "github.com/org/repo", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := model.RepoIDsOverlap(tt.a, tt.b); got != tt.want {
				t.Errorf("overlap of %q and %q = %v", tt.a, tt.b, got)
			}
			if got := model.RepoIDsOverlap(tt.b, tt.a); got != tt.want {
				t.Errorf("overlap of %q and %q = %v, overlap must be symmetric", tt.b, tt.a, got)
			}
		})
	}
}
