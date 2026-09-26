package modtrust

import (
	"context"
	"testing"
)

func TestRun_EndToEnd(t *testing.T) {
	dir := buildRealModule(t)

	report, err := Run(context.Background(), Options{Dir: dir, Verify: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Sum == nil {
		t.Fatalf("expected a Sum report")
	}
	if !report.Sum.Complete {
		t.Errorf("expected the freshly-tidied module's go.sum to be complete: %+v", report.Sum)
	}
	if report.Verify == nil || !report.Verify.Passed {
		t.Errorf("expected go mod verify to pass: %+v", report.Verify)
	}
	if len(report.Config.Vars) == 0 {
		t.Errorf("expected the config section to be populated")
	}
}

func TestRun_SkipsSumWhenDirEmpty(t *testing.T) {
	report, err := Run(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Sum != nil {
		t.Errorf("expected no Sum report when Dir is empty, got %+v", report.Sum)
	}
	if report.Verify != nil {
		t.Errorf("expected no Verify result when Dir is empty, got %+v", report.Verify)
	}
}

func TestReport_FailOn(t *testing.T) {
	report := Report{Findings: []Finding{
		{Code: CodeSumDBOff, Severity: Critical},
		{Code: CodeModFlagRewrite, Severity: Warning},
		{Code: CodeProxyDirectFallback, Severity: Info},
	}}

	cases := []struct {
		tokens []string
		want   bool
	}{
		{nil, false},
		{[]string{""}, false},
		{[]string{"critical"}, true},
		{[]string{"warning"}, true},
		{[]string{"info"}, true},
		{[]string{"sumdb-off"}, true},
		{[]string{"mod-flag-vendor"}, false},
		{[]string{"CRITICAL"}, true}, // case-insensitive
	}
	for _, c := range cases {
		if got := report.FailOn(c.tokens); got != c.want {
			t.Errorf("FailOn(%v) = %v, want %v", c.tokens, got, c.want)
		}
	}
}
