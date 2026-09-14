// Package run is the orchestration layer behind `driftsync check`, `driftsync
// sync`, and `driftsync doctor`: it reads a validated config, extracts the
// code-side spec, canonicalizes both sides, diffs them, and (for sync)
// proposes, enriches, and applies the fix. Doctor runs the same
// extract-and-parse steps as a setup check, without diffing. The low-level
// packages (diff, patch, applier, enrich) stay unaware of the config file.
package run

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/karosia/driftsync/adapters/specfile"
	"github.com/karosia/driftsync/applier"
	"github.com/karosia/driftsync/canonicalize"
	"github.com/karosia/driftsync/config"
	"github.com/karosia/driftsync/diff"
	"github.com/karosia/driftsync/enrich"
	"github.com/karosia/driftsync/ir"
	"github.com/karosia/driftsync/patch"
)

// Options drives a run. The CLI fills it in; tests construct it directly.
type Options struct {
	Config *config.Config
	// Enricher fills descriptions for brand-new fields in sync mode. Nil skips
	// the step entirely (descriptions stay blank). Ignored by Check and Doctor.
	Enricher enrich.Enricher
	// Stderr receives the extraction command's stdout+stderr chatter, so the
	// rendered report on the real stdout stays clean. Defaults to os.Stderr.
	Stderr io.Writer
}

// Result is what a run produced.
type Result struct {
	Report         *diff.Report
	RenderedReport string // per Config.Report (text | md | json)
	Patches        []patch.Patch
	Applied        int
	Failed         []applier.Failure
	Corrected      []byte // sync only: the rewritten published spec
	// RenameHints are LLM-suggested (advisory only) semantic renames the
	// deterministic matcher didn't catch. Sync only, and only when Enricher is
	// set. Already folded into RenderedReport for text/md; kept here too for
	// programmatic access. See enrich.SuggestRenames.
	RenameHints []enrich.RenameHint
	// ExitCode is the process exit code check should use, per Config.FailOn.
	// Always 0 for Sync (the PR is the gate).
	ExitCode int
}

// Check extracts the code spec, diffs it against the published spec, and renders
// a report. It never writes anything.
func Check(ctx context.Context, o Options) (*Result, error) {
	res, _, _, err := diffOnly(ctx, o)
	return res, err
}

// Sync does everything Check does, then proposes patches, optionally enriches
// them, applies them to the published bytes, and returns the corrected spec in
// Result.Corrected. It still does not write to disk — the caller does.
func Sync(ctx context.Context, o Options) (*Result, error) {
	res, _, codeDoc, err := diffOnly(ctx, o)
	if err != nil {
		return nil, err
	}
	res.ExitCode = 0 // sync never gates; the pull request is the approval point

	patches, err := patch.FromReport(res.Report, codeDoc)
	if err != nil {
		return nil, fmt.Errorf("propose patches: %w", err)
	}

	if o.Enricher != nil && len(patches) > 0 {
		enrich.Apply(ctx, o.Enricher, patches)
	}
	if o.Enricher != nil {
		hints := enrich.SuggestRenames(ctx, o.Enricher, res.Report)
		res.RenameHints = hints
		// A hints section is prose glued onto the rendered report — safe for
		// text/md, but would corrupt a machine-readable json report.
		if len(hints) > 0 && o.Config.Report != config.FormatJSON {
			res.RenderedReport += renderHints(hints)
		}
	}
	res.Patches = patches

	publishedRaw, err := os.ReadFile(o.Config.Published)
	if err != nil {
		return nil, fmt.Errorf("read published spec: %w", err)
	}
	corrected, applied, failed, err := applier.Apply(publishedRaw, patches)
	if err != nil {
		return nil, fmt.Errorf("apply patches: %w", err)
	}
	res.Corrected = corrected
	res.Applied = len(applied)
	res.Failed = failed
	return res, nil
}

// DoctorStep is one checklist item from Doctor.
type DoctorStep struct {
	Name string
	OK   bool
	// Detail is a human-readable outcome on success, or the error on failure.
	Detail string
}

// DoctorResult is the outcome of a Doctor run: an ordered checklist, stopping
// at the first failure since every later stage depends on the one before it.
type DoctorResult struct {
	Steps []DoctorStep
	OK    bool
}

// Doctor validates a driftsync SETUP end to end — that code.command runs and
// produces a file, and that both the code and published specs actually parse
// as OpenAPI — without diffing them against each other. It's for onboarding
// ("is my driftsync.yaml wired up right"), not for finding drift; use Check
// for that. o.Config is assumed already loaded (config.Load succeeded) —
// that's the caller's own checklist line, not this function's.
func Doctor(ctx context.Context, o Options) *DoctorResult {
	res := &DoctorResult{OK: true}
	cfg := o.Config
	stderr := o.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	ok := func(name, detail string) {
		res.Steps = append(res.Steps, DoctorStep{Name: name, OK: true, Detail: detail})
	}
	fail := func(name string, err error) {
		res.Steps = append(res.Steps, DoctorStep{Name: name, OK: false, Detail: err.Error()})
		res.OK = false
	}

	if err := extract(ctx, cfg, stderr); err != nil {
		fail("code.command", err)
		return res
	}
	if cfg.Code.Command != "" {
		ok("code.command", "ran: "+cfg.Code.Command)
	} else {
		ok("code.command", "skipped (no command configured; "+cfg.Code.File+" used as-is)")
	}

	codeDoc, err := loadCanonical(cfg.Code.File)
	if err != nil {
		fail("code spec ("+cfg.Code.File+") parses as OpenAPI", err)
		return res
	}
	ok("code spec ("+cfg.Code.File+") parses as OpenAPI", specSummary(codeDoc))

	pubDoc, err := loadCanonical(cfg.Published)
	if err != nil {
		fail("published spec ("+cfg.Published+") parses as OpenAPI", err)
		return res
	}
	ok("published spec ("+cfg.Published+") parses as OpenAPI", specSummary(pubDoc))

	return res
}

