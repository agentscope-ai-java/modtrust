package govars

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// writeGoEnvConfig runs the real `go env -w` against a fresh, temporary
// GOENV file and returns its path. This exercises the actual go binary
// rather than hand-writing the config file's format, so the test fails if
// that format ever changes.
func writeGoEnvConfig(t *testing.T, settings ...string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "env")

	args := append([]string{"env", "-w"}, settings...)
	cmd := exec.Command("go", args...)
	cmd.Env = append(os.Environ(), "GOENV="+path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go env -w %v: %v\n%s", settings, err, out)
	}
	return path
}

func TestRead_Defaults(t *testing.T) {
	// A fresh, empty GOENV and no relevant environment variables: every
	// tracked var should report SourceDefault, and the well-known
	// defaults should be the toolchain's real ones.
	dir := t.TempDir()
	t.Setenv("GOENV", filepath.Join(dir, "env"))
	for _, v := range append([]string{"GONOSUMCHECK"}, Vars...) {
		t.Setenv(v, "")
		os.Unsetenv(v)
	}

	snap, err := Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if got := snap.Vars["GOSUMDB"]; got.Value != "sum.golang.org" || got.Source != SourceDefault {
		t.Errorf("GOSUMDB = %+v, want value sum.golang.org, source default", got)
	}
	if got := snap.Vars["GOPROXY"]; got.Value != "https://proxy.golang.org,direct" || got.Source != SourceDefault {
		t.Errorf("GOPROXY = %+v, want value https://proxy.golang.org,direct, source default", got)
	}
	if got := snap.Vars["GOPRIVATE"]; got.Value != "" || got.Source != SourceDefault {
		t.Errorf("GOPRIVATE = %+v, want empty, source default", got)
	}
	if snap.GONOSUMCHECKSet {
		t.Errorf("GONOSUMCHECKSet = true, want false")
	}
}

func TestRead_EnvironmentSource(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOENV", filepath.Join(dir, "env"))
	t.Setenv("GOSUMDB", "off")

	snap, err := Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	got := snap.Vars["GOSUMDB"]
	if got.Value != "off" {
		t.Fatalf("GOSUMDB value = %q, want off", got.Value)
	}
	if got.Source != SourceEnvironment {
		t.Fatalf("GOSUMDB source = %q, want %q", got.Source, SourceEnvironment)
	}
}

func TestRead_ConfigFileSource(t *testing.T) {
	path := writeGoEnvConfig(t, "GOPROXY=https://example.test/proxy,direct")
	t.Setenv("GOENV", path)
	t.Setenv("GOPROXY", "")
	os.Unsetenv("GOPROXY")

	snap, err := Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	got := snap.Vars["GOPROXY"]
	if got.Value != "https://example.test/proxy,direct" {
		t.Fatalf("GOPROXY value = %q, want the config-file value", got.Value)
	}
	if got.Source != SourceConfigFile {
		t.Fatalf("GOPROXY source = %q, want %q", got.Source, SourceConfigFile)
	}
	if got.ConfigPath != path {
		t.Fatalf("GOPROXY config path = %q, want %q", got.ConfigPath, path)
	}
}

func TestRead_ConfigFilePinnedToDefaultValue(t *testing.T) {
	// `go env -w` can pin a variable to exactly its built-in default.
	// `go env -json -changed` would not surface that (the effective value
	// equals the default), but reading the config file directly must
	// still report it as explicitly configured.
	path := writeGoEnvConfig(t, "GOSUMDB=sum.golang.org")
	t.Setenv("GOENV", path)
	t.Setenv("GOSUMDB", "")
	os.Unsetenv("GOSUMDB")

	snap, err := Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	got := snap.Vars["GOSUMDB"]
	if got.Source != SourceConfigFile {
		t.Fatalf("GOSUMDB source = %q, want %q (value happens to equal the default)", got.Source, SourceConfigFile)
	}
}

func TestRead_EnvironmentOverridesConfigFile(t *testing.T) {
	path := writeGoEnvConfig(t, "GOSUMDB=custom.example.com")
	t.Setenv("GOENV", path)
	t.Setenv("GOSUMDB", "off")

	snap, err := Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	got := snap.Vars["GOSUMDB"]
	if got.Value != "off" || got.Source != SourceEnvironment {
		t.Fatalf("GOSUMDB = %+v, want value off, source environment (env must win over config file)", got)
	}
}

func TestRead_GONOSUMDBInheritsFromGOPRIVATE(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOENV", filepath.Join(dir, "env"))
	t.Setenv("GOPRIVATE", "*")
	t.Setenv("GONOSUMDB", "")
	os.Unsetenv("GONOSUMDB")
	t.Setenv("GONOPROXY", "")
	os.Unsetenv("GONOPROXY")

	snap, err := Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	for _, name := range []string{"GONOSUMDB", "GONOPROXY"} {
		got := snap.Vars[name]
		if got.Value != "*" {
			t.Errorf("%s value = %q, want inherited '*'", name, got.Value)
		}
		if got.Source != SourceInherited {
			t.Errorf("%s source = %q, want %q", name, got.Source, SourceInherited)
		}
	}
}

func TestRead_GONOSUMCHECKIsIgnoredButDetected(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOENV", filepath.Join(dir, "env"))
	t.Setenv("GONOSUMCHECK", "1")

	// Confirm, against the real go binary, that go env itself never
	// surfaces this variable no matter what the environment says.
	cmd := exec.Command("go", "env", "GONOSUMCHECK")
	cmd.Env = append(os.Environ(), "GONOSUMCHECK=1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go env GONOSUMCHECK: %v", err)
	}
	if got := string(out); got != "\n" && got != "" {
		t.Fatalf("go env GONOSUMCHECK printed %q, want empty (this test's premise is stale)", got)
	}

	snap, err := Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !snap.GONOSUMCHECKSet {
		t.Fatalf("GONOSUMCHECKSet = false, want true (it is set in the process environment)")
	}
	if snap.GONOSUMCHECKValue != "1" {
		t.Fatalf("GONOSUMCHECKValue = %q, want 1", snap.GONOSUMCHECKValue)
	}
}

func TestRead_GOENVOffDisablesConfigFile(t *testing.T) {
	path := writeGoEnvConfig(t, "GOSUMDB=custom.example.com")
	// Point GOENV at "off" while a real config file with a dangerous
	// setting exists on disk at path; it must be ignored entirely.
	t.Setenv("GOENV", "off")
	t.Setenv("GOSUMDB", "")
	os.Unsetenv("GOSUMDB")

	snap, err := Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !snap.ConfigDisabled {
		t.Fatalf("ConfigDisabled = false, want true")
	}
	got := snap.Vars["GOSUMDB"]
	if got.Value != "sum.golang.org" || got.Source != SourceDefault {
		t.Fatalf("GOSUMDB = %+v, want the real default (config file at %s must be ignored)", got, path)
	}
}
