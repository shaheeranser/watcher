// Package systemd ships the supervisor unit for a bare-metal install and the
// few commands needed to install, enable, and remove it. The unit is embedded
// so onboarding can write it without the repository, and the release workflow
// publishes this same file as an asset.
package systemd

import _ "embed"

//go:embed watcher.service
var service string

// Unit returns the shipped unit's contents.
func Unit() string { return service }
