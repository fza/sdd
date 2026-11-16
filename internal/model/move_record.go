package model

import (
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Location kinds a graph can live at. A later store adds its own kind; the
// reference format stays.
const (
	// LocationGitDir is a folder in the repository sdd move runs in.
	LocationGitDir = "git-dir"
	// LocationGitPath is a checkout on disk, its path relative to the
	// repository sdd move runs in or absolute, with the graph folder in it.
	LocationGitPath = "git-path"
	// LocationGitRepo is a repository sdd clones and pushes itself.
	LocationGitRepo = "git-repo"
)

// LocationRef names where a graph lives: <kind>:<address>, options as query
// parameters. The kind decides what the address and options mean.
type LocationRef struct {
	Kind    string
	Address string
	Options url.Values
}

// locationKinds checks the address and options each known kind needs.
var locationKinds = map[string]func(LocationRef) error{
	LocationGitDir: func(ref LocationRef) error {
		return requireOptions(ref)
	},
	LocationGitPath: func(ref LocationRef) error {
		return requireOptions(ref, "dir")
	},
	LocationGitRepo: func(ref LocationRef) error {
		err := ValidateRepoID(RepoID(ref.Address))
		if err != nil {
			return err
		}
		return requireOptions(ref, "dir", "branch")
	},
}

func requireOptions(ref LocationRef, names ...string) error {
	for _, name := range names {
		if ref.Options.Get(name) == "" {
			return fmt.Errorf("location %s needs the option %s", ref.Kind, name)
		}
	}
	for name := range ref.Options {
		if !slices.Contains(names, name) {
			return fmt.Errorf("location %s takes no option %s", ref.Kind, name)
		}
	}
	return nil
}

// GitDirLocation is the graph folder dir in the repository sdd move runs in.
func GitDirLocation(dir string) LocationRef {
	return LocationRef{Kind: LocationGitDir, Address: dir}
}

// GitPathLocation is the graph folder dir in the checkout at path.
func GitPathLocation(path, dir string) LocationRef {
	return LocationRef{Kind: LocationGitPath, Address: path, Options: url.Values{"dir": {dir}}}
}

// GitRepoLocation is the graph folder dir on branch of the repository sdd
// clones as repo.
func GitRepoLocation(repo RepoID, dir, branch string) LocationRef {
	return LocationRef{Kind: LocationGitRepo, Address: string(repo), Options: url.Values{"dir": {dir}, "branch": {branch}}}
}

// Dir is the graph folder of a location in a separate repository.
func (r LocationRef) Dir() string { return r.Options.Get("dir") }

// Branch is the branch of a git-repo location.
func (r LocationRef) Branch() string { return r.Options.Get("branch") }

func (r LocationRef) String() string {
	address := strings.NewReplacer("%", "%25", "?", "%3F").Replace(r.Address)
	if len(r.Options) == 0 {
		return r.Kind + ":" + address
	}
	return r.Kind + ":" + address + "?" + r.Options.Encode()
}

// ParseLocationRef reads a location reference, refusing a kind this build
// does not know.
func ParseLocationRef(text string) (LocationRef, error) {
	kind, rest, found := strings.Cut(text, ":")
	if !found || kind == "" {
		return LocationRef{}, fmt.Errorf("location %q: want <kind>:<address>", text)
	}
	check, known := locationKinds[kind]
	if !known {
		return LocationRef{}, fmt.Errorf("location %q: unknown kind %s", text, kind)
	}
	rawAddress, rawOptions, _ := strings.Cut(rest, "?")
	address, err := url.PathUnescape(rawAddress)
	if err != nil || address == "" {
		return LocationRef{}, fmt.Errorf("location %q: the %s kind needs an address", text, kind)
	}
	options, err := url.ParseQuery(rawOptions)
	if err != nil {
		return LocationRef{}, fmt.Errorf("location %q: %w", text, err)
	}
	ref := LocationRef{Kind: kind, Address: address}
	if len(options) > 0 {
		ref.Options = options
	}
	err = check(ref)
	if err != nil {
		return LocationRef{}, fmt.Errorf("location %q: %w", text, err)
	}
	return ref, nil
}

// MoveCall is the sdd move invocation that moves a graph to r, and carries
// any stray entries over to it when the graph is there already.
func (r LocationRef) MoveCall() string {
	switch r.Kind {
	case LocationGitPath:
		return "sdd move --project-dir " + r.Address
	case LocationGitRepo:
		return "sdd move --graph-repo " + r.Address
	default:
		return "sdd move --graph-dir " + r.Address
	}
}

// MoveRecordVersion is the move record format this build writes and reads.
const MoveRecordVersion = "v1"

// Move record fields, written as git trailers on a move commit.
const (
	moveFieldVersion  = "sdd-move"
	moveFieldID       = "sdd-move-id"
	moveFieldFrom     = "sdd-move-from"
	moveFieldTo       = "sdd-move-to"
	moveFieldRevision = "sdd-move-revision"
	moveFieldEntries  = "sdd-move-entries"
)

// MoveRecord is the durable trace a graph move leaves where the graph lived
// and where it went: which revision of the source it was cut at and how many
// entries it carried.
type MoveRecord struct {
	ID       string
	From     LocationRef
	To       LocationRef
	Revision string
	Entries  int
}

// Trailers renders the record as the trailer block of a move commit.
func (m MoveRecord) Trailers() string {
	return strings.Join([]string{
		moveFieldVersion + ": " + MoveRecordVersion,
		moveFieldID + ": " + m.ID,
		moveFieldFrom + ": " + m.From.String(),
		moveFieldTo + ": " + m.To.String(),
		moveFieldRevision + ": " + m.Revision,
		moveFieldEntries + ": " + strconv.Itoa(m.Entries),
	}, "\n") + "\n"
}

// ParseMoveRecord reads the move record from a commit's trailer lines; found
// is false when they carry none. Fields it does not know are ignored, a
// version it does not know is refused.
func ParseMoveRecord(trailers string) (record MoveRecord, found bool, err error) {
	fields := map[string]string{}
	for line := range strings.SplitSeq(trailers, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		if _, seen := fields[key]; !seen {
			fields[key] = strings.TrimSpace(value)
		}
	}
	version, found := fields[moveFieldVersion]
	if !found {
		return MoveRecord{}, false, nil
	}
	if version != MoveRecordVersion {
		return MoveRecord{}, true, fmt.Errorf("move record version %s is unknown to this sdd; upgrade sdd", version)
	}
	for _, key := range []string{moveFieldID, moveFieldFrom, moveFieldTo, moveFieldRevision, moveFieldEntries} {
		if fields[key] == "" {
			return MoveRecord{}, true, fmt.Errorf("move record lacks %s", key)
		}
	}
	record.ID, record.Revision = fields[moveFieldID], fields[moveFieldRevision]
	record.From, err = ParseLocationRef(fields[moveFieldFrom])
	if err != nil {
		return MoveRecord{}, true, fmt.Errorf("move record %s: %w", record.ID, err)
	}
	record.To, err = ParseLocationRef(fields[moveFieldTo])
	if err != nil {
		return MoveRecord{}, true, fmt.Errorf("move record %s: %w", record.ID, err)
	}
	record.Entries, err = strconv.Atoi(fields[moveFieldEntries])
	if err != nil || record.Entries < 0 {
		return MoveRecord{}, true, fmt.Errorf("move record %s: %s is not a count", record.ID, moveFieldEntries)
	}
	return record, true, nil
}

// FormerLocations are the locations move records name that are not current,
// each once, in the order the records name them.
func FormerLocations(records []MoveRecord, current LocationRef) []LocationRef {
	var former []LocationRef
	seen := map[string]bool{current.String(): true}
	for _, record := range records {
		for _, ref := range []LocationRef{record.From, record.To} {
			if seen[ref.String()] {
				continue
			}
			seen[ref.String()] = true
			former = append(former, ref)
		}
	}
	return former
}

// FormerFolder is what a former location holds on this machine: the entry
// shaped files in its graph folder, relative to it.
type FormerFolder struct {
	Location LocationRef
	Dir      string
	Files    []string
}

// Stray is an entry file left in a former location whose ID the current
// graph does not hold.
type Stray struct {
	Location LocationRef
	Path     string
	ID       string
}

// FindStrays lists the entry-shaped files in former folders whose ID the
// current graph does not hold, sorted by path.
func FindStrays(former []FormerFolder, held func(id string) bool) []Stray {
	var strays []Stray
	for _, folder := range former {
		for _, file := range folder.Files {
			if !EntryShaped(file) {
				continue
			}
			id, err := RelPathToID(file)
			if err != nil || held(id) {
				continue
			}
			strays = append(strays, Stray{Location: folder.Location, Path: joinFolder(folder.Dir, file), ID: id})
		}
	}
	sort.Slice(strays, func(i, j int) bool { return strays[i].Path < strays[j].Path })
	return strays
}

func joinFolder(dir, file string) string {
	if dir == "" {
		return file
	}
	return strings.TrimSuffix(dir, "/") + "/" + strings.ReplaceAll(file, "\\", "/")
}

// StrayRefusal is the refusal every command meets while strays exist,
// naming them and the move that carries them over to current; nil without
// strays.
func StrayRefusal(strays []Stray, current LocationRef) error {
	if len(strays) == 0 {
		return nil
	}
	paths := make([]string, len(strays))
	for i, stray := range strays {
		paths[i] = stray.Path
	}
	return fmt.Errorf("%d entries lie in a former graph location and are missing from the graph: %s; carry them over with their history: %s", len(strays), strings.Join(paths, ", "), current.MoveCall())
}

// FormatULID renders a ULID from its millisecond time and 10 bytes of
// entropy: 26 characters of Crockford base32, sortable by time.
func FormatULID(at time.Time, entropy [10]byte) string {
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	var raw [16]byte
	ms := uint64(at.UnixMilli())
	for i := range 6 {
		raw[i] = byte(ms >> (40 - 8*i))
	}
	copy(raw[6:], entropy[:])
	out := make([]byte, 26)
	// 128 bits read as 26 groups of 5, the first group holding 3 bits.
	var acc uint64
	bits, pos := 2, 0
	for _, b := range raw {
		acc = acc<<8 | uint64(b)
		bits += 8
		for bits >= 5 {
			bits -= 5
			out[pos] = alphabet[(acc>>bits)&31]
			pos++
		}
	}
	return string(out)
}

// MoveRecordsMinimumVersion is the first sdd release that reads move
// records; a moved graph raises its minimum_version to it so older builds
// refuse the graph instead of writing into its former location.
const MoveRecordsMinimumVersion = "v0.18.0-alpha.3.fza.43"

// MoveMinimumVersion is the minimum_version a move leaves a graph with:
// MoveRecordsMinimumVersion, whichever build moves it, so every release
// reading move records still accepts the graph; a higher recorded one is kept.
func MoveMinimumVersion(recorded string) string {
	floor := MoveRecordsMinimumVersion
	if recorded != "" && !ShouldBumpMinimumVersion(recorded, floor) {
		return recorded
	}
	return floor
}

// LayoutLocation is where a layout keeps its graph. pointer is the config of
// the repository pointing at the project holding the graph, nil when that
// repository holds its graph itself; project is the config of the project
// holding the graph.
func LayoutLocation(pointer, project *PerRepoConfig) LocationRef {
	graphDir := DefaultGraphDir
	if project != nil && project.GraphDir != "" {
		graphDir = project.GraphDir
	}
	switch {
	case pointer != nil && pointer.GraphRepo != "":
		branch := ""
		if project != nil {
			branch = project.DefaultBranch
		}
		return GitRepoLocation(pointer.GraphRepo, graphDir, branch)
	case pointer != nil && pointer.ProjectDir != "":
		return GitPathLocation(pointer.ProjectDir, graphDir)
	}
	return GitDirLocation(graphDir)
}
