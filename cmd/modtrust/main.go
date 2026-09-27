// Command modtrust reports whether a Go build environment actually
// enforces module integrity: the effective GOPROXY/GOSUMDB/GOPRIVATE/etc.
// configuration and where each value came from, go.sum completeness
// against go.mod, and optionally the module cache via `go mod verify`.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/agentscope-ai-java/modtrust"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("modtrust", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		dir     string
		jsonOut bool
		verify  bool
		expect  string
		failOn  string
		noSum   bool
	)
	fs.StringVar(&dir, "dir", ".", "module directory to check (containing go.mod)")
	fs.BoolVar(&jsonOut, "json", false, "print the full result as JSON instead of the summary")
	fs.BoolVar(&verify, "verify", false, "also run `go mod verify` against the module cache")
	fs.StringVar(&expect, "expect", "", "path to a JSON file declaring the expected go env configuration")
	fs.StringVar(&failOn, "fail-on", "", "comma-separated finding codes or severities that make the exit code 1")
	fs.BoolVar(&noSum, "no-sum", false, "skip the go.sum completeness check")
	fs.Usage = func() {
		fmt.Fprintf(stderr, `modtrust checks whether this build environment actually enforces Go module integrity.

Usage:
  modtrust [flags]

Flags:
`)
		fs.PrintDefaults()
		fmt.Fprintf(stderr, `
Exit codes:
  0  ran cleanly, no finding matched --fail-on (or --fail-on was empty)
  1  a finding matched --fail-on
  2  modtrust itself failed (bad flags, go.mod unreadable, go binary missing, ...)

Examples:
  modtrust
  modtrust --json
  modtrust --fail-on critical
  modtrust --fail-on sumdb-off,proxy-direct-only,private-wildcard
  modtrust --expect expected-go-env.json --fail-on critical,expect-mismatch
`)
	}

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}

	opts := modtrust.Options{Verify: verify}
	if !noSum {
		opts.Dir = dir
	}

	if expect != "" {
		exp, err := modtrust.LoadExpected(expect)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		opts.Expected = exp
	}

	report, err := modtrust.Run(context.Background(), opts)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	if jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
	} else {
		printSummary(stdout, report)
	}

	var failTokens []string
	if failOn != "" {
		failTokens = strings.Split(failOn, ",")
	}
	if report.FailOn(failTokens) {
		return 1
	}
	return 0
}

var severityOrder = map[modtrust.Severity]int{
	modtrust.Critical: 0,
	modtrust.Warning:  1,
	modtrust.Info:     2,
}

func printSummary(w *os.File, r modtrust.Report) {
	fmt.Fprintln(w, "go env configuration:")
	for _, v := range r.Config.Vars {
		src := v.Source
		if v.ConfigPath != "" {
			src = fmt.Sprintf("%s: %s", v.Source, v.ConfigPath)
		}
		value := v.Value
		if value == "" {
			value = "(empty)"
		}
		fmt.Fprintf(w, "  %-12s %-45s [%s]\n", v.Name, value, src)
	}
	if r.Config.GONOSUMCHECKSet {
		fmt.Fprintf(w, "  %-12s %-45s [environment, not recognized by go]\n", "GONOSUMCHECK", r.Config.GONOSUMCHECKValue)
	}
	fmt.Fprintln(w)

	findings := append([]modtrust.Finding(nil), r.Findings...)
	sort.SliceStable(findings, func(i, j int) bool {
		return severityOrder[findings[i].Severity] < severityOrder[findings[j].Severity]
	})

	counts := map[modtrust.Severity]int{}
	for _, f := range findings {
		counts[f.Severity]++
	}
	fmt.Fprintf(w, "%d finding(s): %d critical, %d warning, %d info\n\n",
		len(findings), counts[modtrust.Critical], counts[modtrust.Warning], counts[modtrust.Info])

	for _, f := range findings {
		fmt.Fprintf(w, "%-8s %-24s %s\n", strings.ToUpper(string(f.Severity)), f.Code, f.Message)
		if f.Consequence != "" {
			fmt.Fprintf(w, "         %s\n", f.Consequence)
		}
	}
	if len(findings) > 0 {
		fmt.Fprintln(w)
	}

	if r.Sum != nil {
		ok, missing, local := 0, 0, 0
		for _, e := range r.Sum.Entries {
			switch e.Status {
			case "ok":
				ok++
			case "local-replace":
				local++
			default:
				missing++
			}
		}
		status := "complete"
		if !r.Sum.Complete {
			status = "INCOMPLETE"
		}
		fmt.Fprintf(w, "go.sum (%s): %d ok, %d missing, %d local-replace, %d stale, checked against %s\n",
			status, ok, missing, local, len(r.Sum.Stale), r.Sum.Dir)
	}

	if r.Verify != nil {
		if r.Verify.Passed {
			fmt.Fprintln(w, "go mod verify: passed")
		} else {
			fmt.Fprintf(w, "go mod verify: FAILED\n%s\n", r.Verify.Output)
		}
	}
}
