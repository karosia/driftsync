package enrich

import "strings"

func splitPointer(ptr string) []string {
	ptr = strings.TrimPrefix(ptr, "/")
	if ptr == "" {
		return nil
	}
	toks := strings.Split(ptr, "/")
	for i, t := range toks {
		t = strings.ReplaceAll(t, "~1", "/")
		t = strings.ReplaceAll(t, "~0", "~")
		toks[i] = t
	}
	return toks
}
