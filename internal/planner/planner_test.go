package planner

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/plinth-go/convgen/internal/parser"
	"github.com/plinth-go/convgen/internal/resolver"
)

func TestBuildFullPlan(t *testing.T) {
	plan := build(t, parser.Options{
		SrcType:  "db.User",
		DstType:  "api.User",
		FuncName: "apiUserFull",
		TypeMode: parser.TypeValVal,
		Map: map[string]string{
			"DisplayName": "Name",
			"Avatar":      "Profile.Avatar",
		},
		Conv: map[string]string{
			"CreatedAt": "toTime",
			"UpdatedAt": "toTime",
		},
		Deep: map[string]bool{
			"Tags": true, "Metadata": true, "Ptr": true,
		},
		Ignore: map[string]bool{"InternalID": true},
	})
	if plan.SrcShort != "db.User" || plan.DstShort != "api.User" || plan.InPtr || plan.OutPtr {
		t.Fatalf("unexpected plan header: %#v", plan)
	}
	if len(plan.Assigns) != 10 {
		t.Fatalf("assignments = %d, want 10: %#v", len(plan.Assigns), plan.Assigns)
	}
	byField := make(map[string]string)
	for _, assign := range plan.Assigns {
		byField[assign.DstField] = assign.SrcExpr
	}
	if byField["DisplayName"] != "src.Name" || byField["Avatar"] != "src.Profile.Avatar" {
		t.Fatalf("map assignments = %#v", byField)
	}
	if byField["CreatedAt"] != "toTime(src.CreatedAt)" {
		t.Fatalf("conv assignment = %q", byField["CreatedAt"])
	}
	if !strings.HasPrefix(byField["Tags"], "append([]string(nil)") || !strings.Contains(byField["Metadata"], "func() map[string]string") || !strings.HasPrefix(byField["Ptr"], "func() *int") {
		t.Fatalf("deep assignments = %#v", byField)
	}
}

func TestBuildRequiresIgnoreForUnexportedTarget(t *testing.T) {
	_, err := tryBuild(parser.Options{
		SrcType:  "db.Hidden",
		DstType:  "api.Hidden",
		FuncName: "apiHidden",
		TypeMode: parser.TypeValVal,
	})
	if err == nil || !strings.Contains(err.Error(), "未赋值字段") || !strings.Contains(err.Error(), "internal（跨包未导出") {
		t.Fatalf("error = %v", err)
	}

	plan := build(t, parser.Options{
		SrcType:  "db.Hidden",
		DstType:  "api.Hidden",
		FuncName: "apiHidden",
		TypeMode: parser.TypeValVal,
		Ignore:   map[string]bool{"internal": true},
	})
	if len(plan.Assigns) != 1 || plan.Assigns[0].DstField != "Name" {
		t.Fatalf("assignments = %#v", plan.Assigns)
	}
}

func TestBuildReportsAllUnassignedFields(t *testing.T) {
	_, err := tryBuild(parser.Options{
		SrcType:  "db.Base",
		DstType:  "api.User",
		FuncName: "apiUserFromBase",
		TypeMode: parser.TypeValVal,
	})
	if err == nil {
		t.Fatal("expected unassigned fields")
	}
	for _, field := range []string{"DisplayName", "Avatar", "CreatedAt", "UpdatedAt", "Tags", "Metadata", "Ptr", "InternalID"} {
		if !strings.Contains(err.Error(), field) {
			t.Fatalf("error does not list %s: %v", field, err)
		}
	}
}

func TestBuildPointerModes(t *testing.T) {
	plan := build(t, parser.Options{
		SrcType:  "api.Base",
		DstType:  "db.Base",
		FuncName: "dbBaseFromAPI",
		TypeMode: parser.TypePtrPtr,
	})
	if !plan.InPtr || !plan.OutPtr {
		t.Fatalf("pointer flags = %t/%t", plan.InPtr, plan.OutPtr)
	}
}

