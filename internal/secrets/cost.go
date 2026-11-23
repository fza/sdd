package secrets

import (
	"fmt"
	"regexp/syntax"
	"strings"
)

// A scan runs each rule whose keywords occur in the text, at a cost that
// grows with the size of the rule's compiled pattern. A fetched rule file
// may exceed the built-in file's rule count, rules without keywords and
// total pattern size by at most maxRuleFactor, and the pattern size of rules
// ordinary text runs by at most maxRuleFactor plus a tenth of the built-in
// total. The bounds are counted, not timed, so they judge a copy alike on
// every machine.
const maxRuleFactor = 2

// ordinaryText stands for an entry body: which rules it runs is what most
// scans pay for.
var ordinaryText = strings.ToLower("They keep the key decision in the graph: every token of reasoning, " +
	"the options weighed and the one taken, so a reader a year later sees why. " +
	"Export the report, then check the rows without a date against the source.")

type scanCost struct {
	rules      int
	unfiltered int
	size       int
	ordinary   int
}

func costOf(set *RuleSet) (scanCost, error) {
	cost := scanCost{rules: len(set.rules)}
	for _, r := range set.rules {
		if r.Regex == "" {
			continue
		}
		size, err := patternSize(r.Regex)
		if err != nil {
			return scanCost{}, fmt.Errorf("rule %s: %w", r.ID, err)
		}
		cost.size += size
		runs := len(r.Keywords) == 0
		if runs {
			cost.unfiltered++
		}
		for _, keyword := range r.Keywords {
			if strings.Contains(ordinaryText, keyword) {
				runs = true
			}
		}
		if runs {
			cost.ordinary += size
		}
	}
	return cost, nil
}

func patternSize(pattern string) (int, error) {
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return 0, err
	}
	prog, err := syntax.Compile(re.Simplify())
	if err != nil {
		return 0, err
	}
	return len(prog.Inst), nil
}

// boundsTheScanCost refuses a set that would make scans much slower than
// the built-in set.
func boundsTheScanCost(set *RuleSet) error {
	builtin, err := builtinRules()
	if err != nil {
		return err
	}
	base, err := costOf(builtin)
	if err != nil {
		return err
	}
	cost, err := costOf(set)
	if err != nil {
		return err
	}
	if cost.rules > maxRuleFactor*base.rules {
		return fmt.Errorf("the rule file has %d rules, more than twice the built-in file's %d", cost.rules, base.rules)
	}
	allowed := max(maxRuleFactor*base.unfiltered, maxRuleFactor)
	if cost.unfiltered > allowed {
		return fmt.Errorf("the rule file has %d rules without keywords, which run on every text; at most %d are accepted", cost.unfiltered, allowed)
	}
	if cost.size > maxRuleFactor*base.size {
		return fmt.Errorf("the rule file's patterns compile to %d instructions, more than twice the built-in file's %d", cost.size, base.size)
	}
	allowed = maxRuleFactor*base.ordinary + base.size/10
	if cost.ordinary > allowed {
		return fmt.Errorf("the rule file's patterns that ordinary text runs compile to %d instructions, more than the %d accepted", cost.ordinary, allowed)
	}
	return nil
}
