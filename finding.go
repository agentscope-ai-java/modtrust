package modtrust

// Severity classifies how serious a Finding is.
type Severity string

const (
	// Critical means module integrity verification is disabled or
	// bypassed outright: a compromised or MITM'd source could serve
	// modified code and nothing would detect it.
	Critical Severity = "critical"
	// Warning means verification is weakened or a build can silently
	// change files it shouldn't, but a bypass isn't unconditional.
	Warning Severity = "warning"
	// Info means the setting is worth knowing about but isn't, on its
	// own, a weakening of integrity checks — including reporting the
	// toolchain's own default behavior, so the report is complete.
	Info Severity = "info"
)

// Code identifies the kind of Finding, stable across modtrust versions,
// meant for --fail-on and for scripts consuming --json output.
type Code string

const (
	CodeSumDBOff            Code = "sumdb-off"
	CodeSumDBCustom         Code = "sumdb-custom"
	CodeProxyDirectOnly     Code = "proxy-direct-only"
	CodeProxyDirectFallback Code = "proxy-direct-fallback"
	CodeProxyOff            Code = "proxy-off"
	CodePrivateWildcard     Code = "private-wildcard"
	CodePrivateBroadTLD     Code = "private-broad-tld"
	CodePrivateBare         Code = "private-bare-pattern"
	CodeInsecureSet         Code = "insecure-set"
	CodeModFlagRewrite      Code = "mod-flag-rewrite"
	CodeGONOSUMCHECKIgnored Code = "gonosumcheck-ignored"
	CodeModFlagVendor       Code = "mod-flag-vendor"
	CodeSumMissing          Code = "sum-missing-entry"
	CodeSumLocalReplace     Code = "sum-local-replace"
	CodeSumStale            Code = "sum-stale-entry"
	CodeVerifyFailed        Code = "verify-failed"
	CodeVerifyPassed        Code = "verify-passed"
	CodeVerifySkipped       Code = "verify-skipped"
	CodeExpectMismatch      Code = "expect-mismatch"
)

// Finding is one thing modtrust noticed about the build environment or
// module files, worth a human's attention or a CI gate.
type Finding struct {
	Code     Code     `json:"code"`
	Severity Severity `json:"severity"`
	// Var is the `go env` variable this finding is about, when
	// applicable ("" otherwise, e.g. for go.sum findings).
	Var string `json:"var,omitempty"`
	// Value is the effective value that triggered the finding.
	Value string `json:"value,omitempty"`
	// Source is where that value came from: environment, config-file,
	// inherited-from-GOPRIVATE, or default.
	Source string `json:"source,omitempty"`
	// Message is a one-line, human-readable statement of what was found.
	Message string `json:"message"`
	// Consequence explains what a build actually loses because of this,
	// not just that the setting is present.
	Consequence string `json:"consequence,omitempty"`
}
