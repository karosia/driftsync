package applier

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"driftsync/patch"
)

// Failure records a patch that could not be applied, with why — never silently
// dropped. A pointer that no longer resolves (e.g. the doc was hand-edited)
// surfaces here so a human can reconcile it.
type Failure struct {
	Patch  patch.Patch
	Reason string
}

// Apply executes patches against the PUBLISHED document bytes (the original, not
// the canonical form) so human-written prose and formatting are preserved and
// the resulting diff stays minimal. It returns the edited bytes plus which
// patches applied and which failed. A failing patch does not abort the rest —
// partial success is the contract.
func Apply(publishedRaw []byte, patches []patch.Patch) (result []byte, applied []patch.Patch, failed []Failure, err error) {
	var doc yaml.Node
	if err = yaml.Unmarshal(publishedRaw, &doc); err != nil {
		return nil, nil, nil, fmt.Errorf("applier: parse published: %w", err)
	}
	root := documentRoot(&doc)
	if root == nil {
		return nil, nil, nil, fmt.Errorf("applier: empty document")
	}

	for _, p := range order(patches) {
		if e := applyOne(root, p); e != nil {
			failed = append(failed, Failure{Patch: p, Reason: e.Error()})
			continue
		}
		applied = append(applied, p)
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return nil, applied, failed, fmt.Errorf("applier: render: %w", err)
	}
	return out, applied, failed, nil
}

// order sorts patches so array removals by index run high-index-first (removing
// a low index would shift the ones after it). All non-array-remove ops keep
// their given order; array removes are pushed last and reverse-sorted by index.
func order(patches []patch.Patch) []patch.Patch {
	out := make([]patch.Patch, len(patches))
	copy(out, patches)
	sort.SliceStable(out, func(i, j int) bool {
		ai, aj := arrayRemoveIndex(out[i]), arrayRemoveIndex(out[j])
		if ai >= 0 && aj >= 0 {
			return ai > aj // both array removes: higher index first
		}
		return aj >= 0 // push array removes after everything else
	})
	return out
}

// arrayRemoveIndex returns the numeric last token of a remove-op path, or -1.
func arrayRemoveIndex(p patch.Patch) int {
	if p.Op != patch.OpRemove {
		return -1
	}
	toks := parsePointer(p.Path)
	if len(toks) == 0 {
		return -1
	}
	n, err := strconv.Atoi(toks[len(toks)-1])
	if err != nil {
		return -1
	}
	return n
}

// applyOne resolves p.Path against root and performs the op.
func applyOne(root *yaml.Node, p patch.Patch) error {
	toks := parsePointer(p.Path)
	if len(toks) == 0 {
		return fmt.Errorf("empty pointer")
	}
	parentToks, last := toks[:len(toks)-1], toks[len(toks)-1]

	parent, err := resolve(root, parentToks)
	if err != nil {
		return err
	}

	switch p.Op {
	case patch.OpAdd:
		return addAt(parent, last, p)
	case patch.OpReplace:
		return replaceAt(parent, last, p)
	case patch.OpRemove:
		return removeAt(parent, last, p)
	default:
		return fmt.Errorf("unknown op %q", p.Op)
	}
}

// ---- op implementations ------------------------------------------------------

func addAt(parent *yaml.Node, key string, p patch.Patch) error {
	switch parent.Kind {
	case yaml.MappingNode:
		if v := mapGet(parent, key); v != nil {
			// key already present: treat add as replace (idempotent-ish)
			return replaceInMap(parent, key, p.Value)
		}
		parent.Content = append(parent.Content, scalarNode(key), cloneNode(p.Value))
		return nil
	case yaml.SequenceNode:
		if key == "-" { // append
			parent.Content = append(parent.Content, cloneNode(p.Value))
			return nil
		}
		idx, err := strconv.Atoi(key)
		if err != nil || idx < 0 || idx > len(parent.Content) {
			return fmt.Errorf("bad array index %q", key)
		}
		parent.Content = append(parent.Content[:idx],
			append([]*yaml.Node{cloneNode(p.Value)}, parent.Content[idx:]...)...)
		return nil
	}
	return fmt.Errorf("cannot add under %v node", parent.Kind)
}

