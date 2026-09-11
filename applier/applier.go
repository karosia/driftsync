package applier

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/karosia/driftsync/patch"
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
	if p.Op != patch.OpRemove || p.Match != nil {
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

// applyOne resolves p and performs the op.
func applyOne(root *yaml.Node, p patch.Patch) error {
	// Array-element ops identified by field values (parameters by name+in).
	if p.Match != nil {
		return applyMatched(root, p)
	}

	toks := parsePointer(p.Path)
	if len(toks) == 0 {
		return fmt.Errorf("empty pointer")
	}
	parentToks, last := toks[:len(toks)-1], toks[len(toks)-1]

	var parent *yaml.Node
	var err error
	if p.CreatePath && p.Op == patch.OpAdd {
		parent, err = ensureMapPath(root, parentToks) // create missing ancestors (new paths)
	} else {
		parent, err = resolve(root, parentToks)
	}
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

// applyMatched resolves p.Path to an array and remove/replaces the element whose
// fields all equal p.Match (identity-based, so array order doesn't matter).
func applyMatched(root *yaml.Node, p patch.Patch) error {
	arr, err := resolve(root, parsePointer(p.Path))
	if err != nil {
		return err
	}
	if arr.Kind != yaml.SequenceNode {
		return fmt.Errorf("match target is not an array")
	}
	idx := findMatch(arr, p.Match)
	if idx < 0 {
		return fmt.Errorf("no array element matches %v", p.Match)
	}
	switch p.Op {
	case patch.OpRemove:
		arr.Content = append(arr.Content[:idx], arr.Content[idx+1:]...)
	case patch.OpReplace:
		arr.Content[idx] = cloneNode(p.Value)
	default:
		return fmt.Errorf("op %q unsupported with match", p.Op)
	}
	return nil
}

func findMatch(arr *yaml.Node, match map[string]string) int {
	for i, el := range arr.Content {
		if el.Kind != yaml.MappingNode {
			continue
		}
		ok := true
		for k, v := range match {
			if nodeVal(el, k) != v {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}

// ---- op implementations ------------------------------------------------------

func addAt(parent *yaml.Node, key string, p patch.Patch) error {
	switch parent.Kind {
	case yaml.MappingNode:
		if v := mapGet(parent, key); v != nil {
			return replaceInMap(parent, key, p.Value) // key present: treat as replace
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
		// A JSON Pointer can't address an array element by value, so a
		// value-based remove (e.g. one name out of `required`) addresses the
		// array itself: `key` names the field, and p.Value picks the element
		// to drop from it — not the whole field.
		if p.Value != nil {
			seq := mapGet(parent, key)
			if seq == nil || seq.Kind != yaml.SequenceNode {
				return fmt.Errorf("remove target %q not found", key)
			}
			return removeByValue(seq, p.Value)
		}
		for i := 0; i+1 < len(parent.Content); i += 2 {
			if parent.Content[i].Value == key {
				parent.Content = append(parent.Content[:i], parent.Content[i+2:]...)
				return nil
			}
		}
		return fmt.Errorf("remove target %q not found", key)
	case yaml.SequenceNode:
		// By numeric index, or by value (patch carries the value to find).
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

// ---- path resolution ---------------------------------------------------------

// ensureMapPath walks tokens, creating an empty mapping for any missing key.
// Used only for operation adds, so a brand-new /paths/<path> is created before
// the method is inserted under it (order-independent across sibling methods).
func ensureMapPath(root *yaml.Node, toks []string) (*yaml.Node, error) {
	n := root
	for _, t := range toks {
		if n.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("cannot create under %v at %q", n.Kind, t)
		}
		next := mapGet(n, t)
		if next == nil {
			next = &yaml.Node{Kind: yaml.MappingNode}
			n.Content = append(n.Content, scalarNode(t), next)
		}
		n = next
	}
	return n, nil
}

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

func nodeVal(m *yaml.Node, key string) string {
	if v := mapGet(m, key); v != nil {
		return v.Value
	}
	return ""
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
