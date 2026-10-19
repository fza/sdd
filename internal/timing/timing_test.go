package timing_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/networkteam/sdd/internal/timing"
)

type steppingClock struct {
	now  time.Time
	step time.Duration
}

func (c *steppingClock) Now() time.Time {
	current := c.now
	c.now = c.now.Add(c.step)
	return current
}

func TestStart(t *testing.T) {
	tests := []struct {
		name     string
		recorded bool
		want     []string
	}{
		{name: "context without a recorder", recorded: false, want: nil},
		{
			name:     "context with a recorder",
			recorded: true,
			want: []string{
				"timing: inner                          10.0ms  at     20.0ms",
				"timing: outer                          30.0ms  at     10.0ms",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			ctx := context.Background()
			if tt.recorded {
				clock := &steppingClock{now: time.Unix(0, 0), step: 10 * time.Millisecond}
				ctx = timing.WithRecorder(ctx, timing.NewRecorder(&out, clock.Now))
			}
			stopOuter := timing.Start(ctx, "outer")
			stopInner := timing.Start(ctx, "inner")
			stopInner()
			stopOuter()

			var got []string
			if out.Len() > 0 {
				got = strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
			}
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("phase lines should report duration and offset in end order\ngot  %q\nwant %q", got, tt.want)
			}
		})
	}
}
