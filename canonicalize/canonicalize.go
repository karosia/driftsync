package canonicalize

import (
	"fmt"
	"strings"

	"github.com/pb33f/libopenapi"
	base "github.com/pb33f/libopenapi/datamodel/high/base"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
	yaml "gopkg.in/yaml.v3"

	"driftsync/ir"
)

// Apply normalizes a document into canonical 3.1 form, so two documents can be
// compared without dialect differences producing false drift. It performs the
// mechanical 3.0 -> 3.1 rewrites, bumps the version to 3.1.0, and returns a new
// IR whose Raw bytes are the re-rendered canonical form.
//
// It re-parses from in.Raw so it owns a mutable doc + model, mutates the model,
// then RenderAndReload renders the mutated state (including our edits) to bytes.
func Apply(in *ir.Document) (*ir.Document, error) {
	doc, err := libopenapi.NewDocument(in.Raw)
	if err != nil {
		return nil, fmt.Errorf("canonicalize: parse: %w", err)
	}
	model, err := doc.BuildV3Model()
	if err != nil {
		return nil, fmt.Errorf("canonicalize: build model: %w", err)
	}

	// Rules 2 & 3: typed-model rewrites on every schema under components/schemas.
	if model.Model.Components != nil && model.Model.Components.Schemas != nil {
		for pair := model.Model.Components.Schemas.First(); pair != nil; pair = pair.Next() {
			walkSchema(pair.Value())
		}
	}

	// Rule 1: the 3.0 -> 3.1 promotion (version string).
	model.Model.Version = "3.1.0"

	// Render rules 1-3 back to bytes.
	rendered, _, _, err := doc.RenderAndReload()
	if err != nil {
		return nil, fmt.Errorf("canonicalize: render: %w", err)
	}

	// Rule 4: node-level fold of allOf-wrapped single refs into sibling refs.
	// Done on bytes because it restructures the SHAPE of a reference — a local,
	// controllable rewrite, far safer than mutating typed ref proxies.
	folded, err := foldRefSiblings(rendered)
	if err != nil {
		return nil, fmt.Errorf("canonicalize: fold refs: %w", err)
	}

	// Re-parse so the returned Model stays consistent with the folded bytes.
	finalModel, err := buildModel(folded)
	if err != nil {
		return nil, fmt.Errorf("canonicalize: rebuild: %w", err)
	}
	return &ir.Document{Version: "3.1.0", Raw: folded, Model: finalModel}, nil
}

// buildModel parses bytes into a v3 model (small helper, avoids repetition).
func buildModel(raw []byte) (*v3high.Document, error) {
	doc, err := libopenapi.NewDocument(raw)
	if err != nil {
		return nil, err
	}
	m, err := doc.BuildV3Model()
	if err != nil {
		return nil, err
	}
	return &m.Model, nil
}

// walkSchema normalizes one schema, then recurses into every nested schema.
// A $ref schema is NOT descended into: its target is defined elsewhere (e.g.
// components/schemas) and normalized there. Skipping it prevents both
// double-processing and infinite loops on recursive references.
func walkSchema(sp *base.SchemaProxy) {
	if sp == nil || sp.IsReference() {
		return
	}
	s := sp.Schema()
	if s == nil {
		return
	}

	normalizeNullable(s)        // rule 2
	normalizeExclusiveBounds(s) // rule 3

	recurseInto(s)
}

// Rule 2 — nullable.
// 3.0 says "may be null" via `nullable: true`; 3.1 says it by adding "null" to
// the type list. Move to the 3.1 form and drop the 3.0-only keyword.
func normalizeNullable(s *base.Schema) {
	if s.Nullable == nil || !*s.Nullable {
		return
	}
	if !containsType(s.Type, "null") {
		s.Type = append(s.Type, "null")
	}
	s.Nullable = nil
}

// Rule 3 — exclusiveMinimum / exclusiveMaximum.
// 3.0 form: `minimum: N` + `exclusiveMinimum: true` (a boolean flag).
// 3.1 form: `exclusiveMinimum: N` (the number itself; plain `minimum` dropped).
// Only the boolean 3.0 form is rewritten; a schema already numeric (3.1) is left
// alone. IsA() == the boolean variant is present.
func normalizeExclusiveBounds(s *base.Schema) {
	if s.ExclusiveMinimum != nil && s.ExclusiveMinimum.IsA() && s.ExclusiveMinimum.A {
		if s.Minimum != nil {
			s.ExclusiveMinimum = &base.DynamicValue[bool, float64]{N: 1, B: *s.Minimum}
			s.Minimum = nil
		} else {
			s.ExclusiveMinimum = nil // bool true with no minimum: nothing to carry
		}
	}
	if s.ExclusiveMaximum != nil && s.ExclusiveMaximum.IsA() && s.ExclusiveMaximum.A {
		if s.Maximum != nil {
			s.ExclusiveMaximum = &base.DynamicValue[bool, float64]{N: 1, B: *s.Maximum}
			s.Maximum = nil
		} else {
			s.ExclusiveMaximum = nil
		}
	}
}

