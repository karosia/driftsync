package diff

import (
	base "github.com/pb33f/libopenapi/datamodel/high/base"
)

// ---- direction-aware type-change severity ------------------------------------
//
// A type change used to be BREAKING unconditionally. That's right for a real
// shape change (string -> object, a new format, a different $ref), but wrong
// for the most common edits, which only grow or shrink the set of accepted
// values — and those cut opposite ways depending on direction, exactly like
// added/removed properties do:
//
//                      request (client -> server)   response (server -> client)
//   widened  (+enum,   info: old clients' values    BREAKING: old clients may get
//   +null, int->num)   are still accepted           a value they can't handle
//   narrowed (-enum,   BREAKING: a value old        info: clients just see fewer
//   -null, num->int)   clients send is now rejected  distinct values
//
// Anything that isn't a pure widen or narrow — the type actually changing, a
// format or nested-shape change, or a mix of widening and narrowing — stays
// BREAKING in both directions.

const (
	noteWidened  = "widened"
	noteNarrowed = "narrowed"
)

// typeChangeSeverityFor classifies a property/parameter whose signature
// changed from a to b. The note says why a change was judged non-breaking
// (or breaking only in this direction), for the report.
func typeChangeSeverityFor(dir Direction, a, b *base.SchemaProxy) (Severity, string) {
	sa, sb := plainSchema(a), plainSchema(b)
	if sa == nil || sb == nil {
		return Breaking, "" // a $ref or missing schema on either side: a real shape change
	}
	if formatSig(sa) != formatSig(sb) || nestedSig(sa, 1) != nestedSig(sb, 1) {
		return Breaking, ""
	}

	va, vb := valueSetOf(sa), valueSetOf(sb)
	widened := !va.covers(vb)  // b accepts something a didn't
	narrowed := !vb.covers(va) // a accepted something b doesn't

	switch {
	case widened && narrowed:
		return Breaking, ""
	case widened:
		if dir == Response {
			return Breaking, noteWidened
		}
		return Info, noteWidened
	case narrowed:
		if dir == Request {
			return Breaking, noteNarrowed
		}
		return Info, noteNarrowed
	default:
		// Same value set written differently (e.g. type list reordered).
		return Info, ""
	}
}

func plainSchema(sp *base.SchemaProxy) *base.Schema {
	if sp == nil || sp.IsReference() {
		return nil
	}
	return sp.Schema()
}

// valueSet is the coarse "which values does this schema accept" view used
// above. A nil map means unconstrained in that dimension.
type valueSet struct {
	types map[string]bool // nil: any type
	enum  map[string]bool // nil: no enum constraint
}

func valueSetOf(s *base.Schema) valueSet {
	var v valueSet
	if len(s.Type) > 0 {
		v.types = map[string]bool{}
		for _, t := range s.Type {
			v.types[t] = true
		}
		// 3.0's `nullable` is folded into type by canonicalize, but be
		// robust to an uncanonicalized document.
		if s.Nullable != nil && *s.Nullable {
			v.types["null"] = true
		}
	}
	if len(s.Enum) > 0 {
		v.enum = map[string]bool{}
		for _, n := range s.Enum {
			v.enum[n.Value] = true
		}
	}
	return v
}

// covers reports whether every value o accepts is also accepted by v.
func (v valueSet) covers(o valueSet) bool {
	if v.types != nil {
		if o.types == nil {
			return false
		}
		for t := range o.types {
			if !v.types[t] && !(t == "integer" && v.types["number"]) {
				return false
			}
		}
	}
	if v.enum != nil {
		if o.enum == nil {
			return false
		}
		for e := range o.enum {
			if !v.enum[e] {
				return false
			}
		}
	}
	return true
}
