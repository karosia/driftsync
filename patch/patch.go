package patch

import (
	"fmt"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"driftsync/diff"
	"driftsync/ir"
)

type Op string

const (
	OpAdd     Op = "add"
	OpRemove  Op = "remove"
	OpReplace Op = "replace"
)

// Patch is a single, reviewable INTENT to change the published document.
// It is data, not an edit: a later applier executes it only after human
// approval. Value comes from the code side; Description is a slot the LLM stage
// fills later (empty here, since this generator is fully deterministic).
type Patch struct {
	Op          Op
	Path        string     // JSON Pointer (RFC 6901) into the published document
	Value       *yaml.Node // fragment to add/replace, copied from the code doc; nil for remove
	Description string     // prose slot, filled by the later LLM stage
	Reason      string     // provenance: which drift this resolves (for review)
	Severity    diff.Severity
}

// FromReport turns detected drift into a deterministic list of patch intents.
// The code document supplies the authoritative values (code-first authority).
// Scope (this slice): schema-property drifts. Operation/param/response patches
// come next; those changes carry an empty Target and are skipped here.
func FromReport(report *diff.Report, code *ir.Document) ([]Patch, error) {
	var codeRoot yaml.Node
	if err := yaml.Unmarshal(code.Raw, &codeRoot); err != nil {
		return nil, fmt.Errorf("patch: parse code doc: %w", err)
	}

	var patches []Patch
	for _, c := range report.Changes {
		t := c.Target
		if t.Schema == "" {
			continue // not a schema-property change; out of scope this slice
		}
		propPath := func(prop string) string {
			return "/components/schemas/" + esc(t.Schema) + "/properties/" + esc(prop)
		}
		reqPath := "/components/schemas/" + esc(t.Schema) + "/required"

		switch c.Kind {
		case diff.PropertyRemoved:
			patches = append(patches, Patch{
				Op: OpRemove, Path: propPath(t.Property),
				Reason: reason(c), Severity: c.Severity,
			})

		case diff.PropertyAdded:
			patches = append(patches, Patch{
				Op: OpAdd, Path: propPath(t.Property),
				Value:  extract(&codeRoot, "components", "schemas", t.Schema, "properties", t.Property),
				Reason: reason(c), Severity: c.Severity,
			})

		case diff.PropertyTypeChanged:
			patches = append(patches, Patch{
				Op: OpReplace, Path: propPath(t.Property),
				Value:  extract(&codeRoot, "components", "schemas", t.Schema, "properties", t.Property),
				Reason: reason(c), Severity: c.Severity,
			})

		case diff.PropertyRenamed:
			// A rename = remove the old name + add the new one (copied from code).
			patches = append(patches,
				Patch{Op: OpRemove, Path: propPath(c.From), Reason: reason(c), Severity: c.Severity},
				Patch{Op: OpAdd, Path: propPath(c.To),
					Value:  extract(&codeRoot, "components", "schemas", t.Schema, "properties", c.To),
					Reason: reason(c), Severity: c.Severity},
			)

		case diff.RequiredAdded:
			// append to the required array (JSON Pointer "-" = end of array)
			patches = append(patches, Patch{
				Op: OpAdd, Path: reqPath + "/-", Value: scalarNode(t.Property),
				Reason: reason(c), Severity: c.Severity,
			})

		case diff.RequiredRemoved:
			// Removing an array element is by value here; the applier resolves the
			// index (JSON Pointer addresses arrays by index, not value).
			patches = append(patches, Patch{
				Op: OpRemove, Path: reqPath, Value: scalarNode(t.Property),
				Reason: reason(c), Severity: c.Severity,
			})
		}
	}
	return patches, nil
}

// ---- helpers -----------------------------------------------------------------

// extract returns the node at a mapping-key path in the tree (nil if absent).
// Only mapping navigation is needed for our schema-property targets.
func extract(root *yaml.Node, keys ...string) *yaml.Node {
	n := root
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	for _, k := range keys {
		if n == nil || n.Kind != yaml.MappingNode {
			return nil
		}
		n = mapGet(n, k)
	}
	return n
}

func mapGet(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func scalarNode(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}

// esc applies RFC 6901 JSON Pointer escaping: "~" -> "~0", "/" -> "~1".
func esc(s string) string {
	s = strings.ReplaceAll(s, "~", "~0")
	s = strings.ReplaceAll(s, "/", "~1")
	return s
}

func reason(c diff.Change) string {
	r := string(c.Kind) + " @ " + c.Location
	if c.Note != "" {
		r += " (" + c.Note + ")"
	}
	return r
}
