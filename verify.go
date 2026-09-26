package modtrust

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
)

// VerifyResult is the outcome of running `go mod verify` against the
// module cache. modtrust runs the real command rather than reimplementing
// its hash checks.
type VerifyResult struct {
	Ran     bool   `json:"ran"`
	Passed  bool   `json:"passed"`
	Output  string `json:"output,omitempty"`
	Command string `json:"command"`
}

// VerifyModuleCache runs `go mod verify` in dir and reports its result.
// It does not reimplement the check itself: this is exactly `go mod
// verify`'s own verdict, on the module cache Go itself would build from.
func VerifyModuleCache(ctx context.Context, dir string) (VerifyResult, error) {
	cmd := exec.CommandContext(ctx, "go", "mod", "verify")
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()

	res := VerifyResult{
		Ran:     true,
		Passed:  err == nil,
		Output:  strings.TrimSpace(out.String()),
		Command: "go mod verify",
	}
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			// A non-zero exit with output is `go mod verify` reporting a
			// real mismatch, not a tool failure; surface it as a failed
			// result rather than an error.
			return res, nil
		}
		return res, err
	}
	return res, nil
}

// Findings turns a VerifyResult into a modtrust Finding, present only
// when the result is worth a line (a run, pass or fail).
func (r VerifyResult) Findings() []Finding {
	if !r.Ran {
		return nil
	}
	if r.Passed {
		return []Finding{{
			Code: CodeVerifyPassed, Severity: Info,
			Message: "`go mod verify` passed: all cached modules match their recorded go.sum hashes.",
		}}
	}
	return []Finding{{
		Code: CodeVerifyFailed, Severity: Critical,
		Message: "`go mod verify` reported a mismatch between a cached module and its recorded go.sum hash.",
		Consequence: "A module in the local module cache no longer matches the hash go.sum recorded for " +
			"it — the cache was modified after download, or corrupted, or downloaded from something " +
			"other than what produced the recorded checksum. Building against this cache trusts " +
			"content go.sum itself says shouldn't be trusted.",
	}}
}
