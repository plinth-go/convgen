// Package resolver resolves convgen type expressions with go/packages.
package resolver

import (
	"fmt"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/plinth-go/convgen/internal/scanner"
	"golang.org/x/tools/go/packages"
)

const loadMode = packages.NeedName |
	packages.NeedFiles |
	packages.NeedCompiledGoFiles |
	packages.NeedImports |
	packages.NeedDeps |
	packages.NeedTypes |
	packages.NeedSyntax |
	packages.NeedTypesInfo |
	packages.NeedModule

// Resolved is a named type and the package that provides it.
type Resolved struct {
	Package  *types.Package
	TypeName *types.TypeName
}

// Resolver resolves type expressions from one package load.
type Resolver struct {
	dir      string
	current  *packages.Package
	imports  map[string]string
	packages map[string]*types.Package
}

// LoadPackage loads one Go package directory.
func LoadPackage(dir string) (*packages.Package, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("解析目录 %s 失败: %w", dir, err)
	}
	dir = absDir

	pkg, loadErr := loadPackage(dir, nil, true)
	if loadErr == nil {
		return pkg, nil
	}
	overlay, overlayErr := generatedOverlay(dir)
	if overlayErr != nil {
		return nil, overlayErr
	}
	if len(overlay) == 0 {
		return nil, loadErr
	}
	fallback, fallbackErr := loadPackage(dir, overlay, false)
	if fallbackErr == nil {
		return fallback, nil
	}
	return nil, fallbackErr
}

func loadPackage(dir string, overlay map[string][]byte, allowGeneratedErrors bool) (*packages.Package, error) {
	cfg := &packages.Config{Mode: loadMode, Dir: dir, Overlay: overlay}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return nil, fmt.Errorf("加载包 %s 失败: %w", dir, err)
	}
	if len(pkgs) != 1 {
		return nil, fmt.Errorf("目录 %s 应包含一个非测试包，实际得到 %d 个", dir, len(pkgs))
	}
	pkg := pkgs[0]
	if errs := formatErrors(pkg.Errors); errs != "" {
		if !allowGeneratedErrors || !onlyGeneratedErrors(pkg.Errors) {
			return nil, fmt.Errorf("加载包 %s 失败:\n%s", dir, errs)
		}
	}
	if pkg.Types == nil || pkg.Types.Scope() == nil {
		return nil, fmt.Errorf("加载包 %s 失败: 缺少类型信息", dir)
	}
	return pkg, nil
}

func onlyGeneratedErrors(errs []packages.Error) bool {
	for _, item := range errs {
		if item.Pos == "" {
			return false
		}
		file := item.Pos
		for i := 0; i < 2; i++ {
			if i := strings.LastIndexByte(file, ':'); i >= 0 {
				file = file[:i]
			}
		}
		if filepath.Base(file) != "conv_gen.go" {
			return false
		}
	}
	return len(errs) > 0
}

// generatedOverlay hides an old conv_gen.go from type checking. This lets a
// project regenerate after its source structs change, even if the stale output
// no longer compiles.
func generatedOverlay(dir string) (map[string][]byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var generatedPath string
	packageName := ""
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		filePath := filepath.Join(dir, name)
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, err
		}
		isGenerated := scanner.IsGenerated(data)
		if name == "conv_gen.go" && isGenerated {
			generatedPath = filePath
		}
		if packageName != "" {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filePath, data, parser.PackageClauseOnly)
		if err != nil || file.Name == nil {
			continue
		}
		if !isGenerated || name == "conv_gen.go" {
			packageName = file.Name.Name
		}
	}
	if generatedPath == "" || packageName == "" {
		return nil, nil
	}
	return map[string][]byte{
		generatedPath: []byte(scanner.GeneratedMarker + "\n\npackage " + packageName + "\n"),
	}, nil
}

// New creates a resolver for a loaded current package.
func New(current *packages.Package, dir string) *Resolver {
	r := &Resolver{
		dir:      dir,
		current:  current,
		imports:  make(map[string]string),
		packages: make(map[string]*types.Package),
	}
	r.imports = r.collectImportAliases()
	return r
}

// Current returns the package containing the declaration.
func (r *Resolver) Current() *types.Package {
	if r.current == nil {
		return nil
	}
	return r.current.Types
}

