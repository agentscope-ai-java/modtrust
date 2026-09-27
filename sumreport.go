package modtrust

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/agentscope-ai-java/modtrust/internal/modsum"
)

// SumEntry is the reportable form of one checked requirement's go.sum
// coverage.
type SumEntry struct {
	Required  string `json:"required"`
	Effective string `json:"effective"`
	Replaced  bool   `json:"replaced"`
	Status    string `json:"status"`
}

// SumReport is the go.sum-completeness result for one module directory.
type SumReport struct {
	Dir      string     `json:"dir"`
	Entries  []SumEntry `json:"entries"`
	Stale    []string   `json:"stale,omitempty"`
	Complete bool       `json:"complete"`
}

// CheckSum reads go.mod and go.sum from dir and reports whether every
// module go.mod requires (other than ones replaced to a local filesystem
// path) has the checksum entries go.sum should carry for it.
func CheckSum(dir string) (SumReport, error) {
	modData, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return SumReport{}, fmt.Errorf("modtrust: reading go.mod: %w", err)
	}
	mf, err := modsum.ParseModFile(string(modData))
	if err != nil {
		return SumReport{}, fmt.Errorf("modtrust: parsing go.mod: %w", err)
	}

	var sumEntries map[modsum.Module]*modsum.SumEntry
	sumData, err := os.ReadFile(filepath.Join(dir, "go.sum"))
	switch {
	case err == nil:
		sumEntries, err = modsum.ParseSumFile(string(sumData))
		if err != nil {
			return SumReport{}, fmt.Errorf("modtrust: parsing go.sum: %w", err)
		}
	case os.IsNotExist(err):
		sumEntries = map[modsum.Module]*modsum.SumEntry{}
	default:
		return SumReport{}, fmt.Errorf("modtrust: reading go.sum: %w", err)
	}

	raw := modsum.CheckCompleteness(mf, sumEntries)

	rep := SumReport{Dir: dir, Complete: raw.Complete()}
	for _, c := range raw.Checked {
		rep.Entries = append(rep.Entries, SumEntry{
			Required:  c.Required.String(),
			Effective: c.Effective.String(),
			Replaced:  c.Replaced,
			Status:    string(c.Status),
		})
	}
	for _, m := range raw.Stale {
		rep.Stale = append(rep.Stale, m.String())
	}
	return rep, nil
}

// Findings turns a SumReport into modtrust Findings: one per module
// missing coverage, one per local replace (informational), and one for
// stale entries as a whole.
func (r SumReport) Findings() []Finding {
	var findings []Finding
	for _, e := range r.Entries {
		switch e.Status {
		case string(sumStatusOK):
			continue
		case string(sumStatusLocalReplace):
			findings = append(findings, Finding{
				Code: CodeSumLocalReplace, Severity: Info,
				Message: fmt.Sprintf("%s is replaced with a local filesystem path (effective: %s); it correctly has no go.sum entry.", e.Required, e.Effective),
			})
		default:
			findings = append(findings, Finding{
				Code: CodeSumMissing, Severity: Critical,
				Message:     sumMissingMessage(e),
				Consequence: "A module go.mod requires can be built without its content ever being checked against go.sum, because the entry simply isn't there to check against. `go build` in the default (readonly) mode would normally refuse this and tell you to run `go mod tidy` or `go mod download` — if it isn't refusing, something (GOFLAGS=-mod=mod, or a vendored build) is bypassing that guard.",
			})
		}
	}
	if len(r.Stale) > 0 {
		findings = append(findings, Finding{
			Code: CodeSumStale, Severity: Info,
			Message:     fmt.Sprintf("go.sum has %d entr(y/ies) for modules go.mod no longer requires.", len(r.Stale)),
			Consequence: "Harmless to a build, but usually means go.sum hasn't been through `go mod tidy` recently.",
		})
	}
	return findings
}

func sumMissingMessage(e SumEntry) string {
	switch e.Status {
	case string(sumStatusMissingModuleHash):
		return fmt.Sprintf("%s is missing its go.sum content-hash line.", e.Effective)
	case string(sumStatusMissingGoModHash):
		return fmt.Sprintf("%s is missing its go.sum go.mod-hash line.", e.Effective)
	default:
		return fmt.Sprintf("%s has no go.sum entry at all.", e.Effective)
	}
}

// Local aliases so this file doesn't need to import modsum's Status type
// name into every call site above.
const (
	sumStatusOK                = "ok"
	sumStatusLocalReplace      = "local-replace"
	sumStatusMissingModuleHash = "missing-module-hash"
	sumStatusMissingGoModHash  = "missing-go-mod-hash"
)
