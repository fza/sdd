package gitgate_test

import (
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/gitgate"
)

func demandAt(t *testing.T, setup *gateSetup, target gitgate.Target, at time.Time) gitgate.Demand {
	t.Helper()
	setup.clock.Set(at)
	demand, err := setup.gate().Demand(t.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	return demand
}

func leasedAt(t *testing.T, setup *gateSetup, target gitgate.Target, at time.Time) bool {
	t.Helper()
	setup.clock.Set(at)
	leased, err := setup.gate().Leased(t.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	return leased
}

// A lease that starts past the effective fetch cooldown asks for one sync,
// waited for at most read_wait; renewing a held lease asks for none.
func TestDemandStartsALease(t *testing.T) {
	cases := []struct {
		name   string
		limits func(*gitgate.Limits)
		clones int
		at     time.Duration
		sync   bool
	}{
		{name: "past the cooldown", at: 6 * time.Minute, sync: true},
		{name: "inside the cooldown", at: 4 * time.Minute},
		{name: "inside the effective cooldown", limits: func(l *gitgate.Limits) { l.SSHPerHour = 10 }, clones: 3, at: 6 * time.Minute},
		{name: "past the effective cooldown", limits: func(l *gitgate.Limits) { l.SSHPerHour = 10 }, clones: 3, at: 20 * time.Minute, sync: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setup := newSetup(t)
			if tc.limits != nil {
				tc.limits(&setup.limits)
			}
			setup.limits.ReadWait = 7 * time.Second
			target := setup.clone(t, "git@gitlab.com:group/project.git")
			for i := 1; i < tc.clones; i++ {
				setup.clone(t, "git@gitlab.com:group/other"+string(rune('a'+i))+".git")
			}
			mustRun(t, setup, t.Context(), target, gitgate.Fetch, t0)

			demand := demandAt(t, setup, target, t0.Add(tc.at))
			if demand.Sync != tc.sync {
				t.Errorf("a lease start should ask for a sync only past the effective fetch cooldown, got %+v", demand)
			}
			if demand.Wait != 7*time.Second || !demand.Fetched.Equal(t0) {
				t.Errorf("a demand should carry the read wait and when the clone last fetched, got %+v", demand)
			}
			if again := demandAt(t, setup, target, t0.Add(tc.at+time.Minute)); again.Sync {
				t.Error("renewing a held lease should ask for no sync")
			}
		})
	}
}

// A lease holds lease_pause after its last renewal, in every gate sharing
// the record; a renewal after it ended starts it again.
func TestLeaseEndsAfterThePause(t *testing.T) {
	setup := newSetup(t)
	target := setup.clone(t, "git@gitlab.com:group/project.git")
	if leasedAt(t, setup, target, t0) {
		t.Error("a repository nobody read should hold no lease")
	}
	demandAt(t, setup, target, t0)
	demandAt(t, setup, target, t0.Add(5*time.Minute))
	if !leasedAt(t, setup, target, t0.Add(15*time.Minute-time.Second)) {
		t.Error("a lease should hold until lease_pause after its last renewal")
	}
	if leasedAt(t, setup, target, t0.Add(15*time.Minute)) {
		t.Error("a lease should end lease_pause after its last renewal")
	}
	if !demandAt(t, setup, target, t0.Add(15*time.Minute)).Sync {
		t.Error("a renewal after the lease ended should start it, asking for a sync")
	}
}

// A clone of a local path is never limited: its lease start always asks for a
// sync.
func TestDemandOfALocalRemote(t *testing.T) {
	setup := newSetup(t)
	target := mustTarget(t, "/srv/graphs/project.git")
	if !demandAt(t, setup, target, t0).Sync {
		t.Error("a local remote's lease start should ask for a sync")
	}
	if !leasedAt(t, setup, target, t0.Add(time.Minute)) {
		t.Error("a local remote should hold its lease")
	}
}
