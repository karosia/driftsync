package run_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/karosia/driftsync/config"
	"github.com/karosia/driftsync/enrich"
	"github.com/karosia/driftsync/run"
)

// fixtures lives at the repo root; run/ is one level down.
func fixture(t *testing.T, name string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return abs
}

// writeConfig drops a driftsync.yaml in a temp dir whose code.command just
// copies a fixture into place, and returns the loaded config.
func writeConfig(t *testing.T, codeFixture, extra string) *config.Config {
	t.Helper()
	dir := t.TempDir()
	body := "" +
		"version: 1\n" +
		"published: " + fixture(t, "published.yaml") + "\n" +
		"code:\n" +
		"  command: cp " + fixture(t, codeFixture) + " openapi.gen.yaml\n" +
		"  file: openapi.gen.yaml\n" +
		extra
	p := filepath.Join(dir, "driftsync.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return c
}

func TestCheck_ExitCodes(t *testing.T) {
	cases := []struct {
		name       string
		codeSpec   string
		failOn     string
		wantExit   int
		wantChange bool
	}{
		{"breaking drift, gate=breaking", "code.yaml", "fail_on: breaking\n", 1, true},
		{"breaking drift, gate=any", "code.yaml", "fail_on: any\n", 1, true},
		{"breaking drift, gate=never", "code.yaml", "fail_on: never\n", 0, true},
		{"no drift, gate=any", "published.yaml", "fail_on: any\n", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := writeConfig(t, tc.codeSpec, tc.failOn)
			res, err := run.Check(context.Background(), run.Options{Config: cfg, Stderr: io_discard{}})
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if res.ExitCode != tc.wantExit {
				t.Errorf("ExitCode = %d, want %d", res.ExitCode, tc.wantExit)
			}
			if got := len(res.Report.Changes) > 0; got != tc.wantChange {
				t.Errorf("changes present = %v, want %v", got, tc.wantChange)
			}
		})
	}
}

func TestCheck_MissingCommandOutput(t *testing.T) {
	dir := t.TempDir()
	body := "version: 1\npublished: " + fixture(t, "published.yaml") +
		"\ncode:\n  command: \"true\"\n  file: openapi.gen.yaml\n"
	p := filepath.Join(dir, "driftsync.yaml")
	os.WriteFile(p, []byte(body), 0o644)
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := run.Check(context.Background(), run.Options{Config: cfg, Stderr: io_discard{}}); err == nil {
		t.Fatal("expected an error when code.command produces no file")
	}
}

func TestSync_ProducesFixedSpec(t *testing.T) {
	cfg := writeConfig(t, "code.yaml", "fail_on: breaking\n")
	res, err := run.Sync(context.Background(), run.Options{Config: cfg, Stderr: io_discard{}})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("Sync ExitCode = %d, want 0 (the PR is the gate)", res.ExitCode)
	}
	if len(res.Patches) == 0 || res.Applied == 0 {
		t.Fatalf("expected patches to be proposed and applied: %+v", res)
	}
	want, err := os.ReadFile(fixture(t, "published.fixed.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if norm(string(res.Corrected)) != norm(string(want)) {
		t.Errorf("corrected spec != published.fixed.yaml fixture\n--- got ---\n%s", res.Corrected)
	}

	// Regression for #2: a property rename must converge in one sync pass —
	// re-checking the corrected spec against the same code fixture must find
	// no drift at all (in particular, no leftover/missing `required` entry).
	dir := t.TempDir()
	correctedPath := filepath.Join(dir, "published.yaml")
	if err := os.WriteFile(correctedPath, res.Corrected, 0o644); err != nil {
		t.Fatal(err)
	}
	body := "version: 1\npublished: " + correctedPath +
		"\ncode:\n  command: cp " + fixture(t, "code.yaml") + " openapi.gen.yaml\n  file: openapi.gen.yaml\n"
	p := filepath.Join(dir, "driftsync.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg2, err := config.Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	recheck, err := run.Check(context.Background(), run.Options{Config: cfg2, Stderr: io_discard{}})
	if err != nil {
		t.Fatalf("recheck: %v", err)
	}
	if len(recheck.Report.Changes) != 0 {
		t.Errorf("sync did not converge in one pass, re-check still reports drift: %+v", recheck.Report.Changes)
	}
}

func TestSync_EnricherFillsDescriptions(t *testing.T) {
	cfg := writeConfig(t, "code.yaml", "")
	res, err := run.Sync(context.Background(), run.Options{
		Config: cfg, Enricher: enrich.StubEnricher{}, Stderr: io_discard{},
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	var described int
	for _, p := range res.Patches {
		if p.Description != "" {
			described++
		}
	}
	if described == 0 {
		t.Error("expected the stub enricher to describe at least one added field")
	}
}

func norm(s string) string {
	var out []string
	for _, ln := range strings.Split(s, "\n") {
		if strings.TrimSpace(ln) != "" {
			out = append(out, strings.TrimRight(ln, " "))
		}
	}
	return strings.Join(out, "\n")
}

type io_discard struct{}

func (io_discard) Write(p []byte) (int, error) { return len(p), nil }
