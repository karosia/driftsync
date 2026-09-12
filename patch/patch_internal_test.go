package patch

import (
	"testing"

	yaml "gopkg.in/yaml.v3"

	"github.com/karosia/driftsync/diff"
)

// TestPropertyRenamed_RequiredCombinations covers #2: a rename must carry its
// required-membership across, in every combination, because the old and new
// names differ and `required` is name-keyed (the properties edit alone can't
// fix it up).
func TestPropertyRenamed_RequiredCombinations(t *testing.T) {
	var codeRoot yaml.Node
	if err := yaml.Unmarshal([]byte(
		"components:\n  schemas:\n    User:\n      properties:\n        userId: {type: string}\n"),
		&codeRoot); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name                      string
		fromReq, toReq            bool
		wantRemoveReq, wantAddReq bool
	}{
		{"optional to optional", false, false, false, false},
		{"required to optional", true, false, true, false},
		{"optional to required", false, true, false, true},
		{"required to required", true, true, true, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := diff.Change{
				Kind: diff.PropertyRenamed, From: "user_id", To: "userId",
				FromRequired: tc.fromReq, ToRequired: tc.toReq,
				Target: diff.Target{Schema: "User"},
			}
			patches := patchesFor(c, &codeRoot)

			var gotRemoveReq, gotAddReq bool
			for _, p := range patches {
				if p.Path != "/components/schemas/User/required" &&
					p.Path != "/components/schemas/User/required/-" {
					continue
				}
				switch {
				case p.Op == OpRemove && p.Value.Value == "user_id":
					gotRemoveReq = true
				case p.Op == OpAdd && p.Value.Value == "userId":
					gotAddReq = true
				default:
					t.Errorf("unexpected required patch: %+v", p)
				}
			}
			if gotRemoveReq != tc.wantRemoveReq {
				t.Errorf("remove old name from required = %v, want %v", gotRemoveReq, tc.wantRemoveReq)
			}
			if gotAddReq != tc.wantAddReq {
				t.Errorf("add new name to required = %v, want %v", gotAddReq, tc.wantAddReq)
			}

			// The properties edit itself must always be present regardless of
			// required-ness.
			var gotRemoveProp, gotAddProp bool
			for _, p := range patches {
				if p.Path == "/components/schemas/User/properties/user_id" && p.Op == OpRemove {
					gotRemoveProp = true
				}
				if p.Path == "/components/schemas/User/properties/userId" && p.Op == OpAdd {
					gotAddProp = true
				}
			}
			if !gotRemoveProp || !gotAddProp {
				t.Errorf("expected the properties rename edit, got %+v", patches)
			}
		})
	}
}

// TestPropertyRemoved_CleansRequired is a regression for the removal side of
// #2: removing a property that was required must also drop it from `required`
// — same bug class as rename, just without a "to" side.
func TestPropertyRemoved_CleansRequired(t *testing.T) {
	cases := []struct {
		name          string
		fromReq       bool
		wantRemoveReq bool
	}{
		{"removed optional property", false, false},
		{"removed required property", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := diff.Change{
				Kind: diff.PropertyRemoved, FromRequired: tc.fromReq,
				Target: diff.Target{Schema: "User", Property: "email"},
			}
			patches := patchesFor(c, &yaml.Node{})

			var gotRemoveProp, gotRemoveReq bool
			for _, p := range patches {
				switch {
				case p.Op == OpRemove && p.Path == "/components/schemas/User/properties/email":
					gotRemoveProp = true
				case p.Op == OpRemove && p.Path == "/components/schemas/User/required" && p.Value.Value == "email":
					gotRemoveReq = true
				default:
					t.Errorf("unexpected patch: %+v", p)
				}
			}
			if !gotRemoveProp {
				t.Errorf("expected the properties removal, got %+v", patches)
			}
			if gotRemoveReq != tc.wantRemoveReq {
				t.Errorf("remove from required = %v, want %v", gotRemoveReq, tc.wantRemoveReq)
			}
		})
	}
}

