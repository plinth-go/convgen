// Package parser turns convgen declarations into structured options.
package parser

import (
	"errors"
	"fmt"
	"go/token"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/plinth-go/convgen/internal/scanner"
)

const (
	TypeValVal = "val_val"
	TypeValPtr = "val_ptr"
	TypePtrVal = "ptr_val"
	TypePtrPtr = "ptr_ptr"
)

var (
	flagRE     = regexp.MustCompile(`(?:^|\s)-([a-zA-Z][a-zA-Z0-9]*)`)
	knownFlags = map[string]bool{
		"s": true, "d": true, "fn": true, "type": true,
		"map": true, "conv": true, "ignore": true, "deep": true,
	}
)

// Options is the parsed form of one //convgen: declaration.
type Options struct {
	SrcType  string
	DstType  string
	FuncName string
	TypeMode string

	Map    map[string]string
	Conv   map[string]string
	Ignore map[string]bool
	Deep   map[string]bool

	MapLines    map[string]int
	ConvLines   map[string]int
	IgnoreLines map[string]int
	DeepLines   map[string]int

	File     string
	Line     int
	RawLines []string
}

// Parse parses a scanned declaration.
func Parse(decl scanner.RawDecl) (Options, error) {
	tokens, err := tokenize(decl.Content)
	if err != nil {
		var positioned *contentError
		if errors.As(err, &positioned) {
			return Options{}, declErrorAt(decl, positioned.offset, "%s", positioned.message)
		}
		return Options{}, declError(decl, "%v", err)
	}
	opts := Options{
		TypeMode:    TypeValVal,
		Map:         make(map[string]string),
		Conv:        make(map[string]string),
		Ignore:      make(map[string]bool),
		Deep:        make(map[string]bool),
		MapLines:    make(map[string]int),
		ConvLines:   make(map[string]int),
		IgnoreLines: make(map[string]int),
		DeepLines:   make(map[string]int),
		File:        decl.File,
		Line:        decl.Line,
		RawLines:    append([]string(nil), decl.RawLines...),
	}

	seenFlags := make(map[string]int)
	for _, token := range tokens {
		flag := token.name
		value := token.value
		switch flag {
		case "s", "d", "fn", "type":
			if previous, ok := seenFlags[flag]; ok {
				return Options{}, declErrorAt(decl, token.start, "参数 -%s 重复（首次出现于 %s:%d）", flag, decl.File, previous)
			}
			seenFlags[flag] = declLineAt(decl, token.start)
		}
		switch flag {
		case "s":
			opts.SrcType = value
		case "d":
			opts.DstType = value
		case "fn":
			opts.FuncName = value
		case "type":
			opts.TypeMode = value
		case "map":
			if err := parsePairs(decl, token, opts.Map, opts.MapLines, "map"); err != nil {
				return Options{}, err
			}
		case "conv":
			if err := parsePairs(decl, token, opts.Conv, opts.ConvLines, "conv"); err != nil {
				return Options{}, err
			}
		case "ignore":
			if err := parseFields(decl, token, opts.Ignore, opts.IgnoreLines, "ignore"); err != nil {
				return Options{}, err
			}
		case "deep":
			if err := parseFields(decl, token, opts.Deep, opts.DeepLines, "deep"); err != nil {
				return Options{}, err
			}
		}
	}

	if opts.SrcType == "" {
		return Options{}, declError(decl, "缺少 -s 源类型")
	}
	if opts.DstType == "" {
		return Options{}, declError(decl, "缺少 -d 目标类型")
	}
	switch opts.TypeMode {
	case TypeValVal, TypeValPtr, TypePtrVal, TypePtrPtr:
	default:
		return Options{}, declErrorLine(decl, seenFlags["type"], "-type 值 %q 非法，必须是 val_val、val_ptr、ptr_val 或 ptr_ptr", opts.TypeMode)
	}
	if opts.FuncName == "" {
		opts.FuncName = deriveFuncName(opts)
	}
	if !token.IsIdentifier(opts.FuncName) {
		return Options{}, declErrorLine(decl, seenFlags["fn"], "-fn 值 %q 不是合法的 Go 标识符", opts.FuncName)
	}

	for _, field := range sortedKeys(opts.Ignore) {
		if _, ok := opts.Map[field]; ok {
			return Options{}, declErrorLine(decl, opts.IgnoreLines[field], "-ignore 与 -map 不能同时作用于字段 %s（-map 位于 %s:%d）", field, decl.File, opts.MapLines[field])
		}
		if _, ok := opts.Conv[field]; ok {
			return Options{}, declErrorLine(decl, opts.IgnoreLines[field], "-ignore 与 -conv 不能同时作用于字段 %s（-conv 位于 %s:%d）", field, decl.File, opts.ConvLines[field])
		}
		if opts.Deep[field] {
			return Options{}, declErrorLine(decl, opts.IgnoreLines[field], "-ignore 与 -deep 不能同时作用于字段 %s（-deep 位于 %s:%d）", field, decl.File, opts.DeepLines[field])
		}
	}
	for _, field := range sortedKeys(opts.Conv) {
		if opts.Deep[field] {
			return Options{}, declErrorLine(decl, opts.ConvLines[field], "-deep 与 -conv 不能同时作用于字段 %s（-deep 位于 %s:%d）", field, decl.File, opts.DeepLines[field])
		}
	}
	return opts, nil
}

