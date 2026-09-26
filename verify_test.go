package modtrust

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestVerifyModuleCache_Passes(t *testing.T) {
	dir := buildRealModule(t)

	res, err := VerifyModuleCache(context.Background(), dir)
	if err != nil {
		t.Fatalf("VerifyModuleCache: %v", err)
	}
	if !res.Ran || !res.Passed {
		t.Fatalf("expected go mod verify to pass on a freshly-downloaded cache: %+v", res)
	}
	if res.Command != "go mod verify" {
		t.Errorf("Command = %q", res.Command)
	}

	findings := res.Findings()
	if len(findings) != 1 || findings[0].Code != CodeVerifyPassed {
		t.Errorf("Findings = %+v, want one verify-passed finding", findings)
	}
}

func TestVerifyModuleCache_DetectsTamperedCache(t *testing.T) {
	dir := buildRealModule(t)

	// Find the downloaded rsc.io/quote source in the module cache and
	// corrupt one of its files, then confirm `go mod verify` (the real
	// binary, not a mock) reports the mismatch and modtrust surfaces it.
	out, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Fatalf("go env GOMODCACHE: %v", err)
	}
	cacheDir := trimNewline(string(out))

	pkgDir, findErr := findQuoteCacheDir(cacheDir)
	if findErr != nil {
		t.Skipf("could not locate rsc.io/quote in the module cache to tamper with: %v", findErr)
	}

	// Module cache directories are read-only by default; make this one
	// writable just long enough to tamper with a file, matching what `go
	// mod verify` itself expects to detect.
	if err := os.Chmod(pkgDir, 0o755); err != nil {
		t.Fatalf("chmod cache dir: %v", err)
	}
	t.Cleanup(func() { os.Chmod(pkgDir, 0o555) })

	target := filepath.Join(pkgDir, "quote.go")
	original, err := os.ReadFile(target)
	if err != nil {
		t.Skipf("could not read cached source to tamper with: %v", err)
	}
	if err := os.Chmod(target, 0o644); err != nil {
		t.Fatalf("chmod cached file: %v", err)
	}
	if err := os.WriteFile(target, append(original, []byte("\n// tampered by modtrust's own test\n")...), 0o644); err != nil {
		t.Fatalf("tamper with cached file: %v", err)
	}
	t.Cleanup(func() {
		os.Chmod(target, 0o644)
		os.WriteFile(target, original, 0o644)
	})

	res, err := VerifyModuleCache(context.Background(), dir)
	if err != nil {
		t.Fatalf("VerifyModuleCache: %v", err)
	}
	if res.Passed {
		t.Fatalf("expected go mod verify to fail against a tampered cache entry, output: %s", res.Output)
	}

	findings := res.Findings()
	if len(findings) != 1 || findings[0].Code != CodeVerifyFailed || findings[0].Severity != Critical {
		t.Errorf("Findings = %+v, want one critical verify-failed finding", findings)
	}
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

// findQuoteCacheDir locates the extracted rsc.io/quote@v1.5.2 source
// directory under the module cache.
func findQuoteCacheDir(cacheDir string) (string, error) {
	base := filepath.Join(cacheDir, "rsc.io", "quote@v1.5.2")
	if _, err := os.Stat(filepath.Join(base, "quote.go")); err != nil {
		return "", err
	}
	return base, nil
}
