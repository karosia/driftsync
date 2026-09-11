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