// recurseInto walks every position a nested schema can appear.
func recurseInto(s *base.Schema) {
	if s.Properties != nil {
		for pair := s.Properties.First(); pair != nil; pair = pair.Next() {
			walkSchema(pair.Value())
		}
	}
	for _, sp := range s.AllOf {
		walkSchema(sp)
	}
	for _, sp := range s.AnyOf {
		walkSchema(sp)
	}
	for _, sp := range s.OneOf {
		walkSchema(sp)
	}
	for _, sp := range s.PrefixItems {
		walkSchema(sp)
	}
	// Items and AdditionalProperties can each be a schema (A) or a bool (B);
	// only recurse when they hold a schema.
	if s.Items != nil && s.Items.IsA() {
		walkSchema(s.Items.A)
	}
	if s.AdditionalProperties != nil && s.AdditionalProperties.IsA() {
		walkSchema(s.AdditionalProperties.A)
	}
}

func containsType(types []string, want string) bool {
	for _, t := range types {
		if t == want {
			return true
		}
	}
	return false
}

// --- rule 4: fold allOf-wrapped single refs into sibling refs -----------------

// annotationKeys are the only sibling keys allowed to ride alongside allOf when
// folding. They carry documentation, not validation, so keeping them as $ref
// siblings preserves meaning. Any OTHER key means the outer schema adds a real
// constraint, and folding could change semantics — so we skip those cases.
var annotationKeys = map[string]bool{
	"description": true, "summary": true, "title": true, "deprecated": true,
	"example": true, "examples": true, "readOnly": true, "writeOnly": true,
	"externalDocs": true, "xml": true,
}

func foldRefSiblings(raw []byte) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	foldNode(&root)
	return yaml.Marshal(&root)
}

// foldNode walks the whole tree, attempting the fold on every mapping node.
func foldNode(n *yaml.Node) {
	switch n.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, c := range n.Content {
			foldNode(c)
		}
	case yaml.MappingNode:
		tryFoldMapping(n)
		// Mapping Content is [key0, val0, key1, val1, ...]; recurse into values.
		for i := 1; i < len(n.Content); i += 2 {
			foldNode(n.Content[i])
		}
	}
}

// tryFoldMapping rewrites m IN PLACE when it matches exactly:
//
//	{ allOf: [ { $ref: X } ], <annotation siblings...> }
//
// into:
//
//	{ $ref: X, <annotation siblings...> }
//
// Any deviation (multiple allOf entries, a ref carrying its own siblings, a
// pre-existing $ref, or a non-annotation sibling) leaves m untouched.
func tryFoldMapping(m *yaml.Node) {
	allOfIdx := -1
	for i := 0; i < len(m.Content); i += 2 {
		key := m.Content[i].Value
		switch {
		case key == "allOf":
			allOfIdx = i
		case key == "$ref":
			return // already a ref; nothing to fold
		case annotationKeys[key] || strings.HasPrefix(key, "x-"):
			// allowed to ride along
		default:
			return // a real constraint sits alongside allOf; unsafe to fold
		}
	}
	if allOfIdx == -1 {
		return
	}

	allOfVal := m.Content[allOfIdx+1]
	if allOfVal.Kind != yaml.SequenceNode || len(allOfVal.Content) != 1 {
		return // must be exactly one entry
	}
	entry := allOfVal.Content[0]
	if entry.Kind != yaml.MappingNode || len(entry.Content) != 2 ||
		entry.Content[0].Value != "$ref" {
		return // the single entry must be exactly { $ref: X }
	}
	refValue := entry.Content[1]

	// Rebuild content: $ref first, then every original pair except allOf.
	rebuilt := []*yaml.Node{
		{Kind: yaml.ScalarNode, Tag: "!!str", Value: "$ref"},
		refValue,
	}
	for i := 0; i < len(m.Content); i += 2 {
		if i == allOfIdx {
			continue
		}
		rebuilt = append(rebuilt, m.Content[i], m.Content[i+1])
	}
	m.Content = rebuilt
}
