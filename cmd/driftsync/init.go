package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/karosia/driftsync/config"
)

func cmdInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	stack := fs.String("stack", "", "your stack: "+strings.Join(config.Stacks, " | "))
	withWorkflows := fs.Bool("with-workflows", false, "also write .github/workflows/drift-{check,sync}.yml")
	force := fs.Bool("force", false, "overwrite existing files")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *stack == "" {
		return fmt.Errorf("--stack is required (one of: %s)", strings.Join(config.Stacks, ", "))
	}

	cfgBytes, err := config.ConfigTemplate(*stack)
	if err != nil {
		return err
	}
	if err := writeNew(config.DefaultPath, cfgBytes, *force); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", config.DefaultPath)

	if *stack == "huma" {
		gen, err := config.GenspecTemplate()
		if err != nil {
			return err
		}
		dst := filepath.Join("tools", "genspec", "main.go")
		if err := writeNew(dst, gen, *force); err != nil {
			return err
		}
		fmt.Printf("wrote %s  (edit the import + constructor for your huma.API)\n", dst)
	}

	if *withWorkflows {
		for _, kind := range []string{"check", "sync"} {
			wf, err := config.WorkflowTemplate(kind)
			if err != nil {
				return err
			}
			dst := filepath.Join(".github", "workflows", "drift-"+kind+".yml")
			if err := writeNew(dst, wf, *force); err != nil {
				return err
			}
			fmt.Printf("wrote %s\n", dst)
		}
	}

	fmt.Printf("\nNext: edit %s (the code.command / published path), then run `driftsync check`.\n",
		config.DefaultPath)
	return nil
}

// writeNew writes data to path, creating parent dirs, refusing to clobber unless
// force is set.
func writeNew(path string, data []byte, force bool) error {
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("%s already exists (use --force to overwrite)", path)
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, data, 0o644)
}
