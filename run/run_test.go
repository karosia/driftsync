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

// TestSync_SchemaAddedAndRemoved covers a whole new/removed component schema
// (not just a property within one) actually reaching the corrected spec —
// previously patchesFor had no case for SchemaAdded/SchemaRemoved, so sync
// silently produced zero patches for either.
func TestSync_SchemaAddedAndRemoved(t *testing.T) {
	dir := t.TempDir()
	pub := filepath.Join(dir, "published.yaml")
	code := filepath.Join(dir, "code.yaml")
	if err := os.WriteFile(pub, []byte(`openapi: 3.0.3
info: {title: T, version: '1.0.0'}
paths: {}
components:
  schemas:
    Legacy:
      type: object
      properties: {id: {type: string}}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(code, []byte(`openapi: 3.1.0
info: {title: T, version: '1.0.0'}
paths: {}
components:
  schemas:
    Widget:
      type: object
      properties: {name: {type: string}, count: {type: integer}}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	body := "version: 1\npublished: " + pub +
		"\ncode:\n  command: cp " + code + " openapi.gen.yaml\n  file: openapi.gen.yaml\n"
	cfgPath := filepath.Join(dir, "driftsync.yaml")
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	res, err := run.Sync(context.Background(), run.Options{Config: cfg, Stderr: io_discard{}})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.Applied != 2 {
		t.Fatalf("expected 2 applied patches (add Widget, remove Legacy), got %d: %+v", res.Applied, res.Failed)
	}
	got := string(res.Corrected)
	if strings.Contains(got, "Legacy:") {
		t.Errorf("Legacy should have been removed from the corrected spec:\n%s", got)
	}
	if !strings.Contains(got, "Widget:") {
		t.Errorf("Widget should have been added to the corrected spec:\n%s", got)
	}
}

// TestSync_SchemaRenamed_FixesDanglingRef is the schema-level counterpart to
// the property-rename fix in #2/#3: renaming a whole component schema
// (UserDTO -> User, same shape) must also rewrite the existing $ref that
// points to it — otherwise the corrected spec has a dangling reference to a
// schema that no longer exists.
func TestSync_SchemaRenamed_FixesDanglingRef(t *testing.T) {
	dir := t.TempDir()
	pub := filepath.Join(dir, "published.yaml")
	code := filepath.Join(dir, "code.yaml")
	if err := os.WriteFile(pub, []byte(`openapi: 3.0.3
info: {title: T, version: '1.0.0'}
paths:
  /users/{id}:
    get:
      operationId: get-user
      responses:
        '200':
          description: OK
          content:
            application/json:
              schema: {$ref: '#/components/schemas/UserDTO'}
components:
  schemas:
    UserDTO:
      type: object
      required: [id]
      properties: {id: {type: string}, email: {type: string}}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(code, []byte(`openapi: 3.1.0
info: {title: T, version: '1.0.0'}
paths:
  /users/{id}:
    get:
      operationId: get-user
      responses:
        '200':
          description: OK
          content:
            application/json:
              schema: {$ref: '#/components/schemas/User'}
components:
  schemas:
    User:
      type: object
      required: [id]
      properties: {id: {type: string}, email: {type: string}}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	body := "version: 1\npublished: " + pub +
		"\ncode:\n  command: cp " + code + " openapi.gen.yaml\n  file: openapi.gen.yaml\n"
	cfgPath := filepath.Join(dir, "driftsync.yaml")
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	res, err := run.Sync(context.Background(), run.Options{Config: cfg, Stderr: io_discard{}})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(res.Failed) != 0 {
		t.Fatalf("expected no failed patches, got %+v", res.Failed)
	}
	got := string(res.Corrected)
	if strings.Contains(got, "UserDTO") {
		t.Errorf("no trace of the old schema name should remain (dangling $ref or leftover schema):\n%s", got)
	}
	if !strings.Contains(got, "#/components/schemas/User") {
		t.Errorf("the response $ref should now point at the renamed schema:\n%s", got)
	}

	// Convergence: re-checking the corrected spec against the code fixture
	// must find no drift at all.
	correctedPath := filepath.Join(dir, "published.corrected.yaml")
	if err := os.WriteFile(correctedPath, res.Corrected, 0o644); err != nil {
		t.Fatal(err)
	}
	body2 := "version: 1\npublished: " + correctedPath +
		"\ncode:\n  command: cp " + code + " openapi.gen.yaml\n  file: openapi.gen.yaml\n"
	cfgPath2 := filepath.Join(dir, "driftsync2.yaml")
	if err := os.WriteFile(cfgPath2, []byte(body2), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg2, err := config.Load(cfgPath2)
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
