package docker

import (
	"fmt"
	"strings"
)

type selectMode int

const (
	modeOwnProject selectMode = iota
	modeProject
	modeLabel
	modeName
	modeService
)

// Selector chooses which containers a Docker source attaches to. The zero
// selector targets Watcher's own Compose project; the others are explicit and
// override that default.
type Selector struct {
	mode  selectMode
	value string
}

// ParseSelector decodes --containers. The empty value and "compose" mean the
// default scope (Watcher's own project); the rest are project=, label=,
// name=, and service=.
func ParseSelector(raw string) (Selector, error) {
	raw = strings.TrimSpace(raw)
	switch raw {
	case "", "compose":
		return Selector{mode: modeOwnProject}, nil
	}

	kind, value, ok := strings.Cut(raw, "=")
	if !ok || strings.TrimSpace(value) == "" {
		return Selector{}, fmt.Errorf("docker selector %q must be KIND=VALUE (project, label, name, service)", raw)
	}
	value = strings.TrimSpace(value)

	switch kind {
	case "project":
		return Selector{mode: modeProject, value: value}, nil
	case "label":
		if !strings.Contains(value, "=") {
			return Selector{}, fmt.Errorf("docker selector %q must be label=KEY=VALUE", raw)
		}
		return Selector{mode: modeLabel, value: value}, nil
	case "name":
		return Selector{mode: modeName, value: value}, nil
	case "service":
		return Selector{mode: modeService, value: value}, nil
	default:
		return Selector{}, fmt.Errorf("unknown docker selector kind %q (want project, label, name, service)", kind)
	}
}

// OwnProject reports whether the selector still needs Watcher's own Compose
// project resolved before it can match anything.
func (s Selector) OwnProject() bool { return s.mode == modeOwnProject }

// Matches reports whether a container is in the selector's scope.
func (s Selector) Matches(c Container) bool {
	switch s.mode {
	case modeProject:
		return c.ComposeProject() == s.value
	case modeLabel:
		key, value, _ := strings.Cut(s.value, "=")
		return c.Label(key) == value
	case modeName:
		return c.NamesMatch(s.value)
	case modeService:
		return c.ComposeService() == s.value
	default:
		return false
	}
}

// String renders the selector for diagnostics.
func (s Selector) String() string {
	switch s.mode {
	case modeProject:
		return "project=" + s.value
	case modeLabel:
		return "label=" + s.value
	case modeName:
		return "name=" + s.value
	case modeService:
		return "service=" + s.value
	default:
		return "own compose project"
	}
}
