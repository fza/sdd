package model_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestRelevanceConfigLayout(t *testing.T) {
	on := true
	cases := []struct {
		name   string
		config model.RelevanceConfig
		want   model.RelevanceLayout
	}{
		{"unset keeps the ledger local and commits all once turned on", model.RelevanceConfig{}, model.RelevanceLayout{Content: model.RelevanceContentAll}},
		{"commit with reader content", model.RelevanceConfig{Commit: &on, Content: model.RelevanceContentReader}, model.RelevanceLayout{Commit: true, Content: model.RelevanceContentReader}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.config.Layout()
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("layout %+v", got)
			}
		})
	}
	if _, err := (model.RelevanceConfig{Content: "scores"}).Layout(); err == nil {
		t.Fatal("an unknown content value should be refused")
	}
}