// Resolve resolves User, alias.User, or example.com/mod/pkg.User.
func (r *Resolver) Resolve(expr string) (Resolved, error) {
	if r.current == nil || r.current.Types == nil {
		return Resolved{}, fmt.Errorf("缺少当前包类型信息")
	}
	if token.IsIdentifier(expr) {
		return r.lookup(r.current.Types, expr)
	}

	i := strings.LastIndexByte(expr, '.')
	if i <= 0 || i == len(expr)-1 {
		return Resolved{}, fmt.Errorf("类型 %q 格式非法，应为 Type、pkg.Type 或完整 import path.Type", expr)
	}
	left, right := expr[:i], expr[i+1:]
	if !token.IsIdentifier(right) {
		return Resolved{}, fmt.Errorf("类型 %q 的类型名 %q 不是合法标识符", expr, right)
	}
	if strings.Contains(left, "/") {
		pkg, err := r.load(left)
		if err != nil {
			return Resolved{}, err
		}
		return r.lookup(pkg, right)
	}
	if !token.IsIdentifier(left) {
		return Resolved{}, fmt.Errorf("类型 %q 的包别名 %q 不是合法标识符", expr, left)
	}
	importPath, ok := r.imports[left]
	if !ok {
		return Resolved{}, fmt.Errorf("找不到包 %s（当前文件未 import）", left)
	}
	if pkg := r.importedPackage(importPath); pkg != nil {
		return r.lookup(pkg, right)
	}
	pkg, err := r.load(importPath)
	if err != nil {
		return Resolved{}, err
	}
	return r.lookup(pkg, right)
}

// Import resolves a package qualifier used by a conversion expression. It
// prefers aliases imported by the current package and falls back to loading
// the qualifier as an import path.
func (r *Resolver) Import(alias string) (*types.Package, error) {
	if !token.IsIdentifier(alias) {
		return nil, fmt.Errorf("包标识符 %q 非法", alias)
	}
	if importPath, ok := r.imports[alias]; ok {
		pkg, err := r.load(importPath)
		if err != nil {
			return nil, fmt.Errorf("加载导入包 %s 失败: %w", importPath, err)
		}
		return pkg, nil
	}
	pkg, err := r.load(alias)
	if err != nil {
		return nil, fmt.Errorf("找不到包 %s（当前包未 import，且无法按 import path 加载）", alias)
	}
	return pkg, nil
}

func (r *Resolver) lookup(pkg *types.Package, name string) (Resolved, error) {
	obj := pkg.Scope().Lookup(name)
	if obj == nil {
		if pkg.Path() == r.current.Types.Path() {
			return Resolved{}, fmt.Errorf("当前包中找不到类型 %s", name)
		}
		return Resolved{}, fmt.Errorf("包 %s 中找不到类型 %s", pkg.Path(), name)
	}
	typeName, ok := obj.(*types.TypeName)
	if !ok {
		return Resolved{}, fmt.Errorf("%s 不是类型名", name)
	}
	return Resolved{Package: pkg, TypeName: typeName}, nil
}

func (r *Resolver) collectImportAliases() map[string]string {
	aliases := make(map[string]string)
	for _, file := range r.current.Syntax {
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			if spec.Name != nil {
				alias := spec.Name.Name
				if alias == "_" || alias == "." {
					continue
				}
				aliases[alias] = importPath
				continue
			}
			name := path.Base(importPath)
			if pkg := r.importedPackage(importPath); pkg != nil && pkg.Name() != "" {
				name = pkg.Name()
			}
			aliases[name] = importPath
		}
	}
	return aliases
}

func (r *Resolver) importedPackage(importPath string) *types.Package {
	if r.current == nil || r.current.Types == nil {
		return nil
	}
	for _, pkg := range r.current.Types.Imports() {
		if pkg.Path() == importPath {
			return pkg
		}
	}
	return nil
}

func (r *Resolver) load(importPath string) (*types.Package, error) {
	if importPath == r.current.Types.Path() {
		return r.current.Types, nil
	}
	if pkg := r.packages[importPath]; pkg != nil {
		return pkg, nil
	}
	if pkg := r.importedPackage(importPath); pkg != nil {
		r.packages[importPath] = pkg
		return pkg, nil
	}

	cfg := &packages.Config{Mode: loadMode, Dir: r.dir}
	pkgs, err := packages.Load(cfg, importPath)
	if err != nil {
		return nil, fmt.Errorf("加载包 %s 失败: %w", importPath, err)
	}
	for _, candidate := range pkgs {
		if candidate.PkgPath != importPath {
			continue
		}
		if errs := formatErrors(candidate.Errors); errs != "" {
			return nil, fmt.Errorf("加载包 %s 失败:\n%s", importPath, errs)
		}
		if candidate.Types != nil && candidate.Types.Scope() != nil {
			r.packages[importPath] = candidate.Types
			return candidate.Types, nil
		}
	}
	return nil, fmt.Errorf("找不到包 %s", importPath)
}

func formatErrors(errs []packages.Error) string {
	lines := make([]string, 0, len(errs))
	for _, err := range errs {
		if err.Pos == "" {
			lines = append(lines, err.Msg)
			continue
		}
		lines = append(lines, err.Pos+": "+err.Msg)
	}
	return strings.Join(lines, "\n")
}
