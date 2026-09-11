package patch

import (
	"fmt"
	"sort"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/karosia/driftsync/diff"
	"github.com/karosia/driftsync/ir"
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
	Path        string            // JSON Pointer (RFC 6901) into the published document
	Value       *yaml.Node        // fragment to add/replace, copied from the code doc; nil for remove
	Match       map[string]string // identify an array element (parameters: name+in)
	CreatePath  bool              // for add: create missing map ancestors (new operations)
	Description string            // prose slot, filled by the later LLM stage
	Reason      string            // provenance: which drift this resolves (for review)
	Severity    diff.Severity
}

// FromReport turns detected drift into a deterministic list of patch intents.
// The code document supplies the authoritative values (code-first authority).
//
// A shared schema drifts once per direction (request/response) for severity, but
// a document edit is direction-agnostic, so identical patches are deduped here.
func FromReport(report *diff.Report, code *ir.Document) ([]Patch, error) {
	var codeRoot yaml.Node
	if err := yaml.Unmarshal(code.Raw, &codeRoot); err != nil {
		return nil, fmt.Errorf("patch: parse code doc: %w", err)
	}

	var raw []Patch
	for _, c := range report.Changes {
		raw = append(raw, patchesFor(c, &codeRoot)...)
	}
	return dedup(raw), nil
}

// patchesFor produces the patch intents for a single change (empty if the change
// isn't something this generator handles yet).
func patchesFor(c diff.Change, codeRoot *yaml.Node) []Patch {
	t := c.Target
	propPath := func(prop string) string {
		return "/components/schemas/" + esc(t.Schema) + "/properties/" + esc(prop)
	}
	reqPath := "/components/schemas/" + esc(t.Schema) + "/required"
	opBase := func() string { return "/paths/" + esc(t.Path) + "/" + strings.ToLower(t.Method) }

	switch c.Kind {
	// ---- schemas (whole component) ----
	case diff.SchemaAdded:
		return []Patch{{Op: OpAdd, Path: "/components/schemas/" + esc(t.Schema), CreatePath: true,
			Value:  extract(codeRoot, "components", "schemas", t.Schema),
			Reason: reason(c), Severity: c.Severity}}

	case diff.SchemaRemoved:
		return []Patch{{Op: OpRemove, Path: "/components/schemas/" + esc(t.Schema),
			Reason: reason(c), Severity: c.Severity}}

	// ---- schema properties ----
	case diff.PropertyRemoved:
		patches := []Patch{{Op: OpRemove, Path: propPath(t.Property), Reason: reason(c), Severity: c.Severity}}
		// A removed property that was required would otherwise leave `required`
		// naming a property that no longer exists (same class of bug as a
		// rename — see #2 — just without the rename's "to" side).
		if c.FromRequired {
			patches = append(patches, Patch{Op: OpRemove, Path: reqPath, Value: scalarNode(t.Property),
				Reason: reason(c), Severity: c.Severity})
		}
		return patches

	case diff.PropertyAdded:
		return []Patch{{Op: OpAdd, Path: propPath(t.Property),
			Value:  extract(codeRoot, "components", "schemas", t.Schema, "properties", t.Property),
			Reason: reason(c), Severity: c.Severity}}

	case diff.PropertyTypeChanged:
		return []Patch{{Op: OpReplace, Path: propPath(t.Property),
			Value:  extract(codeRoot, "components", "schemas", t.Schema, "properties", t.Property),
			Reason: reason(c), Severity: c.Severity}}

	case diff.PropertyRenamed:
		patches := []Patch{
			{Op: OpRemove, Path: propPath(c.From), Reason: reason(c), Severity: c.Severity},
			{Op: OpAdd, Path: propPath(c.To),
				Value:  extract(codeRoot, "components", "schemas", t.Schema, "properties", c.To),
				Reason: reason(c), Severity: c.Severity}}
		// A rename changes the property's name, so required[] (which is
		// name-keyed) needs its own add/remove — it isn't covered by the
		// properties edit above. See #2.
		if c.FromRequired {
			patches = append(patches, Patch{Op: OpRemove, Path: reqPath, Value: scalarNode(c.From),
				Reason: reason(c), Severity: c.Severity})
		}
		if c.ToRequired {
			patches = append(patches, Patch{Op: OpAdd, Path: reqPath + "/-", Value: scalarNode(c.To),
				Reason: reason(c), Severity: c.Severity})
		}
		return patches

	case diff.RequiredAdded:
		return []Patch{{Op: OpAdd, Path: reqPath + "/-", Value: scalarNode(t.Property),
			Reason: reason(c), Severity: c.Severity}}

	case diff.RequiredRemoved:
		return []Patch{{Op: OpRemove, Path: reqPath, Value: scalarNode(t.Property),
			Reason: reason(c), Severity: c.Severity}}

	// ---- operations ----
	case diff.OperationAdded:
		return []Patch{{Op: OpAdd, Path: opBase(), CreatePath: true,
			Value:  extract(codeRoot, "paths", t.Path, strings.ToLower(t.Method)),
			Reason: reason(c), Severity: c.Severity}}

	case diff.OperationRemoved:
		return []Patch{{Op: OpRemove, Path: opBase(), Reason: reason(c), Severity: c.Severity}}

	// ---- responses (map keyed by status code) ----
	case diff.ResponseAdded:
		return []Patch{{Op: OpAdd, Path: opBase() + "/responses/" + esc(t.StatusCode),
			Value:  extract(codeRoot, "paths", t.Path, strings.ToLower(t.Method), "responses", t.StatusCode),
			Reason: reason(c), Severity: c.Severity}}

	case diff.ResponseRemoved:
		return []Patch{{Op: OpRemove, Path: opBase() + "/responses/" + esc(t.StatusCode),
			Reason: reason(c), Severity: c.Severity}}

	// ---- parameters (array addressed by identity: name+in) ----
	case diff.ParameterAdded:
		return []Patch{{Op: OpAdd, Path: opBase() + "/parameters/-",
			Value:  findParamNode(codeRoot, t.Path, strings.ToLower(t.Method), t.ParamName, t.ParamIn),
			Reason: reason(c), Severity: c.Severity}}

	case diff.ParameterRemoved:
		return []Patch{{Op: OpRemove, Path: opBase() + "/parameters",
			Match:  map[string]string{"name": t.ParamName, "in": t.ParamIn},
			Reason: reason(c), Severity: c.Severity}}

	case diff.ParameterTypeChanged, diff.ParameterRequiredChanged:
		// Replace the whole parameter with the code version. Params rarely carry
		// hand-written prose, so wholesale replace keeps type+required changes as
		// one idempotent edit (dedup collapses the two kinds into one).
		return []Patch{{Op: OpReplace, Path: opBase() + "/parameters",
			Match:  map[string]string{"name": t.ParamName, "in": t.ParamIn},
			Value:  findParamNode(codeRoot, t.Path, strings.ToLower(t.Method), t.ParamName, t.ParamIn),
			Reason: reason(c), Severity: c.Severity}}

		// diff.RequiredDangling deliberately falls through to nil below: it flags
		// a `required` entry naming no property in ONE document, which isn't a
		// drift between published and code with a code-side value to apply.
	}
	return nil
}

