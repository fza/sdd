package test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/networkteam/sdd/internal/secrets"
)

// GitleaksReleases fakes the two GitHub hosts the secret rule fetch reads:
// the API naming gitleaks' latest release and the raw host serving its rule
// file. One server answers both, so URL is the base of each.
type GitleaksReleases struct {
	URL    string
	Client *http.Client

	mu       sync.Mutex
	latest   string
	files    map[string][]byte
	failWith int
	requests []string
}

// NewGitleaksReleases starts a GitleaksReleases with no release, closed when
// the test ends.
func NewGitleaksReleases(t testing.TB) *GitleaksReleases {
	t.Helper()
	g := &GitleaksReleases{files: map[string][]byte{}}
	server := httptest.NewServer(http.HandlerFunc(g.serve))
	t.Cleanup(server.Close)
	g.URL, g.Client = server.URL, server.Client()
	return g
}

// FixtureToken is a credential only the rule rules/extra-rule.toml adds
// raises.
const FixtureToken = "sddfixture_k3j9x7q2m5n8p4r6"

// NewerGitleaksRules is the built-in rule file with the rule of
// rules/extra-rule.toml appended, as a gitleaks release adding one rule
// would publish it.
func NewerGitleaksRules(t testing.TB) []byte {
	t.Helper()
	builtin, err := secrets.BuiltinRuleFile()
	if err != nil {
		t.Fatal(err)
	}
	return append(append([]byte(nil), builtin.Data...), SecretRuleFixture(t, "extra-rule.toml")...)
}

// Publish makes tag the latest release, serving file as its rule file.
func (g *GitleaksReleases) Publish(tag string, file []byte) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.latest, g.files[tag] = tag, file
}

// FailWith answers every request with status; 0 answers normally again.
func (g *GitleaksReleases) FailWith(status int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.failWith = status
}

// Requests lists the paths requested so far.
func (g *GitleaksReleases) Requests() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.requests...)
}

// Downloads counts the rule file downloads so far.
func (g *GitleaksReleases) Downloads() int {
	count := 0
	for _, path := range g.Requests() {
		if strings.HasSuffix(path, "/config/gitleaks.toml") {
			count++
		}
	}
	return count
}

func (g *GitleaksReleases) serve(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.requests = append(g.requests, r.URL.Path)
	if g.failWith != 0 {
		http.Error(w, "failing on purpose", g.failWith)
		return
	}
	if r.URL.Path == "/repos/gitleaks/gitleaks/releases/latest" {
		if g.latest == "" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": g.latest})
		return
	}
	tag, ok := strings.CutPrefix(r.URL.Path, "/gitleaks/gitleaks/")
	if ok {
		tag, ok = strings.CutSuffix(tag, "/config/gitleaks.toml")
	}
	file, published := g.files[tag]
	if !ok || !published {
		http.NotFound(w, r)
		return
	}
	_, _ = w.Write(file)
}
