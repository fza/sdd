package llmstats

import (
	"context"

	"github.com/networkteam/sdd/pkg/llm"
)

// Fanout hands every call to each sink in turn; with no sinks it records
// nothing.
func Fanout(sinks ...llm.StatsSink) llm.StatsSink {
	return fanout(sinks)
}

type fanout []llm.StatsSink

func (sinks fanout) RecordCall(ctx context.Context, stat llm.CallStat) {
	for _, sink := range sinks {
		sink.RecordCall(ctx, stat)
	}
}
