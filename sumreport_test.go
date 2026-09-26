package modtrust

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildRealModule creates a real module on disk with a real dependency
// and a real local replace, runs the real `go mod tidy` to produce a
// genuine go.sum, then returns the module's directory. Tests use this
// rather than hand-writing go.mod/go.sum so the completeness check is
// exercised against exactly what the go tool itself produces.
func buildRealModule(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	localDir := filepath.Join(root, "localdep")
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(localDir, "go.mod"), "module example.com/localdep\n\ngo 1.21\n")
	writeFile(t, filepath.Join(localDir, "lib.go"), "package localdep\n\nfunc Hello() string { return \"hi\" }\n")

	mainDir := filepath.Join(root, "mainmod")
	if err := os.MkdirAll(mainDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(mainDir, "go.mod"), `module example.com/mainmod

go 1.21

require rsc.io/quote v1.5.2

replace example.com/localdep => ../localdep
`)
	writeFile(t, filepath.Join(mainDir, "main.go"), `package main

import (
	"fmt"

	_ "example.com/localdep"
	"rsc.io/quote"
)

func main() { fmt.Println(quote.Hello()) }
`)

	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = mainDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("skipping: `go mod tidy` needs network access to build the test fixture, and it failed: %v\n%s", err, out)
	}

	return mainDir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckSum_RealModuleIsComplete(t *testing.T) {
	dir := buildRealModule(t)

	report, err := CheckSum(dir)
	if err != nil {
		t.Fatalf("CheckSum: %v", err)
	}
	if !report.Complete {
		t.Fatalf("report should be complete: %+v", report)
	}

	var sawLocalReplace, sawOK bool
	for _, e := range report.Entries {
		if e.Status == "local-replace" {
			sawLocalReplace = true
			if e.Effective != "../localdep" {
				t.Errorf("local-replace entry should report the local path as Effective, got %+v", e)
			}
		}
		if e.Status == "ok" {
			sawOK = true
		}
	}
	if !sawLocalReplace {
		t.Errorf("expected example.com/localdep to be reported as local-replace: %+v", report.Entries)
	}
	if !sawOK {
		t.Errorf("expected at least one ok entry (rsc.io/quote and friends): %+v", report.Entries)
	}

	findings := report.Findings()
	for _, f := range findings {
		if f.Code == CodeSumMissing {
			t.Errorf("unexpected missing-entry finding on a freshly-tidied module: %+v", f)
		}
	}
}

func TestCheckSum_DetectsRemovedEntry(t *testing.T) {
	dir := buildRealModule(t)

	sumPath := filepath.Join(dir, "go.sum")
	data, err := os.ReadFile(sumPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	var trimmed []string
	removed := false
	for _, l := range lines {
		if !removed && strings.Contains(l, "rsc.io/quote") && strings.Contains(l, "/go.mod") {
			removed = true
			continue
		}
		trimmed = append(trimmed, l)
	}
	if !removed {
		t.Fatalf("fixture go.sum didn't contain the expected rsc.io/quote go.mod line: %s", data)
	}
	if err := os.WriteFile(sumPath, []byte(strings.Join(trimmed, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := CheckSum(dir)
	if err != nil {
		t.Fatalf("CheckSum: %v", err)
	}
	if report.Complete {
		t.Fatalf("report should be incomplete after removing a go.sum line: %+v", report)
	}

	var found bool
	for _, e := range report.Entries {
		if e.Effective == "rsc.io/quote@v1.5.2" && e.Status == "missing-go-mod-hash" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected rsc.io/quote@v1.5.2 to be missing-go-mod-hash: %+v", report.Entries)
	}

	findings := report.Findings()
	foundCritical := false
	for _, f := range findings {
		if f.Code == CodeSumMissing && f.Severity == Critical {
			foundCritical = true
		}
	}
	if !foundCritical {
		t.Errorf("expected a critical sum-missing-entry finding, got %+v", findings)
	}
}

func TestCheckSum_MissingGoMod(t *testing.T) {
	dir := t.TempDir()
	_, err := CheckSum(dir)
	if err == nil {
		t.Fatalf("expected an error when go.mod is absent")
	}
}
