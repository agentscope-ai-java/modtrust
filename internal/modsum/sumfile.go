package modsum

import (
	"bufio"
	"fmt"
	"strings"
)

// SumEntry records which of the two go.sum lines a module has: the
// content hash ("module version hash") and the go.mod hash ("module
// version/go.mod hash").
type SumEntry struct {
	HasModuleHash bool
	HasGoModHash  bool
}

// ParseSumFile parses go.sum content into a map keyed by module path and
// version. An empty or missing go.sum parses to an empty, non-nil map —
// callers can tell "no go.sum" from "go.sum with entries" via the second
// return value only in ReadSumFile; ParseSumFile itself has no notion of
// file existence.
func ParseSumFile(data string) (map[Module]*SumEntry, error) {
	entries := make(map[Module]*SumEntry)
	scanner := bufio.NewScanner(strings.NewReader(data))
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return nil, fmt.Errorf("go.sum:%d: expected 3 fields, got %d: %q", lineNo, len(fields), line)
		}
		path, versionField, hash := fields[0], fields[1], fields[2]
		if hash == "" {
			return nil, fmt.Errorf("go.sum:%d: empty hash", lineNo)
		}

		isGoMod := strings.HasSuffix(versionField, "/go.mod")
		version := strings.TrimSuffix(versionField, "/go.mod")

		key := Module{Path: path, Version: version}
		e, ok := entries[key]
		if !ok {
			e = &SumEntry{}
			entries[key] = e
		}
		if isGoMod {
			e.HasGoModHash = true
		} else {
			e.HasModuleHash = true
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}
