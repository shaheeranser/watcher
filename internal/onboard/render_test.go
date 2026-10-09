package onboard

import (
	"strings"
	"testing"

	"github.com/shaheeranser/watcher/internal/config"
)

func TestRenderGolden(t *testing.T) {
	got := Render(Value{
		OllamaURL: "http://localhost:11434",
		Model:     "qwen2.5:1.5b",
		Sources: []config.SourceSpec{
			{Label: "backend", Path: "/var/log/backend.log"},
			{Label: "worker", Path: "-"},
		},
	})
	want := `# Watcher configuration, written by ` + "`watcher onboard`" + `.
# Precedence is flag > environment > this file > default.

model = "qwen2.5:1.5b"
ollama_url = "http://localhost:11434"

[[sources]]
label = "backend"
path = "/var/log/backend.log"

[[sources]]
label = "worker"
path = "-"
`
	if got != want {
		t.Errorf("render mismatch:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestRenderOmitsWebhookWhenEmpty(t *testing.T) {
	got := Render(Value{Model: "m", OllamaURL: "http://localhost:11434"})
	if strings.Contains(got, "[webhook]") {
		t.Errorf("no webhook should be rendered when the URL is empty:\n%s", got)
	}
}

func TestRenderEscapesValues(t *testing.T) {
	got := Render(Value{
		Model:     `weird"model`,
		OllamaURL: "http://localhost:11434",
		Sources:   []config.SourceSpec{{Label: "x", Path: `/var/log/a"b.log`}},
	})
	if !strings.Contains(got, `model = "weird\"model"`) {
		t.Errorf("model not escaped:\n%s", got)
	}
	if !strings.Contains(got, `path = "/var/log/a\"b.log"`) {
		t.Errorf("path not escaped:\n%s", got)
	}
}
