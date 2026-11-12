package model_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestUserWordsOwed(t *testing.T) {
	cases := []struct {
		kind           model.Kind
		reason, intent bool
	}{
		{model.KindDirective, true, true},
		{model.KindPlan, true, false},
		{model.KindRole, true, false},
		{model.KindGap, false, false},
		{model.KindDone, false, false},
		{"", false, false},
	}
	for _, c := range cases {
		t.Run(string(c.kind), func(t *testing.T) {
			reason, intent := model.UserWordsOwed(c.kind)
			if reason != c.reason || intent != c.intent {
				t.Errorf("UserWordsOwed(%q) = (%v, %v), want (%v, %v)", c.kind, reason, intent, c.reason, c.intent)
			}
		})
	}
}
