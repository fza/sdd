// Command refresh replaces the built-in gitleaks rule file with the one of
// gitleaks' latest release, byte for byte, and records its tag and commit in
// gitleaks.source.json. The release runs it before tagging, as
// `devbox run refresh-secret-rules`.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/networkteam/sdd/internal/secrets"
)

func main() {
	dir := flag.String("dir", filepath.Join("internal", "secrets"), "folder holding gitleaks.toml and gitleaks.source.json")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := run(ctx, *dir, os.Stdout)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "refresh-secret-rules:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, dir string, out io.Writer) error {
	stampPath := filepath.Join(dir, "gitleaks.source.json")
	rulesPath := filepath.Join(dir, "gitleaks.toml")
	data, err := os.ReadFile(stampPath)
	if err != nil {
		return err
	}
	var stamp secrets.BuiltinStamp
	err = json.Unmarshal(data, &stamp)
	if err != nil {
		return fmt.Errorf("decoding %s: %w", stampPath, err)
	}

	github := secrets.NewGitHub("", "", nil)
	tag, err := github.LatestTag(ctx)
	if err != nil {
		return err
	}
	if semver.Compare(tag, stamp.Tag) <= 0 {
		fmt.Fprintf(out, "the built-in rule file is gitleaks %s, the latest release is %s: nothing to do\n", stamp.Tag, tag)
		return nil
	}
	fetched, err := github.RuleFile(ctx, tag)
	if err != nil {
		return err
	}
	set, err := secrets.ParseRules(fetched)
	if err != nil {
		return fmt.Errorf("gitleaks %s: %w", tag, err)
	}
	current, err := os.ReadFile(rulesPath)
	if err != nil {
		return err
	}
	previous, err := secrets.ParseRules(current)
	if err != nil {
		return fmt.Errorf("%s: %w", rulesPath, err)
	}
	commit, err := github.TagCommit(ctx, tag)
	if err != nil {
		return err
	}

	err = os.WriteFile(rulesPath, fetched, 0o644)
	if err != nil {
		return err
	}
	replaced := stamp.Tag
	stamp.Tag, stamp.Commit = tag, commit
	data, err = json.MarshalIndent(stamp, "", "  ")
	if err != nil {
		return err
	}
	err = os.WriteFile(stampPath, append(data, '\n'), 0o644)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "built-in rule file: gitleaks %s, now %s (%s)\n", replaced, tag, commit)
	var dropped []string
	ids := set.RuleIDs()
	for _, id := range previous.RuleIDs() {
		if !slices.Contains(ids, id) {
			dropped = append(dropped, id)
		}
	}
	if len(dropped) > 0 {
		fmt.Fprintf(out, "rules gitleaks %s no longer has: %s\n", tag, strings.Join(dropped, ", "))
	}
	return nil
}
