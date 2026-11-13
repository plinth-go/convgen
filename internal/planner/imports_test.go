package planner

import (
	"go/types"
	"testing"
)

func TestImportSetAliasesCollisions(t *testing.T) {
	current := types.NewPackage("example.com/current", "api")
	first := types.NewPackage("example.com/first", "db")
	second := types.NewPackage("example.com/second", "db")
	shadow := types.NewPackage("example.com/shadow", "src")

	set := NewImportSet(current)
	if got := set.Alias(current); got != "" {
		t.Fatalf("current package alias = %q", got)
	}
	if got := set.Alias(first); got != "db" {
		t.Fatalf("first alias = %q", got)
	}
	if got := set.Alias(second); got != "db2" {
		t.Fatalf("second alias = %q", got)
	}
	if got := set.Alias(shadow); got != "srcpkg" {
		t.Fatalf("shadow alias = %q", got)
	}
	imports := set.Imports()
	if len(imports) != 3 || imports[0].Path != "example.com/first" || imports[1].Alias != "db2" {
		t.Fatalf("imports = %#v", imports)
	}
}

func TestImportSetReservesCurrentPackageName(t *testing.T) {
	current := types.NewPackage("example.com/current", "db")
	external := types.NewPackage("example.com/external", "db")
	set := NewImportSet(current)
	if got := set.Alias(external); got != "db2" {
		t.Fatalf("external package with current name alias = %q", got)
	}
}
