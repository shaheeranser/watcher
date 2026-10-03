package webhook

import (
	"fmt"
	"strings"
	"time"

	"github.com/shaheeranser/watcher/internal/sink"
)

// Provider length limits, from each service's documented caps.
const (
	slackHeaderLimit = 150
	slackTextLimit   = 3000

	discordContentLimit     = 2000
	discordTitleLimit       = 256
	discordDescriptionLimit = 4096
)

const truncationMarker = "… [truncated]"

// genericPayload is the flat JSON object for the generic provider and the shape
// written to the fallback spool: the RT-WH-3 field set, with unavailable
// explanation fields represented explicitly rather than omitted.
type genericPayload struct {
	Event                  string   `json:"event"`
	Kind                   string   `json:"kind"`
	Source                 string   `json:"source"`
	Fingerprint            string   `json:"fingerprint"`
	Severity               string   `json:"severity"`
	Count                  int      `json:"count"`
	FirstSeen              string   `json:"first_seen"`
	LastSeen               string   `json:"last_seen"`
	Summary                string   `json:"summary"`
	LikelyCause            string   `json:"likely_cause"`
	SuggestedFix           string   `json:"suggested_fix"`
	Confidence             *float64 `json:"confidence"`
	Model                  string   `json:"model"`
	ExplanationPending     bool     `json:"explanation_pending,omitempty"`
	ExplanationUnavailable bool     `json:"explanation_unavailable,omitempty"`
	DeliveryError          string   `json:"delivery_error,omitempty"`
}

type payloadBuilder func(sink.Result) any

func builderFor(format string) (payloadBuilder, error) {
	switch format {
	case "generic":
		return func(r sink.Result) any { return genericPayloadFor(r) }, nil
	case "slack":
		return slackPayload, nil
	case "discord":
		return discordPayload, nil
	default:
		return nil, fmt.Errorf("unknown webhook provider %q", format)
	}
}

// eventKindOf names the lifecycle stage a notification announces. A result with
// no notification kind is a plain incident result from the local stream.
func eventKindOf(r sink.Result) string {
	if r.Notification != "" {
		return r.Notification
	}
	return "incident"
}

// explanationPending reports whether the notification went out before the model
// returned, as opposed to the model having failed outright.
func explanationPending(r sink.Result) bool {
	return r.Explanation == nil && (r.Pending || r.ExplainErr == "")
}

func genericPayloadFor(r sink.Result) genericPayload {
	p := genericPayload{
		Event:       eventKindOf(r),
		Kind:        r.Kind,
		Source:      r.Source,
		Fingerprint: r.Fingerprint,
		Severity:    severityOf(r),
		Count:       r.Count,
		FirstSeen:   timestamp(r.FirstSeen),
		LastSeen:    timestamp(r.LastSeen),
		Model:       r.Model,
	}
	if r.Explanation != nil {
		e := r.Explanation
		confidence := e.Confidence
		p.Summary = e.Summary
		p.LikelyCause = e.LikelyCause
		p.SuggestedFix = e.SuggestedFix
		p.Confidence = &confidence
	} else if explanationPending(r) {
		p.ExplanationPending = true
	} else {
		p.ExplanationUnavailable = true
	}
	return p
}

// slackPayload renders the Block Kit envelope. The header is plain text and the
// body is mrkdwn, each capped to Slack's limits with a visible marker.
func slackPayload(r sink.Result) any {
	return map[string]any{
		"text": capText(summaryLine(r), slackTextLimit),
		"blocks": []any{
			map[string]any{
				"type": "header",
				"text": map[string]any{"type": "plain_text", "text": capText(header(r), slackHeaderLimit)},
			},
			map[string]any{
				"type": "section",
				"text": map[string]any{"type": "mrkdwn", "text": capText(body(r), slackTextLimit)},
			},
		},
	}
}

// discordPayload renders an embed. The content field carries a one-line summary
// and the embed carries the detail, both capped to Discord's limits.
func discordPayload(r sink.Result) any {
	return map[string]any{
		"content": capText(summaryLine(r), discordContentLimit),
		"embeds": []any{
			map[string]any{
				"title":       capText(header(r), discordTitleLimit),
				"description": capText(body(r), discordDescriptionLimit),
			},
		},
	}
}

func header(r sink.Result) string {
	return fmt.Sprintf("[%s] %s on %s (%s)", strings.ToUpper(eventKindOf(r)), r.Kind, r.Source, severityOf(r))
}

func summaryLine(r sink.Result) string {
	if r.Explanation == nil {
		state := "explanation unavailable"
		if explanationPending(r) {
			state = "explanation pending"
		}
		return fmt.Sprintf("%s — count %d, %s", header(r), r.Count, state)
	}
	return fmt.Sprintf("%s — %s", header(r), r.Explanation.Summary)
}

func body(r sink.Result) string {
	var b strings.Builder
	line := func(label, value string) {
		if value != "" {
			fmt.Fprintf(&b, "%s: %s\n", label, value)
		}
	}
	line("event", eventKindOf(r))
	line("detector", r.Kind)
	line("source", r.Source)
	line("fingerprint", r.Fingerprint)
	line("severity", severityOf(r))
	fmt.Fprintf(&b, "count: %d\n", r.Count)
	line("first_seen", timestamp(r.FirstSeen))
	line("last_seen", timestamp(r.LastSeen))

	if r.Explanation == nil {
		if explanationPending(r) {
			line("summary", "explanation pending")
			return strings.TrimRight(b.String(), "\n")
		}
		line("summary", "explanation unavailable")
		line("reason", r.ExplainErr)
		return strings.TrimRight(b.String(), "\n")
	}

	e := r.Explanation
	line("summary", e.Summary)
	line("likely_cause", e.LikelyCause)
	line("suggested_fix", e.SuggestedFix)
	fmt.Fprintf(&b, "confidence: %.2f\n", e.Confidence)
	line("model", r.Model)
	if len(e.Evidence) > 0 {
		b.WriteString("evidence:\n")
		for _, item := range e.Evidence {
			fmt.Fprintf(&b, "  - %q\n", item)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func severityOf(r sink.Result) string {
	if r.Explanation == nil || r.Explanation.Severity == "" {
		return "unknown"
	}
	return r.Explanation.Severity
}

func timestamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// capText truncates to limit bytes and marks the cut, so a capped body is never
// silently shortened.
func capText(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	if limit <= len(truncationMarker) {
		return s[:limit]
	}
	return s[:limit-len(truncationMarker)] + truncationMarker
}
