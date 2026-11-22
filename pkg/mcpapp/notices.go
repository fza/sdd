package mcpapp

import "reflect"

// Notices holds what the host tells the agent beside a result, at most one
// notice per kind, each kind omitted when empty.
type Notices struct {
	Config string `json:"config,omitempty" jsonschema:"set once after the host's config changed: the changed keys with their old and new values (a secret names only its key); carry on under the new values"`
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

func (f *NoticesField) setNotices(notices Notices) { f.Notices = notices }
