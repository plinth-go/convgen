package scanner

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestScanFileSingleAndMultiline(t *testing.T) {
	path := writeFile(t, `package sample

//convgen: -s db.User -d api.User -fn apiUser
//    -map DisplayName=Name
//	-conv CreatedAt=toTime
//    -conv UpdatedAt=toTime

// normal comment
//convgen: -s A -d B
//convgen: -s C -d D
`)

	decls, err := ScanFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(decls) != 3 {
		t.Fatalf("got %d declarations, want 3", len(decls))
	}
	if got, want := decls[0].Content, `-s db.User -d api.User -fn apiUser -map DisplayName=Name -conv CreatedAt=toTime -conv UpdatedAt=toTime`; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	if want := []string{"convgen: -s db.User -d api.User -fn apiUser", "    -map DisplayName=Name", "\t-conv CreatedAt=toTime", "    -conv UpdatedAt=toTime"}; !reflect.DeepEqual(decls[0].RawLines, want) {
		t.Fatalf("raw lines = %#v, want %#v", decls[0].RawLines, want)
	}
	if decls[0].Line != 3 {
		t.Fatalf("line = %d, want 3", decls[0].Line)
	}
	if decls[1].Content != "-s A -d B" || decls[2].Content != "-s C -d D" {
		t.Fatalf("consecutive declarations were not split: %#v", decls)
	}
}

func TestScanFileTermination(t *testing.T) {
	path := writeFile(t, `package sample

//convgen: -s A -d B
// one space is not a continuation
//convgen: -s C -d D
//    -map X=Y
func f() {}
//convgen: -s E -d F
`)

	decls, err := ScanFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(decls) != 3 {
		t.Fatalf("got %d declarations, want 3", len(decls))
	}
	if decls[0].Content != "-s A -d B" {
		t.Fatalf("ordinary comment was treated as continuation: %q", decls[0].Content)
	}
	if decls[1].Content != "-s C -d D -map X=Y" {
		t.Fatalf("continuation content = %q", decls[1].Content)
	}
}

func TestScanFileSkipsGenerated(t *testing.T) {
	path := writeFile(t, GeneratedMarker+"\n\npackage sample\n\n//convgen: -s A -d B\n")
	decls, generated, err := ScanFileInfo(path)
	if err != nil {
		t.Fatal(err)
	}
	if !generated {
		t.Fatal("generated file was not identified")
	}
	if len(decls) != 0 {
		t.Fatalf("generated file produced %d declarations", len(decls))
	}
}

func TestScanFilesDeterministic(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.go")
	b := filepath.Join(dir, "b.go")
	if err := os.WriteFile(b, []byte("//convgen: -s B -d B\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a, []byte("//convgen: -s A -d A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	decls, err := ScanFiles([]string{b, a})
	if err != nil {
		t.Fatal(err)
	}
	if len(decls) != 2 || decls[0].File != a {
		t.Fatalf("unexpected declaration order: %#v", decls)
	}
}

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.go")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
