package secrets

import (
	"fmt"
	"strings"
	"time"
)

// A fetched rule file may cost every scan at most about this much more than
// the built-in file: rule count, rules that run on any text, and the time to
// scan costCorpus.
const (
	maxRuleFactor     = 2
	maxScanTimeFactor = 4
	scanTimeSlack     = 20 * time.Millisecond
)

// costCorpus is the fixed input the scan-time bound measures: prose like an
// entry body, about 16 KB, holding no credential.
var costCorpus = strings.Repeat("They keep the key decision in the graph: every token of reasoning, "+
	"the options weighed and the one taken, so a reader a year later sees why.\n"+
	"Export the report, then check the rows without a date against the source.\n", 80)

// boundsTheScanCost refuses a set that would make every scan much slower than
// the built-in set: more than maxRuleFactor times its rules or its rules
// without keywords, or a scan of costCorpus taking more than
// maxScanTimeFactor times as long.
func boundsTheScanCost(set *RuleSet) error {
	builtin, err := builtinRules()
	if err != nil {
		return err
	}
	if len(set.rules) > maxRuleFactor*len(builtin.rules) {
		return fmt.Errorf("the rule file has %d rules, more than twice the built-in file's %d", len(set.rules), len(builtin.rules))
	}
	allowed := max(maxRuleFactor*unfiltered(builtin), maxRuleFactor)
	n := unfiltered(set)
	if n > allowed {
		return fmt.Errorf("the rule file has %d rules without keywords, which run on every text; at most %d are accepted", n, allowed)
	}
	builtinTime, err := scanTime(builtin, costCorpus)
	if err != nil {
		return err
	}
	fetchedTime, err := scanTime(set, costCorpus)
	if err != nil {
		return err
	}
	if fetchedTime > maxScanTimeFactor*builtinTime+scanTimeSlack {
		return fmt.Errorf("scanning the built-in samples takes %s with the rule file, %s with the built-in file", fetchedTime.Round(time.Millisecond), builtinTime.Round(time.Millisecond))
	}
	return nil
}

func unfiltered(set *RuleSet) int {
	n := 0
	for _, r := range set.rules {
		if r.Regex != "" && len(r.Keywords) == 0 {
			n++
		}
	}
	return n
}

// scanTime is the faster of two scans of text after a warm-up scan, which
// compiles what the set compiles lazily.
func scanTime(set *RuleSet, text string) (time.Duration, error) {
	_, err := set.Scan(text)
	if err != nil {
		return 0, err
	}
	best := time.Duration(0)
	for i := range 2 {
		start := time.Now()
		_, err = set.Scan(text)
		if err != nil {
			return 0, err
		}
		elapsed := time.Since(start)
		if i == 0 || elapsed < best {
			best = elapsed
		}
	}
	return best, nil
}
