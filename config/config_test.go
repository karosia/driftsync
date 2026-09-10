package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/karosia/driftsync/config"
)

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

func TestLoad_DefaultsAndPathResolution(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "driftsync.yaml", `
version: 1
published: docs/openapi.yaml
code:
  command: make openapi
  file: gen/openapi.yaml
`)

	c, err := config.Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.FailOn != config.FailOnBreaking || c.Enrich != config.EnrichAuto || c.Report != config.FormatText {
		t.Errorf("defaults not applied: %+v", c)
	}
	if c.Published != filepath.Join(dir, "docs/openapi.yaml") {
		t.Errorf("published not resolved against config dir: %s", c.Published)
	}
	if c.Code.File != filepath.Join(dir, "gen/openapi.yaml") {
		t.Errorf("code.file not resolved: %s", c.Code.File)
	}
	if c.Output() != c.Published {
		t.Errorf("Output() should default to Published, got %s", c.Output())
	}
}

func TestLoad_SyncOutputOverridesOutput(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "driftsync.yaml", `
version: 1
published: a.yaml
code: { file: b.yaml }
sync_output: out/c.yaml
`)
	c, err := config.Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Output() != filepath.Join(dir, "out/c.yaml") {
		t.Errorf("Output() = %s", c.Output())
	}
}

func TestLoad_Errors(t *testing.T) {
	cases := map[string]string{
		"bad version": `version: 2
published: a.yaml
code: { file: b.yaml }`,
		"missing published": `version: 1
code: { file: b.yaml }`,
		"missing code.file": `version: 1
published: a.yaml`,
		"bad fail_on": `version: 1
published: a.yaml
code: { file: b.yaml }
fail_on: sometimes`,
		"bad enrich": `version: 1
published: a.yaml
code: { file: b.yaml }
enrich: maybe`,
		"unknown key": `version: 1
published: a.yaml
code: { file: b.yaml }
fial_on: breaking`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			p := write(t, dir, "driftsync.yaml", body)
			if _, err := config.Load(p); err == nil {
				t.Fatalf("expected an error for %q", name)
			}
		})
	}
}

func TestLoad_Missing(t *testing.T) {
	if _, err := config.Load(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func TestValidate_AfterOverride(t *testing.T) {
	c := &config.Config{Version: 1, Published: "a", Code: config.Code{File: "b"},
		FailOn: config.FailOnBreaking, Enrich: config.EnrichAuto, Report: config.FormatText}
	if err := c.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	c.FailOn = "garbage"
	if err := c.Validate(); err == nil {
		t.Fatal("expected Validate to reject a bad fail_on")
	}
}

// Every embedded stack template must itself be a loadable, valid config.
func TestEmbeddedTemplatesAreValid(t *testing.T) {
	for _, stack := range config.Stacks {
		t.Run(stack, func(t *testing.T) {
			b, err := config.ConfigTemplate(stack)
			if err != nil {
				t.Fatalf("ConfigTemplate: %v", err)
			}
			dir := t.TempDir()
			p := write(t, dir, "driftsync.yaml", string(b))
			if _, err := config.Load(p); err != nil {
				t.Fatalf("embedded %s template does not load: %v", stack, err)
			}
		})
	}
	if _, err := config.ConfigTemplate("cobol"); err == nil {
		t.Error("expected an error for an unknown stack")
	}
}

// The browsable examples/ copies must stay byte-identical to what `driftsync
// init` writes (the embedded templates). Regenerate with:
//
//	for f in config/templates/config/*; do cp "$f" "examples/config/$(basename $f)"; done
//	cp config/templates/workflows/*.yml examples/workflows/
func TestExamplesMatchEmbedded(t *testing.T) {
	for _, stack := range config.Stacks {
		want, _ := config.ConfigTemplate(stack)
		got, err := os.ReadFile(filepath.Join("..", "examples", "config", stack+".driftsync.yaml"))
		if err != nil || string(got) != string(want) {
			t.Errorf("examples/config/%s.driftsync.yaml is stale (err=%v)", stack, err)
		}
	}
	for _, kind := range []string{"check", "sync"} {
		want, _ := config.WorkflowTemplate(kind)
		got, err := os.ReadFile(filepath.Join("..", "examples", "workflows", "drift-"+kind+".yml"))
		if err != nil || string(got) != string(want) {
			t.Errorf("examples/workflows/drift-%s.yml is stale (err=%v)", kind, err)
		}
	}
}

func TestWorkflowAndGenspecTemplates(t *testing.T) {
	for _, kind := range []string{"check", "sync"} {
		b, err := config.WorkflowTemplate(kind)
		if err != nil || len(b) == 0 {
			t.Fatalf("WorkflowTemplate(%q): %v", kind, err)
		}
		if !strings.Contains(string(b), "karosia/driftsync@v1") {
			t.Errorf("workflow %q should reference the composite action", kind)
		}
	}
	if _, err := config.WorkflowTemplate("deploy"); err == nil {
		t.Error("expected an error for an unknown workflow kind")
	}
	if b, err := config.GenspecTemplate(); err != nil || !strings.Contains(string(b), "humaadapter") {
		t.Errorf("GenspecTemplate: %v", err)
	}
}
