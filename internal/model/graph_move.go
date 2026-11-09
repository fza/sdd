package model

import (
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// RewriteLines removes from content every line equal to one in remove and
// appends each line of add it lacks, keeping every other line as it was.
func RewriteLines(content []byte, remove, add []string) []byte {
	var kept []string
	present := map[string]bool{}
	text := string(content)
	if text != "" {
		for line := range strings.SplitSeq(strings.TrimSuffix(text, "\n"), "\n") {
			trimmed := strings.TrimSpace(line)
			if slices.Contains(remove, trimmed) && !slices.Contains(add, trimmed) {
				continue
			}
			present[trimmed] = true
			kept = append(kept, line)
		}
	}
	for _, line := range add {
		if !present[line] {
			kept = append(kept, line)
			present[line] = true
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return []byte(strings.Join(kept, "\n") + "\n")
}

// MovedProjectConfig is the committed config of the project holding a graph
// after a move, derived from the config it held before: graph_dir,
// default_branch and repo_id set for the repository it lands in, the
// pointer keys dropped, and skill_scope and supported_agents taken from
// agents when the landing repository is the one agents work in. An empty
// repoID or defaultBranch drops the key.
func MovedProjectConfig(source []byte, graphDir, defaultBranch string, repoID RepoID, agents *PerRepoConfig) ([]byte, error) {
	out := source
	var err error
	set := func(key, value string) {
		if err != nil {
			return
		}
		out, err = setOrDeleteYAMLField(out, key, value)
	}
	set("graph_dir", graphDir)
	set("default_branch", defaultBranch)
	set("repo_id", string(repoID))
	set("project_dir", "")
	set("graph_repo", "")
	if agents != nil {
		set("skill_scope", string(agents.SkillScope))
		if err == nil && len(agents.SupportedAgents) > 0 {
			out, err = SetYAMLSequence(out, "supported_agents", agents.SupportedAgents)
		} else {
			set("supported_agents", "")
		}
	}
	if err != nil {
		return nil, fmt.Errorf("deriving the moved project's config: %w", err)
	}
	return out, nil
}

func setOrDeleteYAMLField(content []byte, key, value string) ([]byte, error) {
	if value != "" {
		return SetYAMLField(content, key, value)
	}
	keys, err := TopLevelKeys(content)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(keys, key) {
		return content, nil
	}
	return DeleteYAMLField(content, key)
}

// MergeLocalConfig moves the machine-local settings of from into into: every
// top-level key of from replaces the one into holds, and the keys in drop
// leave both. Either may be empty.
func MergeLocalConfig(into, from []byte, drop []string) ([]byte, error) {
	var target, source yaml.Node
	err := yaml.Unmarshal(into, &target)
	if err != nil {
		return nil, err
	}
	err = yaml.Unmarshal(from, &source)
	if err != nil {
		return nil, err
	}
	if source.Kind == 0 || len(source.Content) == 0 {
		return into, nil
	}
	if target.Kind == 0 || len(target.Content) == 0 {
		target = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	targetMap, sourceMap := target.Content[0], source.Content[0]
	if targetMap.Kind != yaml.MappingNode || sourceMap.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("a machine-local config must be a mapping")
	}
	var keptTarget []*yaml.Node
	for i := 0; i+1 < len(targetMap.Content); i += 2 {
		if !slices.Contains(drop, targetMap.Content[i].Value) {
			keptTarget = append(keptTarget, targetMap.Content[i], targetMap.Content[i+1])
		}
	}
	targetMap.Content = keptTarget
	for i := 0; i+1 < len(sourceMap.Content); i += 2 {
		key, value := sourceMap.Content[i], sourceMap.Content[i+1]
		if slices.Contains(drop, key.Value) {
			continue
		}
		replaced := false
		for j := 0; j+1 < len(targetMap.Content); j += 2 {
			if targetMap.Content[j].Value == key.Value {
				targetMap.Content[j+1] = value
				replaced = true
				break
			}
		}
		if !replaced {
			targetMap.Content = append(targetMap.Content, key, value)
		}
	}
	return encodeYAML(&target)
}

// MoveCommit is a commit a move builds without touching a working tree: the
// first parent's tree, Remove taken out of it, Graft's tree placed at
// GraftAt and Files written or removed.
type MoveCommit struct {
	Parents []string
	Remove  []string
	Graft   string
	GraftAt string
	Files   []MoveFile
	Message string
}

// MoveFile is one file a move commit writes, or removes.
type MoveFile struct {
	Path    string
	Content []byte
	Remove  bool
}
