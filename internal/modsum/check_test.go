package modsum

import "testing"

func statusOf(t *testing.T, report Report, path string) Status {
	t.Helper()
	for _, c := range report.Checked {
		if c.Required.Path == path {
			return c.Status
		}
	}
	t.Fatalf("no checked entry for %s", path)
	return ""
}

func TestCheckCompleteness_AllPresent(t *testing.T) {
	mf := &ModFile{
		Requires: []Requirement{
			{Module: Module{Path: "example.com/a", Version: "v1.0.0"}},
		},
	}
	sum := map[Module]*SumEntry{
		{Path: "example.com/a", Version: "v1.0.0"}: {HasModuleHash: true, HasGoModHash: true},
	}
	report := CheckCompleteness(mf, sum)
	if !report.Complete() {
		t.Fatalf("report should be complete: %+v", report)
	}
	if statusOf(t, report, "example.com/a") != StatusOK {
		t.Fatalf("status = %v, want OK", statusOf(t, report, "example.com/a"))
	}
}

func TestCheckCompleteness_MissingEntry(t *testing.T) {
	mf := &ModFile{
		Requires: []Requirement{
			{Module: Module{Path: "example.com/a", Version: "v1.0.0"}},
		},
	}
	report := CheckCompleteness(mf, nil)
	if report.Complete() {
		t.Fatalf("report should be incomplete")
	}
	if got := statusOf(t, report, "example.com/a"); got != StatusMissingBoth {
		t.Fatalf("status = %v, want %v", got, StatusMissingBoth)
	}
}

func TestCheckCompleteness_MissingOnlyGoModHash(t *testing.T) {
	mf := &ModFile{
		Requires: []Requirement{
			{Module: Module{Path: "example.com/a", Version: "v1.0.0"}},
		},
	}
	sum := map[Module]*SumEntry{
		{Path: "example.com/a", Version: "v1.0.0"}: {HasModuleHash: true, HasGoModHash: false},
	}
	report := CheckCompleteness(mf, sum)
	if report.Complete() {
		t.Fatalf("report should be incomplete")
	}
	if got := statusOf(t, report, "example.com/a"); got != StatusMissingGoModHash {
		t.Fatalf("status = %v, want %v", got, StatusMissingGoModHash)
	}
}

func TestCheckCompleteness_LocalReplaceNeedsNoChecksum(t *testing.T) {
	mf := &ModFile{
		Requires: []Requirement{
			{Module: Module{Path: "example.com/a", Version: "v0.0.0-00010101000000-000000000000"}},
		},
		Replaces: []Replace{
			{Old: Module{Path: "example.com/a"}, New: Module{Path: "../local/a"}, Local: true},
		},
	}
	report := CheckCompleteness(mf, nil) // no go.sum at all
	if !report.Complete() {
		t.Fatalf("report should be complete: a locally-replaced module needs no checksum: %+v", report)
	}
	if got := statusOf(t, report, "example.com/a"); got != StatusLocalReplace {
		t.Fatalf("status = %v, want %v", got, StatusLocalReplace)
	}
}

func TestCheckCompleteness_VersionedReplaceChecksTheReplacement(t *testing.T) {
	mf := &ModFile{
		Requires: []Requirement{
			{Module: Module{Path: "example.com/a", Version: "v1.0.0"}},
		},
		Replaces: []Replace{
			{
				Old: Module{Path: "example.com/a", Version: "v1.0.0"},
				New: Module{Path: "example.com/a-fork", Version: "v1.0.0-patched"},
			},
		},
	}
	// go.sum has an entry for the *replacement*, not the original: this
	// must count as complete, and checking the original path must not.
	sum := map[Module]*SumEntry{
		{Path: "example.com/a-fork", Version: "v1.0.0-patched"}: {HasModuleHash: true, HasGoModHash: true},
	}
	report := CheckCompleteness(mf, sum)
	if !report.Complete() {
		t.Fatalf("report should be complete once the replacement target has an entry: %+v", report)
	}
	c := report.Checked[0]
	if c.Effective != (Module{Path: "example.com/a-fork", Version: "v1.0.0-patched"}) {
		t.Fatalf("Effective = %+v, want the replacement module", c.Effective)
	}
	if !c.Replaced {
		t.Fatalf("Replaced = false, want true")
	}
}

func TestCheckCompleteness_VersionSpecificReplaceTakesPrecedenceOverPathOnly(t *testing.T) {
	mf := &ModFile{
		Requires: []Requirement{
			{Module: Module{Path: "example.com/a", Version: "v1.0.0"}},
		},
		Replaces: []Replace{
			// Path-only replace: would apply to every version...
			{Old: Module{Path: "example.com/a"}, New: Module{Path: "example.com/a-generic", Version: "v9.9.9"}},
			// ...but a version-specific replace for v1.0.0 must win.
			{Old: Module{Path: "example.com/a", Version: "v1.0.0"}, New: Module{Path: "example.com/a-specific", Version: "v1.0.0-x"}},
		},
	}
	sum := map[Module]*SumEntry{
		{Path: "example.com/a-specific", Version: "v1.0.0-x"}: {HasModuleHash: true, HasGoModHash: true},
	}
	report := CheckCompleteness(mf, sum)
	if report.Checked[0].Effective.Path != "example.com/a-specific" {
		t.Fatalf("Effective = %+v, want the version-specific replacement", report.Checked[0].Effective)
	}
}

func TestCheckCompleteness_StaleEntries(t *testing.T) {
	mf := &ModFile{
		Requires: []Requirement{
			{Module: Module{Path: "example.com/a", Version: "v1.0.0"}},
		},
	}
	sum := map[Module]*SumEntry{
		{Path: "example.com/a", Version: "v1.0.0"}:   {HasModuleHash: true, HasGoModHash: true},
		{Path: "example.com/old", Version: "v0.1.0"}: {HasModuleHash: true, HasGoModHash: true},
	}
	report := CheckCompleteness(mf, sum)
	if len(report.Stale) != 1 || report.Stale[0].Path != "example.com/old" {
		t.Fatalf("Stale = %+v, want exactly example.com/old", report.Stale)
	}
	if !report.Complete() {
		t.Fatalf("a stale entry alone must not make the report incomplete")
	}
}
