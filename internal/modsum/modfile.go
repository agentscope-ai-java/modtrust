// Package modsum parses go.mod and go.sum with a small hand-written
// reader — not golang.org/x/mod/modfile, which is a separate module and
// therefore a third-party dependency — and checks that every module
// go.mod requires has the checksum entries go.sum should carry for it.
package modsum

import (
	"bufio"
	"fmt"
	"strings"
)

// Module identifies a module by path and version.
type Module struct {
	Path    string
	Version string
}

func (m Module) String() string {
	if m.Version == "" {
		return m.Path
	}
	return m.Path + "@" + m.Version
}

// Requirement is one entry from a go.mod require block.
type Requirement struct {
	Module
	Indirect bool
}

// Replace is one entry from a go.mod replace block. Old.Version is empty
// when the replace applies to every version of Old.Path requested.
type Replace struct {
	Old Module
	// New is the replacement. When Local is true, New.Path is a
	// filesystem path (relative or absolute) and New.Version is unused:
	// the module is read straight off disk and never fetched, so it
	// carries no go.sum entry.
	New   Module
	Local bool
}

// ModFile is the subset of go.mod content modtrust cares about.
type ModFile struct {
	ModulePath string
	GoVersion  string
	Requires   []Requirement
	Replaces   []Replace
}

// ParseModFile parses go.mod content. It understands the module, go,
// require and replace directives in both single-line and parenthesized
// block form, which is all modtrust's checks need; toolchain, exclude and
// retract directives are read past without effect.
func ParseModFile(data string) (*ModFile, error) {
	mf := &ModFile{}
	scanner := bufio.NewScanner(strings.NewReader(data))
	var block string // "" , "require", "replace", "exclude", "retract"
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		raw := scanner.Text()
		line, indirect := stripComment(raw)
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if block != "" {
			if line == ")" {
				block = ""
				continue
			}
			if err := parseBlockLine(mf, block, line, indirect); err != nil {
				return nil, fmt.Errorf("go.mod:%d: %w", lineNo, err)
			}
			continue
		}

		fields := splitFields(line)
		if len(fields) == 0 {
			continue
		}
		directive := fields[0]
		rest := strings.TrimSpace(strings.TrimPrefix(line, directive))

		switch directive {
		case "module":
			mf.ModulePath = unquote(rest)
		case "go":
			mf.GoVersion = unquote(rest)
		case "toolchain", "exclude", "retract":
			// Not needed for checksum-completeness checking.
		case "require", "replace":
			if rest == "(" {
				block = directive
				continue
			}
			if err := parseBlockLine(mf, directive, rest, indirect); err != nil {
				return nil, fmt.Errorf("go.mod:%d: %w", lineNo, err)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return mf, nil
}

func parseBlockLine(mf *ModFile, kind, line string, indirect bool) error {
	fields := splitFields(line)
	switch kind {
	case "require":
		if len(fields) < 2 {
			return fmt.Errorf("malformed require line %q", line)
		}
		mf.Requires = append(mf.Requires, Requirement{
			Module:   Module{Path: unquote(fields[0]), Version: unquote(fields[1])},
			Indirect: indirect,
		})
	case "replace":
		// Forms:
		//   old/path => new/path v1.2.3
		//   old/path v1.2.3 => new/path v1.2.3
		//   old/path => ../local/dir
		//   old/path v1.2.3 => ../local/dir
		idx := indexArrow(fields)
		if idx < 0 {
			return fmt.Errorf("malformed replace line %q (no =>)", line)
		}
		left := fields[:idx]
		right := fields[idx+1:]
		if len(left) == 0 || len(right) == 0 {
			return fmt.Errorf("malformed replace line %q", line)
		}
		old := Module{Path: unquote(left[0])}
		if len(left) > 1 {
			old.Version = unquote(left[1])
		}
		r := Replace{Old: old}
		newPath := unquote(right[0])
		if len(right) > 1 {
			r.New = Module{Path: newPath, Version: unquote(right[1])}
		} else {
			// No version on the right side means a filesystem path
			// replacement per the go.mod spec.
			r.New = Module{Path: newPath}
			r.Local = true
		}
		if !r.Local && looksLikeLocalPath(newPath) {
			// Defensive: a bare path with no version is already caught
			// above, but tolerate stray cases the same way `go` does.
			r.Local = true
		}
		mf.Replaces = append(mf.Replaces, r)
	case "exclude", "retract":
		// Ignored.
	}
	return nil
}

func indexArrow(fields []string) int {
	for i, f := range fields {
		if f == "=>" {
			return i
		}
	}
	return -1
}

func looksLikeLocalPath(p string) bool {
	return strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../") ||
		strings.HasPrefix(p, "/") || (len(p) > 1 && p[1] == ':') // crude Windows drive check
}

// stripComment removes a trailing "//" comment from a go.mod line and
// reports whether that comment was exactly the "// indirect" marker (the
// only trailing comment the go tool itself attaches meaning to).
func stripComment(line string) (stripped string, indirect bool) {
	if i := strings.Index(line, "//"); i >= 0 {
		comment := strings.TrimSpace(line[i+2:])
		stripped = line[:i]
		indirect = comment == "indirect"
		return stripped, indirect
	}
	return line, false
}

// splitFields splits on whitespace like strings.Fields, but keeps
// backtick- or double-quoted segments (rare in practice, but legal in
// go.mod) intact as single fields.
func splitFields(s string) []string {
	var fields []string
	var cur strings.Builder
	var quote rune
	inQuote := false
	flush := func() {
		if cur.Len() > 0 {
			fields = append(fields, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case inQuote:
			cur.WriteRune(r)
			if r == quote {
				inQuote = false
			}
		case r == '"' || r == '`':
			inQuote = true
			quote = r
			cur.WriteRune(r)
		case r == ' ' || r == '\t':
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return fields
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '`' && s[len(s)-1] == '`') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
