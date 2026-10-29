package model_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestSelectRepos(t *testing.T) {
	closure := []model.RepoID{"example.test/a", "example.test/b"}
	tests := []struct {
		name    string
		named   []model.RepoID
		all     bool
		want    []model.RepoID
		wantErr error
	}{
		{name: "nothing selected"},
		{name: "all is the whole closure", all: true, want: closure},
		{name: "named in closure, once each", named: []model.RepoID{"example.test/b", "example.test/b"}, want: []model.RepoID{"example.test/b"}},
		{name: "named outside closure", named: []model.RepoID{"example.test/a", "example.test/connected"}, wantErr: model.ErrNotInClosure},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := model.SelectRepos(tt.named, tt.all, closure)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("selected = %v, want %v", got, tt.want)
			}
		})
	}
}
