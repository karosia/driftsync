package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/karosia/driftsync/config"
)

func cmdInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	stack := fs.String("stack", "", "your stack: "+strings.Join(config.Stacks, " | "))
	withWorkflows := fs.Bool("with-workflows", false, "also write .github/workflows/drift-{check,sync}.yml")
	force := fs.Bool("force", false, "overwrite existing files")
	describe := fs.String("describe", "",
		"describe your stack in prose; an LLM drafts code.command for you "+
			"(generic stack only, needs ANTHROPIC_API_KEY or OPENAI_API_KEY)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *stack == "" {
		return fmt.Errorf("--stack is required (one of: %s)", strings.Join(config.Stacks, ", "))
	}
	if *describe != "" && *stack != "generic" {
		return fmt.Errorf("--describe only applies to --stack generic (other stacks already have a fixed code.command)")
	}

	cfgBytes, err := config.ConfigTemplate(*stack)
	if err != nil {
		return err
	}

	if *describe != "" {
		client, ok := buildLLMClient()
		if !ok {
			return fmt.Errorf("--describe needs ANTHROPIC_API_KEY or OPENAI_API_KEY set")
		}
		cmdStr, err := draftCodeCommand(context.Background(), client, *describe)
		if err != nil {
			return fmt.Errorf("draft code.command: %w", err)
		}
		// YAML-marshal the scalar so whatever the model wrote (colons, quotes,
		// #) stays valid YAML instead of being spliced in raw.
		q, err := yaml.Marshal(cmdStr)
		if err != nil {
			return fmt.Errorf("quote drafted command: %w", err)
		}
		quoted := strings.TrimSpace(string(q))
		next := bytesReplaceOnce(cfgBytes, "command: make openapi", "command: "+quoted)
		if next == nil {
			return fmt.Errorf("internal: generic template's command placeholder not found")
		}
		cfgBytes = next
		fmt.Printf("drafted code.command: %s\n  (review this before running `driftsync check` — it was not verified to actually work)\n", cmdStr)
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

// bytesReplaceOnce replaces the first occurrence of old with new, or returns
// nil if old isn't present at all — so a caller can fail loudly instead of
// silently writing an unmodified template.
func bytesReplaceOnce(b []byte, old, new string) []byte {
	if !bytes.Contains(b, []byte(old)) {
		return nil
	}
	return bytes.Replace(b, []byte(old), []byte(new), 1)
}
