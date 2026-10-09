package install

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const stubBinary = "#!/bin/sh\necho watcher-stub\n"

func tarball(t *testing.T, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "watcher", Mode: 0o755, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type release struct {
	asset    string
	artifact []byte
	unit     []byte
	missing  bool
	corrupt  bool
}

func (r release) server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/latest/download/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		if r.missing {
			http.NotFound(w, nil)
			return
		}
		var b strings.Builder
		sum := sha256.Sum256(r.artifact)
		if r.corrupt {
			fmt.Fprintf(&b, "%064d  %s\n", 0, r.asset)
		} else {
			fmt.Fprintf(&b, "%x  %s\n", sum, r.asset)
		}
		if len(r.unit) > 0 {
			usum := sha256.Sum256(r.unit)
			fmt.Fprintf(&b, "%x  watcher.service\n", usum)
		}
		io.WriteString(w, b.String())
	})
	mux.HandleFunc("/latest/download/"+r.asset, func(w http.ResponseWriter, _ *http.Request) {
		w.Write(r.artifact)
	})
	mux.HandleFunc("/latest/download/watcher.service", func(w http.ResponseWriter, _ *http.Request) {
		w.Write(r.unit)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func haveTools(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		if _, werr := exec.LookPath("wget"); werr != nil {
			t.Skip("neither curl nor wget available")
		}
	}
}

func runInstaller(t *testing.T, extra map[string]string, args ...string) (string, int) {
	t.Helper()
	haveTools(t)
	cmd := exec.Command("sh", append([]string{ScriptPath()}, args...)...)
	merged := map[string]string{
		"PATH": os.Getenv("PATH"),
		"HOME": os.Getenv("HOME"),
	}
	for k, v := range extra {
		merged[k] = v
	}
	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		cmd.Env = append(cmd.Env, k+"="+merged[k])
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run installer: %v", err)
	}
	return out.String(), code
}

func TestInstallerIsValidPOSIXShell(t *testing.T) {
	haveTools(t)
	cmd := exec.Command("sh", "-n", ScriptPath())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sh -n: %v\n%s", err, out)
	}
}

func TestInstallHappyPath(t *testing.T) {
	home := t.TempDir()
	bindir := filepath.Join(t.TempDir(), "bin")
	rel := release{asset: "watcher_linux_amd64.tar.gz", artifact: tarball(t, []byte(stubBinary))}
	srv := rel.server(t)

	out, code := runInstaller(t, map[string]string{
		"HOME":                     home,
		"WATCHER_OS":               "linux",
		"WATCHER_ARCH":             "amd64",
		"WATCHER_INSTALL_DIR":      bindir,
		"WATCHER_RELEASE_BASE_URL": srv.URL,
	})
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}

	bin := filepath.Join(bindir, "watcher")
	info, err := os.Stat(bin)
	if err != nil {
		t.Fatalf("binary not installed: %v\n%s", err, out)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("installed binary is not executable: %v", info.Mode())
	}
	run, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("installed binary did not run: %v", err)
	}
	if !strings.Contains(string(run), "watcher-stub") {
		t.Errorf("installed binary output = %q", run)
	}

	// The install dir was not on PATH, so a marked block must have been added.
	profile, err := os.ReadFile(filepath.Join(home, ".profile"))
	if err != nil {
		t.Fatalf("profile not written: %v", err)
	}
	if !strings.Contains(string(profile), "watcher installer") || !strings.Contains(string(profile), bindir) {
		t.Errorf("PATH block missing from profile:\n%s", profile)
	}
}

