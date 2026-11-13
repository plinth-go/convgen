package resolver

import (
	"go/types"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveFormsAndCache(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "input", "internal", "controller")
	pkg, err := LoadPackage(dir)
	if err != nil {
		t.Fatal(err)
	}
	res := New(pkg, dir)
	tests := []struct {
		expr string
		path string
		name string
	}{
		{"Local", "example.com/proj/internal/controller", "Local"},
		{"db.User", "example.com/proj/internal/db", "User"},
		{"example.com/proj/internal/api.User", "example.com/proj/internal/api", "User"},
	}
	var first *types.Package
	for _, tt := range tests {
		got, err := res.Resolve(tt.expr)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", tt.expr, err)
		}
		if got.Package.Path() != tt.path || got.TypeName.Name() != tt.name {
			t.Fatalf("Resolve(%q) = %s.%s", tt.expr, got.Package.Path(), got.TypeName.Name())
		}
		if tt.expr == "db.User" {
			first = got.Package
		}
	}
	again, err := res.Resolve("db.User")
	if err != nil {
		t.Fatal(err)
	}
	if again.Package != first {
		t.Fatal("package cache did not reuse the resolved package")
	}
}

func TestResolveErrors(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "input", "internal", "controller")
	pkg, err := LoadPackage(dir)
	if err != nil {
		t.Fatal(err)
	}
	res := New(pkg, dir)
	tests := []struct {
		expr string
		want string
	}{
		{"nope.User", "未 import"},
		{"db.Nope", "找不到类型 Nope"},
		{"Missing", "当前包中找不到类型 Missing"},
		{"example.com/proj/internal/db.Nope", "找不到类型 Nope"},
	}
	for _, tt := range tests {
		_, err := res.Resolve(tt.expr)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("Resolve(%q) error = %v, want %q", tt.expr, err, tt.want)
		}
	}
}
