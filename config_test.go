package modtrust

import "testing"

func findCode(findings []Finding, code Code) *Finding {
	for i := range findings {
		if findings[i].Code == code {
			return &findings[i]
		}
	}
	return nil
}

func cfgWith(vars map[string]string) Config {
	var c Config
	for name, value := range vars {
		c.Vars = append(c.Vars, EnvVar{Name: name, Value: value, Source: "environment"})
	}
	return c
}

func TestAnalyze_SumDBOff(t *testing.T) {
	cfg := cfgWith(map[string]string{"GOSUMDB": "off"})
	findings := Analyze(cfg)
	f := findCode(findings, CodeSumDBOff)
	if f == nil || f.Severity != Critical {
		t.Fatalf("expected a critical sumdb-off finding, got %+v", findings)
	}
}

func TestAnalyze_SumDBCustom(t *testing.T) {
	cfg := cfgWith(map[string]string{"GOSUMDB": "sum.example.com sumkey123"})
	findings := Analyze(cfg)
	f := findCode(findings, CodeSumDBCustom)
	if f == nil || f.Severity != Info {
		t.Fatalf("expected an info sumdb-custom finding, got %+v", findings)
	}
}

func TestAnalyze_SumDBDefaultProducesNoSumDBFinding(t *testing.T) {
	cfg := cfgWith(map[string]string{"GOSUMDB": "sum.golang.org"})
	findings := Analyze(cfg)
	if f := findCode(findings, CodeSumDBOff); f != nil {
		t.Errorf("unexpected sumdb-off finding: %+v", f)
	}
	if f := findCode(findings, CodeSumDBCustom); f != nil {
		t.Errorf("unexpected sumdb-custom finding for the exact default: %+v", f)
	}
}

func TestAnalyze_ProxyDirectOnly(t *testing.T) {
	cfg := cfgWith(map[string]string{"GOPROXY": "direct"})
	findings := Analyze(cfg)
	f := findCode(findings, CodeProxyDirectOnly)
	if f == nil || f.Severity != Critical {
		t.Fatalf("expected a critical proxy-direct-only finding, got %+v", findings)
	}
}

func TestAnalyze_ProxyDirectFirst(t *testing.T) {
	cfg := cfgWith(map[string]string{"GOPROXY": "direct,https://proxy.example.com"})
	findings := Analyze(cfg)
	f := findCode(findings, CodeProxyDirectFallback)
	if f == nil || f.Severity != Warning {
		t.Fatalf("expected a warning proxy-direct-fallback finding (direct first), got %+v", findings)
	}
}

func TestAnalyze_ProxyDefaultDirectFallbackIsInfo(t *testing.T) {
	cfg := cfgWith(map[string]string{"GOPROXY": "https://proxy.golang.org,direct"})
	findings := Analyze(cfg)
	f := findCode(findings, CodeProxyDirectFallback)
	if f == nil || f.Severity != Info {
		t.Fatalf("expected an info proxy-direct-fallback finding for the toolchain default, got %+v", findings)
	}
}

func TestAnalyze_ProxyNoDirectNoFinding(t *testing.T) {
	cfg := cfgWith(map[string]string{"GOPROXY": "https://proxy.example.com"})
	findings := Analyze(cfg)
	if f := findCode(findings, CodeProxyDirectFallback); f != nil {
		t.Errorf("unexpected proxy-direct-fallback finding: %+v", f)
	}
	if f := findCode(findings, CodeProxyDirectOnly); f != nil {
		t.Errorf("unexpected proxy-direct-only finding: %+v", f)
	}
}

func TestAnalyze_PrivateWildcardIsCritical(t *testing.T) {
	cfg := cfgWith(map[string]string{"GOPRIVATE": "*"})
	findings := Analyze(cfg)
	f := findCode(findings, CodePrivateWildcard)
	if f == nil || f.Severity != Critical || f.Var != "GOPRIVATE" {
		t.Fatalf("expected a critical private-wildcard finding on GOPRIVATE, got %+v", findings)
	}
}

func TestAnalyze_PrivateBroadTLDIsCritical(t *testing.T) {
	for _, pattern := range []string{"*.com", "*.io", "*.dev"} {
		cfg := cfgWith(map[string]string{"GONOSUMDB": pattern})
		findings := Analyze(cfg)
		f := findCode(findings, CodePrivateBroadTLD)
		if f == nil || f.Severity != Critical {
			t.Errorf("pattern %q: expected a critical private-broad-tld finding, got %+v", pattern, findings)
		}
	}
}