func replaceAt(parent *yaml.Node, key string, p patch.Patch) error {
	switch parent.Kind {
	case yaml.MappingNode:
		if mapGet(parent, key) == nil {
			return fmt.Errorf("replace target %q not found", key)
		}
		return replaceInMap(parent, key, p.Value)
	case yaml.SequenceNode:
		idx, err := strconv.Atoi(key)
		if err != nil || idx < 0 || idx >= len(parent.Content) {
			return fmt.Errorf("bad array index %q", key)
		}
		parent.Content[idx] = cloneNode(p.Value)
		return nil
	}
	return fmt.Errorf("cannot replace under %v node", parent.Kind)
}

func removeAt(parent *yaml.Node, key string, p patch.Patch) error {
	switch parent.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(parent.Content); i += 2 {
			if parent.Content[i].Value == key {
				parent.Content = append(parent.Content[:i], parent.Content[i+2:]...)
				return nil
			}
		}
		return fmt.Errorf("remove target %q not found", key)
	case yaml.SequenceNode:
		// Two removal styles: by numeric index, or by value (patch carries the
		// value to find — this is the deferred RequiredRemoved case).
		if idx, err := strconv.Atoi(key); err == nil {
			if idx < 0 || idx >= len(parent.Content) {
				return fmt.Errorf("array index %d out of range", idx)
			}
			parent.Content = append(parent.Content[:idx], parent.Content[idx+1:]...)
			return nil
		}
		return removeByValue(parent, p.Value)
	}
	return fmt.Errorf("cannot remove under %v node", parent.Kind)
}

// removeByValue deletes the first sequence element whose scalar equals want.
// Used when the pointer addresses the array itself and the value identifies the
// element (JSON Pointer can't address array elements by value).
func removeByValue(seq *yaml.Node, want *yaml.Node) error {
	if want == nil {
		return fmt.Errorf("value-based remove needs a value")
	}
	for i, el := range seq.Content {
		if el.Kind == yaml.ScalarNode && el.Value == want.Value {
			seq.Content = append(seq.Content[:i], seq.Content[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("value %q not found in array", want.Value)
}

func replaceInMap(m *yaml.Node, key string, val *yaml.Node) error {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = cloneNode(val)
			return nil
		}
	}
	return fmt.Errorf("key %q not found", key)
}

// ---- pointer resolution ------------------------------------------------------

// parsePointer splits a JSON Pointer into decoded tokens (RFC 6901): leading "/"
// dropped, then "~1" -> "/" and "~0" -> "~" per token.
func parsePointer(ptr string) []string {
	ptr = strings.TrimPrefix(ptr, "/")
	if ptr == "" {
		return nil
	}
	toks := strings.Split(ptr, "/")
	for i, t := range toks {
		t = strings.ReplaceAll(t, "~1", "/")
		t = strings.ReplaceAll(t, "~0", "~")
		toks[i] = t
	}
	return toks
}

// resolve walks tokens from root, returning the node they address.
func resolve(root *yaml.Node, toks []string) (*yaml.Node, error) {
	n := root
	for _, t := range toks {
		switch n.Kind {
		case yaml.MappingNode:
			next := mapGet(n, t)
			if next == nil {
				return nil, fmt.Errorf("path segment %q not found", t)
			}
			n = next
		case yaml.SequenceNode:
			idx, err := strconv.Atoi(t)
			if err != nil || idx < 0 || idx >= len(n.Content) {
				return nil, fmt.Errorf("bad array index %q", t)
			}
			n = n.Content[idx]
		default:
			return nil, fmt.Errorf("cannot descend into %v at %q", n.Kind, t)
		}
	}
	return n, nil
}

// ---- small helpers -----------------------------------------------------------

func documentRoot(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return doc.Content[0]
	}
	return doc
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

// cloneNode deep-copies a node so patch values (taken from the code tree) don't
// alias into the published tree we're editing.
func cloneNode(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	c := *n
	c.Content = make([]*yaml.Node, len(n.Content))
	for i, ch := range n.Content {
		c.Content[i] = cloneNode(ch)
	}
	return &c
}
