package handlers_test

import (
	"context"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/command"
)

func TestConfigSetRefusesTheRetiredContributionKey(t *testing.T) {
	h, _, _ := newConfigHandler(t)

	err := h.ConfigSet(context.Background(), &command.ConfigSetCmd{Target: "local", Key: "contribution", Value: "origin/main"})

	if err == nil || !strings.Contains(err.Error(), "contribution is no longer used: contribution mode was removed") {
		t.Errorf("the retired contribution key should be refused with its reason, got %v", err)
	}
}
