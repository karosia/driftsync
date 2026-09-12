package applier_test

import (
	"strings"
	"testing"

	"github.com/karosia/driftsync/applier"
	"github.com/karosia/driftsync/patch"

	yaml "gopkg.in/yaml.v3"
)

func node(v string) *yaml.Node {
	var n yaml.Node
	_ = yaml.Unmarshal([]byte(v), &n)
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		return n.Content[0]
	}
	return &n
}

func TestApply_AddAndRemove(t *testing.T) {
	published := []byte("components:\n  schemas:\n    User:\n      properties:\n        old: {type: string}\n")
	patches := []patch.Patch{
		{Op: patch.OpRemove, Path: "/components/schemas/User/properties/old"},
		{Op: patch.OpAdd, Path: "/components/schemas/User/properties/new", Value: node("type: string")},
	}
	out, applied, failed, err := applier.Apply(published, patches)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 2 || len(failed) != 0 {
		t.Fatalf("expected 2 applied 0 failed, got %d/%d", len(applied), len(failed))
	}
	s := string(out)
	if strings.Contains(s, "old:") || !strings.Contains(s, "new:") {
		t.Errorf("edit not reflected: %s", s)
	}
}

func TestApply_PartialFailureReported(t *testing.T) {
	published := []byte("components:\n  schemas:\n    User:\n      properties:\n        a: {type: string}\n")
	patches := []patch.Patch{
		{Op: patch.OpRemove, Path: "/components/schemas/User/properties/a"},     // ok
		{Op: patch.OpRemove, Path: "/components/schemas/User/properties/ghost"}, // not found
	}
	_, applied, failed, err := applier.Apply(published, patches)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 || len(failed) != 1 {
		t.Fatalf("expected 1 applied 1 failed, got %d/%d", len(applied), len(failed))
	}
	if !strings.Contains(failed[0].Reason, "not found") {
		t.Errorf("failure reason should mention not found, got %q", failed[0].Reason)
	}
}

// TestApply_RenameRef_RewritesEveryOccurrence covers a schema rename's $ref
// fixup (#<schema-rename>): the old ref can be anywhere in the document — a
// sibling schema's property, an array's items, a path's response — not just
// the one place a JSON Pointer could name.
func TestApply_RenameRef_RewritesEveryOccurrence(t *testing.T) {
	published := []byte(`
paths:
  /users/{id}:
    get:
      responses:
        '200':
          content:
            application/json:
              schema: {$ref: '#/components/schemas/UserDTO'}
components:
  schemas:
    UserDTO:
      type: object
      properties: {id: {type: string}}
    Team:
      type: object
      properties:
        owner: {$ref: '#/components/schemas/UserDTO'}
        members:
          type: array
          items: {$ref: '#/components/schemas/UserDTO'}
    Unrelated:
      type: object
      properties:
        other: {$ref: '#/components/schemas/SomethingElse'}
`)
	patches := []patch.Patch{
		{Op: patch.OpRenameRef,
			RefFrom: "#/components/schemas/UserDTO", RefTo: "#/components/schemas/User"},
	}
	out, applied, failed, err := applier.Apply(published, patches)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 || len(failed) != 0 {
		t.Fatalf("expected 1 applied 0 failed, got %d/%d: %+v", len(applied), len(failed), failed)
	}
	s := string(out)
	// This test isolates the rename_ref op alone (the schema body add/remove
	// is a separate op, covered by the patch-layer test) — so the UserDTO
	// schema *definition* key legitimately remains; only every $ref pointing
	// at it should be gone.
	if strings.Contains(s, "$ref: '#/components/schemas/UserDTO'") {
		t.Errorf("every $ref to UserDTO should have been rewritten:\n%s", s)
	}
	if strings.Count(s, "'#/components/schemas/User'") != 3 {
		t.Errorf("expected exactly 3 occurrences of the new ref (response, owner, items), got:\n%s", s)
	}
	if !strings.Contains(s, "SomethingElse") {
		t.Error("an unrelated $ref must be left untouched")
	}
}

func TestApply_RenameRef_NoMatchIsANoOp(t *testing.T) {
	published := []byte("components:\n  schemas:\n    Widget: {type: object}\n")
	patches := []patch.Patch{
		{Op: patch.OpRenameRef,
			RefFrom: "#/components/schemas/Nonexistent", RefTo: "#/components/schemas/Whatever"},
	}
	_, applied, failed, err := applier.Apply(published, patches)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 || len(failed) != 0 {
		t.Fatalf("a rename with nothing to find should succeed as a no-op, got %d/%d: %+v",
			len(applied), len(failed), failed)
	}
}
