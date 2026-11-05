package diff_test

import (
	"strings"
	"testing"

	"github.com/karosia/driftsync/diff"
)

// TestTypeChangeSeverity_DirectionAware: value-set widening is breaking only
// in responses, narrowing only in requests; any real shape change is breaking
// both ways. User is used in BOTH directions, so each case checks both.
func TestTypeChangeSeverity_DirectionAware(t *testing.T) {
	B, I := diff.Breaking, diff.Info
	cases := []struct {
		name          string
		published     string
		code          string
		req, resp     diff.Severity
		noteSubstring string
	}{
		{"enum value added", "s: {type: string, enum: [a, b]}", "s: {type: string, enum: [a, b, c]}", I, B, "widened"},
		{"enum value removed", "s: {type: string, enum: [a, b, c]}", "s: {type: string, enum: [a, b]}", B, I, "narrowed"},
		{"enum constraint dropped", "s: {type: string, enum: [a, b]}", "s: {type: string}", I, B, "widened"},
		{"enum constraint introduced", "s: {type: string}", "s: {type: string, enum: [a, b]}", B, I, "narrowed"},
		{"nullable added", "s: {type: string}", "s: {type: string, nullable: true}", I, B, "widened"},
		{"nullable removed", "s: {type: string, nullable: true}", "s: {type: string}", B, I, "narrowed"},
		{"integer -> number", "s: {type: integer}", "s: {type: number}", I, B, "widened"},
		{"number -> integer", "s: {type: number}", "s: {type: integer}", B, I, "narrowed"},
		{"enum swapped (add+remove)", "s: {type: string, enum: [a, b]}", "s: {type: string, enum: [a, c]}", B, B, ""},
		{"type changed", "s: {type: string}", "s: {type: integer}", B, B, ""},
		{"format changed", "s: {type: string, format: date}", "s: {type: string, format: date-time}", B, B, ""},
		{"enum added + format changed", "s: {type: string, enum: [a]}", "s: {type: string, format: uuid, enum: [a, b]}", B, B, ""},
		{"array items changed", "s: {type: array, items: {type: string}}", "s: {type: array, items: {type: integer}}", B, B, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pub := mustDoc(t, specUserBothDirections("\n        "+tc.published))
			code := mustDoc(t, specUserBothDirections("\n        "+tc.code))
			r := diff.Diff(pub, code)
			for _, d := range []struct {
				dir  string
				want diff.Severity
			}{{"request", tc.req}, {"response", tc.resp}} {
				c := find(r, diff.PropertyTypeChanged, "User ["+d.dir+"].s")
				if c == nil {
					t.Fatalf("%s: no PropertyTypeChanged; changes: %+v", d.dir, r.Changes)
				}
				if c.Severity != d.want {
					t.Errorf("%s: severity = %s, want %s (%s -> %s)", d.dir, c.Severity, d.want, c.From, c.To)
				}
				if tc.noteSubstring != "" && !strings.Contains(c.Note, tc.noteSubstring) {
					t.Errorf("%s: note = %q, want it to mention %q", d.dir, c.Note, tc.noteSubstring)
				}
			}
		})
	}
}

// Parameters are always request-direction: adding an accepted value is safe,
// removing one breaks callers that send it.
func TestTypeChangeSeverity_Parameter(t *testing.T) {
	spec := func(schema string) string {
		return minimalPaths(`
  /items:
    get:
      parameters:
        - {name: sort, in: query, schema: ` + schema + `}
      responses:
        '200': {description: OK}`)
	}
	cases := []struct {
		name, from, to string
		want           diff.Severity
	}{
		{"enum value added", "{type: string, enum: [asc]}", "{type: string, enum: [asc, desc]}", diff.Info},
		{"enum value removed", "{type: string, enum: [asc, desc]}", "{type: string, enum: [asc]}", diff.Breaking},
		{"type changed", "{type: string}", "{type: integer}", diff.Breaking},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := diff.Diff(mustDoc(t, spec(tc.from)), mustDoc(t, spec(tc.to)))
			c := find(r, diff.ParameterTypeChanged, "query:sort")
			if c == nil {
				t.Fatalf("no ParameterTypeChanged; changes: %+v", r.Changes)
			}
			if c.Severity != tc.want {
				t.Errorf("severity = %s, want %s", c.Severity, tc.want)
			}
		})
	}
}
