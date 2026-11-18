package model

// GraphRuntimeDir is the folder inside the graph directory holding the graph
// store's locks, lineage and publication temp file.
const GraphRuntimeDir = ".sdd-runtime"

// GraphRuntimeIgnore is the content of the .gitignore sdd keeps inside
// GraphRuntimeDir: it ignores the folder from within, in every layout and
// after every move.
const GraphRuntimeIgnore = "*\n"

// SDDIgnoreLines are the lines of the committed .sdd/.gitignore: the
// machine-local state below .sdd/ that git never tracks, the same in every
// repository holding a .sdd/.
var SDDIgnoreLines = []string{"tmp/", "config.local.yaml", "index/", "stats/", "sessions/", "staged-blobs/", "hooks/local/"}

// GraphAttributesLine is the line of the .gitattributes inside the graph
// directory routing its entry files to the entry merge driver. Its pattern
// is relative to the graph directory, so it holds for every graph_dir and
// travels with the graph.
const GraphAttributesLine = EntryPathGlob + " merge=" + EntryMergeDriver
