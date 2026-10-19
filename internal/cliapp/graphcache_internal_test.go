package cliapp

import "testing"

// The kill switch is read where the CLI composes its finder; nothing outside
// the package sees which cache a finder holds.
func TestGraphCacheSwitchesOff(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("SDD_GRAPH_CACHE", "off")
	cache, err := defaultGraphCache()
	if err != nil {
		t.Fatal(err)
	}
	if cache != nil {
		t.Error("SDD_GRAPH_CACHE=off should load every graph from its files")
	}

	t.Setenv("SDD_GRAPH_CACHE", "")
	cache, err = defaultGraphCache()
	if err != nil {
		t.Fatal(err)
	}
	if cache == nil {
		t.Error("the graph cache should be on by default")
	}
}
