package onboard

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/shaheeranser/watcher/internal/config"
)

// Value is the configuration onboarding collected, rendered to TOML.
type Value struct {
	OllamaURL     string
	Model         string
	Sources       []config.SourceSpec
	WebhookURL    string
	WebhookFormat string
}

// Render produces the config file's exact contents: a short header, the
// top-level scalars, an array of source tables, and an optional webhook table.
// The order is fixed so the output is a reviewable golden.
func Render(v Value) string {
	var b strings.Builder
	b.WriteString("# Watcher configuration, written by `watcher onboard`.\n")
	b.WriteString("# Precedence is flag > environment > this file > default.\n\n")
	fmt.Fprintf(&b, "model = %s\n", tomlString(v.Model))
	fmt.Fprintf(&b, "ollama_url = %s\n", tomlString(v.OllamaURL))

	for _, s := range v.Sources {
		b.WriteString("\n[[sources]]\n")
		fmt.Fprintf(&b, "label = %s\n", tomlString(s.EffectiveLabel()))
		fmt.Fprintf(&b, "path = %s\n", tomlString(s.Path))
	}

	if v.WebhookURL != "" {
		b.WriteString("\n[webhook]\n")
		fmt.Fprintf(&b, "url = %s\n", tomlString(v.WebhookURL))
		fmt.Fprintf(&b, "format = %s\n", tomlString(v.WebhookFormat))
	}
	return b.String()
}

// tomlString quotes a value as a TOML basic string. For the paths, URLs, and
// model names onboarding collects, Go's quoting is a valid TOML basic string.
func tomlString(s string) string { return strconv.Quote(s) }
