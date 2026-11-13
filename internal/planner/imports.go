package planner

import (
	"fmt"
	"go/types"
	"path"
	"sort"
)

// Import is one generated-file import.
type Import struct {
	Path  string
	Name  string
	Alias string
}

// ImportSet assigns deterministic aliases to packages used by generated code.
type ImportSet struct {
	current *types.Package
	aliases map[string]string
	names   map[string]string
	byAlias map[string]string
}

var reservedAliases = map[string]bool{
	"src": true,
	"dst": true,
	"k":   true,
	"v":   true,
	"m":   true,
}

// NewImportSet creates an import set for the current package.
func NewImportSet(current *types.Package) *ImportSet {
	s := &ImportSet{
		current: current,
		aliases: make(map[string]string),
		names:   make(map[string]string),
		byAlias: make(map[string]string),
	}
	if current != nil && current.Name() != "" {
		s.byAlias[current.Name()] = current.Path()
	}
	return s
}

// Alias returns the local qualifier for pkg, or "" for the current package.
func (s *ImportSet) Alias(pkg *types.Package) string {
	if pkg == nil || pkg.Path() == "" || (s.current != nil && pkg.Path() == s.current.Path()) {
		return ""
	}
	if alias, ok := s.aliases[pkg.Path()]; ok {
		return alias
	}

	base := pkg.Name()
	if base == "" || base == "." || base == "_" {
		base = path.Base(pkg.Path())
	}
	alias := base
	if reservedAliases[alias] {
		alias += "pkg"
	}
	for i := 2; ; i++ {
		if owner, ok := s.byAlias[alias]; !ok || owner == pkg.Path() {
			break
		}
		alias = fmt.Sprintf("%s%d", base, i)
	}
	s.aliases[pkg.Path()] = alias
	s.names[pkg.Path()] = pkg.Name()
	s.byAlias[alias] = pkg.Path()
	return alias
}

// Qualify renders a package-qualified identifier.
func (s *ImportSet) Qualify(pkg *types.Package, name string) string {
	if alias := s.Alias(pkg); alias != "" {
		return alias + "." + name
	}
	return name
}

// Render renders a type using this import set.
func (s *ImportSet) Render(t types.Type) string {
	return types.TypeString(t, func(pkg *types.Package) string {
		return s.Alias(pkg)
	})
}

// Imports returns the used imports sorted by path.
func (s *ImportSet) Imports() []Import {
	imports := make([]Import, 0, len(s.aliases))
	for importPath, alias := range s.aliases {
		name := s.names[importPath]
		item := Import{Path: importPath, Name: name}
		if alias != name {
			item.Alias = alias
		}
		imports = append(imports, item)
	}
	sort.Slice(imports, func(i, j int) bool { return imports[i].Path < imports[j].Path })
	return imports
}
