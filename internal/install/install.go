// Package install locates the shell installer and holds its end-to-end tests.
// The installer itself is a POSIX shell script at the repository root; the
// tests drive it against a fake release server.
package install

import "path/filepath"

// ScriptPath is the installer's path relative to this package's directory,
// which is the working directory `go test` runs in.
func ScriptPath() string { return filepath.Join("..", "..", "install.sh") }
