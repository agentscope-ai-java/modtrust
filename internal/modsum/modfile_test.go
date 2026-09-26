package modsum

import (
	"reflect"
	"testing"
)

func TestParseModFile_SingleLineDirectives(t *testing.T) {
	src := `module example.com/foo

go 1.21

require example.com/bar v1.2.3
require example.com/baz v0.1.0 // indirect

replace example.com/bar => ../local/bar
`
	mf, err := ParseModFile(src)
	if err != nil {
		t.Fatalf("ParseModFile: %v", err)
	}
	if mf.ModulePath != "example.com/foo" {
		t.Errorf("ModulePath = %q", mf.ModulePath)
	}
	if mf.GoVersion != "1.21" {
		t.Errorf("GoVersion = %q", mf.GoVersion)
	}
	want := []Requirement{
		{Module: Module{Path: "example.com/bar", Version: "v1.2.3"}},
		{Module: Module{Path: "example.com/baz", Version: "v0.1.0"}, Indirect: true},
	}
	if !reflect.DeepEqual(mf.Requires, want) {
		t.Errorf("Requires = %+v, want %+v", mf.Requires, want)
	}
	if len(mf.Replaces) != 1 {
		t.Fatalf("Replaces = %+v, want 1 entry", mf.Replaces)
	}
	r := mf.Replaces[0]
	if r.Old.Path != "example.com/bar" || r.Old.Version != "" {
		t.Errorf("replace Old = %+v", r.Old)
	}
	if !r.Local || r.New.Path != "../local/bar" {
		t.Errorf("replace New = %+v, Local = %v", r.New, r.Local)
	}
}

func TestParseModFile_BlockDirectives(t *testing.T) {
	src := `module example.com/foo

go 1.22

require (
	example.com/bar v1.2.3
	example.com/baz v0.1.0 // indirect
)

replace (
	example.com/bar v1.2.3 => example.com/bar-fork v1.2.3-patched
	example.com/qux => ./vendor/qux
)
`
	mf, err := ParseModFile(src)
	if err != nil {
		t.Fatalf("ParseModFile: %v", err)
	}
	if len(mf.Requires) != 2 {
		t.Fatalf("Requires = %+v", mf.Requires)
	}
	if !mf.Requires[1].Indirect {
		t.Errorf("second require should be indirect: %+v", mf.Requires[1])
	}
	if len(mf.Replaces) != 2 {
		t.Fatalf("Replaces = %+v", mf.Replaces)
	}

	fork := mf.Replaces[0]
	if fork.Old != (Module{Path: "example.com/bar", Version: "v1.2.3"}) {
		t.Errorf("fork.Old = %+v", fork.Old)
	}
	if fork.Local {
		t.Errorf("fork replace should not be local")
	}
	if fork.New != (Module{Path: "example.com/bar-fork", Version: "v1.2.3-patched"}) {
		t.Errorf("fork.New = %+v", fork.New)
	}

	local := mf.Replaces[1]
	if !local.Local || local.New.Path != "./vendor/qux" {
		t.Errorf("local replace = %+v", local)
	}
}

func TestParseModFile_CommentsAndBlankLinesIgnored(t *testing.T) {
	src := `// This module does a thing.
module example.com/foo

// go directive
go 1.21

require ( // trailing comment on the open paren is unusual but tolerate it
	example.com/bar v1.2.3
)
`
	mf, err := ParseModFile(src)
	if err != nil {
		t.Fatalf("ParseModFile: %v", err)
	}
	if len(mf.Requires) != 1 {
		t.Fatalf("Requires = %+v", mf.Requires)
	}
}

func TestParseSumFile(t *testing.T) {
	src := `example.com/bar v1.2.3 h1:abc=
example.com/bar v1.2.3/go.mod h1:def=
example.com/baz v0.1.0/go.mod h1:ghi=
`
	entries, err := ParseSumFile(src)
	if err != nil {
		t.Fatalf("ParseSumFile: %v", err)
	}
	bar := entries[Module{Path: "example.com/bar", Version: "v1.2.3"}]
	if bar == nil || !bar.HasModuleHash || !bar.HasGoModHash {
		t.Errorf("bar entry = %+v, want both hashes present", bar)
	}
	baz := entries[Module{Path: "example.com/baz", Version: "v0.1.0"}]
	if baz == nil || baz.HasModuleHash || !baz.HasGoModHash {
		t.Errorf("baz entry = %+v, want only go.mod hash present", baz)
	}
}

func TestParseSumFile_MalformedLine(t *testing.T) {
	_, err := ParseSumFile("example.com/bar v1.2.3\n")
	if err == nil {
		t.Fatalf("expected an error for a malformed go.sum line")
	}
}
