package backend

import "strings"

const promptTemplate = `You are a reliability engineer's assistant. Explain the root cause of the crash in the log excerpt below.

Respond with ONLY a JSON object matching this schema:
{"summary": string,
 "likely_cause": string,
 "evidence": string[],
 "suggested_fix": string,
 "confidence": number,   // 0.0-1.0
 "severity": "low"|"medium"|"high"|"critical"}

Rules:
- Every string in "evidence" MUST be copied verbatim from the excerpt.
- If the excerpt is insufficient, say so in "summary" and lower "confidence".
  Do not invent details.

Detector kind: {{kind}}
Source: {{source}}

<excerpt>
{{excerpt}}
</excerpt>`

// buildPrompt fills the template. The excerpt is inserted verbatim so the
// model's evidence can be checked against the same bytes the model saw.
func buildPrompt(req Request) string {
	return strings.NewReplacer(
		"{{kind}}", req.Kind,
		"{{source}}", req.Source,
		"{{excerpt}}", req.Excerpt,
	).Replace(promptTemplate)
}
