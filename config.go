package modtrust

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/__GH_OWNER__/modtrust/internal/govars"
)

// EnvVar is the reportable form of one `go env` variable: its effective
// value and where that value came from.
type EnvVar struct {
	Name       string `json:"name"`
	Value      string `json:"value"`
	Source     string `json:"source"`
	ConfigPath string `json:"configPath,omitempty"`
}

// Config is the effective module-integrity-relevant `go env` configuration
// for this environment.
type Config struct {
	Vars []EnvVar `json:"vars"`

	// GONOSUMCHECKSet and GONOSUMCHECKValue report GONOSUMCHECK exactly as
	// found in the process environment; see govars.Snapshot for why this
	// is tracked separately from the `go env` variables.
	GONOSUMCHECKSet   bool   `json:"gonosumcheckSet"`
	GONOSUMCHECKValue string `json:"gonosumcheckValue,omitempty"`

	snap *govars.Snapshot
}

// Get returns the effective value and source for a variable name, or ""
// and SourceDefault-equivalent zero value if the name isn't tracked.
func (c Config) Get(name string) EnvVar {
	for _, v := range c.Vars {
		if v.Name == name {
			return v
		}
	}
	return EnvVar{Name: name}
}

// LoadConfig reads the effective go env configuration from the real `go`
// binary on PATH.
func LoadConfig(ctx context.Context) (Config, error) {
	snap, err := govars.Read(ctx)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		snap:              snap,
		GONOSUMCHECKSet:   snap.GONOSUMCHECKSet,
		GONOSUMCHECKValue: snap.GONOSUMCHECKValue,
	}
	for _, name := range govars.Vars {
		v := snap.Vars[name]
		cfg.Vars = append(cfg.Vars, EnvVar{
			Name:       v.Name,
			Value:      v.Value,
			Source:     string(v.Source),
			ConfigPath: v.ConfigPath,
		})
	}
	return cfg, nil
}

// knownPublicTLDs are single-label suffixes common enough on public
// module-hosting domains that a GOPRIVATE/GONOSUMDB/GONOPROXY wildcard
// pattern ending in one of them is almost certainly broader than
// intended.
var knownPublicTLDs = map[string]bool{
	"com": true, "org": true, "net": true, "io": true, "dev": true,
	"app": true, "co": true, "sh": true, "ai": true, "xyz": true,
	"info": true, "in": true, "to": true, "gg": true,
}

var bareTLDWildcard = regexp.MustCompile(`^\*\.[A-Za-z0-9-]+$`)

// Analyze inspects the effective configuration and returns every notable
// finding, dangerous or merely informational. It never talks to the
// network and never shells out; it works purely from cfg.
func Analyze(cfg Config) []Finding {
	var findings []Finding

	sumdb := cfg.Get("GOSUMDB")
	switch {
	case sumdb.Value == "off":
		findings = append(findings, Finding{
			Code: CodeSumDBOff, Severity: Critical,
			Var: "GOSUMDB", Value: sumdb.Value, Source: sumdb.Source,
			Message: "GOSUMDB=off: checksum database verification is disabled entirely.",
			Consequence: "Every downloaded module's hash is trusted on first sight, from the proxy " +
				"or from source control, with no independent record to compare against. A " +
				"compromised proxy, a MITM'd connection, or a same-version repack on the origin " +
				"host would serve modified code and nothing would detect it.",
		})
	case sumdb.Value != "" && !strings.HasPrefix(sumdb.Value, "sum.golang.org"):
		findings = append(findings, Finding{
			Code: CodeSumDBCustom, Severity: Info,
			Var: "GOSUMDB", Value: sumdb.Value, Source: sumdb.Source,
			Message: fmt.Sprintf("GOSUMDB is set to a non-default checksum database (%s).", sumdb.Value),
			Consequence: "Checksum verification is still active, but it trusts a different operator " +
				"than the public sum.golang.org. Confirm this is the intended, trusted database.",
		})
	}

	findings = append(findings, analyzeProxy(cfg)...)
	findings = append(findings, analyzePatternVar(cfg, "GOPRIVATE")...)
	findings = append(findings, analyzePatternVar(cfg, "GONOSUMDB")...)
	findings = append(findings, analyzeGONOPROXY(cfg)...)

	insecure := cfg.Get("GOINSECURE")
	if insecure.Value != "" {
		findings = append(findings, Finding{
			Code: CodeInsecureSet, Severity: Warning,
			Var: "GOINSECURE", Value: insecure.Value, Source: insecure.Source,
			Message: fmt.Sprintf("GOINSECURE is set (%s): matching modules may be fetched over an insecure transport.", insecure.Value),
			Consequence: "GOINSECURE permits plain HTTP, or TLS with an unverified certificate, for the " +
				"matching module path prefixes — a plaintext or unauthenticated connection an on-path " +
				"attacker could tamper with. It does NOT disable checksum database validation on its " +
				"own; GOSUMDB still checks the result unless GOPRIVATE or GONOSUMDB also covers the " +
				"same modules. Confirm the pattern is as narrow as intended.",
		})
	}

	findings = append(findings, analyzeGOFLAGS(cfg)...)

	if cfg.GONOSUMCHECKSet {
		findings = append(findings, Finding{
			Code: CodeGONOSUMCHECKIgnored, Severity: Info,
			Var: "GONOSUMCHECK", Value: cfg.GONOSUMCHECKValue, Source: "environment",
			Message: fmt.Sprintf("GONOSUMCHECK=%s is set in the environment but the Go toolchain does not recognize it.", cfg.GONOSUMCHECKValue),
			Consequence: "GONOSUMCHECK was a pre-modules (GOPATH-era) variable; module-aware Go (1.13+) " +
				"never reads it — `go env -w GONOSUMCHECK=...` is rejected outright, and `go env " +
				"GONOSUMCHECK` reports empty regardless of what's in the real environment. Setting it " +
				"has zero effect on checksum verification either way, and nothing warns that it was " +
				"ignored. Control verification with GOSUMDB, GONOSUMDB or GOPRIVATE instead.",
		})
	}

	return findings
}

