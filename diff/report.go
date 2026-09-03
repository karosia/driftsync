package diff

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Markdown renders the report as a PR-friendly markdown summary: a one-line
// headline, then a table grouped severity-first (breaking on top) and sorted
// deterministically so the same drift always produces the same document.
func (r *Report) Markdown() string {
	if len(r.Changes) == 0 {
		return "## API doc drift\n\nNo drift detected — the published spec matches the code.\n"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "## API doc drift\n\n**%d change(s), %d breaking.**\n\n",
		len(r.Changes), r.Breaking())

	fmt.Fprintln(&b, "| Severity | Change | Location | Detail |")
	fmt.Fprintln(&b, "|----------|--------|----------|--------|")
	for _, c := range sortedForReport(r.Changes) {
		sev := c.Severity.String()
		if c.Severity == Breaking {
			sev = "**BREAKING**"
		}
		detail := ""
		if c.From != "" || c.To != "" {
			detail = fmt.Sprintf("`%s` → `%s`", c.From, c.To)
		}
		if c.Note != "" {
			if detail != "" {
				detail += " "
			}
			detail += "(" + c.Note + ")"
		}
		fmt.Fprintf(&b, "| %s | %s | `%s` | %s |\n",
			sev, c.Kind, c.Location, detail)
	}

	if r.Breaking() > 0 {
		fmt.Fprintf(&b, "\n> ⚠️ %d breaking change(s) — review carefully before merging.\n",
			r.Breaking())
	}
	return b.String()
}

// Text renders the plain, terminal-style report (what the CLI printed inline).
func (r *Report) Text() string {
	if len(r.Changes) == 0 {
		return "no drift: published spec matches code\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d change(s), %d breaking:\n\n", len(r.Changes), r.Breaking())
	for _, c := range sortedForReport(r.Changes) {
		line := fmt.Sprintf("  [%-8s] %-26s %s", c.Severity, c.Kind, c.Location)
		if c.From != "" || c.To != "" {
			line += fmt.Sprintf("  (%s -> %s)", c.From, c.To)
		}
		if c.Note != "" {
			line += "  [" + c.Note + "]"
		}
		fmt.Fprintln(&b, line)
	}
	return b.String()
}

// sortedForReport orders changes breaking-first, then by kind, then location —
// so reviewers see the dangerous items on top and output is deterministic.
func sortedForReport(in []Change) []Change {
	out := make([]Change, len(in))
	copy(out, in)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return out[i].Severity > out[j].Severity // Breaking(1) before Info(0)
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Location < out[j].Location
	})
	return out
}

// JSON renders the report as machine-readable JSON for other tools/CI.
func (r *Report) JSON() (string, error) {
	out, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}
