# AGENTS.md

本文件面向在本仓库工作的 AI 编码代理（及新加入的开发者），说明项目结构、构建测试方式与必须遵守的开发约定。

## 项目概述

`zckg`（模块路径 `github.com/buexplain/zckg`）是一个 Go 工具库集合，采用**模块化架构**，五个模块相互独立、无循环依赖：

| 模块 | 职责 | 核心文件 |
|---|---|---|
| `zcconfig` | 配置加载与读取：`.env` 文件（Env 通道）+ 业务配置树（Config 通道），泛型读取，另提供 `DBConfig` DSN 生成 | `config.go`、`cast.go`、`env.go`、`dbConfig.go` |
| `zcdb` | 数据库访问：Builder 查询构造器（MySQL / PostgreSQL / SQLite 三方言）、主从连接池、读写分离、事务、Schema 元数据 | `builder_*.go`、`grammar.go`、`{mysql,postgres,sqlite}_grammar.go`、`{mysql,postgres,sqlite}_schema.go`、`pool.go`、`db_dao.go`、`schema_inspector.go` |
| `zchttp` | HTTP 框架：路由（基数树，静态段 + 参数段同树）、反射式参数绑定与校验、中间件、OpenAPI 生成 | `httpEngine.go`、`router.go`、`router_trie.go`、`binding.go`、`validate.go`、`meta.go`、`openapi.go` |
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

**核心原则（必须遵守）**：测试文件与其内的测试用例必须按**被测源码模块**划分——`zchttp/binding_test.go` 只测 `binding.go`，`zcdb/builder_where_mysql_integration_test.go` 只测 `where` 功能在 MySQL 方言下的行为。新测试用例直接归入对应模块的既有测试文件，**禁止新建按覆盖率批次/历史目的聚合的跨模块测试文件**（如 `cov2_*`、`coverage_extra_*`——此类历史遗留文件已全部归位到各模块，新增用例不得重犯）。

| 文件名模式 | 含义 |
|---|---|
| `{源码模块}_test.go` | 标准测试：文件名与被测源码文件同名前缀（如 `binding_test.go` ↔ `binding.go`） |
| `*_unit_test.go` | 纯单元测试，不依赖外部资源 |
| `*_mysql_integration_test.go` / `*_postgres_integration_test.go` / `*_sqlite_integration_test.go` | 按数据库方言拆分的集成测试 |
| `{模块}_extra_test.go` / `{模块}_extra_unit_test.go` | 同一被测模块内的扩展/边界用例（允许；不得跨模块聚合） |
| `*_regression_test.go` | 回归锁死测试（如 zchttp `recursion_regression_test.go`） |
| `docs_examples_compile_test.go` | 文档示例代码的编译级校验（zcdb、zchttp、zcmodel） |
| `docs_deviation_review_test.go` | 文档-代码偏离审查的回归锁死测试 |

**各模块布局**：

- **zcdb**：Builder 相关按「功能 × 方言 × 类型」镜像组织，命名 `builder_{功能}_{方言}_{类型}_test.go`（功能 = select / where / join / order / group / exec / query / cursor / compile；方言 = mysql / postgres / sqlite；类型 = unit / integration）。三方言缺失的 unit 空占位文件是刻意预留的骨架，保持不动。跨方言共享用例集中在 `cross_dialect_integration_test.go`（共享主体）+ `cross_dialect_{mysql,postgres,sqlite}_integration_test.go`（方言执行入口）；方言特有用例必须放回对应 `builder_{功能}_{方言}_integration_test.go`，不得留在入口文件。建连/建表等共享基础设施放 `testhelpers_{mysql,postgres,sqlite}_test.go`。
- **zchttp / zcconfig / zcmodel / zcquit**：按被测源码文件一一对应命名（如 `binding_test.go`、`validate_test.go`、`openapi_test.go`、`httpEngine_test.go`、`cast_test.go`、`quit_test.go`），新用例就近加入对应文件。跨测试文件共享的请求执行/建连等 helper 放 `testhelpers_test.go`。

- 集成测试**内置门控**：目标数据库不可达时 `t.Skip`，保证无数据库环境下 `go test ./...` 不误报。
- SQLite 集成测试使用纯 Go 驱动 `modernc.org/sqlite`，无需外部环境。
- 需要真实数据库时的本地环境（DSN 硬编码于测试文件中）：

