package planner

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"strings"

	"github.com/plinth-go/convgen/internal/resolver"
)

const convPlaceholder = "__convgen_source_value__"

func renderConv(spec, srcPath string, current *types.Package, res *resolver.Resolver, imports *ImportSet) (string, error) {
	prepared, count, err := prepareConvExpr(spec)
	if err != nil {
		return "", err
	}
	expr, err := parser.ParseExpr(prepared)
	if err != nil {
		return "", fmt.Errorf("表达式 %q 无效: %w", spec, err)
	}
	if count == 0 {
		fn, err := convFuncRef(expr, current, res, imports)
		if err != nil {
			return "", err
		}
		return fn + "(src." + srcPath + ")", nil
	}
	if err := rewriteConvPackages(expr, current, res, imports); err != nil {
		return "", err
	}
	if err := validateLocalCalls(expr, current); err != nil {
		return "", err
	}
	var rendered bytes.Buffer
	if err := format.Node(&rendered, token.NewFileSet(), expr); err != nil {
		return "", fmt.Errorf("表达式 %q 格式化失败: %w", spec, err)
	}
	return strings.ReplaceAll(rendered.String(), convPlaceholder, "src."+srcPath), nil
}

func validateLocalCalls(expr ast.Expr, current *types.Package) error {
	var firstErr error
	ast.Inspect(expr, func(node ast.Node) bool {
		if firstErr != nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fun.(*ast.Ident)
		if !ok || ident.Name == convPlaceholder {
			return true
		}
		if current != nil && current.Scope() != nil && current.Scope().Lookup(ident.Name) != nil {
			return true
		}
		if types.Universe.Lookup(ident.Name) != nil {
			return true
		}
		firstErr = fmt.Errorf("当前包中找不到函数 %s", ident.Name)
		return false
	})
	return firstErr
}

func prepareConvExpr(spec string) (string, int, error) {
	var prepared strings.Builder
	count := 0
	var quote byte
	escaped := false
	for i := 0; i < len(spec); i++ {
		char := spec[i]
		if quote != 0 {
			prepared.WriteByte(char)
			if quote == '`' {
				if char == '`' {
					quote = 0
				}
				continue
			}
			if escaped {
				escaped = false
				continue
			}
			if char == '\\' {
				escaped = true
				continue
			}
			if char == quote {
				quote = 0
			}
			continue
		}
		switch char {
		case '\'', '"', '`':
			quote = char
			prepared.WriteByte(char)
		case '$':
			if (i > 0 && isIdentByte(spec[i-1])) || (i+1 < len(spec) && isIdentByte(spec[i+1])) {
				return "", 0, fmt.Errorf("占位符 $ 必须独立出现")
			}
			prepared.WriteString(convPlaceholder)
			count++
		default:
			prepared.WriteByte(char)
		}
	}
	if quote != 0 {
		return "", 0, fmt.Errorf("表达式 %q 的引号未闭合", spec)
	}
	return prepared.String(), count, nil
}

func isIdentByte(char byte) bool {
	return char == '_' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9'
}

func convFuncRef(expr ast.Expr, current *types.Package, res *resolver.Resolver, imports *ImportSet) (string, error) {
	switch value := expr.(type) {
	case *ast.Ident:
		if current == nil || current.Scope() == nil || current.Scope().Lookup(value.Name) == nil {
			return "", fmt.Errorf("转换函数 %s 不存在于当前包", value.Name)
		}
		if !isCallable(current.Scope().Lookup(value.Name)) {
			return "", fmt.Errorf("当前包中的 %s 不是可调用函数", value.Name)
		}
		return value.Name, nil
	case *ast.SelectorExpr:
		root, ok := value.X.(*ast.Ident)
		if !ok {
			return "", fmt.Errorf("-conv 必须是函数引用，或包含 $ 的表达式")
		}
		pkg, err := res.Import(root.Name)
		if err != nil {
			return "", err
		}
		obj := pkg.Scope().Lookup(value.Sel.Name)
		if obj == nil {
			return "", fmt.Errorf("包 %s 中找不到 %s", root.Name, value.Sel.Name)
		}
		if !isCallable(obj) {
			return "", fmt.Errorf("包 %s 中的 %s 不是可调用函数", root.Name, value.Sel.Name)
		}
		return imports.Alias(pkg) + "." + value.Sel.Name, nil
	default:
		return "", fmt.Errorf("-conv 必须是函数引用，或包含 $ 的表达式")
	}
}

func isCallable(obj types.Object) bool {
	switch value := obj.(type) {
	case *types.Func, *types.Builtin:
		return true
	case *types.Var:
		_, ok := value.Type().Underlying().(*types.Signature)
		return ok
	default:
		return false
	}
}

func rewriteConvPackages(expr ast.Expr, current *types.Package, res *resolver.Resolver, imports *ImportSet) error {
	var firstErr error
	ast.Inspect(expr, func(node ast.Node) bool {
		if firstErr != nil {
			return false
		}
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		root, ok := selector.X.(*ast.Ident)
		if !ok || root.Name == convPlaceholder {
			return true
		}
		if current != nil && current.Scope() != nil && current.Scope().Lookup(root.Name) != nil {
			return true
		}
		pkg, err := res.Import(root.Name)
		if err != nil {
			firstErr = err
			return false
		}
		if pkg.Scope().Lookup(selector.Sel.Name) == nil {
			firstErr = fmt.Errorf("包 %s 中找不到 %s", root.Name, selector.Sel.Name)
			return false
		}
		root.Name = imports.Alias(pkg)
		return true
	})
	return firstErr
}
