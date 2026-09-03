package diff

import (
	"sort"
	"strings"

	base "github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"

	"driftsync/ir"
)

// ---- direction + severity (the conceptual heart) -----------------------------

// Direction is where a schema sits on the contract: client->server or server->client.
type Direction int

const (
	Request Direction = iota
	Response
)

func (d Direction) String() string {
	if d == Response {
		return "response"
	}
	return "request"
}

type Severity int

const (
	Info Severity = iota
	Breaking
)

func (s Severity) String() string {
	if s == Breaking {
		return "BREAKING"
	}
	return "info"
}

// addedSeverity: impact of ADDING a property/param, by direction + requiredness.
func addedSeverity(dir Direction, requiredNow bool) Severity {
	if dir == Response {
		return Info // new response fields are additive; consumers ignore them
	}
	if requiredNow {
		return Breaking // request now demands a field old clients don't send
	}
	return Info // optional request field
}

// removedSeverity: impact of REMOVING a property/param, by direction.
func removedSeverity(dir Direction) Severity {
	if dir == Response {
		return Breaking // consumers relied on a field that's now gone
	}
	return Info // server simply accepts less
}

// requiredFlipSeverity: impact of an existing field's requiredness flipping.
func requiredFlipSeverity(dir Direction, becameRequired bool) Severity {
	switch dir {
	case Request:
		if becameRequired {
			return Breaking // clients must now send it
		}
		return Info
	default: // Response
		if becameRequired {
			return Info // stronger guarantee for consumers
		}
		return Breaking // consumers relied on it always being present
	}
}

const typeChangeSeverity = Breaking // a type mismatch breaks either direction

// ---- change model ------------------------------------------------------------

type ChangeKind string

const (
	OperationAdded   ChangeKind = "operation_added"
	OperationRemoved ChangeKind = "operation_removed"

	ParameterAdded           ChangeKind = "parameter_added"
	ParameterRemoved         ChangeKind = "parameter_removed"
	ParameterTypeChanged     ChangeKind = "parameter_type_changed"
	ParameterRequiredChanged ChangeKind = "parameter_required_changed"

	ResponseAdded   ChangeKind = "response_added" // status code
	ResponseRemoved ChangeKind = "response_removed"

	SchemaAdded         ChangeKind = "schema_added"
	SchemaRemoved       ChangeKind = "schema_removed"
	PropertyAdded       ChangeKind = "property_added"
	PropertyRemoved     ChangeKind = "property_removed"
	PropertyTypeChanged ChangeKind = "property_type_changed"
	PropertyRenamed     ChangeKind = "property_renamed"
	RequiredAdded       ChangeKind = "required_added"
	RequiredRemoved     ChangeKind = "required_removed"
)

// Target is the machine-addressable location of a change, so downstream (patch)
// never parses the human-readable Location string. Fields are populated per
// change kind; unused ones stay zero.
type Target struct {
	// schema-property changes
	Schema    string
	Direction Direction
	Property  string
	// operation / parameter / response changes
	Method     string
	Path       string
	ParamName  string
	ParamIn    string
	StatusCode string
}

type Change struct {
	Kind     ChangeKind
	Location string
	From     string
	To       string
	Severity Severity
	Note     string
	Target   Target
}

type Report struct{ Changes []Change }

func (r *Report) Breaking() int {
	n := 0
	for _, c := range r.Changes {
		if c.Severity == Breaking {
			n++
		}
	}
	return n
}

// Diff compares two CANONICAL documents (a=published, b=code). Detection is
// symmetric; direction only affects how severe a found change is.
func Diff(a, b *ir.Document) *Report {
	r := &Report{}
	diffOperations(a, b, r)         // operation presence
	diffOperationDetails(a, b, r)   // parameters + response status codes
	diffDirectionalSchemas(a, b, r) // component schemas, per direction used
	return r
}

// ---- operation presence ------------------------------------------------------

func collectOperationObjects(d *ir.Document) map[string]*v3.Operation {
	out := map[string]*v3.Operation{}
	if d.Model == nil || d.Model.Paths == nil {
		return out
	}
	for p := d.Model.Paths.PathItems.First(); p != nil; p = p.Next() {
		path := p.Key()
		for op := p.Value().GetOperations().First(); op != nil; op = op.Next() {
			out[strings.ToUpper(op.Key())+" "+path] = op.Value()
		}
	}
	return out
}