```powershell
docker run -d -p 3306:3306 -e MYSQL_ROOT_PASSWORD=root --name zcdb_test_mysql mysql:8.4
docker run -d --name zcdb_test_postgres -e POSTGRES_PASSWORD=root -p 5432:5432 postgres:15
```

## 测试用例质量要求

**元规则**：新增或修改测试前，先判断用例属于以下哪个维度，再对照对应条目执行；若与既有测试文件的写法冲突，**以既有写法为准**，违反时必须在提交说明（commit message 或 PR 描述）中注明冲突点与理由。

以下六点适用于全部模块的测试编写与审查（第 6 点为 zcdb 专项）：

1. **测试独立性**
  - **文件隔离**：产出文件的测试使用 `t.TempDir()`，禁止依赖仓库固定目录或其他测试的副作用；每个子测试自行写入完整初始内容，单独运行时具备完整前置条件。权限类测试用 `t.Cleanup` 恢复，平台不支持时明确 `t.Skip`。
  - **数据库隔离**：集成测试各自独立建连（统一通过 `open*DAO(t)` helper），不共享 `DBDao`、事务或连接状态；建表前先 `DROP TABLE IF EXISTS`；启用 `t.Parallel()` 时采用唯一数据库/schema/表名。外部数据库不可达时 `t.Skip`（消息含数据库类型和原始错误），连接成功后的失败一律视为真实错误，纯 Go 驱动（SQLite）的代码缺陷不得降级跳过。
  - **共享数据只读**：包级 map/slice 在测试中仅查找与遍历；helper（如 `testColumns()`）每次返回全新值，不复用包级可变数据。若结构体未来新增 map/slice/指针字段，应重新审查是否需要深拷贝。

2. **测试有效验证**
  - **断言精度匹配语义**：对结构体字段使用字段名+类型+完整 tag 的行级正则（含 `\s+` 边界），对方法使用完整签名，对 import 使用 `ast.ImportSpec` 或完整 import 行。避免 `strings.Contains` 短串在错误位置误命中（如 `"ID"` 命中 `"UserID"`、`"int"` 命中 `"int64"`）。"不得出现"类断言同样需要边界精度。
  - **主流程与增量再生成完整验证**：不仅验证"调用不报错"，还验证目标文件存在、包名、必要 import、全部字段及 tag、方法签名与**完整方法体**（不只签名）、每个字段的转换语句。增量再生成需确认用户手写方法体保留、旧结构被替换、新字段位置正确、转换方法同步更新、生成声明各只有一份；结果至少经 `parser.ParseFile`，含别名 import 的场景应排除编译级错误。
  - **映射完整性**：类型映射测试覆盖当前映射表的全部方言键（含多词类型、长度/精度、大小写、`unsigned`、时区后缀），对照 DDL、期望 map、源码映射表确认键集合一致；按 Go 结果类别覆盖主要类型（整型/浮点/布尔/字符串/时间/字节/未知）。
  - **原子写入验证**：失败阶段准确，且失败后上下文完整——目标目录/文件未被改变、无临时文件残留、不存在的目录未被隐式创建、语法校验失败时目标文件不落盘。

3. **断言与错误处理**
  - **错误路径断言**：除 `err != nil` 外，验证错误消息包含关键信息或用 `errors.Is` 匹配哨兵错误；生成失败测试检查目标文件未创建，若从已有文件开始则检查原内容逐字节未变化。
  - **I/O 错误全部处理**：测试中所有 `os.ReadFile`/`os.WriteFile`/`os.OpenFile`/`Close`/`os.Stat`/`os.ReadDir` 返回的 error 必须显式处理，禁止 `_ =` 忽略。读失败属前置失败应用 `t.Fatalf` 终止；`t.Cleanup` 中故意忽略的错误需登记原因。
  - **正则安全**：动态拼接字段名/列名/类型时用 `regexp.QuoteMeta`；静态正则可用 `regexp.MustCompile`；返回 `(bool, error)` 的 API 必须检查 error；正则必须带字段/空白/行边界。
  - **校验层级区分**：区分 `go/parser` 可解析、`go/format` 可格式化、Go 编译通过三个层级。声称"可编译"必须在独立临时 package 中执行编译级验证；特殊字符转义断言应从 AST 提取 tag 并通过语义 API（如 `reflect.StructTag.Get`）验证还原，不能只检查源码字符串片段。

