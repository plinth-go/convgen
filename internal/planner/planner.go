// Package planner matches source and destination fields and builds assignments.
package planner

import (
	"errors"
	"fmt"
	"go/types"
	"strings"

	"github.com/plinth-go/convgen/internal/parser"
	"github.com/plinth-go/convgen/internal/resolver"
	"golang.org/x/tools/go/packages"
)

// Assign is one generated field assignment.
type Assign struct {
	DstField string
	SrcExpr  string
}

// Plan describes one generated conversion function.
type Plan struct {
	FuncName string
	SrcType  types.Type
	DstType  types.Type
	SrcShort string
	DstShort string
	InPtr    bool
	OutPtr   bool
	Assigns  []Assign
	RawLines []string
	SrcFile  string
	SrcLine  int
}

// Build resolves one declaration and builds its conversion plan.
func Build(opts parser.Options, current *packages.Package, res *resolver.Resolver, imports *ImportSet) (Plan, error) {
	if current == nil || current.Types == nil {
		return Plan{}, declErr(opts, "缺少当前包类型信息")
	}
	srcResolved, err := res.Resolve(opts.SrcType)
	if err != nil {
		return Plan{}, declErr(opts, "解析源类型 %s 失败: %v", opts.SrcType, err)
	}
	dstResolved, err := res.Resolve(opts.DstType)
	if err != nil {
		return Plan{}, declErr(opts, "解析目标类型 %s 失败: %v", opts.DstType, err)
	}

	srcNamed, srcStruct, err := structType(srcResolved)
	if err != nil {
		return Plan{}, declErr(opts, "源类型 %s: %v", opts.SrcType, err)
	}
	dstNamed, dstStruct, err := structType(dstResolved)
	if err != nil {
		return Plan{}, declErr(opts, "目标类型 %s: %v", opts.DstType, err)
	}

	srcDeclPkg := srcResolved.Package
	dstDeclPkg := dstResolved.Package
	if srcDeclPkg.Path() != dstDeclPkg.Path() && srcDeclPkg.Name() == dstDeclPkg.Name() {
		return Plan{}, declErr(opts, "源包与目标包同名 %q，请在生成前调整包名", srcDeclPkg.Name())
	}

	srcAccessPkg := srcNamed.Obj().Pkg()
	dstAccessPkg := dstNamed.Obj().Pkg()
	currentPath := current.Types.Path()
	plan := Plan{
		FuncName: opts.FuncName,
		SrcType:  srcNamed,
		DstType:  dstNamed,
		SrcShort: imports.Qualify(srcDeclPkg, srcResolved.TypeName.Name()),
		DstShort: imports.Qualify(dstDeclPkg, dstResolved.TypeName.Name()),
		InPtr:    opts.TypeMode == parser.TypePtrVal || opts.TypeMode == parser.TypePtrPtr,
		OutPtr:   opts.TypeMode == parser.TypeValPtr || opts.TypeMode == parser.TypePtrPtr,
		RawLines: append([]string(nil), opts.RawLines...),
		SrcFile:  opts.File,
		SrcLine:  opts.Line,
	}

	var unassigned []string
	for i := 0; i < dstStruct.NumFields(); i++ {
		dstField := dstStruct.Field(i)
		name := dstField.Name()
		if !dstField.Exported() && dstAccessPkg.Path() != currentPath {
			if opts.Ignore[name] {
				continue
			}
			unassigned = append(unassigned, fmt.Sprintf("%s（跨包未导出，无法赋值）", name))
			continue
		}
		if opts.Ignore[name] {
			continue
		}

		srcPath := name
		if mapped, ok := opts.Map[name]; ok {
			srcPath = mapped
		}
		srcField, err := resolvePath(srcStruct, srcAccessPkg, currentPath, name, srcPath)
		if err != nil {
			var missing *missingSourceError
			if errors.As(err, &missing) {
				unassigned = append(unassigned, name)
			} else {
				unassigned = append(unassigned, fmt.Sprintf("%s（%v）", name, err))
			}
			continue
		}

		var expr string
		if conv, ok := opts.Conv[name]; ok {
			expr, err = renderConv(conv, srcPath, current.Types, res, imports)
			if err != nil {
				return Plan{}, declErrAt(opts, lineOr(opts.ConvLines[name], opts.Line), "字段 %s 的 -conv 无效: %v", name, err)
			}
		} else if types.AssignableTo(srcField.Type(), dstField.Type()) {
			expr = "src." + srcPath
		} else {
			unassigned = append(unassigned, fmt.Sprintf("%s（类型不匹配：%s → %s，需 -conv）", name, imports.Render(srcField.Type()), imports.Render(dstField.Type())))
			continue
		}

		if opts.Deep[name] {
			if _, ok := opts.Conv[name]; ok {
				return Plan{}, declErrAt(opts, lineOr(opts.DeepLines[name], opts.Line), "-deep 与 -conv 不能同时作用于字段 %s", name)
			}
			expr, err = deepExpr(srcField.Type(), srcPath, imports)
			if err != nil {
				return Plan{}, declErrAt(opts, lineOr(opts.DeepLines[name], opts.Line), "%v", err)
			}
		}
		plan.Assigns = append(plan.Assigns, Assign{DstField: name, SrcExpr: expr})
	}
	if len(unassigned) > 0 {
		return Plan{}, declErr(opts, "目标类型 %s 存在未赋值字段: %s；请添加 -map/-conv，或使用 -ignore 显式跳过", opts.DstType, strings.Join(unassigned, "、"))
	}
	return plan, nil
}

