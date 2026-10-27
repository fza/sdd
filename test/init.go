package test

import _ "embed"

// SpacedConfig is a committed .sdd/config.yaml whose keys sit in comment
// blocks separated by blank lines, recording supported_agents: [claude].
//
//go:embed fixtures/init/spaced-config.yaml
var SpacedConfig string
