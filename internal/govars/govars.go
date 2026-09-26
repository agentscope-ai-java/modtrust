// Package govars reads the Go toolchain's effective environment
// configuration (the variables `go env` reports) and, for each one,
// determines whether its value came from the process environment, from
// the `go env -w` configuration file, or from the toolchain's built-in
// default.
//
// The distinction matters: a value set with `go env -w` persists silently
// on a machine, invisible to anyone reading the shell environment or CI
// job definition — "it works on my machine" is often exactly that.
package govars

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Vars is the set of `go env` variables modtrust inspects, in the order
// they should be reported.
var Vars = []string{
	"GOPROXY",
	"GOSUMDB",
	"GOPRIVATE",
	"GONOSUMDB",
	"GONOPROXY",
	"GOINSECURE",
	"GOFLAGS",
	"GOENV",
}

// Source describes where a variable's effective value was determined.
type Source string

const (
	// SourceEnvironment means the value was set in the process's
	// environment (or inherited from the shell/CI job that launched it).
	SourceEnvironment Source = "environment"
	// SourceConfigFile means the value was set with `go env -w` and is
	// recorded in the on-disk Go environment configuration file.
	SourceConfigFile Source = "config-file"
	// SourceInherited means the variable has no value of its own; its
	// effective value was inherited from GOPRIVATE (this applies only to
	// GONOSUMDB and GONOPROXY, which fall back to GOPRIVATE when unset).
	SourceInherited Source = "inherited-from-GOPRIVATE"
	// SourceDefault means neither the environment nor the config file set
	// the variable; the toolchain's built-in default is in effect.
	SourceDefault Source = "default"
)

// Var is one `go env` variable together with its effective value and where
// that value came from.
type Var struct {
	Name string
	// Value is the effective value, exactly as `go env <NAME>` reports it.
	Value  string
	Source Source
	// ConfigPath is the path to the `go env -w` configuration file, set
	// only when Source is SourceConfigFile.
	ConfigPath string
}

// Snapshot is the full set of inspected variables plus the raw data used
// to compute their provenance, kept around so callers (and tests) can
// double-check the reasoning.
type Snapshot struct {
	Vars map[string]Var

	// ConfigPath is the effective GOENV file path ("" if disabled with
	// GOENV=off).
	ConfigPath string
	// ConfigDisabled is true when GOENV=off, meaning `go env -w` settings
	// are never consulted.
	ConfigDisabled bool
	// ConfigFileVars are the keys explicitly present in the config file,
	// regardless of value.
	ConfigFileVars map[string]string
	// EnvVars are the Vars keys that are set (non-empty) in the process
	// environment, regardless of value.
	EnvVars map[string]string

	// GONOSUMCHECKSet reports whether GONOSUMCHECK is set in the process
	// environment. GONOSUMCHECK is not a variable the Go toolchain
	// recognizes in module-aware mode — `go env -w GONOSUMCHECK=...` is
	// rejected outright, and `go env GONOSUMCHECK` always reports an empty
	// string regardless of what's in the real environment. A value set
	// there has no effect on checksum verification and `go env` gives no
	// indication that anything was ignored.
	GONOSUMCHECKSet   bool
	GONOSUMCHECKValue string
}

// Read gathers the effective `go env` values for Vars and works out each
// one's provenance by cross-referencing `go env -json`, `go env -json
// -changed`, the raw process environment, and the on-disk `go env -w`
// configuration file. It shells out to the real `go` binary; it does not
// parse or guess at go env's own logic.
func Read(ctx context.Context) (*Snapshot, error) {
	effective, err := runEnvJSON(ctx)
	if err != nil {
		return nil, fmt.Errorf("govars: reading effective go env: %w", err)
	}

	snap := &Snapshot{
		Vars:           make(map[string]Var, len(Vars)),
		ConfigFileVars: make(map[string]string),
		EnvVars:        make(map[string]string),
	}

	snap.ConfigPath = effective["GOENV"]
	snap.ConfigDisabled = strings.EqualFold(os.Getenv("GOENV"), "off")

	if !snap.ConfigDisabled && snap.ConfigPath != "" {
		fileVars, err := readConfigFile(snap.ConfigPath)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("govars: reading go env config file %s: %w", snap.ConfigPath, err)
		}
		snap.ConfigFileVars = fileVars
	}

	for _, name := range Vars {
		if v, ok := os.LookupEnv(name); ok && v != "" {
			snap.EnvVars[name] = v
		}
	}

	if v, ok := os.LookupEnv("GONOSUMCHECK"); ok && v != "" {
		snap.GONOSUMCHECKSet = true
		snap.GONOSUMCHECKValue = v
	}

	for _, name := range Vars {
		snap.Vars[name] = Var{
			Name:   name,
			Value:  effective[name],
			Source: sourceFor(name, effective, snap),
		}
		if v := snap.Vars[name]; v.Source == SourceConfigFile {
			v.ConfigPath = snap.ConfigPath
			snap.Vars[name] = v
		}
	}

	return snap, nil
}

func sourceFor(name string, effective map[string]string, snap *Snapshot) Source {
	if _, ok := snap.EnvVars[name]; ok {
		return SourceEnvironment
	}
	if _, ok := snap.ConfigFileVars[name]; ok {
		return SourceConfigFile
	}
	// GONOSUMDB and GONOPROXY fall back to GOPRIVATE when neither the
	// environment nor the config file sets them explicitly, but `go env`
	// already reports the inherited value as their effective value.
	if name == "GONOSUMDB" || name == "GONOPROXY" {
		if effective[name] != "" && effective[name] == effective["GOPRIVATE"] {
			if _, ok := snap.EnvVars["GOPRIVATE"]; ok {
				return SourceInherited
			}
			if _, ok := snap.ConfigFileVars["GOPRIVATE"]; ok {
				return SourceInherited
			}
		}
	}
	return SourceDefault
}

func runEnvJSON(ctx context.Context) (map[string]string, error) {
	cmd := exec.CommandContext(ctx, "go", "env", "-json")
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go env -json: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	out := make(map[string]string)
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return nil, fmt.Errorf("parsing go env -json output: %w", err)
	}
	return out, nil
}

// readConfigFile parses a `go env -w` configuration file directly. The
// format is simple KEY=VALUE lines, one per setting, with '#' comments
// permitted by the toolchain; this reads it independently of `go env
// -json -changed` because a config file can pin a variable to a value
// that happens to equal the toolchain default, which -changed would not
// surface.
func readConfigFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	vars := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		vars[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return vars, nil
}