func structType(resolved resolver.Resolved) (*types.Named, *types.Struct, error) {
	t := types.Unalias(resolved.TypeName.Type())
	named, ok := t.(*types.Named)
	if !ok {
		return nil, nil, fmt.Errorf("不是命名结构体类型")
	}
	if named.TypeArgs().Len() != 0 || named.TypeParams().Len() != 0 {
		return nil, nil, fmt.Errorf("泛型类型 %s 不受支持", resolved.TypeName.Name())
	}
	strct, ok := named.Underlying().(*types.Struct)
	if !ok {
		return nil, nil, fmt.Errorf("不是结构体类型")
	}
	return named, strct, nil
}

func resolvePath(src *types.Struct, srcPkg *types.Package, currentPath, dstField, srcPath string) (*types.Var, error) {
	segments := strings.Split(srcPath, ".")
	current := src
	for i, segment := range segments {
		field := directField(current, segment)
		if field == nil {
			if i == 0 && len(segments) == 1 {
				return nil, &missingSourceError{dstField: dstField, srcPath: srcPath}
			}
			if i < len(segments)-1 {
				return nil, fmt.Errorf("嵌套路径 %s 的中间段 %s 不存在", srcPath, segment)
			}
			return nil, fmt.Errorf("源路径 %s 中找不到字段 %s", srcPath, segment)
		}
		if !field.Exported() && (srcPkg == nil || srcPkg.Path() != currentPath) {
			return nil, fmt.Errorf("源字段 %s 未导出，无法在生成包中访问", srcPath)
		}
		if i == len(segments)-1 {
			return field, nil
		}
		switch underlying := field.Type().Underlying().(type) {
		case *types.Pointer:
			return nil, fmt.Errorf("嵌套路径 %s 的中间段 %s 是指针类型，MVP 不支持", srcPath, segment)
		case *types.Struct:
			current = underlying
		default:
			return nil, fmt.Errorf("嵌套路径 %s 的中间段 %s 不是结构体", srcPath, segment)
		}
	}
	return nil, &missingSourceError{dstField: dstField, srcPath: srcPath}
}

type missingSourceError struct {
	dstField string
	srcPath  string
}

func (e *missingSourceError) Error() string {
	return fmt.Sprintf("目标字段 %s 找不到源字段 %s", e.dstField, e.srcPath)
}

func directField(strct *types.Struct, name string) *types.Var {
	for i := 0; i < strct.NumFields(); i++ {
		field := strct.Field(i)
		if field.Name() == name {
			return field
		}
	}
	return nil
}

func deepExpr(t types.Type, srcPath string, imports *ImportSet) (string, error) {
	switch underlying := t.Underlying().(type) {
	case *types.Slice:
		elem := imports.Render(underlying.Elem())
		return fmt.Sprintf("append([]%s(nil), src.%s...)", elem, srcPath), nil
	case *types.Map:
		key := imports.Render(underlying.Key())
		elem := imports.Render(underlying.Elem())
		return fmt.Sprintf("func() map[%s]%s { if src.%s == nil { return nil }; m := make(map[%s]%s, len(src.%s)); for k, v := range src.%s { m[k] = v }; return m }()", key, elem, srcPath, key, elem, srcPath, srcPath), nil
	case *types.Pointer:
		elem := imports.Render(underlying.Elem())
		return fmt.Sprintf("func() *%s { if src.%s == nil { return nil }; v := *src.%s; return &v }()", elem, srcPath, srcPath), nil
	default:
		return "", fmt.Errorf("-deep 只支持 slice/map/pointer，字段 %s 类型为 %s", srcPath, imports.Render(t))
	}
}

func declErr(opts parser.Options, format string, args ...any) error {
	return declErrAt(opts, opts.Line, format, args...)
}

func declErrAt(opts parser.Options, line int, format string, args ...any) error {
	return fmt.Errorf("%s:%d: %s", opts.File, lineOr(line, opts.Line), fmt.Sprintf(format, args...))
}

func lineOr(line, fallback int) int {
	if line > 0 {
		return line
	}
	return fallback
}
