package mcpapp_test

import (
	mcpserver "github.com/networkteam/sdd/pkg/mcpapp"
)

// Programs built on the upstream packages compare these argument types; a
// field that makes one incomparable fails the build here.
var (
	_ = mcpserver.AbandonArgs{} == mcpserver.AbandonArgs{}
	_ = mcpserver.ParkArgs{} == mcpserver.ParkArgs{}
)
