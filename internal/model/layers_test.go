package model_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestLayerMeaningsCoverEveryLayer(t *testing.T) {
	layers := []model.Layer{model.LayerStrategic, model.LayerConceptual, model.LayerTactical, model.LayerOperational, model.LayerProcess}
	meanings := model.LayerMeanings()
	if len(meanings) != len(layers) {
		t.Fatalf("every layer should carry exactly one meaning, got %d meanings", len(meanings))
	}
	for i, m := range meanings {
		if m.Layer != layers[i] {
			t.Errorf("meaning %d should describe the layer in depth order, got %q", i, m.Layer)
		}
		for _, field := range []string{m.Abbrev, m.Definition, m.Example, m.German.Name, m.German.Definition, m.German.Example} {
			if strings.TrimSpace(field) == "" {
				t.Errorf("the %s meaning should fill every field, got %+v", m.Layer, m)
			}
		}
		if model.LayerAbbrev[m.Layer] != m.Abbrev || model.LayerFromAbbrev[m.Abbrev] != m.Layer {
			t.Errorf("the %s abbreviation should resolve both ways", m.Layer)
		}
		for _, rendered := range []string{model.LayerGuide(), model.LayerGist()} {
			if !strings.Contains(rendered, m.Definition) {
				t.Errorf("the rendered layers should carry the %s definition, got %q", m.Layer, rendered)
			}
		}
		if !strings.Contains(model.LayerGuide(), m.Example) {
			t.Errorf("the layer guide should carry the %s example", m.Layer)
		}
	}
}
