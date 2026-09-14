package enrich

import (
	"context"
	"sort"

	"github.com/karosia/driftsync/diff"
)

// RenameHint is a suggestion, never a decision: the underlying patches stay a
// plain remove+add regardless of the answer. It exists purely so a human
// reviewer sees "this might be the same field renamed" instead of two
// unrelated-looking edits — the exact "semantic rename" gap noted in the
// README (qty -> quantity isn't matched structurally the way user_id <->
// userId is).
type RenameHint struct {
	Schema, Direction, From, To, Note string
}

// SuggestRenames looks for PropertyRemoved/PropertyAdded pairs that diff's
// deterministic matcher didn't pair (different name, so each stayed a plain
// remove+add) but share a schema, direction, and type. It only asks about an
// UNAMBIGUOUS candidate — exactly one removed and one added property of that
// type in that schema+direction — same "ambiguous stays split" rule
// matchRenames itself uses; multiple same-typed candidates is exactly the
// case an LLM is likely to guess wrong on, so it's left alone. Best-effort: a
// provider error just drops that one candidate, never fails the caller.
func SuggestRenames(ctx context.Context, e Enricher, r *diff.Report) []RenameHint {
	type key struct{ schema, dir, typeSig string }
	removed := map[key][]diff.Change{}
	added := map[key][]diff.Change{}
	var keys []key
	seen := map[key]bool{}

	for _, c := range r.Changes {
		if c.TypeSig == "" || (c.Kind != diff.PropertyRemoved && c.Kind != diff.PropertyAdded) {
			continue
		}
		k := key{c.Target.Schema, c.Target.Direction.String(), c.TypeSig}
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
		if c.Kind == diff.PropertyRemoved {
			removed[k] = append(removed[k], c)
		} else {
			added[k] = append(added[k], c)
		}
	}

	// Deterministic order: map iteration isn't, and this feeds a rendered report.
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.schema != b.schema {
			return a.schema < b.schema
		}
		if a.dir != b.dir {
			return a.dir < b.dir
		}
		return a.typeSig < b.typeSig
	})

	var hints []RenameHint
	for _, k := range keys {
		rems, adds := removed[k], added[k]
		if len(rems) != 1 || len(adds) != 1 {
			continue // ambiguous on one side or the other
		}
		from, to := rems[0], adds[0]
		ok, note, err := e.SuggestRename(ctx, RenameCandidate{
			Schema: k.schema, Direction: k.dir,
			FromName: from.Target.Property, ToName: to.Target.Property, TypeSig: k.typeSig,
		})
		if err != nil || !ok {
			continue
		}
		hints = append(hints, RenameHint{
			Schema: k.schema, Direction: k.dir,
			From: from.Target.Property, To: to.Target.Property, Note: note,
		})
	}
	return hints
}
