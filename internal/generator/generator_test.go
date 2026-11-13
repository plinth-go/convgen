package generator

import (
	"strings"
	"testing"

	"github.com/plinth-go/convgen/internal/planner"
)

func TestGenerateGroupsStandardAndThirdPartyImports(t *testing.T) {
	source, err := Generate("sample", nil, []planner.Import{
		{Path: "example.com/project/model", Name: "model"},
		{Path: "strings", Name: "strings"},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if !strings.Contains(text, "\"strings\"\n\n\t\"example.com/project/model\"") {
		t.Fatalf("imports are not grouped:\n%s", text)
	}
}
