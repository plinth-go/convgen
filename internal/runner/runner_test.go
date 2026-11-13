package runner

import (
	"bytes"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoldenAndIdempotent(t *testing.T) {
	moduleDir := copyFixture(t, filepath.Join("..", "..", "testdata", "input"))

	var stderr bytes.Buffer
	if err := Run([]string{filepath.Join(moduleDir, "./...")}, &stderr); err != nil {
		t.Fatalf("first run: %v\n%s", err, stderr.String())
	}
	outputPath := filepath.Join(moduleDir, "internal", "controller", "conv_gen.go")
	first := readFile(t, outputPath)
	want := readFile(t, filepath.Join("..", "..", "testdata", "want", "conv_gen.go"))
	if string(first) != string(want) {
		t.Fatalf("generated file differs from golden:\n--- got ---\n%s\n--- want ---\n%s", first, want)
	}
	for _, name := range []string{"apiBaseFromDB", "apiUserFull", "apiUserRich", "dbBaseFromAPI"} {
		if !bytes.Contains(stderr.Bytes(), []byte("+ "+name)) {
			t.Fatalf("missing change line for %s:\n%s", name, stderr.String())
		}
	}
	if bytes.Contains([]byte(first), []byte("panic(")) {
		t.Fatal("generated file contains panic")
	}
	if !bytes.Contains([]byte(first), []byte("//convgen: -s db.User")) || bytes.Contains([]byte(first), []byte("// convgen:")) {
		t.Fatal("declaration echo was not preserved in the standard format")
	}
	if !bytes.Contains([]byte(first), []byte("// Source: internal/controller/user.go")) {
		t.Fatal("generated file is missing its source path")
	}
	formatted, err := format.Source([]byte(first))
	if err != nil || string(formatted) != first {
		t.Fatalf("generated file is not gofmt-stable: %v", err)
	}

	stderr.Reset()
	if err := Run([]string{filepath.Join(moduleDir, "./...")}, &stderr); err != nil {
		t.Fatalf("second run: %v\n%s", err, stderr.String())
	}
	second := readFile(t, outputPath)
	if string(first) != string(second) {
		t.Fatal("generation is not byte-for-byte idempotent")
	}
	if stderr.Len() != 0 {
		t.Fatalf("idempotent run printed changes:\n%s", stderr.String())
	}

	testFile := filepath.Join(moduleDir, "internal", "controller", "conv_test.go")
	writeFile(t, testFile, `package controller

import (
	"testing"

	"example.com/proj/internal/api"
	"example.com/proj/internal/db"
)

func TestGeneratedConversions(t *testing.T) {
	if got := dbBaseFromAPI(nil); got != nil {
		t.Fatalf("nil ptr_ptr = %#v", got)
	}
	if got := apiUserRich(nil); got != (api.Rich{}) {
		t.Fatalf("nil ptr_val = %#v", got)
	}
	value := 7
	src := db.User{Name: "Ada", CreatedAt: "c", UpdatedAt: "u", Ptr: &value, Tags: []string{"a"}, Metadata: map[string]string{"k": "v"}}
	got := apiUserFull(src)
	if got.DisplayName != "Ada" || got.CreatedAt != "time:c" || got.UpdatedAt != "time:u" {
		t.Fatalf("mapped/conv result = %#v", got)
	}
	if got.Ptr == nil || got.Ptr == src.Ptr || *got.Ptr != 7 {
		t.Fatalf("deep pointer was not cloned: %#v", got.Ptr)
	}
	src.Tags[0] = "changed"
	src.Metadata["k"] = "changed"
	if got.Tags[0] != "a" || got.Metadata["k"] != "v" {
		t.Fatalf("deep slice/map shares storage: %#v", got)
	}
	nilDeep := apiUserFull(db.User{})
	if nilDeep.Tags != nil || nilDeep.Metadata != nil || nilDeep.Ptr != nil {
		t.Fatalf("deep nil fields = %#v", nilDeep)
	}
}
`)
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = moduleDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated module tests failed: %v\n%s", err, output)
	}
}

func TestDoesNotOverwriteManualOutput(t *testing.T) {
	moduleDir := writeModule(t, map[string]string{
		"go.mod": "module example.com/manual\n\ngo 1.22.0\n",
		"types.go": `package manual

type A struct{ ID int }
type B struct{ ID int }

//convgen: -s A -d B -fn BFromA
`,
		"conv_gen.go": "package manual\n\n// handwritten\n",
	})
	var stderr bytes.Buffer
	err := Run([]string{filepath.Join(moduleDir, "./...")}, &stderr)
	if err == nil || !strings.Contains(err.Error(), "请手动删除") {
		t.Fatalf("error = %v", err)
	}
	if got := readFile(t, filepath.Join(moduleDir, "conv_gen.go")); got != "package manual\n\n// handwritten\n" {
		t.Fatalf("manual file was modified: %q", got)
	}
}