func diffOperations(a, b *ir.Document, r *Report) {
	ao, bo := collectOperationObjects(a), collectOperationObjects(b)
	for _, k := range sortedKeysMissing(ao, bo) {
		m, p := splitOpKey(k)
		r.Changes = append(r.Changes, Change{Kind: OperationRemoved, Location: k,
			Severity: Breaking, Target: Target{Method: m, Path: p}})
	}
	for _, k := range sortedKeysMissing(bo, ao) {
		m, p := splitOpKey(k)
		r.Changes = append(r.Changes, Change{Kind: OperationAdded, Location: k,
			Severity: Info, Target: Target{Method: m, Path: p}})
	}
}

// ---- per-operation: parameters + response codes ------------------------------

func diffOperationDetails(a, b *ir.Document, r *Report) {
	ao, bo := collectOperationObjects(a), collectOperationObjects(b)
	for _, key := range sortedKeysCommon(ao, bo) {
		diffParameters(key, ao[key], bo[key], r)
		diffResponseCodes(key, ao[key], bo[key], r)
	}
}

type paramInfo struct {
	sig      string
	required bool
}

func collectParams(op *v3.Operation) map[string]paramInfo {
	out := map[string]paramInfo{}
	for _, p := range op.Parameters {
		out[p.In+":"+p.Name] = paramInfo{ // key e.g. "query:limit"
			sig:      propSignature(p.Schema),
			required: p.Required != nil && *p.Required,
		}
	}
	return out
}

func diffParameters(opKey string, a, b *v3.Operation, r *Report) {
	ap, bp := collectParams(a), collectParams(b)
	method, path := splitOpKey(opKey)
	tgt := func(pk string) Target {
		in, name := splitParamKey(pk)
		return Target{Method: method, Path: path, ParamName: name, ParamIn: in}
	}
	loc := func(k string) string { return opKey + " (param " + k + ")" }

	for _, k := range sortedKeysMissing(ap, bp) { // params are always request-direction
		r.Changes = append(r.Changes, Change{Kind: ParameterRemoved, Location: loc(k),
			Severity: removedSeverity(Request), Target: tgt(k)})
	}
	for _, k := range sortedKeysMissing(bp, ap) {
		r.Changes = append(r.Changes, Change{Kind: ParameterAdded, Location: loc(k),
			Severity: addedSeverity(Request, bp[k].required), Target: tgt(k)})
	}
	for _, k := range sortedKeysCommon(ap, bp) {
		if ap[k].sig != bp[k].sig {
			r.Changes = append(r.Changes, Change{Kind: ParameterTypeChanged, Location: loc(k),
				From: ap[k].sig, To: bp[k].sig, Severity: typeChangeSeverity, Target: tgt(k)})
		}
		if ap[k].required != bp[k].required {
			r.Changes = append(r.Changes, Change{Kind: ParameterRequiredChanged, Location: loc(k),
				Severity: requiredFlipSeverity(Request, bp[k].required), Target: tgt(k)})
		}
	}
}

func collectResponseCodes(op *v3.Operation) map[string]bool {
	out := map[string]bool{}
	if op.Responses == nil || op.Responses.Codes == nil {
		return out
	}
	for c := op.Responses.Codes.First(); c != nil; c = c.Next() {
		out[c.Key()] = true
	}
	return out
}

func diffResponseCodes(opKey string, a, b *v3.Operation, r *Report) {
	ac, bc := collectResponseCodes(a), collectResponseCodes(b)
	method, path := splitOpKey(opKey)
	for _, code := range sortedKeysMissing(ac, bc) {
		r.Changes = append(r.Changes, Change{Kind: ResponseRemoved,
			Location: opKey + " (response " + code + ")", Severity: Breaking,
			Target: Target{Method: method, Path: path, StatusCode: code}})
	}
	for _, code := range sortedKeysMissing(bc, ac) {
		r.Changes = append(r.Changes, Change{Kind: ResponseAdded,
			Location: opKey + " (response " + code + ")", Severity: Info,
			Target: Target{Method: method, Path: path, StatusCode: code}})
	}
}

// ---- directional schema diff -------------------------------------------------

type dirSet struct{ req, resp bool }

