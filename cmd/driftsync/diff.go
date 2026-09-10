package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/karosia/driftsync/diff"
)

func cmdDiff(args []string) error {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	format := fs.String("format", "text", "output format: text | md | json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 2 {
		return fmt.Errorf("usage: driftsync diff [--format text|md|json] <published-spec> <code-spec>")
	}

	published, err := loadCanonical(rest[0])
	if err != nil {
		return fmt.Errorf("load published: %w", err)
	}
	code, err := loadCanonical(rest[1])
	if err != nil {
		return fmt.Errorf("load code: %w", err)
	}

	report := diff.Diff(published, code)

	switch *format {
	case "md":
		fmt.Print(report.Markdown())
	case "json":
		out, err := report.JSON()
		if err != nil {
			return err
		}
		fmt.Println(out)
	default:
		fmt.Print(report.Text())
	}

	// CI gate stays on stderr/exit so it works with any format on stdout.
	if report.Breaking() > 0 {
		fmt.Fprintf(os.Stderr, "%d breaking change(s)\n", report.Breaking())
		os.Exit(1)
	}
	return nil
}
