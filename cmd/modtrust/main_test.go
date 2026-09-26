package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildTestModule creates a real module with a real dependency on disk
// and runs the real `go mod tidy`, returning its directory. Skips if
// there's no network to resolve the dependency.
func buildTestModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module example.com/clitest\n\ngo 1.21\n\nrequire rsc.io/quote v1.5.2\n")
	mustWrite(t, filepath.Join(dir, "main.go"), "package main\n\nimport (\n\t\"fmt\"\n\n\t\"rsc.io/quote\"\n)\n\nfunc main() { fmt.Println(quote.Hello()) }\n")

	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("go mod tidy needs network access, skipping: %v\n%s", err, out)
	}
	return dir
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runCLI(t *testing.T, args []string) (stdout, stderr string, code int) {
	t.Helper()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	code = run(args, outW, errW)
	outW.Close()
	errW.Close()

	outBuf := make([]byte, 1<<20)
	n, _ := outR.Read(outBuf)
	stdout = string(outBuf[:n])

	errBuf := make([]byte, 1<<20)
	n, _ = errR.Read(errBuf)
	stderr = string(errBuf[:n])

	return stdout, stderr, code
}

func TestRun_HumanReadableSummary(t *testing.T) {
	dir := buildTestModule(t)
	stdout, _, code := runCLI(t, []string{"--dir", dir})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (default --fail-on is empty)", code)
	}
	if !strings.Contains(stdout, "go env configuration:") {
		t.Errorf("stdout missing config section:\n%s", stdout)
	}
	if !strings.Contains(stdout, "go.sum (complete)") {
		t.Errorf("stdout missing go.sum summary:\n%s", stdout)
	}
}

func TestRun_JSONOutput(t *testing.T) {
	dir := buildTestModule(t)
	stdout, _, code := runCLI(t, []string{"--dir", dir, "--json"})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, stdout)
	}
	if _, ok := parsed["config"]; !ok {
		t.Errorf("JSON missing 'config' key: %s", stdout)
	}
	if _, ok := parsed["sum"]; !ok {
		t.Errorf("JSON missing 'sum' key: %s", stdout)
	}
}

func TestRun_FailOnTriggersExitCode1(t *testing.T) {
	dir := buildTestModule(t)
	t.Setenv("GOSUMDB", "off")

	stdout, _, code := runCLI(t, []string{"--dir", dir, "--fail-on", "critical"})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 with GOSUMDB=off and --fail-on critical\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "sumdb-off") {
		t.Errorf("stdout should mention sumdb-off:\n%s", stdout)
	}
}

func TestRun_NoFailOnNeverFails(t *testing.T) {
	dir := buildTestModule(t)
	t.Setenv("GOSUMDB", "off")

	_, _, code := runCLI(t, []string{"--dir", dir})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (no --fail-on given, must never fail the build by default)", code)
	}
}

func TestRun_ExpectMismatchFailsWithFailOn(t *testing.T) {
	dir := buildTestModule(t)
	expectPath := filepath.Join(t.TempDir(), "expected.json")
	mustWrite(t, expectPath, `{"GOSUMDB": "sum.golang.org"}`)
	t.Setenv("GOSUMDB", "off")

	_, _, code := runCLI(t, []string{"--dir", dir, "--expect", expectPath, "--fail-on", "expect-mismatch"})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func TestRun_BadFlagExitsTwo(t *testing.T) {
	_, _, code := runCLI(t, []string{"--not-a-real-flag"})
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

func TestRun_MissingGoModExitsTwo(t *testing.T) {
	_, _, code := runCLI(t, []string{"--dir", t.TempDir()})
	if code != 2 {
		t.Fatalf("exit code = %d, want 2 (unreadable go.mod is a tool error, not a finding)", code)
	}
}
