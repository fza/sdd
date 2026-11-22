package mcpapp

import "reflect"

// Notices holds what the host tells the agent beside a result, at most one
// notice per kind, each kind omitted when empty.
type Notices struct {
	Cache string `json:"cache,omitempty" jsonschema:"set once after sdd's background cache pruning failed: the error and where to look; pass it on to the user"`
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
