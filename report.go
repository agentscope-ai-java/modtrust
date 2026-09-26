// Package modtrust (continued): Run wires the individual checks
// (environment analysis, go.sum completeness, module cache verification,
// expected-configuration assertion) into one Report.
package modtrust

import (
	"context"
	"strings"
)

// Options controls which checks Run performs.
type Options struct {
	// Dir is the module directory to check go.mod/go.sum in. Empty skips
	// the go.sum completeness check.
	Dir string
	// Verify, if true, additionally runs `go mod verify` in Dir.
	Verify bool
	// Expected, if non-nil, asserts the effective configuration against
	// a declared expected configuration (see Compare).
	Expected Expected
}

// Report is the full result of one modtrust run.
type Report struct {
	Config   Config        `json:"config"`
	Findings []Finding     `json:"findings"`
	Sum      *SumReport    `json:"sum,omitempty"`
	Verify   *VerifyResult `json:"verify,omitempty"`
}

// Run performs every check Options selects and returns the combined
// Report. It shells out to the real `go` binary (for `go env` and,
// optionally, `go mod verify`) and reads go.mod/go.sum from disk; it
// makes no network calls itself.
func Run(ctx context.Context, opts Options) (Report, error) {
	cfg, err := LoadConfig(ctx)
	if err != nil {
		return Report{}, err
	}

	report := Report{Config: cfg}
	report.Findings = append(report.Findings, Analyze(cfg)...)

	if opts.Expected != nil {
		report.Findings = append(report.Findings, Compare(cfg, opts.Expected)...)
	}

	if opts.Dir != "" {
		sumRep, err := CheckSum(opts.Dir)
		if err != nil {
			return Report{}, err
		}
		report.Sum = &sumRep
		report.Findings = append(report.Findings, sumRep.Findings()...)

		if opts.Verify {
			verifyRes, err := VerifyModuleCache(ctx, opts.Dir)
			if err != nil {
				return Report{}, err
			}
			report.Verify = &verifyRes
			report.Findings = append(report.Findings, verifyRes.Findings()...)
		}
	}

	return report, nil
}

// FailOn reports whether any Finding matches one of the given tokens.
// Each token is compared case-insensitively against both the Finding's
// Code and its Severity, so callers can gate on precise codes ("sumdb-off")
// or whole severities ("critical").
func (r Report) FailOn(tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	set := make(map[string]bool, len(tokens))
	for _, t := range tokens {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" {
			set[t] = true
		}
	}
	for _, f := range r.Findings {
		if set[strings.ToLower(string(f.Code))] || set[strings.ToLower(string(f.Severity))] {
			return true
		}
	}
	return false
}
