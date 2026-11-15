package proctest_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/baseprocedures"
	"github.com/networkteam/sdd/internal/model"
	"github.com/networkteam/sdd/internal/proctest"
)

func TestCaptureServesTheLayerMeanings(t *testing.T) {
	world := proctest.NewWorld(t)
	session := world.Open(t, "layers-capture")
	session.Start(t, "capture", nil)
	// The second capture no longer carries the overview fact, so what it
	// serves comes from its own text.
	serve := session.Start(t, "capture", nil)
	proctest.RequireStep(t, serve, "assemble")
	if !strings.Contains(serve.Instructions, "parser rejects empty input") {
		t.Fatalf("assemble should tell tactical from operational by example, got:\n%s", serve.Instructions)
	}
	if !strings.Contains(serve.Instructions, model.LayerGuide()) {
		t.Fatalf("assemble should list the model's layer meanings, got:\n%s", serve.Instructions)
	}
}

func TestUserDialogueOpeningRendersLayerMeaningsFromModel(t *testing.T) {
	world := proctest.NewWorld(t)
	_, serve := openShell(t, world, "dlg-layers")
	if !strings.Contains(serve.Instructions, model.LayerGist()) {
		t.Fatalf("opening should name the model's layer meanings, got:\n%s", serve.Instructions)
	}
}

func TestShippedProceduresHardCodeNoLayerMeaning(t *testing.T) {
	entries, err := baseprocedures.Entries()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		for _, m := range model.LayerMeanings() {
			if strings.Contains(entry.Content, m.Definition) {
				t.Errorf("%s hard-codes the %s definition instead of rendering it", entry.Canonical, m.Layer)
			}
		}
	}
}
