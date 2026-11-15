package secrets_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/secrets"
)

// A refusal names the part the user wrote, a frontmatter field by its key,
// and quotes the words before the credential so the user finds it.
func TestCheckEntryNamesThePartAndQuotesTheWordsBefore(t *testing.T) {
	credential := credentialSample(t)
	cases := []struct {
		name    string
		entry   model.Entry
		where   string
		passage string
	}{
		{name: "body", entry: model.Entry{Type: model.TypeSignal, Layer: model.LayerTactical, Kind: model.KindGap, Content: "Intro.\nRun " + credential}, where: "the body", passage: "Run export"},
		{name: "frontmatter field", entry: model.Entry{Type: model.TypeSignal, Layer: model.LayerTactical, Kind: model.KindGap, Content: "A clean body.", Participants: []string{credential}}, where: "the frontmatter field participants"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := secrets.CheckEntry(&tc.entry)

			if !errors.Is(err, secrets.ErrCredential) || !strings.Contains(err.Error(), tc.where+" looks like a credential") {
				t.Fatalf("the refusal should name the part: %v", err)
			}
			if strings.Contains(err.Error(), credential) {
				t.Error("the refusal must never repeat the credential")
			}
			if tc.passage == "" {
				return
			}
			if got := secrets.RefusedPassage(err); got != tc.passage {
				t.Errorf("the refusal should locate the words before the credential, got %q", got)
			}
			if !strings.Contains(err.Error(), "line 2, column 12") {
				t.Errorf("the refusal should give the credential's line and column in its part: %v", err)
			}
		})
	}
}
