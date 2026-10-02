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
- "summary": one short sentence naming the error.
- "likely_cause": one or two sentences. Name the failing request, handler, or
  function; the error as the log states it (for example a TypeError,
  AttributeError, panic, or heap exhaustion); and the specific field, route,
  key, or value involved. Reuse the excerpt's own wording for these; do not
  paraphrase them into different terms.
- "evidence": at most two short lines copied verbatim from the excerpt.
- "suggested_fix": one short sentence.
- Stop as soon as the JSON object is complete. Never repeat a sentence.
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