// ---- dedup -------------------------------------------------------------------

// dedup collapses patches that make the SAME edit (same op+path+value+match). A
// document edit is direction-agnostic — request and response drift on a shared
// schema produce identical patches — so we keep one and retain the WORST
// severity (e.g. info in request but BREAKING in response -> BREAKING).
func dedup(in []Patch) []Patch {
	seen := map[string]int{} // key -> index in out
	var out []Patch
	for _, p := range in {
		key := string(p.Op) + " " + p.Path + " " + valueKey(p) + " " + matchKey(p)
		if idx, ok := seen[key]; ok {
			if p.Severity > out[idx].Severity {
				out[idx].Severity = p.Severity
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, p)
	}
	return out
}

// valueKey distinguishes patches sharing op+path but carrying different values
// (e.g. two different appends to the same "required/-" position).
func valueKey(p Patch) string {
	if p.Value == nil {
		return ""
	}
	out, err := yaml.Marshal(p.Value)
	if err != nil {
		return ""
	}
	return strings.Join(strings.Fields(strings.TrimSpace(string(out))), " ")
}

// matchKey serializes the Match map deterministically so two patches targeting
// the same array element (same name+in) collapse together.
func matchKey(p Patch) string {
	if p.Match == nil {
		return ""
	}
	keys := make([]string, 0, len(p.Match))
	for k := range p.Match {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k + "=" + p.Match[k] + ";")
	}
	return b.String()
}

// ---- node helpers ------------------------------------------------------------

// extract returns the node at a mapping-key path in the tree (nil if absent).
// Only mapping navigation is needed for our targets.
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

// findParamNode locates a parameter element in the code tree by name+in, so its
// full node can be copied into an add/replace patch value.
func findParamNode(root *yaml.Node, path, method, name, in string) *yaml.Node {
	params := extract(root, "paths", path, method, "parameters")
	if params == nil || params.Kind != yaml.SequenceNode {
		return nil
	}
	for _, el := range params.Content {
		if el.Kind == yaml.MappingNode && nodeVal(el, "name") == name && nodeVal(el, "in") == in {
			return el
		}
	}
	return nil
}

func mapGet(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func nodeVal(m *yaml.Node, key string) string {
	if v := mapGet(m, key); v != nil {
		return v.Value
	}
	return ""
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
