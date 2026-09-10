package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/karosia/driftsync/config"
	"github.com/karosia/driftsync/run"
)

func cmdCheck(args []string) error {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	cfgPath := fs.String("config", config.DefaultPath, "path to the driftsync config file")
	format := fs.String("format", "", "override report format: text | md | json")
	failOn := fs.String("fail-on", "", "override CI gate: breaking | any | never")
	reportFile := fs.String("report-file", "", "also write the report to this file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	if err := applyOverrides(cfg, *format, *failOn, *reportFile); err != nil {
		return err
	}

	res, err := run.Check(context.Background(), run.Options{Config: cfg, Stderr: os.Stderr})
	if err != nil {
		return err
	}

	fmt.Println(res.RenderedReport)
	if err := writeReportFile(cfg, res.RenderedReport); err != nil {
		return err
	}

	if res.ExitCode != 0 {
		fmt.Fprintf(os.Stderr, "drift gate failed (fail_on=%s): %d change(s), %d breaking\n",
			cfg.FailOn, len(res.Report.Changes), res.Report.Breaking())
		os.Exit(res.ExitCode)
	}
	return nil
}

// applyOverrides lets CLI flags win over config keys. Empty flag = keep config.
func applyOverrides(cfg *config.Config, format, failOn, reportFile string) error {
	if format != "" {
		cfg.Report = config.Format(format)
	}
	if failOn != "" {
		cfg.FailOn = config.FailOn(failOn)
	}
	if reportFile != "" {
		cfg.ReportFile = reportFile
	}
	return cfg.Validate()
}

func writeReportFile(cfg *config.Config, rendered string) error {
	if cfg.ReportFile == "" {
		return nil
	}
	if err := os.WriteFile(cfg.ReportFile, []byte(rendered+"\n"), 0o644); err != nil {
		return fmt.Errorf("write report file: %w", err)
	}
	return nil
}
