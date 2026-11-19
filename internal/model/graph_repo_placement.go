package model

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// KeyPlace is the config file a setting belongs in, judged by the layers sdd
// honors it in.
type KeyPlace int

const (
	// PlaceUnknown is a key sdd does not know.
	PlaceUnknown KeyPlace = iota
	// PlaceRetired is a key sdd no longer uses.
	PlaceRetired
	// PlaceGraphRepo is the graph repository's committed config: a property
	// of the repository, on no user-global config.
	PlaceGraphRepo
	// PlaceTeamOrPerson is the graph repository's committed config for the
	// team, or a person's own config.
	PlaceTeamOrPerson
	// PlacePerson is a person's own config only: config.local.yaml or the
	// user-global config.
	PlacePerson
	// PlaceUserGlobal is the user-global config only.
	PlaceUserGlobal
)

// KeyPlacement is where a setting belongs; Reason says what replaced a
// PlaceRetired key.
type KeyPlacement struct {
	Place  KeyPlace
	Reason string
}

// MisplacedKey is a setting a committed config beside graph_repo carries
// that belongs elsewhere. Key is the top-level key, or, when its settings
// belong in different places, the dotted settings of one place.
type MisplacedKey struct {
	Key       string
	Placement KeyPlacement
}

// GraphRepoMisplacedKeys returns the keys of a committed config beside
// graph_repo other than GraphRepoPointerKeys and IgnoredPointerKeys, in file
// order, each with where it belongs.
func GraphRepoMisplacedKeys(data []byte) ([]MisplacedKey, error) {
	var root yaml.Node
	err := yaml.Unmarshal(data, &root)
	if err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil, nil
	}
	known := map[string]bool{}
	for _, leaf := range collectConfigLeaves(reflect.TypeFor[PerRepoConfig](), nil, nil) {
		known[leaf.path] = true
	}
	branches := configBranches(known)

	var misplaced []MisplacedKey
	mapping := root.Content[0]
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key := mapping.Content[i].Value
		if slices.Contains(GraphRepoPointerKeys, key) || slices.Contains(IgnoredPointerKeys, key) {
			continue
		}
		var settings []placedSetting
		err = placeSettings([]string{key}, mapping.Content[i+1], known, branches, &settings)
		if err != nil {
			return nil, err
		}
		misplaced = append(misplaced, groupByPlacement(key, settings)...)
	}
	return misplaced, nil
}

type placedSetting struct {
	path      string
	placement KeyPlacement
}

func placeSettings(path []string, value *yaml.Node, known, branches map[string]bool, out *[]placedSetting) error {
	dotted := strings.Join(path, ".")
	if reason, retired := RetiredConfigKeys[dotted]; retired {
		*out = append(*out, placedSetting{path: dotted, placement: KeyPlacement{Place: PlaceRetired, Reason: reason}})
		return nil
	}
	if !known[dotted] && branches[dotted] && value.Kind == yaml.MappingNode && len(value.Content) > 0 {
		for i := 0; i+1 < len(value.Content); i += 2 {
			err := placeSettings(append(slices.Clone(path), value.Content[i].Value), value.Content[i+1], known, branches, out)
			if err != nil {
				return err
			}
		}
		return nil
	}
	if !KnownYAMLKey(dotted, reflect.TypeFor[PerRepoConfig]()) {
		*out = append(*out, placedSetting{path: dotted, placement: KeyPlacement{Place: PlaceUnknown}})
		return nil
	}
	place, err := settingPlace(path, value)
	if err != nil {
		return err
	}
	*out = append(*out, placedSetting{path: dotted, placement: KeyPlacement{Place: place}})
	return nil
}

// settingPlace asks the layer rules (IgnoredSettings and the user-global
// schema) which layers keep a config holding the one setting at path. HonoredCommitted and HonoredLocal drop nothing that changes the
// answer: the rest of what they drop is retired or on no user-global config.
func settingPlace(path []string, value *yaml.Node) (KeyPlace, error) {
	doc := value
	for i := len(path) - 1; i >= 0; i-- {
		doc = &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{{Kind: yaml.ScalarNode, Value: path[i]}, doc}}
	}
	data, err := yaml.Marshal(doc)
	if err != nil {
		return PlaceUnknown, fmt.Errorf("placing %s: %w", strings.Join(path, "."), err)
	}
	cfg := &PerRepoConfig{}
	err = UnmarshalYAML(data, cfg)
	if err != nil {
		return PlaceUnknown, fmt.Errorf("placing %s: %w", strings.Join(path, "."), err)
	}
	committed := !misplacedIn(cfg.BaseConfig, ConfigSourceProject)
	local := !misplacedIn(cfg.BaseConfig, ConfigSourceLocal)
	global := KnownYAMLKey(strings.Join(path, "."), reflect.TypeFor[BaseConfig]())
	switch {
	case committed && global:
		return PlaceTeamOrPerson, nil
	case committed:
		return PlaceGraphRepo, nil
	case local && global:
		return PlacePerson, nil
	case global:
		return PlaceUserGlobal, nil
	}
	return PlaceUnknown, nil
}

// misplacedIn reports whether IgnoredSettings reads past a setting of layer
// for the layer it stands in, not for its value.
func misplacedIn(layer BaseConfig, source ConfigValueSource) bool {
	return slices.ContainsFunc(IgnoredSettings(layer, source), func(s IgnoredSetting) bool {
		return s.Reason == globalOnlyReason || s.Reason == personalOnlyReason
	})
}

func groupByPlacement(key string, settings []placedSetting) []MisplacedKey {
	var placements []KeyPlacement
	paths := map[KeyPlacement][]string{}
	for _, s := range settings {
		if _, seen := paths[s.placement]; !seen {
			placements = append(placements, s.placement)
		}
		paths[s.placement] = append(paths[s.placement], s.path)
	}
	if len(placements) == 1 {
		return []MisplacedKey{{Key: key, Placement: placements[0]}}
	}
	grouped := make([]MisplacedKey, 0, len(placements))
	for _, p := range placements {
		grouped = append(grouped, MisplacedKey{Key: strings.Join(paths[p], ", "), Placement: p})
	}
	return grouped
}