func splitProxyList(value string) []string {
	if value == "" {
		return nil
	}
	// GOPROXY entries are separated by ',' (fall back only on 404/410) or
	// '|' (fall back on any error); either separator can appear.
	repl := strings.NewReplacer("|", ",")
	parts := strings.Split(repl.Replace(value), ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func analyzeProxy(cfg Config) []Finding {
	proxy := cfg.Get("GOPROXY")
	entries := splitProxyList(proxy.Value)
	if len(entries) == 0 {
		return nil
	}

	if len(entries) == 1 && entries[0] == "direct" {
		return []Finding{{
			Code: CodeProxyDirectOnly, Severity: Critical,
			Var: "GOPROXY", Value: proxy.Value, Source: proxy.Source,
			Message: "GOPROXY=direct: no module proxy is used at all.",
			Consequence: "Every module is fetched straight from its version control host on every " +
				"build, with no proxy caching, no availability guarantee if the origin disappears " +
				"or rewrites history, and no proxy-side immutability. GOSUMDB, if still on, catches " +
				"a tampered result but only after the fact and only if it's also enabled.",
		}}
	}

	for i, e := range entries {
		if e != "direct" {
			continue
		}
		if i == 0 {
			return []Finding{{
				Code: CodeProxyDirectFallback, Severity: Warning,
				Var: "GOPROXY", Value: proxy.Value, Source: proxy.Source,
				Message: "GOPROXY lists 'direct' as the first entry: the module proxy is never tried first.",
				Consequence: "Modules are fetched straight from version control on the normal path, " +
					"with the proxy (if any follows) only used as a fallback. This forfeits the " +
					"proxy's caching and availability benefits for every build, not just outages.",
			}}
		}
		return []Finding{{
			Code: CodeProxyDirectFallback, Severity: Info,
			Var: "GOPROXY", Value: proxy.Value, Source: proxy.Source,
			Message: "GOPROXY includes 'direct' as a fallback (this is the toolchain default).",
			Consequence: "If the configured proxy is unreachable, or returns 404/410 for a module, " +
				"the go command silently falls back to fetching directly from the module's version " +
				"control host — no proxy caching or availability guarantee applies to that fetch. " +
				"GOSUMDB, if enabled, still verifies the result against the checksum database, so " +
				"this alone doesn't disable integrity checking, but it does mean a proxy failure " +
				"changes where code comes from without anyone being told.",
		}}
	}

	for _, e := range entries {
		if e == "off" {
			return []Finding{{
				Code: CodeProxyOff, Severity: Info,
				Var: "GOPROXY", Value: proxy.Value, Source: proxy.Source,
				Message: "GOPROXY includes 'off': module downloads beyond the local cache are disabled.",
				Consequence: "Builds requiring an uncached module will fail closed rather than fetch " +
					"it from anywhere. Not an integrity weakening — the opposite, if unintended it's " +
					"a build-availability problem, not a security one.",
			}}
		}
	}
	return nil
}

func analyzeGONOPROXY(cfg Config) []Finding {
	v := cfg.Get("GONOPROXY")
	if v.Value == "" {
		return nil
	}
	var findings []Finding
	for _, pattern := range strings.Split(v.Value, ",") {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if sev, broad := classifyPattern(pattern); broad {
			findings = append(findings, Finding{
				Code: patternCode(pattern), Severity: downgrade(sev),
				Var: "GONOPROXY", Value: v.Value, Source: v.Source,
				Message: fmt.Sprintf("GONOPROXY contains the pattern %q, which matches module paths well beyond a private/internal set.", pattern),
				Consequence: "GONOPROXY only changes which modules skip the proxy and fetch directly " +
					"from version control; it does not disable checksum verification on its own. " +
					"GOSUMDB (or GONOSUMDB/GOPRIVATE) still checks these modules unless one of those " +
					"is also broadened. The risk here is losing the proxy's caching/availability " +
					"guarantee for a much wider set of modules than intended.",
			})
		}
	}
	return findings
}

// downgrade lowers GONOPROXY-only findings one severity step relative to
// the same pattern on GOPRIVATE/GONOSUMDB, since GONOPROXY alone doesn't
// touch checksum verification.
func downgrade(s Severity) Severity {
	if s == Critical {
		return Warning
	}
	return s
}

func analyzePatternVar(cfg Config, name string) []Finding {
	v := cfg.Get(name)
	if v.Value == "" {
		return nil
	}
	var findings []Finding
	for _, pattern := range strings.Split(v.Value, ",") {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if sev, broad := classifyPattern(pattern); broad {
			var consequence string
			if name == "GOPRIVATE" {
				consequence = "GOPRIVATE sets the default for both GONOPROXY and GONOSUMDB. A pattern " +
					"this broad means matching modules skip both the proxy and the checksum " +
					"database — including public modules that happen to match. A compromised " +
					"proxy or on-path attacker could serve modified code for any of them and " +
					"nothing would detect it."
			} else {
				consequence = "Modules matching this pattern skip checksum database verification " +
					"entirely. If the pattern reaches public modules, a compromised source or " +
					"MITM'd fetch for any of them would go undetected."
			}
			findings = append(findings, Finding{
				Code: patternCode(pattern), Severity: sev,
				Var: name, Value: v.Value, Source: v.Source,
				Message:     fmt.Sprintf("%s contains the pattern %q, which matches module paths well beyond a private/internal set.", name, pattern),
				Consequence: consequence,
			})
		}
	}
	return findings
}

func patternCode(pattern string) Code {
	switch {
	case pattern == "*":
		return CodePrivateWildcard
	case bareTLDWildcard.MatchString(pattern):
		return CodePrivateBroadTLD
	default:
		return CodePrivateBare
	}
}

// classifyPattern reports whether a GOPRIVATE/GONOSUMDB/GONOPROXY glob
// pattern is broad enough to plausibly cover public modules, and how
// severe that is.
func classifyPattern(pattern string) (Severity, bool) {
	if pattern == "*" {
		return Critical, true
	}
	if bareTLDWildcard.MatchString(pattern) {
		label := strings.TrimPrefix(pattern, "*.")
		if knownPublicTLDs[strings.ToLower(label)] {
			return Critical, true
		}
		return Warning, true
	}
	if !strings.ContainsAny(pattern, "./*") {
		// A bare single label with no dot, no slash, no wildcard: only
		// matches a module whose first path element is exactly this
		// word, which is unusual for public code (real hosts are
		// "github.com", not "com"), but still worth a second look,
		// especially if it happens to spell a public TLD.
		if knownPublicTLDs[strings.ToLower(pattern)] {
			return Warning, true
		}
		return Info, false
	}
	return Info, false
}

func analyzeGOFLAGS(cfg Config) []Finding {
	v := cfg.Get("GOFLAGS")
	if v.Value == "" {
		return nil
	}
	var findings []Finding
	for _, tok := range strings.Fields(v.Value) {
		switch {
		case tok == "-mod=mod":
			findings = append(findings, Finding{
				Code: CodeModFlagRewrite, Severity: Warning,
				Var: "GOFLAGS", Value: v.Value, Source: v.Source,
				Message: "GOFLAGS sets -mod=mod: build commands may rewrite go.mod and go.sum automatically.",
				Consequence: "A plain `go build` or `go test` is allowed to add requirements and " +
					"checksums to go.mod/go.sum on its own, without an explicit `go get` or `go mod " +
					"tidy` a reviewer would notice in a diff. A dependency resolving to a new " +
					"transitive module gets silently trusted and recorded rather than surfaced for " +
					"review.",
			})
		case tok == "-mod=vendor":
			findings = append(findings, Finding{
				Code: CodeModFlagVendor, Severity: Info,
				Var: "GOFLAGS", Value: v.Value, Source: v.Source,
				Message: "GOFLAGS sets -mod=vendor: builds use the committed vendor/ directory.",
				Consequence: "GOPROXY and GOSUMDB do not apply to this build at all — source comes " +
					"straight from vendor/, not the module cache. Integrity depends entirely on " +
					"whatever review vendor/ itself gets (and on `go mod vendor` having been run " +
					"faithfully from a verified go.sum in the first place).",
			})
		}
	}
	return findings
}
