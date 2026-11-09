// Package bundledskills renders the agent-neutral skill templates embedded in
// the sdd binary into per-agent skill bundles.
//
// The source of truth is one neutral template tree under templates/ (files
// named *.md.tmpl). "claude" is no longer a source directory — it is a render
// profile: a model.AgentTarget value and the inject helper's per-agent output.
// Load executes the templates for a
// given target; per-agent deviations live in {{ if eq .Agent "..." }}
// conditionals and the inject helper, never as duplicated files. //go:embed
// compiles the templates into the binary; sdd init renders + stamps them into
// the target agent's skill directory.
//
// Editing: edit the templates under templates/<skill>/. After changes, rebuild
// the binary and run ./bin/sdd init to refresh the installed copy under
// .claude/skills/ (or ~/.claude/skills/).
package bundledskills

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"text/template"

	"github.com/networkteam/sdd/internal/model"
)

// templatesFS holds the embedded agent-neutral skill template tree.
//
// Plain `//go:embed templates` recurses the whole tree but excludes files whose
// names start with '.' or '_' (Go's default), keeping OS junk like a
// `.DS_Store` out of the embedded FS. To ship a template's own dotfile, add an
// explicit pattern naming that file.
//
//go:embed templates
var templatesFS embed.FS

const templatesRoot = "templates"

// agentsFS holds the harness subagent definition templates: one template per
// definition, rendered to Markdown for Claude Code and to TOML for Codex.
//
//go:embed agents
var agentsFS embed.FS

const agentsRoot = "agents"

// agentDefinitionExt is the file type each target reads subagent definitions
// in.
var agentDefinitionExt = map[model.AgentTarget]string{model.AgentClaude: ".md", model.AgentCodex: ".toml"}

// templateExt is the extension carried by source template files. It is stripped
// from the output path so a file at templates/sdd/SKILL.md.tmpl renders to the
// bundle entry sdd/SKILL.md.
const templateExt = ".tmpl"

// renderData is the value passed to every skill template at execution.
type renderData struct {
	// Agent is the render target's identifier (e.g. "claude"), used by
	// {{ if eq .Agent "claude" }} conditionals to express per-agent prose
	// deviations from one source.
	Agent string
}

// Load renders the embedded skill templates for the given agent target into a
// SkillBundle. Each *.md.tmpl file is parsed and executed with the target's
// render data and helper set; cross-file includes (`{{ template "rel/path" . }}`)
// resolve within the same skill. The returned entries carry no install stamps —
// stamping happens at install time (see model.RenderSkillFile).
func Load(target model.AgentTarget) (*model.SkillBundle, error) {
	switch target {
	case model.AgentClaude, model.AgentCodex:
		// supported
	default:
		return nil, fmt.Errorf("unsupported agent target: %s", target)
	}

	funcs := renderFuncs(target)
	data := renderData{Agent: string(target)}

	skills, err := skillDirs()
	if err != nil {
		return nil, err
	}

	bundle := &model.SkillBundle{Target: target}
	for _, skill := range skills {
		entries, err := renderSkill(skill, funcs, data)
		if err != nil {
			return nil, err
		}
		bundle.Entries = append(bundle.Entries, entries...)
	}
	bundle.Agents, err = renderAgents(funcs, data, agentDefinitionExt[target])
	if err != nil {
		return nil, err
	}
	return bundle, nil
}

// renderAgents executes every subagent definition template for the target,
// naming each output after its template with the target's file type.
func renderAgents(funcs template.FuncMap, data renderData, ext string) ([]model.SkillBundleEntry, error) {
	files, err := agentsFS.ReadDir(agentsRoot)
	if err != nil {
		return nil, fmt.Errorf("reading agent definition templates: %w", err)
	}
	var entries []model.SkillBundleEntry
	for _, file := range files {
		source := path.Join(agentsRoot, file.Name())
		content, err := agentsFS.ReadFile(source)
		if err != nil {
			return nil, fmt.Errorf("reading embedded %s: %w", source, err)
		}
		tmpl, err := template.New(file.Name()).Funcs(funcs).Parse(string(content))
		if err != nil {
			return nil, fmt.Errorf("parsing template %s: %w", source, err)
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return nil, fmt.Errorf("executing template %s: %w", source, err)
		}
		entries = append(entries, model.SkillBundleEntry{RelPath: strings.TrimSuffix(file.Name(), templateExt) + ext, Content: buf.Bytes()})
	}
	return entries, nil
}