func TestInstallChecksumMismatchAborts(t *testing.T) {
	bindir := filepath.Join(t.TempDir(), "bin")
	rel := release{asset: "watcher_linux_amd64.tar.gz", artifact: tarball(t, []byte(stubBinary)), corrupt: true}
	srv := rel.server(t)

	out, code := runInstaller(t, map[string]string{
		"HOME":                     t.TempDir(),
		"WATCHER_OS":               "linux",
		"WATCHER_ARCH":             "amd64",
		"WATCHER_INSTALL_DIR":      bindir,
		"WATCHER_RELEASE_BASE_URL": srv.URL,
	})
	if code == 0 {
		t.Fatalf("exit = 0, want non-zero on checksum mismatch\n%s", out)
	}
	if !strings.Contains(out, "checksum mismatch") {
		t.Errorf("output missing checksum message:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(bindir, "watcher")); !os.IsNotExist(err) {
		t.Error("a partial install was left behind")
	}
}

func TestInstallUnsupportedArchAborts(t *testing.T) {
	rel := release{asset: "watcher_linux_amd64.tar.gz", artifact: tarball(t, []byte(stubBinary))}
	srv := rel.server(t)
	out, code := runInstaller(t, map[string]string{
		"HOME":                     t.TempDir(),
		"WATCHER_OS":               "linux",
		"WATCHER_ARCH":             "ppc64le",
		"WATCHER_INSTALL_DIR":      filepath.Join(t.TempDir(), "bin"),
		"WATCHER_RELEASE_BASE_URL": srv.URL,
	})
	if code == 0 || !strings.Contains(out, "unsupported architecture") {
		t.Fatalf("exit = %d, output:\n%s", code, out)
	}
}

func TestInstallUnsupportedOSAborts(t *testing.T) {
	rel := release{asset: "watcher_windows_amd64.tar.gz", artifact: tarball(t, []byte(stubBinary))}
	srv := rel.server(t)
	out, code := runInstaller(t, map[string]string{
		"HOME":                     t.TempDir(),
		"WATCHER_OS":               "windows",
		"WATCHER_ARCH":             "amd64",
		"WATCHER_INSTALL_DIR":      filepath.Join(t.TempDir(), "bin"),
		"WATCHER_RELEASE_BASE_URL": srv.URL,
	})
	if code == 0 || !strings.Contains(out, "unsupported operating system") {
		t.Fatalf("exit = %d, output:\n%s", code, out)
	}
}

func TestInstallMissingArtifactAborts(t *testing.T) {
	rel := release{asset: "watcher_linux_amd64.tar.gz", artifact: tarball(t, []byte(stubBinary)), missing: true}
	srv := rel.server(t)
	bindir := filepath.Join(t.TempDir(), "bin")
	out, code := runInstaller(t, map[string]string{
		"HOME":                     t.TempDir(),
		"WATCHER_OS":               "linux",
		"WATCHER_ARCH":             "amd64",
		"WATCHER_INSTALL_DIR":      bindir,
		"WATCHER_RELEASE_BASE_URL": srv.URL,
	})
	if code == 0 {
		t.Fatalf("exit = 0, want non-zero when checksums are absent\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(bindir, "watcher")); !os.IsNotExist(err) {
		t.Error("a partial install was left behind")
	}
}

func TestUninstallKeepsConfigUnlessPurged(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".config", "watcher")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(configDir, "config.toml")
	if err := os.WriteFile(configPath, []byte("model = \"m\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	bindir := filepath.Join(t.TempDir(), "bin")
	rel := release{asset: "watcher_linux_amd64.tar.gz", artifact: tarball(t, []byte(stubBinary))}
	srv := rel.server(t)
	env := map[string]string{
		"HOME":                     home,
		"WATCHER_OS":               "linux",
		"WATCHER_ARCH":             "amd64",
		"WATCHER_INSTALL_DIR":      bindir,
		"WATCHER_RELEASE_BASE_URL": srv.URL,
		"WATCHER_UNIT_DIR":         filepath.Join(t.TempDir(), "units"),
	}
	if out, code := runInstaller(t, env); code != 0 {
		t.Fatalf("install exit = %d\n%s", code, out)
	}

	out, code := runInstaller(t, env, "--uninstall")
	if code != 0 {
		t.Fatalf("uninstall exit = %d\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(bindir, "watcher")); !os.IsNotExist(err) {
		t.Error("binary not removed")
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Errorf("config removed without --purge: %v", err)
	}
	profile, _ := os.ReadFile(filepath.Join(home, ".profile"))
	if strings.Contains(string(profile), "watcher installer") {
		t.Error("PATH block not removed on uninstall")
	}

	out, code = runInstaller(t, env, "--uninstall", "--purge")
	if code != 0 {
		t.Fatalf("purge exit = %d\n%s", code, out)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Error("config not removed with --purge")
	}
}

func TestServiceInstall(t *testing.T) {
	unit := []byte("[Service]\nExecStart=/usr/local/bin/watcher run\n")
	rel := release{asset: "watcher_linux_amd64.tar.gz", artifact: tarball(t, []byte(stubBinary)), unit: unit}
	srv := rel.server(t)

	// A fake systemctl on PATH records its calls.
	fakeBin := t.TempDir()
	log := filepath.Join(fakeBin, "calls.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"" + log + "\"\n"
	if err := os.WriteFile(filepath.Join(fakeBin, "systemctl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	unitDir := filepath.Join(t.TempDir(), "units")

	out, code := runInstaller(t, map[string]string{
		"HOME":                     t.TempDir(),
		"PATH":                     fakeBin + ":" + os.Getenv("PATH"),
		"WATCHER_OS":               "linux",
		"WATCHER_ARCH":             "amd64",
		"WATCHER_INSTALL_DIR":      filepath.Join(t.TempDir(), "bin"),
		"WATCHER_UNIT_DIR":         unitDir,
		"WATCHER_RELEASE_BASE_URL": srv.URL,
	}, "--service")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(unitDir, "watcher.service")); err != nil {
		t.Errorf("unit not installed: %v\n%s", err, out)
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "enable --now watcher") {
		t.Errorf("systemctl calls = %q, want an enable", calls)
	}
}
