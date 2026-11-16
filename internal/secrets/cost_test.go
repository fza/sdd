package secrets_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/secrets"
)

// extraRules is n rules added to the built-in file, each built by rule from
// its index.
func extraRules(t *testing.T, n int, rule func(i int) string) []byte {
	t.Helper()
	builtin, err := secrets.BuiltinRuleFile()
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.Write(builtin.Data)
	for i := range n {
		b.WriteString(rule(i))
	}
	return []byte(b.String())
}

// Every scan pays for every rule a fetched copy adds, so a copy that would
// make scans much slower than the built-in file is refused.
func TestParseFetchedRulesBoundsTheScanCost(t *testing.T) {
	cases := []struct {
		name   string
		data   func(t *testing.T) []byte
		reason string
	}{
		{name: "more than twice the built-in rules", reason: "more than twice", data: func(t *testing.T) []byte {
			return extraRules(t, 300, func(i int) string {
				return fmt.Sprintf("\n[[rules]]\nid = \"sdd-extra-%d\"\nregex = '''sddextra%d_[a-z0-9]{16}'''\nkeywords = [\"sddextra%d_\"]\n", i, i, i)
			})
		}},
		{name: "rules without keywords", reason: "without keywords", data: func(t *testing.T) []byte {
			return extraRules(t, 3, func(i int) string {
				return fmt.Sprintf("\n[[rules]]\nid = \"sdd-extra-%d\"\nregex = '''sddextra%d_[a-z0-9]{16}'''\n", i, i)
			})
		}},
		{name: "rules slow on common text", reason: "takes", data: func(t *testing.T) []byte {
			return extraRules(t, 150, func(i int) string {
				return fmt.Sprintf("\n[[rules]]\nid = \"sdd-extra-%d\"\nregex = '''(?i)[a-z0-9]{8,200}q[a-z0-9 ]{4,300}zx%d'''\nkeywords = [\"ey\"]\n", i, i)
			})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := secrets.ParseFetchedRules(tc.data(t))

			if err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Errorf("the copy should be refused saying why, got %v", err)
			}
		})
	}
}
