# AGENTS.md

本文件面向在本仓库工作的 AI 编码代理（及新加入的开发者），说明项目结构、构建测试方式与必须遵守的开发约定。

## 项目概述

`zckg`（模块路径 `github.com/buexplain/zckg`）是一个 Go 工具库集合，采用**模块化架构**，五个模块相互独立、无循环依赖：

| 模块 | 职责 | 核心文件 |
|---|---|---|
| `zcconfig` | 配置加载与读取：`.env` 文件（Env 通道）+ 业务配置树（Config 通道），泛型读取，另提供 `DBConfig` DSN 生成 | `config.go`、`cast.go`、`env.go`、`dbConfig.go` |
| `zcdb` | 数据库访问：Builder 查询构造器（MySQL / PostgreSQL / SQLite 三方言）、主从连接池、读写分离、事务、Schema 元数据 | `builder_*.go`、`grammar.go`、`pool.go`、`db_dao.go` |
| `zchttp` | HTTP 框架：路由（基数树，静态段 + 参数段同树）、反射式参数绑定与校验、中间件、OpenAPI 生成 | `httpEngine.go`、`router.go`、`binding.go`、`openapi.go` |
| `zcmodel` | 数据库模型代码生成：输入表结构，输出 Entity/DO 结构体与互转方法的 Go 源码，支持 AST 增量再生成 | `generate.go`、`build.go`、`columnTypeToGoType.go` |
| `zcquit` | 优雅退出：全局可取消上下文 + 信号监听 + 分级清理 handler | `quit.go` |

- Go 版本：**1.25.11**（见 `go.mod`）
- 依赖管理：Go Modules，添加依赖后运行 `go mod tidy`
- 许可证：Apache-2.0

## 构建与测试命令

```powershell
# 构建全部包
go build ./...

# 运行全部测试（集成测试在数据库不可达时自动 Skip，不会失败）
go test ./...

# 只跑某个模块
go test ./zcdb/...

# 静态检查
go vet ./...
gofmt -l .   # 输出应为空
```

**环境注意**：本机为 Windows + PowerShell，不支持 `&&` 作为语句分隔符，多条命令请用 `;` 分隔。

### 测试文件命名约定

| 文件名模式 | 含义 |
|---|---|
| `*_unit_test.go` | 纯单元测试，不依赖外部资源 |
| `*_mysql_integration_test.go` / `*_postgres_integration_test.go` / `*_sqlite_integration_test.go` | 按数据库方言拆分的集成测试 |
| `docs_examples_compile_test.go` | 文档示例代码的编译级校验（zcdb） |
| `docs_deviation_review_test.go` | 文档-代码偏离审查的回归锁死测试 |

- 集成测试**内置门控**：目标数据库不可达时 `t.Skip`，保证无数据库环境下 `go test ./...` 不误报。
- SQLite 集成测试使用纯 Go 驱动 `modernc.org/sqlite`，无需外部环境。
- 需要真实数据库时的本地环境（DSN 硬编码于测试文件中）：

```powershell
docker run -d -p 3306:3306 -e MYSQL_ROOT_PASSWORD=root --name zcdb_test_mysql mysql:8.4
docker run -d --name zcdb_test_postgres -e POSTGRES_PASSWORD=root -p 5432:5432 postgres:15
```

## 文档约定

每个模块都有 `docs/` 目录，文档与代码同等重要：

- 模块主文档：`zcconfig/docs/zcconfig.md`、`zcdb/docs/README.md` 及其分主题文档、`zcmodel/docs/zcmodel.md`、`zcquit/docs/zcquit.md`、`zchttp/docs/*.md`
- 审查类文档模板：`code-review-plan.md`（审查计划）、`code-review-report.md`（审查报告）、`docs-code-deviation-review-plan.md` / `docs-code-deviation-review-report.md`（文档偏离审查）

**四者一致性原则（必须遵守）**：任何修复或功能变更涉及代码、注释、文档、测试的，必须同步修改对应部分，禁止只改代码不改文档（或反之）。实现逻辑、函数注释、用户文档、回归测试用例必须保持严格一致。

## 编码约定

1. **gofmt 强制**：提交前 `gofmt -l .` 必须无输出。Go 文档注释中的编号列表（步骤/清单）必须用**双空格**前缀格式 `//  1.`，单空格会被 gofmt 标记差异。
2. **核心行为用测试锁死**：修改核心路径（路由、状态切换、数据库读写、类型转换语义）时，必须用单元 + 集成测试双重验证，并新增/保留锁死关键行为的用例防回归。
3. **错误处理**：可导出错误统一定义为 `errors` 变量（如 zcdb 的 `ErrXxx` 系列），调用方可用 `errors.Is` 匹配；错误包装需携带上下文（如 zcconfig 的文件路径格式 `file_path: %s: %w`）。

## 关键行为不变量（改动前务必确认）

这些行为已被测试锁死，修改需极其谨慎：

- **zchttp**：`HttpEngine` 必须通过 `NewEngine()` 构造（自动装配默认回调，`MaxBodyBytes` 与 `MultipartFormMaxMemory` 默认 32 MB），禁止字面量构造；handler 签名固定为 `func(ctx context.Context, req Req) (Res, error)`，注册时校验不通过即 panic。
- **zcquit**：`Shutdown()` 先于 `Listen()` 调用时，`Listen()` 必须立即返回。
- **zcmodel**：`ColumnTagName` 为空时不生成对应标签；生成文件落盘前经 go/format 语法自校验 + 临时文件原子写入，非法产物不落盘。
- **zcconfig**：float→int 采用 Go 原生向零截断语义；数值→bool 为 C 风格（非零为 true）；`.env` 解析首行需剥离 UTF-8 BOM。
- **zcdb**：无 WHERE 的 Update/Delete 默认拒绝（需 `Force()`）；`Primary()` 标记的查询强制走主库；三方言的标识符包裹与占位符差异由 Grammar 隔离，勿在 Builder 层写死方言细节。
