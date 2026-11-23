package presenters_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/presenters"
)

func TestCachePruneRemovedNamesTheGateRecords(t *testing.T) {
	line := presenters.CachePruneRemoved(model.CachePruneCounts{Records: 2, GateRecords: 3})

	if !strings.Contains(line, "2 calibrations") || !strings.Contains(line, "3 gate records") {
		t.Errorf("the line should count calibrations and gate records apart, got %q", line)
	}
}
