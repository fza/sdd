package model_test

import (
	"slices"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestGraphRepoMisplacedKeys(t *testing.T) {
	const pointer = "graph_repo: example.test/acme/app-sdd\nskill_scope: project\n"
	cases := []struct {
		name string
		yaml string
		want []model.MisplacedKey
	}{
		{
			name: "pointer keys only",
			yaml: pointer + "graph_dir: graph\nsupported_agents: [claude]\nhooks:\n  - event: capture\n    run: x\n",
		},
		{
			name: "project_dir is read past",
			yaml: pointer + "project_dir: ../project\n",
		},
		{
			name: "dialogue holding mode belongs to a person",
			yaml: pointer + "dialogue:\n  mode: auto\n",
			want: []model.MisplacedKey{{Key: "dialogue", Placement: model.KeyPlacement{Place: model.PlacePerson}}},
		},
		{
			name: "llm belongs to the team or a person",
			yaml: pointer + "llm:\n  provider: claude-cli\n  model: sonnet\n",
			want: []model.MisplacedKey{{Key: "llm", Placement: model.KeyPlacement{Place: model.PlaceTeamOrPerson}}},
		},
		{
			name: "inbound belongs to the graph repository",
			yaml: pointer + "inbound:\n  accept: true\n",
			want: []model.MisplacedKey{{Key: "inbound", Placement: model.KeyPlacement{Place: model.PlaceGraphRepo}}},
		},
		{
			name: "language is a property of the graph repository",
			yaml: pointer + "language: de\n",
			want: []model.MisplacedKey{{Key: "language", Placement: model.KeyPlacement{Place: model.PlaceGraphRepo}}},
		},
		{
			name: "the retired contribution key names its reason",
			yaml: pointer + "contribution: origin/main\n",
			want: []model.MisplacedKey{{Key: "contribution", Placement: model.KeyPlacement{Place: model.PlaceRetired, Reason: model.RetiredConfigKeys["contribution"]}}},
		},
		{
			name: "session retention belongs to the user-global config",
			yaml: pointer + "sessions:\n  retention: 14d\n",
			want: []model.MisplacedKey{{Key: "sessions", Placement: model.KeyPlacement{Place: model.PlaceUserGlobal}}},
		},
		{
			name: "llm holding debug and model splits by setting",
			yaml: pointer + "llm:\n  debug: true\n  model: sonnet\n",
			want: []model.MisplacedKey{
				{Key: "llm.debug", Placement: model.KeyPlacement{Place: model.PlacePerson}},
				{Key: "llm.model", Placement: model.KeyPlacement{Place: model.PlaceTeamOrPerson}},
			},
		},
		{
			name: "sync holding git limits and a retired mode splits by setting",
			yaml: pointer + "sync:\n  mode: auto\n  hosts:\n    example.test:\n      retries: 2\n",
			want: []model.MisplacedKey{
				{Key: "sync.mode", Placement: model.KeyPlacement{Place: model.PlaceRetired, Reason: model.RetiredConfigKeys["sync.mode"]}},
				{Key: "sync.hosts", Placement: model.KeyPlacement{Place: model.PlaceUserGlobal}},
			},
		},
		{
			name: "a value sdd reads past does not move its key",
			yaml: pointer + "verify:\n  mode: sometimes\n",
			want: []model.MisplacedKey{{Key: "verify", Placement: model.KeyPlacement{Place: model.PlaceTeamOrPerson}}},
		},
		{
			name: "an unknown key",
			yaml: pointer + "colour: blue\n",
			want: []model.MisplacedKey{{Key: "colour", Placement: model.KeyPlacement{Place: model.PlaceUnknown}}},
		},
		{
			name: "several keys in file order",
			yaml: "inbound:\n  accept: true\n" + pointer + "participant: Ada\ndialogue:\n  mode: guided\n",
			want: []model.MisplacedKey{
				{Key: "inbound", Placement: model.KeyPlacement{Place: model.PlaceGraphRepo}},
				{Key: "participant", Placement: model.KeyPlacement{Place: model.PlaceTeamOrPerson}},
				{Key: "dialogue", Placement: model.KeyPlacement{Place: model.PlacePerson}},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := model.GraphRepoMisplacedKeys([]byte(tc.yaml))

			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("each key beside the pointer should be placed where the layer rules keep it, in file order: got %+v", got)
			}
		})
	}
}