func TestErrorsAggregateWithLocation(t *testing.T) {
	moduleDir := writeModule(t, map[string]string{
		"go.mod": "module example.com/bad\n\ngo 1.22.0\n",
		"types.go": `package bad

type A struct{ ID int }
type B struct{ Missing string }

//convgen: -s A -d B -fn BFromA
`,
	})
	var stderr bytes.Buffer
	err := Run([]string{filepath.Join(moduleDir, "./...")}, &stderr)
	if err == nil {
		t.Fatal("expected error")
	}
	text := err.Error()
	if !strings.Contains(text, "types.go:6:") || !strings.Contains(text, "Missing") {
		t.Fatalf("error lacks location or field: %v", err)
	}
	if _, err := os.Stat(filepath.Join(moduleDir, "conv_gen.go")); !os.IsNotExist(err) {
		t.Fatal("conv_gen.go should not be written on failure")
	}
}

func TestDuplicateConvFieldFailsWithoutWriting(t *testing.T) {
	moduleDir := writeModule(t, map[string]string{
		"go.mod": "module example.com/duplicate\n\ngo 1.22.0\n",
		"types.go": `package duplicate

type A struct{ Name string }
type B struct{ Name string }

//convgen: -s A -d B -fn BFromA
//    -conv Name=strings.TrimSpace
//    -conv Name=strings.ToLower
`,
	})
	var stderr bytes.Buffer
	err := Run([]string{filepath.Join(moduleDir, "./...")}, &stderr)
	if err == nil || !strings.Contains(err.Error(), "types.go:8: -conv 字段 Name 重复（首次出现于 types.go:7）") {
		t.Fatalf("error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(moduleDir, "conv_gen.go")); !os.IsNotExist(statErr) {
		t.Fatal("conv_gen.go should not be written on duplicate fields")
	}
}

func TestConvErrorUsesDirectiveLine(t *testing.T) {
	moduleDir := writeModule(t, map[string]string{
		"go.mod": "module example.com/badconv\n\ngo 1.22.0\n",
		"types.go": `package badconv

type A struct{ Name string }
type B struct{ Name string }

//convgen: -s A -d B -fn BFromA
//    -conv Name=strings.NotAFunction
`,
	})
	var stderr bytes.Buffer
	err := Run([]string{filepath.Join(moduleDir, "./...")}, &stderr)
	if err == nil || !strings.Contains(err.Error(), "types.go:7: 字段 Name 的 -conv 无效") {
		t.Fatalf("error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(moduleDir, "conv_gen.go")); !os.IsNotExist(statErr) {
		t.Fatal("conv_gen.go should not be written on invalid -conv")
	}
}

func TestUnassignedDstFieldsFailWithoutWriting(t *testing.T) {
	moduleDir := writeModule(t, map[string]string{
		"go.mod": "module example.com/unassigned\n\ngo 1.22.0\n",
		"types.go": `package unassigned

type A struct{ ID int; Name string }
type B struct{ ID int; Name string; Email string; Role string }

//convgen: -s A -d B -fn BFromA
`,
	})
	var stderr bytes.Buffer
	err := Run([]string{filepath.Join(moduleDir, "./...")}, &stderr)
	if err == nil {
		t.Fatal("expected unassigned destination fields")
	}
	text := err.Error()
	for _, want := range []string{"types.go:6:", "存在未赋值字段", "Email", "Role", "使用 -ignore"} {
		if !strings.Contains(text, want) {
			t.Fatalf("error missing %q: %v", want, err)
		}
	}
	if _, statErr := os.Stat(filepath.Join(moduleDir, "conv_gen.go")); !os.IsNotExist(statErr) {
		t.Fatal("conv_gen.go should not be written with unassigned destination fields")
	}
}

func TestRegeneratesAfterSourceChangeWithStaleOutput(t *testing.T) {
	moduleDir := writeModule(t, map[string]string{
		"go.mod": "module example.com/stale\n\ngo 1.22.0\n",
		"types.go": `package stale

type A struct{ ID int; Name string }
type B struct{ ID int; Name string }

//convgen: -s A -d B -fn BFromA
`,
	})
	var stderr bytes.Buffer
	if err := Run([]string{filepath.Join(moduleDir, "./...")}, &stderr); err != nil {
		t.Fatalf("initial generation: %v", err)
	}

	writeFile(t, filepath.Join(moduleDir, "types.go"), `package stale

type A struct{ ID int }
type B struct{ ID int }

//convgen: -s A -d B -fn BFromA
`)
	stderr.Reset()
	if err := Run([]string{filepath.Join(moduleDir, "./...")}, &stderr); err != nil {
		t.Fatalf("regeneration with stale output: %v\n%s", err, stderr.String())
	}
	generated := readFile(t, filepath.Join(moduleDir, "conv_gen.go"))
	if strings.Contains(generated, "dst.Name") {
		t.Fatalf("stale field remained in output:\n%s", generated)
	}
}

func TestRegeneratesWhenPackageUsesGeneratedFunction(t *testing.T) {
	moduleDir := writeModule(t, map[string]string{
		"go.mod": "module example.com/used\n\ngo 1.22.0\n",
		"types.go": `package used

type A struct{ ID int }
type B struct{ ID int }

//convgen: -s A -d B -fn BFromA
`,
	})
	var stderr bytes.Buffer
	if err := Run([]string{filepath.Join(moduleDir, "./...")}, &stderr); err != nil {
		t.Fatalf("initial generation: %v\n%s", err, stderr.String())
	}
	writeFile(t, filepath.Join(moduleDir, "use.go"), `package used

func convertA(value A) B {
	return BFromA(value)
}
`)
	stderr.Reset()
	if err := Run([]string{filepath.Join(moduleDir, "./...")}, &stderr); err != nil {
		t.Fatalf("regeneration with generated function usage: %v\n%s", err, stderr.String())
	}
}

func TestRegeneratesAfterSourceChangeWhenGeneratedFunctionIsUsed(t *testing.T) {
	moduleDir := writeModule(t, map[string]string{
		"go.mod": "module example.com/usedstale\n\ngo 1.22.0\n",
		"types.go": `package usedstale

type A struct{ ID int; Name string }
type B struct{ ID int; Name string }

//convgen: -s A -d B -fn BFromA
`,
	})
	var stderr bytes.Buffer
	if err := Run([]string{filepath.Join(moduleDir, "./...")}, &stderr); err != nil {
		t.Fatalf("initial generation: %v\n%s", err, stderr.String())
	}
	writeFile(t, filepath.Join(moduleDir, "use.go"), `package usedstale

func convertA(value A) B {
	return BFromA(value)
}
`)
	writeFile(t, filepath.Join(moduleDir, "types.go"), `package usedstale

type A struct{ ID int }
type B struct{ ID int }

//convgen: -s A -d B -fn BFromA
`)
	stderr.Reset()
	if err := Run([]string{filepath.Join(moduleDir, "./...")}, &stderr); err != nil {
		t.Fatalf("regeneration after source change: %v\n%s", err, stderr.String())
	}
	if generated := readFile(t, filepath.Join(moduleDir, "conv_gen.go")); strings.Contains(generated, "dst.Name") {
		t.Fatalf("stale field remained in output:\n%s", generated)
	}
}

func TestQualifiedAndTemplateConversionsCompile(t *testing.T) {
	moduleDir := writeModule(t, map[string]string{
		"go.mod": "module example.com/expressions\n\ngo 1.22.0\n",
		"types.go": `package expressions

type A struct {
	Name string
	Code string
}

type B struct {
	Name string
	Code string
}

//convgen: -s A -d B -fn BFromA
//    -conv Name=str.TrimSpace, Code=strings.ReplaceAll($, "x", "y")
`,
		"helper.go": `package expressions

import str "strings"

func clean(value string) string {
	return str.TrimSpace(value)
}
`,
	})
	var stderr bytes.Buffer
	if err := Run([]string{filepath.Join(moduleDir, "./...")}, &stderr); err != nil {
		t.Fatalf("generation: %v\n%s", err, stderr.String())
	}
	generated := readFile(t, filepath.Join(moduleDir, "conv_gen.go"))
	for _, want := range []string{`"strings"`, "strings.TrimSpace(src.Name)", `strings.ReplaceAll(src.Code, "x", "y")`} {
		if !strings.Contains(generated, want) {
			t.Fatalf("generated output missing %q:\n%s", want, generated)
		}
	}
	writeFile(t, filepath.Join(moduleDir, "consumer.go"), `package expressions

func convertA(value A) B {
	return BFromA(value)
}
`)
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = moduleDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated module tests failed: %v\n%s", err, output)
	}
}

func TestOverlappingRootsDoNotDuplicateDeclarations(t *testing.T) {
	moduleDir := writeModule(t, map[string]string{
		"go.mod": "module example.com/overlap\n\ngo 1.22.0\n",
		"types.go": `package overlap

type A struct{ ID int }
type B struct{ ID int }

//convgen: -s A -d B -fn BFromA
`,
	})
	var stderr bytes.Buffer
	err := Run([]string{moduleDir, filepath.Join(moduleDir, "./...")}, &stderr)
	if err != nil {
		t.Fatalf("overlapping roots failed: %v\n%s", err, stderr.String())
	}
}

func TestAggregatesErrorsAcrossPackages(t *testing.T) {
	moduleDir := writeModule(t, map[string]string{
		"go.mod": "module example.com/multi\n\ngo 1.22.0\n",
		"a/a.go": `package a

type A struct{ ID int }
type B struct{ Missing string }

//convgen: -s A -d B -fn BFromA
`,
		"b/b.go": `package b

type A struct{ Count int }
type B struct{ Count string }

//convgen: -s A -d B -fn BFromA
`,
	})
	var stderr bytes.Buffer
	err := Run([]string{filepath.Join(moduleDir, "./...")}, &stderr)
	if err == nil {
		t.Fatal("expected aggregated errors")
	}
	text := err.Error()
	if !strings.Contains(text, "a/a.go:6:") || !strings.Contains(text, "b/b.go:6:") {
		t.Fatalf("errors were not aggregated:\n%s", text)
	}
}

func copyFixture(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		writeFile(t, filepath.Join(dir, name), content)
	}
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
