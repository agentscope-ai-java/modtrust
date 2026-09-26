package modtrust

import (
	"os"
	"path/filepath"
	"testing"
)

func writeExpectFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "expected.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadExpected_Valid(t *testing.T) {
	path := writeExpectFile(t, `{"GOSUMDB": "sum.golang.org", "goproxy": "https://proxy.golang.org,direct"}`)
	exp, err := LoadExpected(path)
	if err != nil {
		t.Fatalf("LoadExpected: %v", err)
	}
	if exp["GOSUMDB"] != "sum.golang.org" {
		t.Errorf("GOSUMDB = %q", exp["GOSUMDB"])
	}
	if exp["GOPROXY"] != "https://proxy.golang.org,direct" {
		t.Errorf("GOPROXY = %q (lowercase key should be normalized to uppercase)", exp["GOPROXY"])
	}
}

func TestLoadExpected_UnknownKeyRejected(t *testing.T) {
	path := writeExpectFile(t, `{"GOSUMDB": "sum.golang.org", "NOTAREALVAR": "x"}`)
	_, err := LoadExpected(path)
	if err == nil {
		t.Fatalf("expected an error for an unrecognized key")
	}
}

func TestCompare_MatchProducesNoFinding(t *testing.T) {
	cfg := cfgWith(map[string]string{"GOSUMDB": "sum.golang.org"})
	findings := Compare(cfg, Expected{"GOSUMDB": "sum.golang.org"})
	if len(findings) != 0 {
		t.Errorf("expected no findings for a match, got %+v", findings)
	}
}

func TestCompare_MismatchProducesCriticalFinding(t *testing.T) {
	cfg := cfgWith(map[string]string{"GOSUMDB": "off"})
	findings := Compare(cfg, Expected{"GOSUMDB": "sum.golang.org"})
	if len(findings) != 1 {
		t.Fatalf("expected exactly one finding, got %+v", findings)
	}
	f := findings[0]
	if f.Code != CodeExpectMismatch || f.Severity != Critical || f.Var != "GOSUMDB" {
		t.Errorf("finding = %+v, want a critical expect-mismatch on GOSUMDB", f)
	}
}

func TestCompare_UncheckedVarsIgnored(t *testing.T) {
	cfg := cfgWith(map[string]string{"GOSUMDB": "off", "GOPROXY": "direct"})
	// Only GOSUMDB is declared; GOPROXY=direct (which Analyze would flag)
	// must not produce a Compare finding since it isn't in Expected.
	findings := Compare(cfg, Expected{"GOSUMDB": "off"})
	if len(findings) != 0 {
		t.Errorf("expected no findings (GOSUMDB matches, GOPROXY isn't asserted), got %+v", findings)
	}
}
