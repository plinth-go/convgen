// Package generator renders a package-level conv_gen.go file.
package generator

import (
	"fmt"
	"go/format"
	"sort"
	"strings"

	"github.com/plinth-go/convgen/internal/planner"
	"github.com/plinth-go/convgen/internal/scanner"
)

// Generate renders and formats the generated file for one package.
func Generate(pkgName string, plans []planner.Plan, imports []planner.Import) ([]byte, error) {
	ordered := append([]planner.Plan(nil), plans...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].FuncName < ordered[j].FuncName })

	seen := make(map[string]planner.Plan, len(ordered))
	for _, plan := range ordered {
		if previous, ok := seen[plan.FuncName]; ok {
			return nil, fmt.Errorf("%s:%d: 生成函数名 %s 与 %s:%d 冲突", plan.SrcFile, plan.SrcLine, plan.FuncName, previous.SrcFile, previous.SrcLine)
		}
		seen[plan.FuncName] = plan
	}

	var b strings.Builder
	b.WriteString(scanner.GeneratedMarker)
	b.WriteString("\n\npackage ")
	b.WriteString(pkgName)
	b.WriteString("\n")
	if len(imports) > 0 {
		b.WriteString("\nimport (\n")
		writeImportGroup(&b, imports, true)
		writeImportGroup(&b, imports, false)
		b.WriteString(")\n")
	}

	for _, plan := range ordered {
		b.WriteByte('\n')
		if err := writeHeader(&b, plan); err != nil {
			return nil, err
		}
		b.WriteByte('\n')
		writeFunction(&b, plan)
	}

	source := []byte(b.String())
	formatted, err := format.Source(source)
	if err != nil {
		prefix := ""
		if len(ordered) > 0 {
			prefix = fmt.Sprintf("%s:%d: ", ordered[0].SrcFile, ordered[0].SrcLine)
		}
		return nil, fmt.Errorf("%s格式化包 %s 的生成代码失败: %w\n%s", prefix, pkgName, err, source)
	}
	return formatted, nil
}

func writeImportGroup(b *strings.Builder, imports []planner.Import, stdlib bool) {
	wrote := false
	for _, item := range imports {
		if isStdlib(item.Path) != stdlib {
			continue
		}
		if stdlib && !wrote && b.Len() > 0 && b.String()[b.Len()-1] != '\n' {
			b.WriteByte('\n')
		}
		if !stdlib && !wrote && hasStdlib(imports) {
			b.WriteByte('\n')
		}
		b.WriteByte('\t')
		if item.Alias != "" && item.Alias != item.Name {
			b.WriteString(item.Alias)
			b.WriteByte(' ')
		}
		fmt.Fprintf(b, "%q\n", item.Path)
		wrote = true
	}
}

func hasStdlib(imports []planner.Import) bool {
	for _, item := range imports {
		if isStdlib(item.Path) {
			return true
		}
	}
	return false
}

func isStdlib(importPath string) bool {
	first, _, _ := strings.Cut(importPath, "/")
	return !strings.Contains(first, ".")
}

func writeHeader(b *strings.Builder, plan planner.Plan) error {
	if len(plan.RawLines) == 0 {
		return fmt.Errorf("%s:%d: %s 缺少原始声明用于回显", plan.SrcFile, plan.SrcLine, plan.FuncName)
	}
	for i, raw := range plan.RawLines {
		content := strings.TrimSpace(raw)
		if i == 0 {
			content = strings.TrimSpace(strings.TrimPrefix(content, "convgen:"))
			if content == "" {
				b.WriteString("//convgen:\n")
			} else {
				fmt.Fprintf(b, "//convgen: %s\n", content)
			}
			continue
		}
		fmt.Fprintf(b, "//    %s\n", content)
	}
	fmt.Fprintf(b, "// Source: %s\n", plan.SrcFile)
	return nil
}

func writeFunction(b *strings.Builder, plan planner.Plan) {
	fmt.Fprintf(b, "func %s(src ", plan.FuncName)
	if plan.InPtr {
		b.WriteByte('*')
	}
	b.WriteString(plan.SrcShort)
	b.WriteString(") ")
	if plan.OutPtr {
		b.WriteByte('*')
		b.WriteString(plan.DstShort)
		b.WriteString(" {\n")
	} else {
		fmt.Fprintf(b, "(dst %s) {\n", plan.DstShort)
	}

	if plan.InPtr {
		b.WriteString("\tif src == nil {\n")
		if plan.OutPtr {
			b.WriteString("\t\treturn nil\n")
		} else {
			b.WriteString("\t\treturn\n")
		}
		b.WriteString("\t}\n")
	}
	if plan.OutPtr {
		fmt.Fprintf(b, "\tdst := &%s{}\n", plan.DstShort)
	}
	for _, assign := range plan.Assigns {
		fmt.Fprintf(b, "\tdst.%s = %s\n", assign.DstField, assign.SrcExpr)
	}
	if plan.OutPtr {
		b.WriteString("\treturn dst\n")
	} else {
		b.WriteString("\treturn\n")
	}
	b.WriteString("}\n")
}
