package modtrust

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Expected is a declared expected configuration a team can commit to
// their repository and gate CI on, so a drift from it (an unnoticed `go
// env -w`, a new CI image, a changed base environment) fails the build
// instead of quietly shipping.
//
// Only the variables present in the map are asserted; anything else in
// the effective configuration is left to Analyze's own rules.
type Expected map[string]string

// LoadExpected reads an expected configuration from a JSON file, e.g.:
//
//	{
//	  "GOSUMDB": "sum.golang.org",
//	  "GOPROXY": "https://proxy.internal.example.com,direct",
//	  "GOPRIVATE": "*.internal.example.com",
//	  "GOFLAGS": ""
//	}
//
// Keys are matched case-insensitively against the `go env` variable
// names; an unrecognized key is rejected rather than silently ignored.
func LoadExpected(path string) (Expected, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("modtrust: reading expected-config file: %w", err)
	}
	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("modtrust: parsing expected-config file %s: %w", path, err)
	}

	known := make(map[string]bool, len(knownVarNames))
	for _, n := range knownVarNames {
		known[strings.ToUpper(n)] = true
	}

	exp := make(Expected, len(raw))
	for k, v := range raw {
		upper := strings.ToUpper(strings.TrimSpace(k))
		if !known[upper] {
			names := append([]string(nil), knownVarNames...)
			sort.Strings(names)
			return nil, fmt.Errorf("modtrust: expected-config file %s: unrecognized variable %q (known: %s)",
				path, k, strings.Join(names, ", "))
		}
		exp[upper] = v
	}
	return exp, nil
}

// knownVarNames lists every variable name LoadExpected and Compare accept.
var knownVarNames = []string{
	"GOPROXY", "GOSUMDB", "GOPRIVATE", "GONOSUMDB", "GONOPROXY", "GOINSECURE", "GOFLAGS",
}

// Compare checks cfg against every variable named in exp and returns a
// Finding for each one whose effective value doesn't match.
func Compare(cfg Config, exp Expected) []Finding {
	names := make([]string, 0, len(exp))
	for name := range exp {
		names = append(names, name)
	}
	sort.Strings(names)

	var findings []Finding
	for _, name := range names {
		want := exp[name]
		got := cfg.Get(name)
		if got.Value == want {
			continue
		}
		findings = append(findings, Finding{
			Code: CodeExpectMismatch, Severity: Critical,
			Var: name, Value: got.Value, Source: got.Source,
			Message: fmt.Sprintf("%s is %q, expected %q.", name, got.Value, want),
			Consequence: fmt.Sprintf(
				"This environment's effective %s doesn't match the configuration this team declared "+
					"as required. Whatever protection that declared value was meant to guarantee may "+
					"not actually be in effect here.", name),
		})
	}
	return findings
}
