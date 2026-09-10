// Package config loads and validates a driftsync.yaml file — the per-project
// description of how to obtain the code-side spec and what to compare it against.
// It is the single source of truth for the `check`, `sync`, and `init` commands.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

// DefaultPath is where driftsync looks for the config when none is given.
const DefaultPath = "driftsync.yaml"

// FailOn is the CI gate policy for `driftsync check`.
type FailOn string

const (
	FailOnBreaking FailOn = "breaking" // exit non-zero only on BREAKING drift (default)
	FailOnAny      FailOn = "any"      // exit non-zero on any drift at all
	FailOnNever    FailOn = "never"    // always exit zero (report only)
)

// Enrich controls the optional LLM description step in `driftsync sync`.
type Enrich string

const (
	EnrichAuto Enrich = "auto" // run iff an LLM API key is present (default)
	EnrichOn   Enrich = "on"   // always run (falls back to an offline stub with no key)
	EnrichOff  Enrich = "off"  // never run; new-field descriptions stay blank
)

// Format is the drift-report rendering.
type Format string

const (
	FormatText Format = "text" // terminal-style (default)
	FormatMD   Format = "md"   // GitHub-flavored markdown, for a PR body
	FormatJSON Format = "json" // machine-readable
)

// Code describes how to get the OpenAPI spec your code actually produces.
type Code struct {
	// Command is a shell snippet that writes the code-side spec to File.
	// Optional: omit it when File is already generated/committed. Chain any
	// conversion here too (e.g. swaggo's 2.0 -> 3.x step).
	Command string `yaml:"command"`
	// File is the path the code-side spec is read from after Command runs
	// (or a pre-existing file when Command is empty).
	File string `yaml:"file"`
}

// Config is a parsed, validated, path-resolved driftsync.yaml.
type Config struct {
	Version int `yaml:"version"`
	// Published is the spec you maintain — the contract consumers see.
	Published string `yaml:"published"`
	Code      Code   `yaml:"code"`

	FailOn     FailOn `yaml:"fail_on"`
	Enrich     Enrich `yaml:"enrich"`
	Report     Format `yaml:"report"`
	ReportFile string `yaml:"report_file"`
	// SyncOutput is where `sync` writes the corrected spec. Empty = overwrite
	// Published in place.
	SyncOutput string `yaml:"sync_output"`

	// BaseDir is the directory the config lives in; every relative path above
	// is resolved against it and Command runs with it as the working directory.
	// Not a YAML field.
	BaseDir string `yaml:"-"`
	// Source is the path the config was loaded from, for error messages.
	Source string `yaml:"-"`
}

// Load reads, unmarshals, defaults, resolves, and validates a config file.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no config at %s — run `driftsync init --stack <your-stack>` to create one", path)
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	var c Config
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true) // reject typo'd keys instead of ignoring them
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	c.Source = path
	c.BaseDir = filepath.Dir(abs)

	c.applyDefaults()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	c.resolvePaths()
	return &c, nil
}

func (c *Config) applyDefaults() {
	if c.Version == 0 {
		c.Version = 1
	}
	if c.FailOn == "" {
		c.FailOn = FailOnBreaking
	}
	if c.Enrich == "" {
		c.Enrich = EnrichAuto
	}
	if c.Report == "" {
		c.Report = FormatText
	}
}

// Validate checks the config is internally consistent. Load calls it; the CLI
// calls it again after applying flag overrides.
func (c *Config) Validate() error {
	where := c.Source
	if where == "" {
		where = "driftsync config"
	}
	if c.Version != 1 {
		return fmt.Errorf("%s: unsupported version %d (only 1 is supported)", where, c.Version)
	}
	if strings.TrimSpace(c.Published) == "" {
		return fmt.Errorf("%s: `published` is required (path to the spec you maintain)", where)
	}
	if strings.TrimSpace(c.Code.File) == "" {
		return fmt.Errorf("%s: `code.file` is required (path driftsync reads the code-side spec from)", where)
	}
	if !oneOf(string(c.FailOn), "breaking", "any", "never") {
		return fmt.Errorf("%s: `fail_on` must be one of breaking|any|never, got %q", where, c.FailOn)
	}
	if !oneOf(string(c.Enrich), "auto", "on", "off") {
		return fmt.Errorf("%s: `enrich` must be one of auto|on|off, got %q", where, c.Enrich)
	}
	if !oneOf(string(c.Report), "text", "md", "json") {
		return fmt.Errorf("%s: `report` must be one of text|md|json, got %q", where, c.Report)
	}
	return nil
}

// resolvePaths rewrites every relative path to be absolute under BaseDir, so the
// caller can chdir freely. Code.File is intentionally left relative-friendly:
// it is resolved here too, but Command still runs with cwd = BaseDir.
func (c *Config) resolvePaths() {
	c.Published = c.abs(c.Published)
	c.Code.File = c.abs(c.Code.File)
	if c.ReportFile != "" {
		c.ReportFile = c.abs(c.ReportFile)
	}
	if c.SyncOutput != "" {
		c.SyncOutput = c.abs(c.SyncOutput)
	}
}

func (c *Config) abs(p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(c.BaseDir, p)
}

// Output is where `sync` should write the corrected published spec.
func (c *Config) Output() string {
	if c.SyncOutput != "" {
		return c.SyncOutput
	}
	return c.Published
}

func oneOf(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}
