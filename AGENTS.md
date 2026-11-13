# convgen Agent Guide

面向维护者、外部维护者和编码代理的工程入口。保持简短；公开行为以
`SPEC.md` 为准。

## 项目定位

- `convgen` 是独立 Go 代码生成工具，面向外部 Go module 使用。
- 最低 Go 版本为 1.22；新增依赖或语言特性不得抬高该下限，除非明确更新规格。
- 输入只有 `//convgen:` 注释，不增加 Anchor、`var _` 或其他虚拟声明。
- 生成代码必须零反射、零运行时依赖、零 `panic`。
- 修改公开声明语法、参数、生成文件名或函数语义时，必须同步更新
  `SPEC.md` 与 `README.md`。

## 不可破坏的契约

- 只允许标准库与 `golang.org/x/tools/go/packages`；禁止 `flag.FlagSet`、
  运行时反射库和第二套解析入口。
- 每包一个 `conv_gen.go`；函数按名称字典序；输出幂等且经 `go/format`。
- 保留生成文件标记、声明回显、`// Source:` 和固定 4 空格续行。
- 回显块与函数之间必须保留空行。较新的 `gofmt` 会把紧贴函数的
  `//convgen:` 改成 `// convgen:`；删除空行会破坏公开契约。
- 失败时不写半成品；同一次 `./...` 运行聚合错误并统一返回非零。
- 声明、解析和规划错误必须以 `file:line:` 开头并指向确切声明行，保证终端与 IDE
  可直接跳转。
- 每个目标字段必须被赋值或显式 `-ignore`；遗漏时列出全部字段并在写文件前失败。

## 代码结构

| 路径 | 职责 |
|:---|:---|
| `internal/scanner` | 注释状态机与生成文件跳过 |
| `internal/parser` | 自研 tokenizer、参数与冲突校验 |
| `internal/resolver` | `go/packages` 类型解析与缓存 |
| `internal/planner` | 字段匹配、`-conv` 表达式、可见性、深拷贝与 import 别名 |
| `internal/generator` | 单文件渲染、回显、格式化 |
| `internal/differ` | 生成前后函数名对比 |
| `internal/runner` | CLI 编排、错误聚合、原子写入 |
| `testdata/input` | 可生成的临时 module 夹具 |
| `testdata/want` | golden 输出 |

## 工程命令

```sh
make check           # gofmt、go vet、go test
make hooks           # 启用仓库内 DCO 提交钩子
make dco-check       # 校验提交作者与 Signed-off-by 一致
make test
make test-race       # race detector，随机测试顺序
make build
make generate-check  # 检查 conv_gen.go 是否漂移
make license-check   # 校验第三方依赖许可证
make license-update  # 刷新 THIRD_PARTY_NOTICES.md
make quality         # 许可证、通知清单与 workflow lint
make verify          # 本地执行 CI 的完整校验
```

CI 覆盖 Go 1.22 与最新稳定版，并校验模块整洁性；不要在代码中引入高于
Go 1.22 的语言特性而不更新规格。

## 修改规则

- 行为变化必须增加测试；生成结果需要 golden 或临时 module 端到端覆盖。
- 不要手改 `testdata/want/conv_gen.go`；先运行测试场景生成，再核对并更新。
- 新增依赖前先确认标准库或 `go/packages` 无法完成，并更新设计。
- 新增或升级依赖后运行 `make license-update`，并提交更新后的
  `THIRD_PARTY_NOTICES.md`。
- README 只保留使用者需要的信息；实现细节、测试清单和发布期 TODO 放入
  `AGENTS.md`。
- module path 固定为 `github.com/plinth-go/convgen`；变更时必须同步 `go.mod`、
  内部 import 和文档。

## 提交与许可证

- 每个提交必须包含与作者身份一致的 DCO `Signed-off-by`；首次 clone 后运行
  `make hooks`。
- 项目采用 Apache License 2.0；版权见 `NOTICE`，第三方许可见
  `THIRD_PARTY_NOTICES.md`。