// specSummary is a quick sanity-check count for a Doctor step's detail line —
// not exhaustive, just enough to show the file wasn't parsed as something
// trivially empty.
func specSummary(doc *ir.Document) string {
	if doc == nil || doc.Model == nil {
		return "parsed"
	}
	ops := 0
	if doc.Model.Paths != nil {
		for p := doc.Model.Paths.PathItems.First(); p != nil; p = p.Next() {
			for op := p.Value().GetOperations().First(); op != nil; op = op.Next() {
				ops++
			}
		}
	}
	schemas := 0
	if doc.Model.Components != nil && doc.Model.Components.Schemas != nil {
		for pair := doc.Model.Components.Schemas.First(); pair != nil; pair = pair.Next() {
			schemas++
		}
	}
	return fmt.Sprintf("%d operation(s), %d schema(s)", ops, schemas)
}

// diffOnly is the shared front half: extract -> load both -> diff -> render.
func diffOnly(ctx context.Context, o Options) (*Result, *ir.Document, *ir.Document, error) {
	cfg := o.Config
	stderr := o.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	if err := extract(ctx, cfg, stderr); err != nil {
		return nil, nil, nil, err
	}

	published, err := loadCanonical(cfg.Published)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load published spec %s: %w", cfg.Published, err)
	}
	code, err := loadCanonical(cfg.Code.File)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load code spec %s: %w", cfg.Code.File, err)
	}

	report := diff.Diff(published, code)
	rendered, err := render(report, cfg.Report)
	if err != nil {
		return nil, nil, nil, err
	}

	res := &Result{
		Report:         report,
		RenderedReport: rendered,
		ExitCode:       exitCode(report, cfg.FailOn),
	}
	return res, published, code, nil
}

// extract runs the code.command (if any) with the config's directory as cwd,
// then verifies code.file exists.
func extract(ctx context.Context, cfg *config.Config, stderr io.Writer) error {
	if cfg.Code.Command != "" {
		fmt.Fprintf(stderr, "driftsync: extracting code spec: %s\n", cfg.Code.Command)
		cmd := exec.CommandContext(ctx, "sh", "-c", cfg.Code.Command)
		cmd.Dir = cfg.BaseDir
		cmd.Env = os.Environ()
		var buf bytes.Buffer
		cmd.Stdout = io.MultiWriter(stderr, &buf)
		cmd.Stderr = io.MultiWriter(stderr, &buf)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("code.command failed (%v): %s", err, tail(buf.String(), 800))
		}
	}
	if _, err := os.Stat(cfg.Code.File); err != nil {
		if cfg.Code.Command == "" {
			return fmt.Errorf("code.file %s does not exist and no code.command is set to create it", cfg.Code.File)
		}
		return fmt.Errorf("code.command ran but did not produce code.file %s", cfg.Code.File)
	}
	return nil
}

// loadCanonical: file -> IR -> canonical 3.1 (mirrors cmd/driftsync/main.go).
func loadCanonical(path string) (*ir.Document, error) {
	doc, err := specfile.New(path).Extract(context.Background())
	if err != nil {
		return nil, err
	}
	return canonicalize.Apply(doc)
}

func render(r *diff.Report, f config.Format) (string, error) {
	switch f {
	case config.FormatMD:
		return r.Markdown(), nil
	case config.FormatJSON:
		return r.JSON()
	default:
		return r.Text(), nil
	}
}

// renderHints appends a plain-text/markdown section listing LLM-suggested
// renames the deterministic matcher didn't catch. Advisory only: it never
// implies the patches above changed — they're still a separate remove+add.
func renderHints(hints []enrich.RenameHint) string {
	var b strings.Builder
	b.WriteString("\n\n## Possible renames (unconfirmed — please verify)\n\n")
	b.WriteString("driftsync's deterministic matcher didn't pair these (different names), " +
		"but an LLM flagged them as plausibly the same field renamed. The patches above " +
		"still apply them as a separate removal and addition — nothing here changed that.\n\n")
	for _, h := range hints {
		fmt.Fprintf(&b, "- **%s** [%s]: `%s` -> `%s` — %s\n", h.Schema, h.Direction, h.From, h.To, h.Note)
	}
	return b.String()
}

func exitCode(r *diff.Report, f config.FailOn) int {
	switch f {
	case config.FailOnAny:
		if len(r.Changes) > 0 {
			return 1
		}
	case config.FailOnNever:
		return 0
	default: // breaking
		if r.Breaking() > 0 {
			return 1
		}
	}
	return 0
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}