// discoverDirections walks operations and marks which component schemas are used
// in request bodies (req) and/or responses (resp), by following top-level $refs.
func discoverDirections(d *ir.Document) map[string]dirSet {
	dirs := map[string]dirSet{}
	mark := func(name string, f func(*dirSet)) {
		s := dirs[name]
		f(&s)
		dirs[name] = s
	}
	if d.Model == nil || d.Model.Paths == nil {
		return dirs
	}
	for p := d.Model.Paths.PathItems.First(); p != nil; p = p.Next() {
		for op := p.Value().GetOperations().First(); op != nil; op = op.Next() {
			o := op.Value()
			if o.RequestBody != nil {
				for name := range refsInContent(o.RequestBody.Content) {
					mark(name, func(s *dirSet) { s.req = true })
				}
			}
			if o.Responses != nil && o.Responses.Codes != nil {
				for c := o.Responses.Codes.First(); c != nil; c = c.Next() {
					for name := range refsInContent(c.Value().Content) {
						mark(name, func(s *dirSet) { s.resp = true })
					}
				}
			}
		}
	}
	return dirs
}

func refsInContent(content *orderedmap.Map[string, *v3.MediaType]) map[string]bool {
	out := map[string]bool{}
	if content == nil {
		return out
	}
	for mt := content.First(); mt != nil; mt = mt.Next() {
		if sp := mt.Value().Schema; sp != nil && sp.IsReference() {
			out[refName(sp.GetReference())] = true
		}
	}
	return out
}

func refName(ref string) string { // "#/components/schemas/User" -> "User"
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		return ref[i+1:]
	}
	return ref
}

func diffDirectionalSchemas(a, b *ir.Document, r *Report) {
	as, bs := collectSchemas(a), collectSchemas(b)

	// Presence is reported as info; real impact is decided at use sites below.
	for _, name := range sortedKeysMissing(as, bs) {
		r.Changes = append(r.Changes, Change{Kind: SchemaRemoved, Location: name,
			Severity: Info, Target: Target{Schema: name}})
	}
	for _, name := range sortedKeysMissing(bs, as) {
		r.Changes = append(r.Changes, Change{Kind: SchemaAdded, Location: name,
			Severity: Info, Target: Target{Schema: name}})
	}

	da, db := discoverDirections(a), discoverDirections(b)

	for _, name := range sortedKeysCommon(as, bs) {
		set := dirSet{req: da[name].req || db[name].req, resp: da[name].resp || db[name].resp}
		if set.req {
			diffSchemaBody(name, Request, as[name], bs[name], r)
		}
		if set.resp {
			diffSchemaBody(name, Response, as[name], bs[name], r)
		}
	}
}

func collectSchemas(d *ir.Document) map[string]*base.Schema {
	out := map[string]*base.Schema{}
	if d.Model == nil || d.Model.Components == nil || d.Model.Components.Schemas == nil {
		return out
	}
	for pair := d.Model.Components.Schemas.First(); pair != nil; pair = pair.Next() {
		if s := pair.Value().Schema(); s != nil {
			out[pair.Key()] = s
		}
	}
	return out
}

func diffSchemaBody(name string, dir Direction, a, b *base.Schema, r *Report) {
	loc := name + " [" + dir.String() + "]"
	tgt := func(prop string) Target { return Target{Schema: name, Direction: dir, Property: prop} }
	ap, bp := collectProps(a), collectProps(b)
	areq, breq := toSet(a.Required), toSet(b.Required)

	removed := sortedKeysMissing(ap, bp)
	added := sortedKeysMissing(bp, ap)

	// Deterministic rename pass: pair removed<->added of the SAME signature.
	// Ambiguous cases stay split — that's the fuzzy zone deferred to an LLM.
	renames, remOnly, addOnly := matchRenames(removed, added, ap, bp)

	for _, m := range renames {
		r.Changes = append(r.Changes, Change{
			Kind: PropertyRenamed, Location: loc + "." + m.from,
			From: m.from, To: m.to, Severity: renameSeverity(dir, breq[m.to]),
			Note: m.rule, Target: tgt(m.from),
		})
	}
	for _, pn := range remOnly {
		r.Changes = append(r.Changes, Change{
			Kind: PropertyRemoved, Location: loc + "." + pn,
			Severity: removedSeverity(dir), Target: tgt(pn),
		})
	}
	for _, pn := range addOnly {
		r.Changes = append(r.Changes, Change{
			Kind: PropertyAdded, Location: loc + "." + pn,
			Severity: addedSeverity(dir, breq[pn]), Target: tgt(pn),
		})
	}
	for _, pn := range sortedKeysCommon(ap, bp) {
		if ap[pn] != bp[pn] {
			r.Changes = append(r.Changes, Change{
				Kind: PropertyTypeChanged, Location: loc + "." + pn,
				From: ap[pn], To: bp[pn], Severity: typeChangeSeverity, Target: tgt(pn),
			})
		}
		if !areq[pn] && breq[pn] {
			r.Changes = append(r.Changes, Change{
				Kind: RequiredAdded, Location: loc + "." + pn,
				Severity: requiredFlipSeverity(dir, true), Target: tgt(pn),
			})
		}
		if areq[pn] && !breq[pn] {
			r.Changes = append(r.Changes, Change{
				Kind: RequiredRemoved, Location: loc + "." + pn,
				Severity: requiredFlipSeverity(dir, false), Target: tgt(pn),
			})
		}
	}
}

