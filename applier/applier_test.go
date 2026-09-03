package applier_test

import (
	"strings"
	"testing"

	"driftsync/applier"
	"driftsync/patch"

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
