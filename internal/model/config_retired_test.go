package model_test

import (
	"regexp"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

var graphEntryID = regexp.MustCompile(`\d{8}-\d{6}-[sad]-[a-z]{3}-[a-z0-9]+`)

func TestRetiredConfigKeys_ReasonsNameNoGraphEntry(t *testing.T) {
	for key, reason := range model.RetiredConfigKeys {
		t.Run(key, func(t *testing.T) {
			if graphEntryID.MatchString(reason) {
				t.Errorf("reason %q shown to users should not name a graph entry", reason)
			}
		})
	}
}