func TestAnalyze_PrivateNarrowPatternNoFinding(t *testing.T) {
	cfg := cfgWith(map[string]string{"GOPRIVATE": "*.corp.example.com,github.com/myorg/*"})
	findings := Analyze(cfg)
	if f := findCode(findings, CodePrivateWildcard); f != nil {
		t.Errorf("unexpected private-wildcard finding: %+v", f)
	}
	if f := findCode(findings, CodePrivateBroadTLD); f != nil {
		t.Errorf("unexpected private-broad-tld finding: %+v", f)
	}
	if f := findCode(findings, CodePrivateBare); f != nil {
		t.Errorf("unexpected private-bare-pattern finding: %+v", f)
	}
}

func TestAnalyze_GONOPROXYBroadIsWarningNotCritical(t *testing.T) {
	// GONOPROXY alone doesn't touch checksum verification, so it should
	// never be Critical even for the disaster patterns.
	cfg := cfgWith(map[string]string{"GONOPROXY": "*"})
	findings := Analyze(cfg)
	f := findCode(findings, CodePrivateWildcard)
	if f == nil {
		t.Fatalf("expected a private-wildcard finding for GONOPROXY=*, got %+v", findings)
	}
	if f.Severity != Warning {
		t.Errorf("GONOPROXY=* severity = %v, want Warning (downgraded from Critical)", f.Severity)
	}
}

func TestAnalyze_Insecure(t *testing.T) {
	cfg := cfgWith(map[string]string{"GOINSECURE": "example.com/*"})
	findings := Analyze(cfg)
	f := findCode(findings, CodeInsecureSet)
	if f == nil || f.Severity != Warning {
		t.Fatalf("expected a warning insecure-set finding, got %+v", findings)
	}
}

func TestAnalyze_GOFLAGSModRewrite(t *testing.T) {
	cfg := cfgWith(map[string]string{"GOFLAGS": "-mod=mod -v"})
	findings := Analyze(cfg)
	f := findCode(findings, CodeModFlagRewrite)
	if f == nil || f.Severity != Warning {
		t.Fatalf("expected a warning mod-flag-rewrite finding, got %+v", findings)
	}
}

func TestAnalyze_GOFLAGSVendor(t *testing.T) {
	cfg := cfgWith(map[string]string{"GOFLAGS": "-mod=vendor"})
	findings := Analyze(cfg)
	f := findCode(findings, CodeModFlagVendor)
	if f == nil || f.Severity != Info {
		t.Fatalf("expected an info mod-flag-vendor finding, got %+v", findings)
	}
}

func TestAnalyze_GOFLAGSReadonlyNoFinding(t *testing.T) {
	cfg := cfgWith(map[string]string{"GOFLAGS": "-mod=readonly"})
	findings := Analyze(cfg)
	if f := findCode(findings, CodeModFlagRewrite); f != nil {
		t.Errorf("unexpected mod-flag-rewrite finding: %+v", f)
	}
	if f := findCode(findings, CodeModFlagVendor); f != nil {
		t.Errorf("unexpected mod-flag-vendor finding: %+v", f)
	}
}

func TestAnalyze_GONOSUMCHECK(t *testing.T) {
	cfg := cfgWith(nil)
	cfg.GONOSUMCHECKSet = true
	cfg.GONOSUMCHECKValue = "1"
	findings := Analyze(cfg)
	f := findCode(findings, CodeGONOSUMCHECKIgnored)
	if f == nil || f.Severity != Info {
		t.Fatalf("expected an info gonosumcheck-ignored finding, got %+v", findings)
	}
}

func TestClassifyPattern(t *testing.T) {
	cases := []struct {
		pattern  string
		wantSev  Severity
		wantWide bool
	}{
		{"*", Critical, true},
		{"*.com", Critical, true},
		{"*.io", Critical, true},
		{"*.internal", Warning, true},
		{"com", Warning, true},
		{"mycompany", Info, false},
		{"*.corp.example.com", Info, false},
		{"github.com/myorg/*", Info, false},
	}
	for _, c := range cases {
		sev, wide := classifyPattern(c.pattern)
		if sev != c.wantSev || wide != c.wantWide {
			t.Errorf("classifyPattern(%q) = (%v, %v), want (%v, %v)", c.pattern, sev, wide, c.wantSev, c.wantWide)
		}
	}
}
