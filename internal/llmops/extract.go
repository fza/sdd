package llmops

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"text/template"

	"github.com/networkteam/sdd/pkg/llm"
)

// extraction describes the second call a JSON-shaped check falls back to when
// its first response does not parse. The prompt carries the unparseable text,
// the target shape, and the severity scale, and nothing else: with no task
// rubric in hand the model can only reformat, never reach a different verdict.
type extraction struct {
	purpose llm.Purpose
	// schema is the target shape, written as one compact example line.
	schema string
	// scaleTemplate names the severity partial the check scores on.
	scaleTemplate string
}

var preflightExtraction = extraction{
	purpose:       llm.PurposePreflightExtract,
	schema:        `{"findings":[{"observation":"...","category":"short-hyphenated-tag","severity":"high|medium|low"}]}`,
	scaleTemplate: "severity_scale",
}

var writingGuideExtraction = extraction{
	purpose:       llm.PurposeWritingGuideExtract,
	schema:        `{"findings":[{"reasoning":"...","axis":"stranding|dilution|conflation|pointing|form","quote":"...","repair":"cut|write-in|split|point|reword","severity":"substantive|minor"}]}`,
	scaleTemplate: "guide_severity_scale",
}

// runJSONCheck runs a JSON-shaped check and parses its response, falling back
// to one extraction call when the first response does not parse. A model asked
// to reason and to emit strict machine-readable output in one breath writes
// its conclusion into a field that has no value for it, and discarding a whole
// response over one such field loses every finding that parsed.
//
// Both responses failing is an error naming both attempts, because a checker
// that cannot produce its own output shape has stopped working and must not
// read as a clean result.
func runJSONCheck[T any](ctx context.Context, runner llm.Runner, req llm.Request, spec extraction, parse func(string) (T, error)) (T, error) {
	var zero T

	first, err := runner.Run(ctx, req)
	if err != nil {
		return zero, err
	}
	result, firstErr := parse(first.Text)
	if firstErr == nil {
		return result, nil
	}

	extractReq, err := renderExtractPrompt(spec, first.Text)
	if err != nil {
		return zero, fmt.Errorf("rendering extraction prompt after %v: %w", firstErr, err)
	}
	second, err := runner.Run(ctx, extractReq)
	if err != nil {
		return zero, fmt.Errorf("extracting a verdict after %v: %w", firstErr, err)
	}
	result, secondErr := parse(second.Text)
	if secondErr != nil {
		return zero, fmt.Errorf("unparseable in both phases: %v, then %w", firstErr, secondErr)
	}
	return result, nil
}

func renderExtractPrompt(spec extraction, priorOutput string) (llm.Request, error) {
	tmpl, err := sharedPartials()
	if err != nil {
		return llm.Request{}, err
	}

	var scale strings.Builder
	if err := tmpl.ExecuteTemplate(&scale, spec.scaleTemplate, nil); err != nil {
		return llm.Request{}, fmt.Errorf("executing template %s: %w", spec.scaleTemplate, err)
	}

	var system strings.Builder
	data := struct {
		Schema string
		Scale  string
	}{Schema: spec.schema, Scale: strings.TrimSpace(scale.String())}
	if err := tmpl.ExecuteTemplate(&system, "extractor_system", data); err != nil {
		return llm.Request{}, fmt.Errorf("executing template extractor_system: %w", err)
	}

	return llm.Request{
		Purpose:      spec.purpose,
		SystemPrompt: strings.TrimSpace(system.String()),
		UserPrompt:   strings.TrimSpace(priorOutput),
	}, nil
}

var (
	partialsOnce sync.Once
	partialsTmpl *template.Template
	partialsErr  error
)

// sharedPartials parses the shared partials alone. The extraction prompt needs
// only static text, so it never pays for a check's own template set or the
// per-call functions those sets close over.
func sharedPartials() (*template.Template, error) {
	partialsOnce.Do(func() {
		partialsTmpl, partialsErr = template.New("shared_partials").ParseFS(sharedPromptTemplates, "shared_templates/*.tmpl")
		if partialsErr != nil {
			partialsErr = fmt.Errorf("parsing shared prompt templates: %w", partialsErr)
		}
	})
	return partialsTmpl, partialsErr
}
