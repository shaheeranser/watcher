package backend

import "strings"

// ground enforces that evidence is quotable: any entry that is not a verbatim
// substring of the excerpt is dropped, and if anything was dropped the
// explanation is marked low confidence (CORE-BE-5).
func ground(e Explanation, excerpt string) Explanation {
	if len(e.Evidence) == 0 {
		return e
	}

	kept := make([]string, 0, len(e.Evidence))
	dropped := false
	for _, ev := range e.Evidence {
		if ev != "" && strings.Contains(excerpt, ev) {
			kept = append(kept, ev)
			continue
		}
		dropped = true
	}
	e.Evidence = kept

	if dropped {
		if e.Confidence > 0.3 {
			e.Confidence = 0.3
		}
		e.Summary = strings.TrimSpace(e.Summary) + " (some cited evidence was not found in the excerpt; confidence lowered)"
	}
	return e
}
