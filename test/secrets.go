package test

import (
	"embed"
	"io/fs"
	"path"
	"testing"
)

//go:embed fixtures/secrets
var secretFixtures embed.FS

// SecretFixtures holds credential-shaped samples: positive/<rule-id>.txt must
// raise that rule, negative/<rule-id>.txt must not, clean.txt raises nothing.
// rules/ holds small gitleaks rule files for the loader. Every credential in
// it is synthetic.
func SecretFixtures() fs.FS {
	sub, err := fs.Sub(secretFixtures, "fixtures/secrets")
	if err != nil {
		panic(err)
	}
	return sub
}

// SecretRuleFixture reads the rule file rules/<name> of SecretFixtures.
func SecretRuleFixture(t testing.TB, name string) []byte {
	t.Helper()
	data, err := fs.ReadFile(SecretFixtures(), path.Join("rules", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// FixtureToken is a credential only the sdd-fixture-token rule of the rules/
// fixtures raises.
const FixtureToken = "sddfixture_k3j9x7q2m5n8p4r6"
