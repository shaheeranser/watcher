package docker

import "strings"

// Compose label keys Docker attaches to containers it manages.
const (
	labelComposeProject = "com.docker.compose.project"
	labelComposeService = "com.docker.compose.service"
)

// Container is the union of the fields Watcher reads from the list and inspect
// endpoints, flattened so the rest of the package does not care which endpoint
// a value came from.
type Container struct {
	ID      string
	Name    string
	Names   []string
	Running bool
	Tty     bool
	Labels  map[string]string
}

// Label reads a container label from either shape the API returns it in.
func (c Container) Label(key string) string {
	return c.Labels[key]
}

// ComposeProject is the container's Compose project, or "" if it is not part of
// one.
func (c Container) ComposeProject() string { return c.Labels[labelComposeProject] }

// ComposeService is the container's Compose service name, or "".
func (c Container) ComposeService() string { return c.Labels[labelComposeService] }

// DisplayName is the identity used as an incident label: the Compose service
// name when there is one, otherwise the container name.
func (c Container) DisplayName() string {
	if service := c.ComposeService(); service != "" {
		return service
	}
	for _, candidate := range append([]string{c.Name}, c.Names...) {
		if trimmed := strings.TrimPrefix(candidate, "/"); trimmed != "" {
			return trimmed
		}
	}
	return c.ID
}

// NamesMatch reports whether the container is known by name. Docker reports
// names with a leading slash, so both sides are normalized.
func (c Container) NamesMatch(name string) bool {
	target := strings.TrimPrefix(name, "/")
	if strings.TrimPrefix(c.Name, "/") == target {
		return true
	}
	for _, candidate := range c.Names {
		if strings.TrimPrefix(candidate, "/") == target {
			return true
		}
	}
	return false
}

// listContainer is the /containers/json shape, where State is a string.
type listContainer struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	State  string            `json:"State"`
	Labels map[string]string `json:"Labels"`
}

func (l listContainer) container() Container {
	return Container{
		ID:      l.ID,
		Names:   l.Names,
		Running: l.State == "running",
		Labels:  l.Labels,
	}
}

// inspectContainer is the /containers/{id}/json shape, where State is an object
// and the labels and TTY flag live under Config.
type inspectContainer struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`
	Config struct {
		Tty    bool              `json:"Tty"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	State struct {
		Running bool   `json:"Running"`
		Status  string `json:"Status"`
	} `json:"State"`
}

func (i inspectContainer) container() Container {
	return Container{
		ID:      i.ID,
		Name:    i.Name,
		Running: i.State.Running,
		Tty:     i.Config.Tty,
		Labels:  i.Config.Labels,
	}
}
