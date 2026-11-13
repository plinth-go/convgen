// Package runner orchestrates scanning, planning, generation, and output.
package runner

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/plinth-go/convgen/internal/differ"
	"github.com/plinth-go/convgen/internal/generator"
	"github.com/plinth-go/convgen/internal/parser"
	"github.com/plinth-go/convgen/internal/planner"
	"github.com/plinth-go/convgen/internal/resolver"
	"github.com/plinth-go/convgen/internal/scanner"
)

const outputName = "conv_gen.go"

type directoryState struct {
	dir       string
	decls     []scanner.RawDecl
	hasOutput bool
	hasDecls  bool
}

// Run executes convgen for directory patterns such as ./... or ./internal/api.
func Run(args []string, stderr io.Writer) error {
	roots, err := rootsFromArgs(args)
	if err != nil {
		return err
	}

	states, err := discover(roots)
	if err != nil {
		return err
	}
	var errs []error
	for _, state := range states {
		if err := runPackage(state, stderr); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func rootsFromArgs(args []string) ([]string, error) {
	if len(args) == 0 {
		args = []string{"./..."}
	}
	seen := make(map[string]bool)
	var roots []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return nil, fmt.Errorf("不支持参数 %s；请传入 ./... 或目录", arg)
		}
		root := strings.TrimSuffix(arg, "/...")
		if root == "" {
			root = "."
		}
		abs, err := filepath.Abs(root)
		if err != nil {
			return nil, fmt.Errorf("解析路径 %s 失败: %w", root, err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("访问路径 %s 失败: %w", root, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%s 不是目录", root)
		}
		if !seen[abs] {
			seen[abs] = true
			roots = append(roots, abs)
		}
	}
	sort.Strings(roots)
	return roots, nil
}

func discover(roots []string) ([]directoryState, error) {
	byDir := make(map[string]*directoryState)
	seenFiles := make(map[string]bool)
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if path != root && skipDir(entry.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				return nil
			}
			if seenFiles[path] {
				return nil
			}
			seenFiles[path] = true
			decls, generated, err := scanner.ScanFileInfo(path)
			if err != nil {
				return fmt.Errorf("扫描 %s 失败: %w", path, err)
			}
			state := byDir[filepath.Dir(path)]
			if state == nil {
				state = &directoryState{dir: filepath.Dir(path)}
				byDir[filepath.Dir(path)] = state
			}
			if generated && entry.Name() == outputName {
				state.hasOutput = true
			}
			if len(decls) > 0 {
				state.decls = append(state.decls, decls...)
				state.hasDecls = true
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	states := make([]directoryState, 0, len(byDir))
	for _, state := range byDir {
		if !state.hasDecls && !state.hasOutput {
			continue
		}
		sort.Slice(state.decls, func(i, j int) bool {
			if state.decls[i].File == state.decls[j].File {
				return state.decls[i].Line < state.decls[j].Line
			}
			return state.decls[i].File < state.decls[j].File
		})
		states = append(states, *state)
	}
	sort.Slice(states, func(i, j int) bool { return states[i].dir < states[j].dir })
	return states, nil
}

func skipDir(name string) bool {
	switch name {
	case "vendor", ".git", "testdata", "node_modules":
		return true
	default:
		return false
	}
}

func runPackage(state directoryState, stderr io.Writer) error {
	moduleRoot, err := findModuleRoot(state.dir)
	if err != nil {
		return err
	}
	displayDir, _ := filepath.Rel(moduleRoot, state.dir)
	displayDir = filepath.ToSlash(displayDir)

	outputPath := filepath.Join(state.dir, outputName)
	oldSource, exists, err := readOptional(outputPath)
	if err != nil {
		return fmt.Errorf("读取 %s 失败: %w", outputPath, err)
	}
	if exists && !scanner.IsGenerated(oldSource) {
		return fmt.Errorf("%s: 已存在但不是 convgen 生成的文件，请手动删除后再运行", displayPath(moduleRoot, outputPath))
	}

	pkg, err := resolver.LoadPackage(state.dir)
	if err != nil {
		return err
	}
	res := resolver.New(pkg, state.dir)
	imports := planner.NewImportSet(pkg.Types)
	var errs []error
	plans := make([]planner.Plan, 0, len(state.decls))
	for _, original := range state.decls {
		decl := original
		rel, err := filepath.Rel(moduleRoot, decl.File)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s:%d: 计算源文件相对路径失败: %w", decl.File, decl.Line, err))
			continue
		}
		decl.File = filepath.ToSlash(rel)
		opts, err := parser.Parse(decl)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		plan, err := planner.Build(opts, pkg, res, imports)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		plans = append(plans, plan)
	}
	errs = append(errs, duplicateErrors(plans)...)
	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	newSource, err := generator.Generate(pkg.Types.Name(), plans, imports.Imports())
	if err != nil {
		return fmt.Errorf("%s: %w", displayDir, err)
	}
	added, removed, err := differ.Diff(oldSource, newSource)
	if err != nil {
		return fmt.Errorf("比较 %s 的函数变更失败: %w", displayPath(moduleRoot, outputPath), err)
	}
	for _, name := range added {
		fmt.Fprintf(stderr, "[%s] + %s\n", pkg.Types.Name(), name)
	}
	for _, name := range removed {
		fmt.Fprintf(stderr, "[%s] - %s\n", pkg.Types.Name(), name)
	}
	if exists && string(oldSource) == string(newSource) {
		return nil
	}
	if err := writeAtomic(outputPath, newSource); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", displayPath(moduleRoot, outputPath), err)
	}
	return nil
}

func duplicateErrors(plans []planner.Plan) []error {
	seen := make(map[string]planner.Plan)
	var errs []error
	for _, plan := range plans {
		if previous, ok := seen[plan.FuncName]; ok {
			errs = append(errs, fmt.Errorf("%s:%d: 生成函数名 %s 与 %s:%d 冲突", plan.SrcFile, plan.SrcLine, plan.FuncName, previous.SrcFile, previous.SrcLine))
			continue
		}
		seen[plan.FuncName] = plan
	}
	return errs
}

func findModuleRoot(dir string) (string, error) {
	current := dir
	for {
		if info, err := os.Stat(filepath.Join(current, "go.mod")); err == nil && !info.IsDir() {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("目录 %s 不在 Go module 中（找不到 go.mod）", dir)
		}
		current = parent
	}
}

func readOptional(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return data, true, nil
	}
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	return nil, false, err
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".conv_gen-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func displayPath(moduleRoot, path string) string {
	rel, err := filepath.Rel(moduleRoot, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}
