package diff

import (
	"reflect"
	"testing"
)

func TestSeverityTable(t *testing.T) {
	cases := []struct {
		name string
		got  Severity
		want Severity
	}{
		{"add required in request", addedSeverity(Request, true), Breaking},
		{"add optional in request", addedSeverity(Request, false), Info},
		{"add in response", addedSeverity(Response, true), Info},
		{"remove in request", removedSeverity(Request), Info},
		{"remove in response", removedSeverity(Response), Breaking},
		{"became required in request", requiredFlipSeverity(Request, true), Breaking},
		{"became required in response", requiredFlipSeverity(Response, true), Info},
		{"became optional in request", requiredFlipSeverity(Request, false), Info},
		{"became optional in response", requiredFlipSeverity(Response, false), Breaking},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %v want %v", c.name, c.got, c.want)
		}
	}
}

func TestRenameTier1CaseSeparator(t *testing.T) {
	ap := map[string]string{"user_id": "type:string"}
	bp := map[string]string{"userId": "type:string"}
	m, remL, addL := matchRenames([]string{"user_id"}, []string{"userId"}, ap, bp)
	if len(m) != 1 || m[0].from != "user_id" || m[0].to != "userId" || m[0].rule != "case/separator" {
		t.Fatalf("expected 1 case/separator rename, got %+v", m)
	}
	if len(remL) != 0 || len(addL) != 0 {
		t.Errorf("expected no leftovers, got rem=%v add=%v", remL, addL)
	}
}

func TestRenameTier2NearMatch(t *testing.T) {
	ap := map[string]string{"color": "type:string"}
	bp := map[string]string{"colour": "type:string"}
	m, _, _ := matchRenames([]string{"color"}, []string{"colour"}, ap, bp)
	if len(m) != 1 || m[0].rule != "near-match" {
		t.Fatalf("expected 1 near-match rename, got %+v", m)
	}
}

func TestRenameRefusedOnTypeMismatch(t *testing.T) {
	ap := map[string]string{"user_id": "type:string"}
	bp := map[string]string{"userId": "type:integer"} // different signature
	m, remL, addL := matchRenames([]string{"user_id"}, []string{"userId"}, ap, bp)
	if len(m) != 0 {
		t.Fatalf("expected no rename on type mismatch, got %+v", m)
	}
	if len(remL) != 1 || len(addL) != 1 {
		t.Errorf("expected both left as remove+add, got rem=%v add=%v", remL, addL)
	}
}

func TestRenameAmbiguousStaysSplit(t *testing.T) {
	ap := map[string]string{"name": "type:string"}
	bp := map[string]string{"nome": "type:string", "namn": "type:string"}
	m, remL, _ := matchRenames([]string{"name"}, []string{"nome", "namn"}, ap, bp)
	if len(m) != 0 {
		t.Fatalf("ambiguous match should not pair, got %+v", m)
	}
	if len(remL) != 1 {
		t.Errorf("expected 'name' left as removed, got %v", remL)
	}
}

func TestRenameSeverityWorstCase(t *testing.T) {
	if renameSeverity(Response, false) != Breaking {
		t.Error("rename in response should be breaking")
	}
	if renameSeverity(Request, false) != Info {
		t.Error("rename in request (optional) should be info")
	}
}

func TestNormalize(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"user_id", "userid"}, {"userId", "userid"}, {"USER-ID", "userid"}, {"user id", "userid"},
	} {
		if got := normalize(c.in); got != c.want {
			t.Errorf("normalize(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestLevenshtein(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"color", "colour", 1}, {"kitten", "sitting", 3}, {"same", "same", 0},
	} {
		if got := levenshtein(c.a, c.b); got != c.want {
			t.Errorf("levenshtein(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

var _ = reflect.DeepEqual