// TestSchemaAddedRemoved covers issue-1: a whole new/removed component schema
// must actually be patched into/out of the published document, not silently
// dropped by patchesFor.
func TestSchemaAddedRemoved(t *testing.T) {
	var codeRoot yaml.Node
	if err := yaml.Unmarshal([]byte(
		"components:\n  schemas:\n    Widget:\n      type: object\n      properties:\n        id: {type: string}\n"),
		&codeRoot); err != nil {
		t.Fatal(err)
	}

	t.Run("added", func(t *testing.T) {
		c := diff.Change{Kind: diff.SchemaAdded, Target: diff.Target{Schema: "Widget"}}
		patches := patchesFor(c, &codeRoot)
		if len(patches) != 1 {
			t.Fatalf("expected exactly 1 patch, got %+v", patches)
		}
		p := patches[0]
		if p.Op != OpAdd || p.Path != "/components/schemas/Widget" {
			t.Errorf("unexpected patch: %+v", p)
		}
		if !p.CreatePath {
			t.Error("expected CreatePath so a fresh components/schemas still resolves")
		}
		if p.Value == nil {
			t.Error("expected the whole schema body copied from the code doc")
		}
	})

	t.Run("removed", func(t *testing.T) {
		c := diff.Change{Kind: diff.SchemaRemoved, Target: diff.Target{Schema: "Legacy"}}
		patches := patchesFor(c, &codeRoot)
		if len(patches) != 1 {
			t.Fatalf("expected exactly 1 patch, got %+v", patches)
		}
		p := patches[0]
		if p.Op != OpRemove || p.Path != "/components/schemas/Legacy" {
			t.Errorf("unexpected patch: %+v", p)
		}
	})
}

// TestRequiredDangling_NoPatch: it's a self-consistency lint on one document,
// not a drift to fix, so patchesFor must produce nothing for it.
func TestRequiredDangling_NoPatch(t *testing.T) {
	c := diff.Change{Kind: diff.RequiredDangling, Target: diff.Target{Schema: "User", Property: "ghost"}}
	if patches := patchesFor(c, &yaml.Node{}); patches != nil {
		t.Errorf("expected no patch for a dangling-required flag, got %+v", patches)
	}
}

// TestSchemaRenamed_RewritesBodyAndRefs: a schema-level rename must emit the
// body swap (remove old, add new) AND a document-wide $ref fixup — the body
// swap alone would leave every existing reference to the old name dangling.
func TestSchemaRenamed_RewritesBodyAndRefs(t *testing.T) {
	var codeRoot yaml.Node
	if err := yaml.Unmarshal([]byte(
		"components:\n  schemas:\n    User:\n      properties:\n        id: {type: string}\n"),
		&codeRoot); err != nil {
		t.Fatal(err)
	}
	c := diff.Change{Kind: diff.SchemaRenamed, From: "UserDTO", To: "User"}
	patches := patchesFor(c, &codeRoot)
	if len(patches) != 3 {
		t.Fatalf("expected 3 patches (remove old body, add new body, rename_ref), got %+v", patches)
	}

	var gotRemoveOld, gotAddNew, gotRenameRef bool
	for _, p := range patches {
		switch {
		case p.Op == OpRemove && p.Path == "/components/schemas/UserDTO":
			gotRemoveOld = true
		case p.Op == OpAdd && p.Path == "/components/schemas/User":
			if !p.CreatePath {
				t.Error("expected CreatePath on the new schema body add")
			}
			if p.Value == nil {
				t.Error("expected the new schema body copied from the code doc")
			}
			gotAddNew = true
		case p.Op == OpRenameRef:
			if p.RefFrom != "#/components/schemas/UserDTO" || p.RefTo != "#/components/schemas/User" {
				t.Errorf("unexpected rename_ref from/to: %q -> %q", p.RefFrom, p.RefTo)
			}
			gotRenameRef = true
		default:
			t.Errorf("unexpected patch: %+v", p)
		}
	}
	if !gotRemoveOld || !gotAddNew || !gotRenameRef {
		t.Errorf("missing one of the three expected patches: %+v", patches)
	}
}
