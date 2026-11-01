package application

import "github.com/networkteam/sdd/pkg/application/types"

// ShownEntry / ShownChainItem / SearchHit / ViewSection are the show, search
// and view results as data (20260928-080143-d-cpt-kv4). They live in types so
// the renderers that turn them into text can name them
// (20261101-182333-d-tac-jiy, 20261101-194502-d-tac-r3f).
type (
	ShownEntry     = types.ShownEntry
	ShownChainItem = types.ShownChainItem
	SearchHit      = types.SearchHit
	ViewSection    = types.ViewSection
)
