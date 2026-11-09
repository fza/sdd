package hooks

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/networkteam/sdd/internal/model"
)

// headLines bounds the head of the command file a review shows.
const headLines = 20

// hashInput is the canonical form a hook's hash covers: its normalized
// config and the digest of every file it names.
type hashInput struct {
	Event    string           `json:"event"`
	Command  string           `json:"command"`
	Args     []string         `json:"args"`
	Timeout  int              `json:"timeout"`
	Findings bool             `json:"findings"`
	Files    []string         `json:"files"`
	Contents []model.HookFile `json:"contents"`
}

// Hash returns the SHA-256 over the hook's normalized config and the
// contents of its command file, of every args entry naming an existing
// file, and of every file its files list matches, with those files and
// their digests.
func Hash(hook model.Hook) (string, []model.HookFile, error) {
	rel, err := model.HookCommandPath(hook.Command, hook.Origin.Layer)
	if err != nil {
		return "", nil, err
	}
	command := filepath.ToSlash(rel)
	contents := []model.HookFile{}
	digest, err := fileDigest(hook.Path)
	if err != nil {
		return "", nil, err
	}
	contents = append(contents, model.HookFile{Role: "command", Path: command, Digest: digest})
	for _, arg := range hook.Args {
		path := resolve(hook.Origin.Dir, arg)
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		digest, err := fileDigest(path)
		if err != nil {
			return "", nil, err
		}
		contents = append(contents, model.HookFile{Role: "arg", Path: arg, Digest: digest})
	}
	matched, err := matchFiles(hook.Origin.Dir, hook.Files)
	if err != nil {
		return "", nil, err
	}
	contents = append(contents, matched...)
	input := hashInput{
		Event: hook.Event, Command: command, Args: nonNil(hook.Args), Timeout: int(hook.TimeoutDuration().Seconds()),
		Findings: hook.Findings, Files: nonNil(hook.Files), Contents: contents,
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), contents, nil
}

// Review returns what a person sees before confirming the hook.
func Review(hook model.Hook) (model.HookReview, error) {
	_, files, err := Hash(hook)
	if err != nil {
		return model.HookReview{}, err
	}
	head, err := commandHead(hook.Path)
	if err != nil {
		return model.HookReview{}, err
	}
	return model.HookReview{Hook: hook, Files: files, Head: head}, nil
}

// matchFiles digests every file the patterns match, a matched folder
// standing for the files below it. A pattern matching nothing is refused.
func matchFiles(dir string, patterns []string) ([]model.HookFile, error) {
	var files []model.HookFile
	seen := map[string]bool{}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(resolve(dir, pattern))
		if err != nil {
			return nil, fmt.Errorf("files entry %q: %w", pattern, err)
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("files entry %q matches nothing in %s", pattern, dir)
		}
		var found []string
		for _, match := range matches {
			err := filepath.WalkDir(match, func(path string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !entry.IsDir() {
					found = append(found, path)
				}
				return nil
			})
			if err != nil {
				return nil, fmt.Errorf("files entry %q: %w", pattern, err)
			}
		}
		slices.Sort(found)
		for _, path := range found {
			if seen[path] {
				continue
			}
			seen[path] = true
			digest, err := fileDigest(path)
			if err != nil {
				return nil, fmt.Errorf("files entry %q: %w", pattern, err)
			}
			files = append(files, model.HookFile{Role: "file", Path: relative(dir, path), Digest: digest})
		}
	}
	return files, nil
}

func resolve(dir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(dir, path)
}

func relative(dir, path string) string {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func fileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// commandHead returns the first lines of a text command file, or a line
// naming a binary one.
func commandHead(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	peek, _ := reader.Peek(8192)
	if bytes.IndexByte(peek, 0) >= 0 {
		info, err := file.Stat()
		if err != nil {
			return nil, err
		}
		return []string{fmt.Sprintf("(binary file, %d bytes)", info.Size())}, nil
	}
	var head []string
	scanner := bufio.NewScanner(reader)
	for len(head) < headLines && scanner.Scan() {
		head = append(head, strings.TrimRight(scanner.Text(), "\r"))
	}
	return head, scanner.Err()
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
