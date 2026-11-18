// Command relevancelabels moves a project's relevance labels in and out of
// its ledger as JSON Lines. It is a development tool, never built into the
// sdd binary: sdd itself neither imports nor exports labels.
//
// Run it from the repository root:
//
//	go run ./scripts/relevancelabels -ledger DIR export > ledger.jsonl
//	go run ./scripts/relevancelabels -ledger DIR import FILE
//	go run ./scripts/relevancelabels -ledger DIR seed FILE
//
// DIR is the ledger's folder, ~/.local/state/sdd/relevance/<repository>/,
// or $XDG_STATE_HOME/sdd/relevance/<repository>/ when that variable is set,
// keyed like the search index. FILE is a path, or - for standard input.
//
//   - export writes the folded ledger: reader verdicts, scores, then drift
//     steps (application.ExportRelevanceJSON).
//   - import appends the records of a file as export writes it, as one
//     segment (application.ImportRelevanceJSON).
//   - seed appends a replayed reference label for each pair a file names as
//     {"a": "<entry ID>", "b": "<entry ID>"}, skipping pairs already
//     labelled (application.SeedReplayLabels); `sdd grounding update` then
//     scores them, and `sdd relevance compact --drop-replay` takes them back.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	sdd "github.com/networkteam/sdd/pkg/application"
	"github.com/networkteam/sdd/pkg/local"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "relevancelabels:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := flag.NewFlagSet("relevancelabels", flag.ContinueOnError)
	ledger := flags.String("ledger", "", "the ledger's folder")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *ledger == "" {
		return errors.New("pass -ledger DIR")
	}
	store, err := local.NewFilesystemRelevanceStore(*ledger)
	if err != nil {
		return err
	}
	switch command := flags.Args(); {
	case len(command) == 1 && command[0] == "export":
		return sdd.ExportRelevanceJSON(ctx, store, stdout)
	case len(command) == 2 && command[0] == "import":
		return withInput(command[1], stdin, func(input io.Reader) error {
			count, err := sdd.ImportRelevanceJSON(ctx, store, input)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(stdout, "Imported %d records\n", count)
			return err
		})
	case len(command) == 2 && command[0] == "seed":
		return withInput(command[1], stdin, func(input io.Reader) error {
			result, err := sdd.SeedReplayLabels(ctx, store, input)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(stdout, "Seeded %d replayed reference labels; %d pairs already labelled\n", result.Added, result.Skipped)
			return err
		})
	}
	return errors.New("pass export, import FILE or seed FILE")
}

// withInput hands use the file name names, or stdin for -.
func withInput(name string, stdin io.Reader, use func(io.Reader) error) error {
	if name == "-" {
		return use(stdin)
	}
	file, err := os.Open(name)
	if err != nil {
		return err
	}
	defer file.Close()
	return use(file)
}