4. **代码复用**
  - **抽取 helper**：重复的 setup/断言模式抽取为辅助函数（如 `assertContains`/`assertNotContains`/`writeAndVerify`），helper 必须调用 `t.Helper()`，失败信息包含缺失片段与截断后的实际内容。权限测试、平台分支等语义特殊场景不强求合并。
  - **集中建连**：集成测试统一通过 `open*DAO(t)` helper 建连，不存在散落的 DSN、`NewPool` 或 `NewDBDao` 重复逻辑；helper 统一调用 `t.Helper()`、注册关闭 cleanup、执行连通性检查。
  - **table-driven + `t.Run`**：同质测试（枚举映射、命名风格、输入校验等）优先使用 table-driven，每个子用例通过 `t.Run` 输出可定位名称；长流程回归测试（AST 混合声明、权限失败等）保留独立函数，避免大表驱动掩盖测试意图与失败阶段。
  - **断言精度优先**：对关键结构、字段和 import，优先使用 AST 或行级正则，不因有 helper 而扩大宽松子串断言的使用范围。

5. **注释与实现同步**
  - **注释覆盖**：每个 `Test*` 函数必须有说明其验证行为的注释。普通枚举表驱动可接受简短注释；AST 保留、路径安全、原子写入、跨平台跳过、历史回归测试的注释必须说明触发条件与锁死语义。
  - **注释-断言一致**：注释所描述的断言范围、错误分支、平台条件必须与实际代码一致（注释声称"保留用户方法体"则断言不能只检查签名；声称"覆盖全部存储类型"则 DDL、期望 map 和映射表键集合必须一致）。注释收窄或断言增强应同步进行。
  - **边界测试注释**：必须指出覆盖的具体错误阶段（如"`os.Stat` 返回非 NotExist 错误时直接上报"、"`CreateTemp` 阶段失败"），不能仅写"测试失败情况"而无法判断覆盖价值；不能声称覆盖实际未进入的分支。
  - **集成测试前置说明**：注释应包含环境依赖（本地数据库/内存 SQLite）、默认 DSN、不可达时的跳过行为、Docker 启动命令；配置变更时同步更新注释。
  - **易过时标识**：引用具体实现细节的注释（gofmt 对齐规则、AST Spec 粒度、别名 import 判重、命名风格转换规则等）应逐项与源码核对；历史回归编号若已无追踪上下文，改写为直接描述回归风险；"代表性覆盖"不得误写为"全覆盖"。

6. **注释注入（zcdb 专项）**
  - 只要测试通过 Builder 构造 SQL，就必须让产物携带统一测试注释：DAO 路径由 `open*TestDB` helper 的第五参数注入，语法路径统一用 `newTestBuilder` 而非 `NewBuilder`。目的是让整套测试顺带验证 `Comment` 不干扰既有行为。
  - 测试目标不是注释本身时**无须断言注释**：`assertSQL` 容忍固定后缀，未走该 helper 的本地比较先 `stripTestComment` 再比对正文。注释被插错位置、重复追加或泄漏进子查询仍会导致正文不一致而失败。
  - 注释专项用例（`TestNormalizeSQLComment`、`TestBuilder_Comment*`、`*Compile_SQLComment*`、`crossDialectComment*` 等）不得使用 `newTestBuilder`，也不得注入 DAO 默认注释，其断言必须精确相等（不走容忍规则），否则会掩盖注释状态缺陷。

## 文档约定

每个模块都有 `docs/` 目录，文档与代码同等重要：

- 模块主文档：
  - `zcconfig/docs/zcconfig.md`
  - `zcdb/docs/README.md`（分主题：`compile.md`、`connection.md`、`mutate.md`、`query-builder.md`、`query-exec.md`、`schema.md`）
  - `zcmodel/docs/zcmodel.md`
  - `zcquit/docs/zcquit.md`
  - `zchttp/docs/*.md`（`routing.md`、`parameter-binding.md`、`parameter-validate.md`、`request.md`、`middleware.md`、`http-engine-callback.md`、`openapi.md`）

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
