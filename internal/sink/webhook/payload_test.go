package webhook

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaheeranser/watcher/internal/backend"
	"github.com/shaheeranser/watcher/internal/sink"
)

var update = flag.Bool("update", false, "rewrite golden files")

func fixtureResult() sink.Result {
	return sink.Result{
		Fingerprint: "abc123",
		Kind:        "go-panic",
		Source:      "backend",
		Count:       3,
		FirstSeen:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		LastSeen:    time.Date(2026, 1, 2, 3, 5, 5, 0, time.UTC),
		Explanation: &backend.Explanation{
			Summary:      "nil pointer dereference",
			LikelyCause:  "the handler dereferenced a nil request",
			Evidence:     []string{"panic: boom"},
			SuggestedFix: "guard the nil check",
			Confidence:   0.8,
			Severity:     "high",
		},
		Model: "qwen2.5-coder:0.5b",
	}
}

func unavailableResult() sink.Result {
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return sink.Result{
		Fingerprint: "def456",
		Kind:        "generic-fatal",
		Source:      "/var/log/app.log",
		Count:       1,
		FirstSeen:   base,
		LastSeen:    base,
		ExplainErr:  "dial tcp 127.0.0.1:11434: connect: connection refused",
	}
}

func TestPayloadGoldens(t *testing.T) {
	cases := []struct {
		golden   string
		provider string
		result   sink.Result
	}{
		{"generic.json", "generic", fixtureResult()},
		{"slack.json", "slack", fixtureResult()},
		{"discord.json", "discord", fixtureResult()},
		{"generic-unavailable.json", "generic", unavailableResult()},
	}

	for _, c := range cases {
		t.Run(c.golden, func(t *testing.T) {
			build, err := builderFor(c.provider)
			if err != nil {
				t.Fatal(err)
			}
			got, err := json.MarshalIndent(build(c.result), "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			checkGolden(t, c.golden, string(got)+"\n")
		})
	}
}

func TestGenericPayloadCarriesEveryField(t *testing.T) {
	got, err := json.Marshal(genericPayloadFor(fixtureResult()))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(got, &payload); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"event", "kind", "source", "fingerprint", "severity", "count",
		"first_seen", "last_seen", "summary", "likely_cause", "suggested_fix", "confidence", "model"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("generic payload missing %q: %s", key, got)
		}
	}
	if payload["event"] != "incident" {
		t.Errorf("event = %v, want incident", payload["event"])
	}
}

func TestUnavailableExplanationIsExplicit(t *testing.T) {
	got, err := json.Marshal(genericPayloadFor(unavailableResult()))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(got, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["explanation_unavailable"] != true {
		t.Errorf("explanation_unavailable = %v, want true: %s", payload["explanation_unavailable"], got)
	}
	if payload["confidence"] != nil {
		t.Errorf("confidence = %v, want null when unavailable", payload["confidence"])
	}
}

func TestLongTextIsCappedAndMarked(t *testing.T) {
	r := fixtureResult()
	r.Explanation.Summary = strings.Repeat("x", 8000)

	build, err := builderFor("discord")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(build(r))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), truncationMarker) {
		t.Error("a capped payload must mark the truncation")
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if content := payload["content"].(string); len(content) > discordContentLimit {
		t.Errorf("discord content = %d bytes, want <= %d", len(content), discordContentLimit)
	}
	embed := payload["embeds"].([]any)[0].(map[string]any)
	if description := embed["description"].(string); len(description) > discordDescriptionLimit {
		t.Errorf("discord description = %d bytes, want <= %d", len(description), discordDescriptionLimit)
	}
}

func TestBuilderForRejectsUnknownProvider(t *testing.T) {
	if _, err := builderFor("teams"); err == nil {
		t.Fatal("expected an error for an unknown provider")
	}
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("golden mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