// renderFuncs builds the template FuncMap bound to a specific target. inject
// renders a CLI command as Claude's dynamic-injection token (`!`+"`cmd`"+`) that
// Claude Code expands at skill-load, or — for agents without that mechanism — as
// an explicit instruction to run the command. Binding the target via closure
// lets templates call `{{ inject "sdd info" }}` without threading the agent
// through every call site.
func renderFuncs(target model.AgentTarget) template.FuncMap {
	return template.FuncMap{
		"inject": func(cmd string) string {
			if target == model.AgentClaude {
				return "!`" + cmd + "`"
			}
			return "Run `" + cmd + "` and use its output as context."
		},
	}
}

// skillDirs returns the immediate subdirectories of templates/, one per skill.
func skillDirs() ([]string, error) {
	entries, err := templatesFS.ReadDir(templatesRoot)
	if err != nil {
		return nil, fmt.Errorf("reading templates root: %w", err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	return dirs, nil
}

// renderSkill parses every template file under one skill into a shared template
// set (so `{{ template }}` includes resolve across files), then executes each
// file with data. Templates are named by their skill-relative output path
// (`.tmpl` stripped), e.g. "SKILL.md", "references/topic.md". Parsing all
// files before executing any lets a file include another regardless of order.
func renderSkill(skill string, funcs template.FuncMap, data renderData) ([]model.SkillBundleEntry, error) {
	skillRoot := path.Join(templatesRoot, skill)
	set := template.New(skill).Funcs(funcs)

	var outRels []string
	err := fs.WalkDir(templatesFS, skillRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := relUnder(skillRoot, p)
		if err != nil {
			return err
		}
		outRel := strings.TrimSuffix(rel, templateExt)

		content, err := templatesFS.ReadFile(p)
		if err != nil {
			return fmt.Errorf("reading embedded %s: %w", p, err)
		}
		if _, err := set.New(outRel).Parse(string(content)); err != nil {
			return fmt.Errorf("parsing template %s: %w", p, err)
		}
		outRels = append(outRels, outRel)
		return nil
	})
	if err != nil {
		return nil, err
	}

	entries := make([]model.SkillBundleEntry, 0, len(outRels))
	for _, outRel := range outRels {
		var buf bytes.Buffer
		if err := set.ExecuteTemplate(&buf, outRel, data); err != nil {
			return nil, fmt.Errorf("executing template %s/%s: %w", skill, outRel, err)
		}
		entries = append(entries, model.SkillBundleEntry{
			Skill:   skill,
			RelPath: outRel,
			Content: buf.Bytes(),
		})
	}
	return entries, nil
}

// vocabularyFS holds the per-locale SDD vocabulary the engine serves at session
// open until it moves into the graph (d-cpt-xob). It is engine data, not skill
// content, so it sits outside templates/ and is never installed with a skill.
//
//go:embed vocabulary
var vocabularyFS embed.FS

// Vocabulary returns the bundled vocabulary for locale (e.g. "de").
func Vocabulary(locale string) ([]byte, error) {
	full := path.Join("vocabulary", locale+".md")
	data, err := vocabularyFS.ReadFile(full)
	if err != nil {
		return nil, fmt.Errorf("reading bundled vocabulary %s: %w", full, err)
	}
	return data, nil
}

// relUnder returns full expressed relative to root, with leading separators
// trimmed and the path cleaned.
func relUnder(root, full string) (string, error) {
	rel := strings.TrimPrefix(full, root)
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		return "", fmt.Errorf("unexpected empty relative path for %q", full)
	}
	return path.Clean(rel), nil
}