func TestBuildQualifiedAndTemplateConversions(t *testing.T) {
	plan := build(t, parser.Options{
		SrcType:  "db.User",
		DstType:  "api.User",
		FuncName: "apiUserFromExpressions",
		TypeMode: parser.TypeValVal,
		Map: map[string]string{
			"DisplayName": "Name",
			"Avatar":      "Profile.Avatar",
		},
		Conv: map[string]string{
			"Name":        "strings.TrimSpace",
			"DisplayName": "strings.TrimSpace($)",
			"CreatedAt":   `strings.ReplaceAll($, " ", "-")`,
			"UpdatedAt":   `strings.ReplaceAll($, " ", "_")`,
		},
		Ignore: map[string]bool{"InternalID": true},
	})
	byField := make(map[string]string)
	for _, assign := range plan.Assigns {
		byField[assign.DstField] = assign.SrcExpr
	}
	if byField["Name"] != "strings.TrimSpace(src.Name)" {
		t.Fatalf("qualified conv = %q", byField["Name"])
	}
	if byField["DisplayName"] != "strings.TrimSpace(src.Name)" {
		t.Fatalf("template conv = %q", byField["DisplayName"])
	}
	if byField["CreatedAt"] != `strings.ReplaceAll(src.CreatedAt, " ", "-")` {
		t.Fatalf("nested comma conv = %q", byField["CreatedAt"])
	}
}

func TestBuildErrors(t *testing.T) {
	tests := []struct {
		name string
		opts parser.Options
		want string
	}{
		{
			name: "pointer intermediate",
			opts: parser.Options{SrcType: "db.PointerSource", DstType: "api.PointerTarget", FuncName: "bad", TypeMode: parser.TypeValVal, Map: map[string]string{"Avatar": "Profile.Avatar"}},
			want: "中间段 Profile 是指针类型",
		},
		{
			name: "missing intermediate",
			opts: parser.Options{SrcType: "db.Base", DstType: "api.Base", FuncName: "bad", TypeMode: parser.TypeValVal, Map: map[string]string{"ID": "Missing.ID"}},
			want: "中间段 Missing 不存在",
		},
		{
			name: "source unexported",
			opts: parser.Options{SrcType: "db.SecretSource", DstType: "api.SecretTarget", FuncName: "bad", TypeMode: parser.TypeValVal, Map: map[string]string{"Value": "secret"}},
			want: "源字段 secret 未导出",
		},
		{
			name: "type mismatch",
			opts: parser.Options{SrcType: "db.BadSource", DstType: "api.BadTarget", FuncName: "bad", TypeMode: parser.TypeValVal},
			want: "类型不匹配",
		},
		{
			name: "deep value",
			opts: parser.Options{SrcType: "db.CountSource", DstType: "api.CountTarget", FuncName: "bad", TypeMode: parser.TypeValVal, Deep: map[string]bool{"Count": true}},
			want: "只支持 slice/map/pointer",
		},
		{
			name: "generic",
			opts: parser.Options{SrcType: "db.Base", DstType: "db.Box", FuncName: "bad", TypeMode: parser.TypeValVal},
			want: "泛型类型 Box 不受支持",
		},
		{
			name: "missing conv",
			opts: parser.Options{SrcType: "db.Base", DstType: "api.Base", FuncName: "bad", TypeMode: parser.TypeValVal, Conv: map[string]string{"ID": "missing"}},
			want: "转换函数 missing 不存在",
		},
		{
			name: "missing qualified conv",
			opts: parser.Options{SrcType: "db.Base", DstType: "api.Base", FuncName: "bad", TypeMode: parser.TypeValVal, Conv: map[string]string{"Name": "strings.NotAFunction"}},
			want: "包 strings 中找不到 NotAFunction",
		},
		{
			name: "missing local expression function",
			opts: parser.Options{SrcType: "db.Base", DstType: "api.Base", FuncName: "bad", TypeMode: parser.TypeValVal, Conv: map[string]string{"Name": "missing($)"}},
			want: "当前包中找不到函数 missing",
		},
		{
			name: "missing source",
			opts: parser.Options{SrcType: "db.Base", DstType: "api.Rich", FuncName: "bad", TypeMode: parser.TypeValVal},
			want: "存在未赋值字段: DisplayName",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tryBuild(tt.opts)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
			if !strings.Contains(err.Error(), "internal/controller/user.go:10:") {
				t.Fatalf("error lacks location: %v", err)
			}
		})
	}
}

func build(t *testing.T, opts parser.Options) Plan {
	t.Helper()
	plan, err := tryBuild(opts)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func tryBuild(opts parser.Options) (Plan, error) {
	dir := filepath.Join("..", "..", "testdata", "input", "internal", "controller")
	pkg, err := resolver.LoadPackage(dir)
	if err != nil {
		return Plan{}, err
	}
	res := resolver.New(pkg, dir)
	if opts.File == "" {
		opts.File = "internal/controller/user.go"
	}
	if opts.Line == 0 {
		opts.Line = 10
	}
	imports := NewImportSet(pkg.Types)
	return Build(opts, pkg, res, imports)
}
