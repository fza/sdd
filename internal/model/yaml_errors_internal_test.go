package model

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// A mismatch of a shape yaml.v3 does not produce today keeps its line and key
// and loses the rest, value included; ParseConfig cannot reach one.
func TestWithoutValuesFailsClosedOnAnUnknownMismatch(t *testing.T) {
	data := []byte("llm:\n  api_keys: QSEC\n")
	err := withoutValues(data, &yaml.TypeError{Errors: []string{"line 2: cannot unmarshal !!str `QSEC as map[string]string"}})
	if strings.Contains(err.Error(), "QSEC") {
		t.Errorf("the error should not show the value: %v", err)
	}
	if !strings.Contains(err.Error(), "line 2: llm.api_keys: cannot unmarshal") {
		t.Errorf("the error should keep the line, the key and the mismatch: %v", err)
	}
}
