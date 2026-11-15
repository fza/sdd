package model_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestIsRefusedConfigValue(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "unknown dialogue.mode", err: model.DialogueMode("autonomous").Validate(), want: true},
		{name: "unknown secrets.rules_update", err: model.SecretRulesUpdate("weekly").Validate(), want: true},
		{name: "wrapped with the file", err: fmt.Errorf("global config x.yaml: %w", model.DialogueMode("autonomous").Validate()), want: true},
		{name: "malformed YAML", err: errors.New("yaml: line 1: did not find expected node content")},
		{name: "no error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := model.IsRefusedConfigValue(tc.err)
			if got != tc.want {
				t.Errorf("only a refused value of a known key should count as one: %v", tc.err)
			}
		})
	}
}
