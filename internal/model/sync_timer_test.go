package model_test

import (
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestPlanTimerRound(t *testing.T) {
	cases := []struct {
		name                      string
		leased, fetchDue, pushDue bool
		want                      model.TimerRound
	}{
		{name: "leased and due", leased: true, fetchDue: true, want: model.TimerRoundSync},
		{name: "leased and due with commits waiting", leased: true, fetchDue: true, pushDue: true, want: model.TimerRoundSync},
		{name: "leased inside the cooldown", leased: true, want: model.TimerRoundNone},
		{name: "fetch due without a lease", fetchDue: true, want: model.TimerRoundNone},
		{name: "commits waiting without a lease", fetchDue: true, pushDue: true, want: model.TimerRoundPush},
		{name: "commits waiting inside the fetch cooldown", leased: true, pushDue: true, want: model.TimerRoundPush},
		{name: "nothing due", want: model.TimerRoundNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := model.PlanTimerRound(tc.leased, tc.fetchDue, tc.pushDue); got != tc.want {
				t.Errorf("the timer should fetch only under a lease and push waiting commits regardless, got %q", got)
			}
		})
	}
}
