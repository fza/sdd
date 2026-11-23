package model_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestValidateEmbeddingPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path    string
		wantErr bool
	}{
		{path: ""},
		{path: "/v1/embeddings"},
		{path: "/v3/compat/embeddings"},
		{path: "v1/embeddings", wantErr: true},
		{path: "//gateway.example/v1/embeddings", wantErr: true},
		{path: "https://gateway.example/v1/embeddings", wantErr: true},
		{path: "/proxy?to=https://gateway.example", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			if err := model.ValidateEmbeddingPath(tc.path); (err != nil) != tc.wantErr {
				t.Errorf("validating the path should fail %v, got %v", tc.wantErr, err)
			}
		})
	}
}