func collectProps(s *base.Schema) map[string]string {
	out := map[string]string{}
	if s.Properties == nil {
		return out
	}
	for pair := s.Properties.First(); pair != nil; pair = pair.Next() {
		out[pair.Key()] = propSignature(pair.Value())
	}
	return out
}

func propSignature(sp *base.SchemaProxy) string {
	if sp == nil {
		return ""
	}
	if sp.IsReference() {
		return "$ref:" + sp.GetReference()
	}
	s := sp.Schema()
	if s == nil {
		return ""
	}
	if len(s.Type) > 0 {
		return "type:" + strings.Join(s.Type, "|")
	}
	return "type:?"
}

// ---- deterministic rename matching -------------------------------------------

type renameMatch struct{ from, to, rule string }

// matchRenames pairs removed and added props that are the same field renamed.
// Rules by confidence: (1) same sig + equal normalized name; (2) same sig +
// normalized edit distance <= 2 (min length 4). A pair forms only when there is
// EXACTLY ONE candidate — anything ambiguous stays split.
func matchRenames(removed, added []string, ap, bp map[string]string) (
	matches []renameMatch, leftoverRemoved, leftoverAdded []string) {

	usedRemoved := map[string]bool{}
	usedAdded := map[string]bool{}

	candidates := func(rp string, pred func(a, b string) bool) []string {
		var c []string
		for _, ad := range added {
			if usedAdded[ad] || ap[rp] != bp[ad] { // signature must match
				continue
			}
			if pred(normalize(rp), normalize(ad)) {
				c = append(c, ad)
			}
		}
		return c
	}
	pair := func(rp, ad, rule string) {
		matches = append(matches, renameMatch{from: rp, to: ad, rule: rule})
		usedRemoved[rp] = true
		usedAdded[ad] = true
	}

	// Tier 1: normalized-equal (case/separator change).
	for _, rp := range removed {
		if c := candidates(rp, func(a, b string) bool { return a == b }); len(c) == 1 {
			pair(rp, c[0], "case/separator")
		}
	}
	// Tier 2: near-match by edit distance.
	for _, rp := range removed {
		if usedRemoved[rp] {
			continue
		}
		c := candidates(rp, func(a, b string) bool {
			return len(a) >= 4 && len(b) >= 4 && levenshtein(a, b) <= 2
		})
		if len(c) == 1 {
			pair(rp, c[0], "near-match")
		}
	}

	for _, rp := range removed {
		if !usedRemoved[rp] {
			leftoverRemoved = append(leftoverRemoved, rp)
		}
	}
	for _, ad := range added {
		if !usedAdded[ad] {
			leftoverAdded = append(leftoverAdded, ad)
		}
	}
	return matches, leftoverRemoved, leftoverAdded
}

func renameSeverity(dir Direction, requiredNow bool) Severity {
	if removedSeverity(dir) == Breaking || addedSeverity(dir, requiredNow) == Breaking {
		return Breaking
	}
	return Info
}

// normalize lowercases and strips separators: "user_id" and "userId" -> "userid".
func normalize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '_', '-', ' ':
			// drop separators
		default:
			b.WriteRune(toLower(r))
		}
	}
	return b.String()
}

func toLower(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min3(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}

// ---- generic deterministic set helpers ---------------------------------------

func splitOpKey(key string) (method, path string) {
	if i := strings.IndexByte(key, ' '); i >= 0 {
		return key[:i], key[i+1:]
	}
	return key, ""
}

func splitParamKey(k string) (in, name string) {
	if i := strings.IndexByte(k, ':'); i >= 0 {
		return k[:i], k[i+1:]
	}
	return "", k
}

func sortedKeysMissing[V any](from, other map[string]V) []string {
	var out []string
	for k := range from {
		if _, ok := other[k]; !ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func sortedKeysCommon[V any](a, b map[string]V) []string {
	var out []string
	for k := range a {
		if _, ok := b[k]; ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func toSet(xs []string) map[string]bool {
	out := map[string]bool{}
	for _, x := range xs {
		out[x] = true
	}
	return out
}
