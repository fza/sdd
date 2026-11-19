package handlers_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/networkteam/sdd/internal/bundledskills"
	"github.com/networkteam/sdd/internal/command"
	"github.com/networkteam/sdd/internal/model"
)

// Init writes each skill file byte for byte as upstream's render does: stamps
// at the top level in the Claude render, under metadata: in the Codex render.
// Upstream then reads the stamps and computes the hash they record, so it
// takes the file for one no one edited. The upstream* functions below are
// upstream's render, stamp read and hash, kept apart from the code under test.
func TestInit_WritesSkillFilesAsUpstreamRendersThem(t *testing.T) {
	tmp := t.TempDir()
	initExistingWithAgents(t, tmp, model.AgentClaude, model.AgentCodex)

	for _, target := range []model.AgentTarget{model.AgentClaude, model.AgentCodex} {
		bundle, err := bundledskills.Load(target)
		if err != nil {
			t.Fatal(err)
		}
		installDir, err := model.SkillInstallDir(target, model.ScopeProject, tmp, "")
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range bundle.Entries {
			name := string(target) + "/" + entry.Skill + "/" + entry.RelPath
			written := readFile(t, filepath.Join(installDir, entry.Skill, entry.RelPath))

			want := upstreamRenderSkillFile(t, entry.Content, target, "v0.2.0", upstreamSkillHash(entry.Content))
			if !bytes.Equal(written, want) {
				t.Errorf("%s differs from upstream's render\ngot:\n%.400s\nwant:\n%.400s", name, written, want)
			}

			fm, _ := upstreamSplitFrontmatter(written)
			_, stored := upstreamReadStamps(fm)
			if stored == "" || upstreamSkillHash(written) != stored {
				t.Errorf("%s: upstream computes hash %s, the file records %q", name, upstreamSkillHash(written), stored)
			}
			_, topLevel := fm[model.SkillStampHash]
			if topLevel != (target == model.AgentClaude) {
				t.Errorf("%s: content hash at the top level = %v", name, topLevel)
			}
		}
	}
}

// The Claude subagent definition is no skill file: its stamps stay under
// metadata:.
func TestInit_KeepsAgentDefinitionStampsUnderMetadata(t *testing.T) {
	tmp := t.TempDir()
	h := initExistingWithAgents(t, tmp, model.AgentClaude)

	path := filepath.Join(tmp, ".claude", "agents", model.DelegateAgentName+".md")
	data := readFile(t, path)
	fm, _ := upstreamSplitFrontmatter(data)
	if _, topLevel := fm[model.SkillStampHash]; topLevel {
		t.Errorf("the agent definition carries a top-level stamp:\n%.400s", data)
	}
	meta, _ := fm["metadata"].(map[string]any)
	if meta[model.SkillStampVersion] != "v0.2.0" || meta[model.SkillStampHash] == nil {
		t.Errorf("the agent definition should carry its stamps under metadata, got %v", fm["metadata"])
	}

	var installed []command.SkillInstallResult
	err := h.Init(context.Background(), &command.InitCmd{
		RepoRoot: tmp, BinaryVersion: "v0.2.0", Scope: model.ScopeProject,
		OnSkillsInstalled: func(r command.SkillInstallResult) { installed = append(installed, r) },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range installed {
		if changed := len(r.Installed) + len(r.Refreshed) + len(r.Overwritten) + len(r.SkippedModified); changed != 0 {
			t.Errorf("a repeat init should find every file current, got %+v", r)
		}
	}
}

func upstreamRenderSkillFile(t *testing.T, content []byte, target model.AgentTarget, version, contentHash string) []byte {
	t.Helper()
	fm, body := upstreamSplitFrontmatter(content)
	if fm == nil {
		fm = map[string]any{}
	}
	if target == model.AgentCodex {
		meta, ok := fm["metadata"].(map[string]any)
		if !ok {
			meta = map[string]any{}
		}
		meta[model.SkillStampVersion] = version
		meta[model.SkillStampHash] = contentHash
		fm["metadata"] = meta
	} else {
		fm[model.SkillStampVersion] = version
		fm[model.SkillStampHash] = contentHash
	}
	yamlBytes, err := yaml.Marshal(fm)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(yamlBytes)
	buf.WriteString("---\n")
	buf.Write(body)
	return buf.Bytes()
}

func upstreamReadStamps(fm map[string]any) (version, hash string) {
	version, _ = fm[model.SkillStampVersion].(string)
	hash, _ = fm[model.SkillStampHash].(string)
	if version != "" && hash != "" {
		return version, hash
	}
	if meta, ok := fm["metadata"].(map[string]any); ok {
		if version == "" {
			version, _ = meta[model.SkillStampVersion].(string)
		}
		if hash == "" {
			hash, _ = meta[model.SkillStampHash].(string)
		}
	}
	return version, hash
}

func upstreamSkillHash(fileContent []byte) string {
	fm, body := upstreamSplitFrontmatter(fileContent)
	var canonical []byte
	if fm != nil {
		stripped := make(map[string]any, len(fm))
		for k, v := range fm {
			if k != model.SkillStampVersion && k != model.SkillStampHash {
				stripped[k] = v
			}
		}
		if meta, ok := stripped["metadata"].(map[string]any); ok {
			rest := make(map[string]any, len(meta))
			for k, v := range meta {
				if k != model.SkillStampVersion && k != model.SkillStampHash {
					rest[k] = v
				}
			}
			if len(rest) == 0 {
				delete(stripped, "metadata")
			} else {
				stripped["metadata"] = rest
			}
		}
		if len(stripped) > 0 {
			canonical, _ = json.Marshal(upstreamNormalize(stripped))
		}
	}
	h := sha256.New()
	h.Write(canonical)
	h.Write([]byte{'\n'})
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

func upstreamNormalize(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = upstreamNormalize(val)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[fmt.Sprint(k)] = upstreamNormalize(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = upstreamNormalize(val)
		}
		return out
	default:
		return x
	}
}

func upstreamSplitFrontmatter(content []byte) (map[string]any, []byte) {
	if !bytes.HasPrefix(content, []byte("---\n")) && !bytes.HasPrefix(content, []byte("---\r\n")) {
		return nil, content
	}
	rest := content[4:]
	if bytes.HasPrefix(content, []byte("---\r\n")) {
		rest = content[5:]
	}
	lines := bytes.Split(rest, []byte("\n"))
	var fmLines [][]byte
	bodyStart := -1
	for i, line := range lines {
		if strings.TrimRight(string(line), "\r") == "---" {
			bodyStart = i + 1
			break
		}
		fmLines = append(fmLines, line)
	}
	if bodyStart < 0 {
		return nil, content
	}
	var fm map[string]any
	err := yaml.Unmarshal(bytes.Join(fmLines, []byte("\n")), &fm)
	if err != nil || fm == nil {
		return nil, content
	}
	var body []byte
	if bodyStart < len(lines) {
		body = bytes.Join(lines[bodyStart:], []byte("\n"))
	}
	return fm, body
}
