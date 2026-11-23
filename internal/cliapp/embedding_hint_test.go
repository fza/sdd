package cliapp_test

import (
	"strings"
	"testing"
)

// Without an embedding provider each command that needs one names the
// user-global config, where the configuration reference places
// embedding.provider.
func TestMissingEmbeddingProviderNamesTheUserGlobalConfig(t *testing.T) {
	for _, args := range [][]string{
		{"search", "--query", "alpha"},
		{"index"},
		{"index", "gc"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			f := newAppFixture(t)
			result := f.run(t, args...)
			if result.err == nil {
				t.Fatalf("the command should refuse without an embedding provider, stdout: %q", result.stdout)
			}
			message := result.err.Error()
			if !strings.Contains(message, "user-global config (`sdd config set embedding.provider <provider>`)") {
				t.Errorf("the refusal should name the user-global config and how to set it: %q", message)
			}
			if strings.Contains(message, "config.local.yaml") {
				t.Errorf("the refusal should not send the provider to the local file: %q", message)
			}
		})
	}
}
