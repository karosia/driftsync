package config

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed templates
var templatesFS embed.FS

// Stacks are the recognized `driftsync init --stack` values.
var Stacks = []string{"fastapi", "nestjs", "spring", "huma", "swaggo", "generic"}

// ConfigTemplate returns the starter driftsync.yaml for a stack.
func ConfigTemplate(stack string) ([]byte, error) {
	if !oneOf(stack, Stacks...) {
		return nil, fmt.Errorf("unknown stack %q (want one of: %s)", stack, strings.Join(Stacks, ", "))
	}
	return templatesFS.ReadFile("templates/config/" + stack + ".driftsync.yaml")
}

// WorkflowTemplate returns a GitHub Actions workflow: "check" or "sync".
func WorkflowTemplate(kind string) ([]byte, error) {
	switch kind {
	case "check", "sync":
		return templatesFS.ReadFile("templates/workflows/drift-" + kind + ".yml")
	default:
		return nil, fmt.Errorf("unknown workflow %q (want: check, sync)", kind)
	}
}

// GenspecTemplate returns the tools/genspec/main.go starter for in-process
// (huma) extraction. Only meaningful for --stack huma.
func GenspecTemplate() ([]byte, error) {
	return templatesFS.ReadFile("templates/huma/genspec.go.tmpl")
}

// ExampleConfigs lists the embedded per-stack configs, for docs/tests.
func ExampleConfigs() []string {
	var out []string
	_ = fs.WalkDir(templatesFS, "templates/config", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out
}