type flagToken struct {
	name       string
	value      string
	start      int
	valueStart int
	items      []valueItem
}

type valueItem struct {
	text   string
	offset int
}

type contentError struct {
	offset  int
	message string
}

func (e *contentError) Error() string { return e.message }

func contentErrorf(offset int, format string, args ...any) error {
	return &contentError{offset: offset, message: fmt.Sprintf(format, args...)}
}

// Tokenize splits a logical declaration into alternating flag and value tokens.
func Tokenize(content string) ([]string, error) {
	tokens, err := tokenize(content)
	if err != nil {
		return nil, err
	}
	values := make([]string, 0, len(tokens)*2)
	for _, token := range tokens {
		values = append(values, "-"+token.name, token.value)
	}
	return values, nil
}

func tokenize(content string) ([]flagToken, error) {
	matches := flagMatches(content)
	if len(matches) == 0 {
		if strings.TrimSpace(content) != "" {
			return nil, contentErrorf(0, "声明必须由 -flag 组成")
		}
		return nil, nil
	}
	if prefix := strings.TrimSpace(content[:matches[0][0]]); prefix != "" {
		return nil, contentErrorf(0, "无法识别的声明前缀 %q", prefix)
	}

	tokens := make([]flagToken, 0, len(matches))
	for i, match := range matches {
		name := content[match[2]:match[3]]
		if !knownFlags[name] {
			return nil, contentErrorf(match[2]-1, "未知参数 -%s", name)
		}
		end := match[1]
		nextStart := len(content)
		if i+1 < len(matches) {
			nextStart = matches[i+1][0]
		}
		valueStart := end
		for valueStart < nextStart && isSpace(content[valueStart]) {
			valueStart++
		}
		valueEnd := nextStart
		for valueEnd > valueStart && isSpace(content[valueEnd-1]) {
			valueEnd--
		}
		value := content[valueStart:valueEnd]
		if value == "" {
			return nil, contentErrorf(match[2]-1, "参数 -%s 缺少值", name)
		}
		token := flagToken{name: name, start: match[2] - 1, valueStart: valueStart}
		if isMultiFlag(name) {
			items, err := splitTopLevelItems(value)
			if err != nil {
				return nil, contentErrorf(valueStart, "参数 -%s 值格式非法: %v", name, err)
			}
			for j := range items {
				if items[j].text == "" {
					return nil, contentErrorf(valueStart+items[j].offset, "参数 -%s 包含空项", name)
				}
			}
			values := make([]string, len(items))
			for j := range items {
				values[j] = items[j].text
			}
			token.value = strings.Join(values, ", ")
			token.items = items
		} else if strings.ContainsAny(value, " \t\r\n") {
			return nil, contentErrorf(valueStart, "参数 -%s 的值不能包含空格", name)
		} else {
			token.value = value
		}
		tokens = append(tokens, token)
	}
	return tokens, nil
}

func isSpace(char byte) bool {
	return char == ' ' || char == '\t' || char == '\r' || char == '\n'
}

func flagMatches(content string) [][]int {
	all := flagRE.FindAllStringSubmatchIndex(content, -1)
	topLevel := topLevelStates(content)
	matches := all[:0]
	for _, match := range all {
		if !topLevel[match[2]-1] {
			continue
		}
		end := match[1]
		if end < len(content) && content[end] != ' ' && content[end] != '\t' && content[end] != '\n' && content[end] != '\r' {
			continue
		}
		matches = append(matches, match)
	}
	return matches
}

func topLevelStates(content string) []bool {
	states := make([]bool, len(content))
	depth := 0
	var quote byte
	escaped := false
	for i := 0; i < len(content); i++ {
		states[i] = depth == 0 && quote == 0
		char := content[i]
		if quote != 0 {
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
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		}
	}
	return states
}

