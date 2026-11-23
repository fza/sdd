package mcpapp

import "reflect"

// Notices holds what the host tells the agent beside a result, at most one
// notice per kind, each kind omitted when empty.
type Notices struct {
	Cache  string `json:"cache,omitempty" jsonschema:"set once after sdd's background cache pruning failed: the error and where to look; pass it on to the user"`
	Config string `json:"config,omitempty" jsonschema:"set once after the host's config changed: the changed keys with their old and new values (a secret names only its key); carry on under the new values"`
	Sync   string `json:"sync,omitempty" jsonschema:"set when automatic sync of the graph repository becomes blocked, the background title backfill fails, a connected repository's last sync round fails, or a refresh the call started still runs past its wait, and whenever that notice changes. One line each: which, and since when, why or how old the clone it read is, plus the fix steps when the attached session owns the fix. While it stands unchanged, later results leave it out; resume_session serves it again. Act on it as it says"`
}

// texts lists the notices set, in field order.
func (n Notices) texts() []string {
	var texts []string
	v := reflect.ValueOf(n)
	for i := range v.NumField() {
		if text := v.Field(i).String(); text != "" {
			texts = append(texts, text)
		}
	}
	return texts
}

// NoticesField carries a result's notices, the field omitted when none is set.
type NoticesField struct {
	Notices Notices `json:"notices,omitzero" jsonschema:"what the host tells you beside the result, keyed by kind, each told once. Plain status, not a tool error"`
}

// setNotices sets each kind notices carries, keeping a kind the handler set
// itself.
func (f *NoticesField) setNotices(notices Notices) {
	dst, src := reflect.ValueOf(&f.Notices).Elem(), reflect.ValueOf(notices)
	for i := range src.NumField() {
		if text := src.Field(i).String(); text != "" {
			dst.Field(i).SetString(text)
		}
	}
}
