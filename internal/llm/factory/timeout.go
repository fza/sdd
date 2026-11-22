package factory

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/networkteam/sdd/pkg/llm"
)

// bound is a call's timeout and what set it: the command-line flag named, or
// else the setting named.
type bound struct {
	timeout time.Duration
	setting string
	flag    string
}

func (b bound) hint() string {
	if b.flag != "" {
		return fmt.Sprintf("no answer within --%s (%s); raise it with `--%s <duration>`", b.flag, b.timeout, b.flag)
	}
	return fmt.Sprintf("no answer within %s (%s); raise it with `sdd config set %s <duration>`", b.setting, b.timeout, b.setting)
}

// namedTimeout names what set the bound of a call that ran out of it: the
// error otherwise reaches a CLI user or an MCP agent with no hint how to
// raise it. A deadline the caller set itself is passed through.
func namedTimeout(r llm.Runner, b bound) llm.Runner {
	return llm.RunnerFunc(func(ctx context.Context, req llm.Request) (llm.Result, error) {
		result, err := r.Run(ctx, req)
		if err != nil && ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
			return result, fmt.Errorf("%w: %s", err, b.hint())
		}
		return result, err
	})
}
