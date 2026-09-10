package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/karosia/driftsync/config"
	"github.com/karosia/driftsync/enrich"
	"github.com/karosia/driftsync/run"
)

func cmdSync(args []string) error {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	cfgPath := fs.String("config", config.DefaultPath, "path to the driftsync config file")
	output := fs.String("output", "", "write the corrected spec here (default: sync_output, else published)")
	format := fs.String("format", "", "override report format: text | md | json")
	reportFile := fs.String("report-file", "", "also write the report to this file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	if err := applyOverrides(cfg, *format, "", *reportFile); err != nil {
		return err
	}
	if *output != "" {
		cfg.SyncOutput = *output
	}

	res, err := run.Sync(context.Background(), run.Options{
		Config:   cfg,
		Enricher: enricherForSync(cfg.Enrich),
		Stderr:   os.Stderr,
	})
	if err != nil {
		return err
	}

	if err := writeReportFile(cfg, res.RenderedReport); err != nil {
		return err
	}

	if len(res.Report.Changes) == 0 {
		fmt.Fprintln(os.Stderr, "no drift: published spec already matches code")
		return nil
	}

	out := cfg.Output()
	if err := os.WriteFile(out, res.Corrected, 0o644); err != nil {
		return fmt.Errorf("write corrected spec: %w", err)
	}

	fmt.Fprintf(os.Stderr, "synced %s: applied %d/%d patch(es), %d failed, %d breaking\n",
		out, res.Applied, len(res.Patches), len(res.Failed), res.Report.Breaking())
	for _, f := range res.Failed {
		fmt.Fprintf(os.Stderr, "  FAILED %s %s: %s\n", f.Patch.Op, f.Patch.Path, f.Reason)
	}
	fmt.Fprintln(os.Stderr, "\n"+res.RenderedReport)
	return nil
}

// enricherForSync resolves the config's `enrich` policy against the environment.
// Nil means "skip enrichment; leave new-field descriptions blank".
func enricherForSync(policy config.Enrich) enrich.Enricher {
	hasKey := os.Getenv("ANTHROPIC_API_KEY") != "" || os.Getenv("OPENAI_API_KEY") != ""
	switch policy {
	case config.EnrichOff:
		return nil
	case config.EnrichOn:
		return buildEnricher()
	default: // auto
		if hasKey {
			return buildEnricher()
		}
		return nil
	}
}
