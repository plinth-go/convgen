// Package differ compares generated function names before and after generation.
package differ

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
)

// Diff returns added and removed top-level function names.
func Diff(oldSource, newSource []byte) (added, removed []string, err error) {
	oldNames, err := functionNames(oldSource)
	if err != nil {
		return nil, nil, err
	}
	newNames, err := functionNames(newSource)
	if err != nil {
		return nil, nil, err
	}
	for name := range newNames {
		if !oldNames[name] {
			added = append(added, name)
		}
	}
	for name := range oldNames {
		if !newNames[name] {
			removed = append(removed, name)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed, nil
}

func functionNames(source []byte) (map[string]bool, error) {
	names := make(map[string]bool)
	if len(source) == 0 {
		return names, nil
	}
	file, err := parser.ParseFile(token.NewFileSet(), "conv_gen.go", source, 0)
	if err != nil {
		return nil, err
	}
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil {
			names[fn.Name.Name] = true
		}
	}
	return names, nil
}
