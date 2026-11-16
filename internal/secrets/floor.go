package secrets

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// floor.json is generated from gitleaks' fixtures; see FloorSample.
//
//go:embed floor.json
var floorJSON []byte

// FloorSample is a credential a built-in rule flags, taken from gitleaks'
// own true positives. A fetched rule file must still flag every one: keeping
// a rule's ID is not enough, since a loosened pattern or a global allowlist
// can turn the rule off.
type FloorSample struct {
	Rule string `json:"rule"`
	Path string `json:"path,omitempty"`
	Text string `json:"text"`
}

var floorSamples = sync.OnceValues(func() ([]FloorSample, error) {
	var samples []FloorSample
	err := json.Unmarshal(floorJSON, &samples)
	if err != nil {
		return nil, fmt.Errorf("decoding the built-in rules' samples: %w", err)
	}
	return samples, nil
})

// keepsTheFloor refuses a set that no longer flags a sample of the built-in
// rules.
func keepsTheFloor(set *RuleSet) error {
	samples, err := floorSamples()
	if err != nil {
		return err
	}
	var missed []string
	for _, sample := range samples {
		var findings []Finding
		if sample.Path != "" {
			findings, err = set.ScanFile(sample.Path, []byte(sample.Text))
		} else {
			findings, err = set.Scan(sample.Text)
		}
		if err != nil {
			return err
		}
		flagged := slices.ContainsFunc(findings, func(f Finding) bool { return f.RuleID == sample.Rule })
		if !flagged {
			missed = append(missed, sample.Rule)
		}
	}
	if len(missed) > 0 {
		return fmt.Errorf("the rule file no longer flags the built-in samples of: %s", strings.Join(missed, ", "))
	}
	return nil
}
