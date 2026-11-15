package model

import (
	"fmt"
	"slices"
	"strings"
)

// LayerMeaning is what entries at one layer are about: a definition that
// tells the layer from its neighbours and an example, in English and German.
type LayerMeaning struct {
	Layer      Layer
	Abbrev     string
	Definition string
	Example    string
	German     LayerTerms
}

// LayerTerms is a layer's name, definition and example in one locale.
type LayerTerms struct {
	Name       string
	Definition string
	Example    string
}

var layerMeanings = []LayerMeaning{
	{
		Layer: LayerStrategic, Abbrev: "stg",
		Definition: "why the work exists and where it heads", Example: "serve small teams first",
		German: LayerTerms{Name: "strategisch", Definition: "warum es die Arbeit gibt und wohin sie geht", Example: "zuerst kleine Teams bedienen"},
	},
	{
		Layer: LayerConceptual, Abbrev: "cpt",
		Definition: "the approach, its core ideas and boundaries", Example: "entries are immutable",
		German: LayerTerms{Name: "konzeptionell", Definition: "der Ansatz, seine Kernideen und Grenzen", Example: "Einträge sind unveränderlich"},
	},
	{
		Layer: LayerTactical, Abbrev: "tac",
		Definition: "how a module or component behaves, its rules and structure", Example: "the parser rejects empty input",
		German: LayerTerms{Name: "taktisch", Definition: "wie sich ein Modul oder eine Komponente verhält, ihre Regeln und Struktur", Example: "der Parser lehnt leere Eingaben ab"},
	},
	{
		Layer: LayerOperational, Abbrev: "ops",
		Definition: "one work step or task", Example: "rename a flag, bump a dependency, run a migration",
		German: LayerTerms{Name: "operativ", Definition: "ein einzelner Arbeitsschritt oder Auftrag", Example: "ein Flag umbenennen, eine Abhängigkeit anheben, eine Migration ausführen"},
	},
	{
		Layer: LayerProcess, Abbrev: "prc",
		Definition: "how we work, conventions and review rules", Example: "every release gets a review",
		German: LayerTerms{Name: "Prozess", Definition: "wie wir arbeiten, Konventionen und Review-Regeln", Example: "jedes Release bekommt ein Review"},
	},
}

// LayerMeanings returns every layer's meaning, strategic first.
func LayerMeanings() []LayerMeaning {
	return slices.Clone(layerMeanings)
}

// LayerGuide renders each layer with its definition and example, one list
// item per layer.
func LayerGuide() string {
	lines := make([]string, len(layerMeanings))
	for i, m := range layerMeanings {
		lines[i] = fmt.Sprintf("- %s: %s (e.g. %s)", m.Layer, m.Definition, m.Example)
	}
	return strings.Join(lines, "\n")
}

// LayerGist renders each layer with its definition on one line.
func LayerGist() string {
	parts := make([]string, len(layerMeanings))
	for i, m := range layerMeanings {
		parts[i] = fmt.Sprintf("%s (%s)", m.Layer, m.Definition)
	}
	return strings.Join(parts, ", ")
}

func layerAbbrevs() map[Layer]string {
	abbrevs := make(map[Layer]string, len(layerMeanings))
	for _, m := range layerMeanings {
		abbrevs[m.Layer] = m.Abbrev
	}
	return abbrevs
}

func layersByAbbrev() map[string]Layer {
	layers := make(map[string]Layer, len(layerMeanings))
	for _, m := range layerMeanings {
		layers[m.Abbrev] = m.Layer
	}
	return layers
}