func parsePairs(decl scanner.RawDecl, flagTok flagToken, dst map[string]string, seen map[string]int, flag string) error {
	for _, item := range flagTok.items {
		offset := flagTok.valueStart + item.offset
		text := item.text
		if flag == "conv" {
			key, val, ok := strings.Cut(text, "=")
			if !ok {
				return declErrorAt(decl, offset, "-%s 项 %q 必须是 key=value", flag, text)
			}
			key = strings.TrimSpace(key)
			val = strings.TrimSpace(val)
			if !token.IsIdentifier(key) {
				return declErrorAt(decl, offset, "-%s 目标字段 %q 不是合法的 Go 标识符", flag, key)
			}
			if val == "" {
				return declErrorAt(decl, offset, "-%s 项 %q 缺少表达式", flag, text)
			}
			if previous, exists := seen[key]; exists {
				return declErrorAt(decl, offset, "-%s 字段 %s 重复（首次出现于 %s:%d）", flag, key, decl.File, previous)
			}
			seen[key] = declLineAt(decl, offset)
			dst[key] = val
			continue
		}
		if strings.Count(text, "=") != 1 {
			return declErrorAt(decl, offset, "-%s 项 %q 必须是 key=value 且只能包含一个 =", flag, text)
		}
		key, val, _ := strings.Cut(text, "=")
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if !token.IsIdentifier(key) {
			return declErrorAt(decl, offset, "-%s 目标字段 %q 不是合法的 Go 标识符", flag, key)
		}
		if err := validatePath(val); err != nil {
			return declErrorAt(decl, offset, "-map 源路径 %q 非法: %v", val, err)
		}
		if previous, exists := seen[key]; exists {
			return declErrorAt(decl, offset, "-%s 字段 %s 重复（首次出现于 %s:%d）", flag, key, decl.File, previous)
		}
		seen[key] = declLineAt(decl, offset)
		dst[key] = val
	}
	return nil
}

func parseFields(decl scanner.RawDecl, flagTok flagToken, dst map[string]bool, seen map[string]int, flag string) error {
	for _, item := range flagTok.items {
		offset := flagTok.valueStart + item.offset
		if !token.IsIdentifier(item.text) {
			return declErrorAt(decl, offset, "-%s 字段 %q 不是合法的 Go 标识符", flag, item.text)
		}
		if previous, exists := seen[item.text]; exists {
			return declErrorAt(decl, offset, "-%s 字段 %s 重复（首次出现于 %s:%d）", flag, item.text, decl.File, previous)
		}
		seen[item.text] = declLineAt(decl, offset)
		dst[item.text] = true
	}
	return nil
}

func splitTopLevelItems(value string) ([]valueItem, error) {
	var items []valueItem
	start := 0
	depth := 0
	var quote byte
	escaped := false
	for i := 0; i < len(value); i++ {
		char := value[i]
		if quote != 0 {
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
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth < 0 {
				return nil, fmt.Errorf("括号不匹配")
			}
		case ',':
			if depth == 0 {
				items = append(items, trimValueItem(value, start, i))
				start = i + 1
			}
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("引号未闭合")
	}
	if depth != 0 {
		return nil, fmt.Errorf("括号不匹配")
	}
	items = append(items, trimValueItem(value, start, len(value)))
	return items, nil
}

func trimValueItem(value string, start, end int) valueItem {
	raw := value[start:end]
	left := len(raw) - len(strings.TrimLeft(raw, " \t\r\n"))
	return valueItem{
		text:   strings.TrimSpace(raw),
		offset: start + left,
	}
}

func validatePath(path string) error {
	for _, part := range strings.Split(path, ".") {
		if !token.IsIdentifier(part) {
			return fmt.Errorf("路径段 %q 不是合法的 Go 标识符", part)
		}
	}
	return nil
}

func deriveFuncName(opts Options) string {
	src := exportName(shortTypeName(opts.SrcType))
	dst := exportName(shortTypeName(opts.DstType))
	switch opts.TypeMode {
	case TypeValPtr:
		return dst + "PtrFrom" + src
	case TypePtrVal:
		return dst + "From" + src + "Ptr"
	case TypePtrPtr:
		return dst + "PtrFrom" + src + "Ptr"
	default:
		return dst + "From" + src
	}
}

func shortTypeName(typeName string) string {
	if i := strings.LastIndexByte(typeName, '.'); i >= 0 {
		return typeName[i+1:]
	}
	return typeName
}

func exportName(name string) string {
	runes := []rune(name)
	if len(runes) == 0 {
		return name
	}
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func isMultiFlag(name string) bool {
	switch name {
	case "map", "conv", "ignore", "deep":
		return true
	default:
		return false
	}
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func declError(decl scanner.RawDecl, format string, args ...any) error {
	return fmt.Errorf("%s:%d: %s", decl.File, decl.Line, fmt.Sprintf(format, args...))
}

func declErrorAt(decl scanner.RawDecl, offset int, format string, args ...any) error {
	return fmt.Errorf("%s:%d: %s", decl.File, declLineAt(decl, offset), fmt.Sprintf(format, args...))
}

func declErrorLine(decl scanner.RawDecl, line int, format string, args ...any) error {
	if line == 0 {
		line = decl.Line
	}
	return fmt.Errorf("%s:%d: %s", decl.File, line, fmt.Sprintf(format, args...))
}

func declLineAt(decl scanner.RawDecl, offset int) int {
	if len(decl.LineStarts) == 0 || len(decl.LineStarts) != len(decl.RawLines) {
		return decl.Line
	}
	line := decl.Line
	for i, start := range decl.LineStarts {
		if start > offset {
			break
		}
		line = decl.Line + i
	}
	return line
}
