package parser

import (
	"strings"
	"testing"

	"github.com/plinth-go/convgen/internal/scanner"
)

func TestParseFullDeclaration(t *testing.T) {
	opts, err := Parse(decl(`-s db.User -d api.User -fn apiUserFull -map DisplayName=Name, Avatar=Profile.Avatar -conv CreatedAt=toTime, UpdatedAt=toTime -deep Tags, Metadata -ignore InternalID`))
	if err != nil {
		t.Fatal(err)
	}
	if opts.SrcType != "db.User" || opts.DstType != "api.User" || opts.FuncName != "apiUserFull" || opts.TypeMode != TypeValVal {
		t.Fatalf("unexpected options: %#v", opts)
	}
	if opts.Map["Avatar"] != "Profile.Avatar" || opts.Conv["UpdatedAt"] != "toTime" || !opts.Deep["Tags"] || !opts.Ignore["InternalID"] {
		t.Fatalf("unexpected maps: %#v", opts)
	}
}

func TestTokenizeMultivalueWhitespace(t *testing.T) {
	tokens, err := Tokenize(`-s A -d B -map X=Y,  Z=W.Q  -deep Tags,Slices`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-s", "A", "-d", "B", "-map", "X=Y, Z=W.Q", "-deep", "Tags, Slices"}
	if strings.Join(tokens, "|") != strings.Join(want, "|") {
		t.Fatalf("tokens = %#v, want %#v", tokens, want)
	}
}

func TestTokenizeConvExpressionKeepsNestedCommas(t *testing.T) {
	tokens, err := Tokenize(`-s A -d B -conv Name=strings.ReplaceAll($, "a,b", "-") -deep Tags`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-s", "A", "-d", "B", "-conv", `Name=strings.ReplaceAll($, "a,b", "-")`, "-deep", "Tags"}
	if strings.Join(tokens, "|") != strings.Join(want, "|") {
		t.Fatalf("tokens = %#v, want %#v", tokens, want)
	}
}

func TestTokenizeConvExpressionIgnoresFlagLikeLiterals(t *testing.T) {
	tokens, err := Tokenize(`-s A -d B -conv Name=strings.ReplaceAll($, " -x ", "") -deep Tags`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-s", "A", "-d", "B", "-conv", `Name=strings.ReplaceAll($, " -x ", "")`, "-deep", "Tags"}
	if strings.Join(tokens, "|") != strings.Join(want, "|") {
		t.Fatalf("tokens = %#v, want %#v", tokens, want)
	}
}

func TestParseConvExpression(t *testing.T) {
	opts, err := Parse(decl(`-s A -d B -conv CreatedAt=time.Unix(0, $).UTC(), Name=strings.TrimSpace($)`))
	if err != nil {
		t.Fatal(err)
	}
	if opts.Conv["CreatedAt"] != "time.Unix(0, $).UTC()" || opts.Conv["Name"] != "strings.TrimSpace($)" {
		t.Fatalf("conv = %#v", opts.Conv)
	}
}

func TestParseRepeatedConvFlags(t *testing.T) {
	opts, err := Parse(decl(`-s A -d B -conv Name=strings.TrimSpace -conv CreatedAt=$.UTC().Format(time.RFC3339) -conv Tags=strings.Join($, ",")`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"Name":      "strings.TrimSpace",
		"CreatedAt": "$.UTC().Format(time.RFC3339)",
		"Tags":      `strings.Join($, ",")`,
	}
	for field, expression := range want {
		if opts.Conv[field] != expression {
			t.Fatalf("conv[%s] = %q, want %q", field, opts.Conv[field], expression)
		}
	}
}

func TestParseRejectsDuplicateFieldsWithLine(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  string
	}{
		{
			name:  "conv across flags",
			lines: []string{"-s A -d B -conv Name=strings.TrimSpace", "    -conv Name=strings.ToLower"},
			want:  "sample.go:13: -conv 字段 Name 重复（首次出现于 sample.go:12）",
		},
		{
			name:  "conv in one value",
			lines: []string{"-s A -d B -conv Name=strings.TrimSpace, Name=strings.ToLower"},
			want:  `sample.go:12: -conv 字段 Name 重复`,
		},
		{
			name:  "map",
			lines: []string{"-s A -d B -map Name=First, Name=Second"},
			want:  "sample.go:12: -map 字段 Name 重复",
		},
		{
			name:  "ignore",
			lines: []string{"-s A -d B -ignore Name, Name"},
			want:  "sample.go:12: -ignore 字段 Name 重复",
		},
		{
			name:  "deep",
			lines: []string{"-s A -d B -deep Tags, Tags"},
			want:  "sample.go:12: -deep 字段 Tags 重复",
		},
		{
			name:  "single value flag",
			lines: []string{"-s A -d B", "    -s A"},
			want:  "sample.go:13: 参数 -s 重复（首次出现于 sample.go:12）",
		},
		{
			name:  "ignore conv conflict",
			lines: []string{"-s A -d B -conv Name=strings.TrimSpace", "    -ignore Name"},
			want:  "sample.go:13: -ignore 与 -conv 不能同时作用于字段 Name（-conv 位于 sample.go:12）",
		},
		{
			name:  "unknown flag on continuation",
			lines: []string{"-s A -d B", "    -unknown X"},
			want:  "sample.go:13: 未知参数 -unknown",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(declLines(12, tt.lines...))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestParseDerivedNames(t *testing.T) {
	tests := []struct {
		mode string
		want string
	}{
		{TypeValVal, "UserFromRecord"},
		{TypeValPtr, "UserPtrFromRecord"},
		{TypePtrVal, "UserFromRecordPtr"},
		{TypePtrPtr, "UserPtrFromRecordPtr"},
	}
	for _, tt := range tests {
		opts, err := Parse(decl(`-s db.Record -d api.User -type ` + tt.mode))
		if err != nil {
			t.Fatal(err)
		}
		if opts.FuncName != tt.want {
			t.Errorf("mode %s: fn = %s, want %s", tt.mode, opts.FuncName, tt.want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"missing source", `-d User`, "缺少 -s"},
		{"missing destination", `-s User`, "缺少 -d"},
		{"unknown flag", `-s User -d User -nil`, "未知参数 -nil"},
		{"missing value", `-s User -d`, "缺少值"},
		{"invalid type", `-s User -d User -type ptr`, "-type"},
		{"single spaced", `-s foo bar -d User`, "不能包含空格"},
		{"ignore map conflict", `-s User -d User -map X=Y -ignore X`, "-ignore 与 -map"},
		{"ignore conv conflict", `-s User -d User -conv X=toX -ignore X`, "-ignore 与 -conv"},
		{"ignore deep conflict", `-s User -d User -deep X -ignore X`, "-ignore 与 -deep"},
		{"conv deep conflict", `-s User -d User -conv X=toX -deep X`, "-deep 与 -conv"},
		{"bad map pair", `-s User -d User -map X=Y=Z`, "只能包含一个 ="},
		{"unbalanced conv", `-s User -d User -conv X=foo($`, "括号不匹配"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(decl(tt.content))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
			if !strings.Contains(err.Error(), "sample.go:12:") {
				t.Fatalf("error lacks location: %v", err)
			}
		})
	}
}

func TestParseAllowsMapAndConvOnSameField(t *testing.T) {
	opts, err := Parse(decl(`-s User -d User -map DisplayName=Name -conv DisplayName=normalizeName`))
	if err != nil {
		t.Fatal(err)
	}
	if opts.Map["DisplayName"] != "Name" || opts.Conv["DisplayName"] != "normalizeName" {
		t.Fatalf("unexpected options: %#v", opts)
	}
}

func decl(content string) scanner.RawDecl {
	return scanner.RawDecl{
		File:     "sample.go",
		Line:     12,
		Content:  content,
		RawLines: []string{"convgen: " + content},
	}
}

func declLines(line int, lines ...string) scanner.RawDecl {
	content := lines[0]
	starts := []int{0}
	rawLines := []string{"convgen: " + lines[0]}
	for _, next := range lines[1:] {
		next = strings.TrimSpace(next)
		starts = append(starts, len(content)+1)
		content += " " + next
		rawLines = append(rawLines, "    "+next)
	}
	return scanner.RawDecl{
		File:       "sample.go",
		Line:       line,
		Content:    content,
		RawLines:   rawLines,
		LineStarts: starts,
	}
}
