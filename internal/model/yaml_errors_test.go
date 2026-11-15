package model_test

import (
	"strings"
	"testing"

	"github.com/networkteam/sdd/internal/model"
)

func TestParseConfigTypeErrorNamesKeyAndLineWithoutTheValue(t *testing.T) {
	cases := []struct {
		name, config, secret, wantKey string
	}{
		{name: "long value", config: "participant: Ada\nllm:\n  api_keys: sk-proj-abcdefghijklmnop\n", secret: "sk-proj", wantKey: "line 3: llm.api_keys:"},
		{name: "short value", config: "llm:\n  api_keys: sk-short\n", secret: "sk-short", wantKey: "line 2: llm.api_keys:"},
		{name: "flow mapping", config: "llm: {api_keys: sk-inline-secret}\n", secret: "sk-inline", wantKey: "line 1: llm.api_keys:"},
		{name: "value holding a backtick", config: "llm:\n  api_keys: \"ab`cdefgh\"\n", secret: "cdefgh", wantKey: "line 2: llm.api_keys:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := model.ParseConfig([]byte(tc.config))
			if err == nil {
				t.Fatal("a scalar where a map belongs should fail to parse")
			}
			if strings.Contains(err.Error(), tc.secret) || strings.Contains(err.Error(), "`") {
				t.Errorf("the error should not show the value: %v", err)
			}
			if !strings.Contains(err.Error(), tc.wantKey) || !strings.Contains(err.Error(), "cannot unmarshal !!str into map[string]string") {
				t.Errorf("the error should name the line, the key and the mismatch: %v", err)
			}
		})
	}
}
