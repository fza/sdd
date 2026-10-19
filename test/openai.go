// Package test holds test support shared across packages: fakes standing in
// for the processes and services sdd talks to, so no test reaches the network.
package test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// RefusedJSONSchemaBody is the 400 an OpenAI-compatible endpoint answers a
// json_schema request with when it serves the model in json_object mode only.
const RefusedJSONSchemaBody = `{"error":{"message":"The model 'glm-5.3-flash' does not support JSON schema mode. Use 'json_object' mode instead","type":"invalid_request_error","code":"model_not_capable"}}`

// OpenAIBehaviour scripts how an OpenAIServer answers.
type OpenAIBehaviour struct {
	// RefuseJSONSchema answers a json_schema request with RefusedJSONSchemaBody.
	RefuseJSONSchema bool
	// RejectWith, when set, answers every request with a 400 carrying it.
	RejectWith string
	// Answer is the message content of every accepted request.
	Answer string
	// Delay holds back every answer, refusals included, or until the client
	// gives up.
	Delay time.Duration
}

// OpenAIServer fakes an OpenAI-compatible chat-completions endpoint and
// records the request bodies it received.
type OpenAIServer struct {
	URL string

	behaviour OpenAIBehaviour
	mu        sync.Mutex
	requests  []map[string]any
}

// NewOpenAIServer starts an OpenAIServer, closed when the test ends.
func NewOpenAIServer(t testing.TB, behaviour OpenAIBehaviour) *OpenAIServer {
	t.Helper()
	server := &OpenAIServer{behaviour: behaviour}
	listener := httptest.NewServer(http.HandlerFunc(server.serve))
	t.Cleanup(listener.Close)
	server.URL = listener.URL
	return server
}

func (s *OpenAIServer) serve(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	s.mu.Lock()
	s.requests = append(s.requests, body)
	s.mu.Unlock()

	if s.behaviour.Delay > 0 {
		select {
		case <-time.After(s.behaviour.Delay):
		case <-r.Context().Done():
			return
		}
	}
	if s.behaviour.RejectWith != "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(s.behaviour.RejectWith))
		return
	}
	if s.behaviour.RefuseJSONSchema && responseFormatType(body) == "json_schema" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(RefusedJSONSchemaBody))
		return
	}
	answer, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"content": s.behaviour.Answer}}},
	})
	_, _ = w.Write(answer)
}

// Requests returns the request bodies received so far.
func (s *OpenAIServer) Requests() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.requests...)
}

// ResponseFormats returns the response_format type of every request received
// so far, empty for a request carrying none.
func (s *OpenAIServer) ResponseFormats() []string {
	requests := s.Requests()
	formats := make([]string, len(requests))
	for i, body := range requests {
		formats[i] = responseFormatType(body)
	}
	return formats
}

// SystemMessage returns the system message content of the request at index.
func (s *OpenAIServer) SystemMessage(index int) string {
	requests := s.Requests()
	messages, _ := requests[index]["messages"].([]any)
	for _, message := range messages {
		fields, _ := message.(map[string]any)
		if fields["role"] == "system" {
			content, _ := fields["content"].(string)
			return content
		}
	}
	return ""
}

func responseFormatType(body map[string]any) string {
	format, _ := body["response_format"].(map[string]any)
	formatType, _ := format["type"].(string)
	return formatType
}
