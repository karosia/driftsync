package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/karosia/driftsync/config"
	"github.com/karosia/driftsync/run"
)

// cmdDoctor validates a driftsync setup — config, extraction, both specs
// parsing as OpenAPI — WITHOUT diffing them against each other. It's meant
// for onboarding friction: "why isn't this working" before you ever get to
// "what changed."
func cmdDoctor(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	cfgPath := fs.String("config", config.DefaultPath, "path to the driftsync config file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	fmt.Printf("✓ %s\n    loaded from %s\n", *cfgPath, cfg.Source)

	res := run.Doctor(context.Background(), run.Options{Config: cfg, Stderr: os.Stderr})
	for _, s := range res.Steps {
		mark := "✓"
		if !s.OK {
			mark = "✗"
		}
		fmt.Printf("%s %s\n", mark, s.Name)
		if s.Detail != "" {
			fmt.Printf("    %s\n", s.Detail)
		}
	}

	if !res.OK {
		return fmt.Errorf("setup check failed — see ✗ above")
	}
	fmt.Println("\nsetup looks good — try `driftsync check`.")
	return nil
}
