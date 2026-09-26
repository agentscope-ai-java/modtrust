package modsum

// Status classifies one required module's go.sum coverage.
type Status string

const (
	// StatusOK means both the content hash and the go.mod hash are
	// present in go.sum for the effective (post-replace) module.
	StatusOK Status = "ok"
	// StatusLocalReplace means the module is replaced with a local
	// filesystem path. It is read straight off disk, never fetched, and
	// correctly has no go.sum entry at all.
	StatusLocalReplace Status = "local-replace"
	// StatusMissingModuleHash means the content hash line is absent.
	StatusMissingModuleHash Status = "missing-module-hash"
	// StatusMissingGoModHash means the "/go.mod" hash line is absent.
	StatusMissingGoModHash Status = "missing-go-mod-hash"
	// StatusMissingBoth means neither go.sum line is present at all.
	StatusMissingBoth Status = "missing-both"
)

// CheckedModule is the go.sum coverage verdict for one require'd module,
// after applying any replace directive that affects it.
type CheckedModule struct {
	// Required is the module exactly as it appears in go.mod's require
	// block.
	Required Module
	// Effective is the module actually fetched and built, after applying
	// a replace directive. It equals Required when no replace applies.
	Effective Module
	Replaced  bool
	Status    Status
}

// Report is the result of comparing go.mod's requirements against
// go.sum's entries.
type Report struct {
	Checked []CheckedModule
	// Stale lists modules present in go.sum but not required by go.mod at
	// all (after replacement), which is harmless but often means go.sum
	// hasn't been run through `go mod tidy` recently.
	Stale []Module
}

// Complete reports whether every module that should carry a checksum has
// one. Modules replaced to a local filesystem path do not count against
// completeness.
func (r Report) Complete() bool {
	for _, c := range r.Checked {
		if c.Status != StatusOK && c.Status != StatusLocalReplace {
			return false
		}
	}
	return true
}

// CheckCompleteness cross-references a parsed go.mod against a parsed
// go.sum. sum may be nil or empty (a missing or empty go.sum), in which
// case every non-locally-replaced requirement is reported missing.
func CheckCompleteness(mf *ModFile, sum map[Module]*SumEntry) Report {
	if sum == nil {
		sum = map[Module]*SumEntry{}
	}

	var report Report
	seen := make(map[Module]bool, len(mf.Requires))

	for _, req := range mf.Requires {
		effective, replaced, local := resolveReplace(mf, req.Module)
		cm := CheckedModule{
			Required:  req.Module,
			Effective: effective,
			Replaced:  replaced,
		}
		if local {
			cm.Status = StatusLocalReplace
		} else {
			seen[effective] = true
			entry := sum[effective]
			switch {
			case entry == nil:
				cm.Status = StatusMissingBoth
			case entry.HasModuleHash && entry.HasGoModHash:
				cm.Status = StatusOK
			case !entry.HasModuleHash && !entry.HasGoModHash:
				cm.Status = StatusMissingBoth
			case !entry.HasModuleHash:
				cm.Status = StatusMissingModuleHash
			default:
				cm.Status = StatusMissingGoModHash
			}
		}
		report.Checked = append(report.Checked, cm)
	}

	for mod := range sum {
		if !seen[mod] {
			report.Stale = append(report.Stale, mod)
		}
	}

	return report
}

// resolveReplace applies the go.mod replace directives that match req, if
// any. A version-specific replace (old path + old version) takes
// precedence over a path-only replace (which applies to every requested
// version of that path), matching the go tool's own precedence rule.
func resolveReplace(mf *ModFile, req Module) (effective Module, replaced bool, local bool) {
	var pathOnly, versioned *Replace
	for i := range mf.Replaces {
		r := &mf.Replaces[i]
		if r.Old.Path != req.Path {
			continue
		}
		if r.Old.Version == "" {
			pathOnly = r
		} else if r.Old.Version == req.Version {
			versioned = r
		}
	}
	r := versioned
	if r == nil {
		r = pathOnly
	}
	if r == nil {
		return req, false, false
	}
	return r.New, true, r.Local
}
