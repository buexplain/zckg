# zcmodel 模糊测试（fuzz）设计与执行

本文是 zcmodel 的模糊测试计划。fuzz 是既有定向单测、集成测试和 golden 测试的补充：定向测试负责精确期望，fuzz 负责在更大的输入空间里寻找**不变量违背、panic、非法产物和错误原子性**问题。fuzz **不替代**精确断言，也不以语句覆盖率作为主要目标。

当前 zcmodel 已引入 P0/P1 五个 fuzz 目标（类型映射、命名转换、Generate、增量 AST、DDL 片段）；P2 真库目标按计划保留为可选夜间项。本文给出目标设计、种子来源、oracle、执行方式与维护约定。

## 一、范围与非目标

**覆盖范围**

- 纯内存逻辑：列类型归一化/映射、命名转换、JSON tag、DDL 片段拼装。
- 文件落盘：`Generate` 的路径校验、创建目录、原子写入、失败不残留。
- 增量 AST 合并：骨架保持、用户代码/注释/指令保留、生成声明替换、import 增删、幂等性。

**非目标**

- 数据库元数据本身的正确性（由 `integration_test.go` 的真库用例负责）。
- 并发/竞态、性能基准、跨模块行为。
- 对任意 DDL 文本的解析：zcmodel 的输入是结构化 `Input/Column`，不是 SQL 解析器；`assembleDDLFragment` 只负责生成类 DDL 注释文本。

## 二、与既有测试的分工

| 测试类型 | 职责 | 典型位置 |
|---|---|---|
| 定向单测 | 精确输入/输出、错误消息、AST 结构断言 | `generate_test.go`、`build_test.go`、`toCase_test.go`、`columnTypeToGoType_test.go` |
| 集成测试 | 真实 MySQL/PostgreSQL/SQLite 元数据 → 生成 → 扫描 | `integration_test.go`（数据库不可达时 Skip） |
| fuzz | 组合边界、随机输入下的不变量、崩溃/半成品检测 | 下文各目标 |

fuzz 发现失败后，应把**最小化输入**写入 `zcmodel/testdata/fuzz/<Target>/`，并补一条定向回归用例；修复实现后普通 `go test` 会持续回归该输入。

## 三、目标清单（按优先级）

| 优先级 | 目标 | 所属测试文件 | 被测符号 | 输入编码 | 核心不变量 |
|---|---|---|---|---|---|
| P0 | `FuzzColumnTypeToGoType` | `columnTypeToGoType_test.go` | `normalizeColumnType`、`formatStructFieldType` | 方言索引 + 类型字符串（≤256 字节） | 确定性、无 panic；独立期望表的归一化结果与 Go 类型精确匹配；映射表全键覆盖；未知类型返回空串；归一化幂等；输出可被 `parser.ParseExpr` 接受 |
| P0 | `FuzzNameConversion` | `toCase_test.go` | `SplitWords`、`ToPascalCase`、`formatJSONTag` | 名称字符串（≤256 字节）+ `NameCase` 索引 | `SplitWords` 无空词且命中独立字面量期望；`ToPascalCase` 去除分隔符、首字符可大写时被大写、单词 `id` 统一为 `ID`；`formatJSONTag` 命中独立期望并满足各风格形态约束 |
| P0 | `FuzzGenerate` | `generate_test.go` | `Generate` | 方言、表名、文件名、tag 名、列（≤16，单项≤1KB）及 nullable/default/extra/index 标志 | 产物确实写入唯一目标文件；Entity/DO 的字段数量/顺序/名称/类型/tag 与输入一致；可解析、gofmt 稳定；二次生成字节一致；失败不改写哨兵、路径解析失败不建目录、无临时文件 |
| P0 | `FuzzWriteOrReplaceStruct` | `build_test.go` | `writeOrReplaceStruct` | raw 既有源码或按位组合的合法骨架 + 生成代码/import 变体 | 用户声明的规范 AST 文本与顺序保留；游离注释保留；生成声明各恰一份；import 增删正确（含别名/`_`/`.`）；二次生成幂等；损坏文件逐字节不变 |
| P1 | `FuzzAssembleDDLFragment` | `build_test.go` | `assembleDDLFragment`、`sanitizeTagValue` | `Column` 的 Type/Default/Extra/Nullable 组合 | `Nullable=nil` 不输出 NULL；`Default=nil` 不输出 DEFAULT；空串 DEFAULT 渲染 `''`；EXTRA 白名单/顺序/精度由独立字面量期望锚定；tag 经反射完整还原 |
| P2（可选/夜间） | `FuzzGenerateAgainstEngine` | `integration_test.go` | `Generate` × 真库 Schema | 同上，列≤8 | 真库元数据 → 生成 → 结构体可扫描；数据库不可达时 Skip |

每个 fuzz 目标的注释必须说明：普通 `go test` 只运行 `f.Add` 种子；探索命令如下（一次只能跑一个目标）：

```bash
go test ./zcmodel -run '^$' -fuzz '^FuzzColumnTypeToGoType$' -fuzztime=30s
go test ./zcmodel -run '^$' -fuzz '^FuzzNameConversion$' -fuzztime=30s
go test ./zcmodel -run '^$' -fuzz '^FuzzGenerate$' -fuzztime=30s
go test ./zcmodel -run '^$' -fuzz '^FuzzWriteOrReplaceStruct$' -fuzztime=30s
go test ./zcmodel -run '^$' -fuzz '^FuzzAssembleDDLFragment$' -fuzztime=30s
```

## 四、各目标详细设计

### 4.1 `FuzzColumnTypeToGoType`

**输入**

```go
f.Fuzz(func(t *testing.T, dialectIndex uint8, columnType string) { ... })
```

方言索引映射到 `mysql/postgres/sqlite`；`columnType` 长度上限 256。

**种子**

- `fuzzColumnTypeRefs()` 独立期望表的三方言全部映射键与代表修饰形式（长度/精度、`unsigned`、时区、引号、未知类型）；
- 大小写混写、前后空白、带引号类型、空串与括号不配对等边界。

**oracle / 不变量**

- 同一输入两次调用结果相同；
- `normalizeColumnType` 对归一化后的结果再归一化保持不变（幂等），且结果小写、空白已压缩、不含括号；
- 命中独立期望表的输入必须得到精确的归一化结果与 Go 类型；该表是手写的，不由 `makeColumnTypeToGoTypeMap` 生成，避免实现自证；
- 与 `makeColumnTypeToGoTypeMap` 的查询结果保持一致（附加一致性检查，语义正确性以上一条为准）；
- 未知类型/未知方言 `formatStructFieldType` 返回空串；
- 非空输出可被 `parser.ParseExpr` 接受（`time.Time`、`[]byte` 等）；
- 任意输入不得 panic。

**预期失败模式**

- `unsigned`/`zerofill`/时区后缀/多词类型未归一化；
- 空串或大小写变体误命中；
- 输出空串后未在上层兜底。

### 4.2 `FuzzNameConversion`

**输入**

```go
f.Fuzz(func(t *testing.T, name string, caseIndex uint8) { ... })
```

`caseIndex` 覆盖六种 `NameCase` 与“空/非法”取值；名称长度上限 256。

**种子**

- `toCase_test.go` 的空串、单字符、数字开头、连续分隔符、缩写、CJK、混合 snake/camel/kebab；
- `id`、`ID`、`userId`、`user_id`、`USER_ID`、`user-id`、`用户`、`_2fa`。

**oracle / 不变量**

- `SplitWords` 输出不含空词，且命中 `fuzzSplitWordsExpectations` 的独立字面量切分（snake/camel/Pascal/混合大小写/CJK）；
- `ToPascalCase` 去除 `_`/`-`/空格分隔符；存在大写映射的首字符必须被大写（部分 Unicode 小写字符没有大写映射，允许保持原样）；单词 `id`（不区分大小写）统一转 `ID`；命中 `fuzzPascalExpectations`；
- `formatJSONTag` 的结果必须等于对应 `ToX(SplitWords(name))`，并命中 `fuzzJSONTagExpectations` 的独立字面量；
- 各命名风格满足形态约束：lower 风格整体小写、upper 风格整体大写、驼峰首字符大小写正确、分隔符已去除；
- 不可导出/非法标识符（数字开头、CJK 等）由 `Generate` 的字段名校验拒绝，不在本目标的纯函数断言范围内；
- 空/非法 case 返回空串；非法输入允许由上层返回错误，不允许 panic。

**预期失败模式**

- 数字开头、CJK、连续分隔符导致空/非法标识符；
- 两个不同列名转换为同一字段名（由 `Generate` 的重复检测兜底）；
- JSON tag 中出现未处理的分隔符。

### 4.3 `FuzzGenerate`

**输入**

使用标量参数编码结构化输入，避免无法反序列化的复杂类型：

```go
f.Fuzz(func(t *testing.T,
    dialectIndex uint8,
    tableName, fileName, columnTagName, tableComment string,
    columnsSpec string,
    flags uint8,
) { ... })
```

- 列名/类型/注释通过分隔符切分并限制数量（≤16）、单项长度（≤1KB）；
- `dialectIndex` 255 专用于未知方言错误分支，其余按 3 取模映射到 MySQL/PostgreSQL/SQLite，避免随机输入大比例落在错误分支；
- `flags` 控制 `Nullable` 是否 nil/true/false、`Default` 是否 nil/空串/普通值、`PrimaryKey`、`Extra`、`JsonTagValueCase`、`Indexes`。

**种子**

- `generate_test.go`：表名路径穿越/Windows 字符/CJK/点号/美元符、文件名冲突与覆盖、GOOS/GOARCH 规则、数字开头列名、重复字段、`ToDO`/`ToEntity`、nil column、损坏现有文件、alias import 再生成；
- `sampleColumns` / `sampleIndexes` / `docs_examples_compile_test.go` 的手工输入。

**oracle / 不变量**

- 成功时：
  - 输出目录内 `.go` 文件集合恰为 `{resolved}.go`，且该文件确实被重写（不等于预写哨兵）；
  - `parser.ParseFile` 成功，`go/format` 输出稳定；
  - Entity/DO 声明存在且各恰一份；字段数量、顺序、名称（导出、唯一）与输入列一致；
  - Entity 字段类型等于方言映射结果（未知类型兜底 `string`），DO 字段类型恒为 `any`；
  - `json`/列 tag/`primary_key`/`ddl`/`description` 与输入一致，并经 `reflect.StructTag` 语义读取还原；
  - 索引注释包含索引名；仅使用标准库 import 时按内容哈希抽样做编译级检查（约 1/16）；
  - 第二次 `Generate` 输出字节级相同（增量幂等）。
- 失败时：
  - 既有哨兵文件逐字节未变；
  - 路径解析失败（`resolveFileName` 报错）时不创建输出目录；
  - 无 `*.tmp*` 残留。
- 错误消息上下文与输出目录创建时机由定向用例锁死，不在 fuzz 中断言全部错误分支。

**预期失败模式**

- 路径穿越、`_test`/`_GOOS` 文件名、非法 tag key；
- tag/comment 中的反引号、换行、引号破坏语法；
- 失败后留下半成品或临时文件。

### 4.4 `FuzzWriteOrReplaceStruct`

**输入**

- `existing`：raw 既有 Go 源码（种子可解析；随机变异后可能语法错误，走原子性错误路径）；
- `layoutIdx`：非 raw 模式下按位组合合法骨架（build tag、const/var/func/type/type block/方法、`//go:generate`）；
- `variant`：bit7 选择 raw 模式；低两位控制旧生成声明与 time import 形态（默认/别名/`_`/`.`）；bit3/bit4 控制新生成代码是否含 `time.Time` 字段、ToDO 是否使用值接收者。

每个 fuzz worker 使用独立 `t.TempDir()`；每次输入先写入完整的初始文件，再调用 `writeOrReplaceStruct`。

**种子**

- `build_test.go` 的用户声明保留、混合 type block、单行 import、alias import、build tags、CRLF、空文件、损坏文件、原子写失败；
- `generate_test.go` 的 alias-import 再生成、`snapshotFiles` 逃逸检测；
- 结构化种子覆盖：用户 const/var/func/type/方法、旧生成声明、别名/`_`/`.` import、time 字段的增删组合。

**oracle / 不变量**

- 成功产物可由 `parser.ParseFile` 解析且 gofmt 稳定；
- 对同一输入连续生成两次，第二次与第一次字节一致；
- 非生成的顶层声明按 `go/printer` 规范文本、数量与相对顺序不变（go/format 可能调整块内对齐，故不比较原始空白）；
- 用户注释、`//go:generate`、行尾/文件尾注释保留；
- Entity/DO/ToDO/ToEntity 各恰一份；
- 生成所需的 import 存在；仅由旧生成代码引用的 import 被移除；`_`/`.` import 不被误删；用户默认 import 保留；结构化模式下别名 time import 的移除/保留符合上述规则；
- 损坏的既有文件：返回解析错误，文件逐字节不变，无临时文件。

**预期失败模式**

- 骨架槽位错位导致注释丢失或声明顺序变化；
- type block/单行 import 处理错误；
- alias import 与所需 import 冲突（`format.Source` 只检查语法，不检查类型/import 冲突）；
- 失败路径留下半成品。

### 4.5 `FuzzAssembleDDLFragment`

**输入**

```go
f.Fuzz(func(t *testing.T,
    columnType, defaultText, extra, comment string,
    nullableIdx uint8, // nil/true/false
    defaultSet bool,
) { ... })
```

**oracle / 不变量**

- `Nullable == nil` 时片段不含 `NULL`/`NOT NULL`；`true` 为 `NULL`；`false` 为 `NOT NULL`；
- `Default == nil` 时不含 `DEFAULT`；空串指针渲染 `DEFAULT ''`；
- EXTRA 仅保留白名单值，顺序固定为 `AUTO_INCREMENT` → `ON UPDATE CURRENT_TIMESTAMP` → `VIRTUAL GENERATED` → `STORED GENERATED`，`ON UPDATE ... (n)` 仅保留纯数字精度；白名单语义另由 `fuzzExtraFragmentExpectations` 字面量期望锚定；
- 生成的结构体 tag 经 `sanitizeTagValue` 后不含能破坏 tag 的反引号/换行；用 `reflect.StructTag.Get` 语义还原；
- 任意输入不得 panic。

doc 注释中的表注释/索引名净化（换行、连续引号与 U+200B）由定向测试
`TestBuildStruct_DocCommentQuotesSurviveGofmt` 锁死；本目标只负责 tag 与片段。

### 4.6 `FuzzGenerateAgainstEngine`（可选，夜间）

- 仅设置 `ZCMODEL_ENGINE_FUZZ=1` 时运行；数据库不可达时 `t.Skip`。
- 复用 `integration_test.go` 的建连/建表 helper（必要时把 helper 签名泛化为 `testing.TB`，与 zcdb 的外部 fuzz 做法一致）。
- 输入列数 ≤8；真库元数据 → `Generate` → 生成文件解析/类型检查 → 用生成类型执行一次查询。
- 放夜间任务，不进入常规 PR CI；命令示例：

```bash
ZCMODEL_ENGINE_FUZZ=1 go test ./zcmodel -run '^$' -fuzz '^FuzzGenerateAgainstEngine$' -fuzztime=60s
```

## 五、种子与 fixture 复用

| 来源 | 可复用内容 |
|---|---|
| `columnTypeToGoType_test.go` | `fuzzColumnTypeRefs` 独立期望表、全部映射键、归一化边界、未知方言/空类型 |
| `toCase_test.go` | `fuzzSplitWordsExpectations`/`fuzzPascalExpectations`/`fuzzJSONTagExpectations`、六种命名风格、`id`、CJK、数字/分隔符边界 |
| `generate_test.go` | 路径/文件名/表名/字段名冲突、损坏文件、alias import、元数据 golden、`fuzzAssertGeneratedStruct` |
| `build_test.go` | tag 特殊字符、骨架顺序、混合 type block、单行 import、原子写失败、用户代码保留、`fuzzExistingSource`/`fuzzNewGeneratedCode` |
| `testhelpers_test.go` | `boolPtr`/`strPtr`、`writeAndVerify`、`typeCheckGoSource`、`decodeFuzzColumns`、`assertFuzzNoTempFiles`、`assertDeclOrder`、`importPathsOf` |
| `integration_test.go` | 真库全类型映射、索引变更再生成（仅 P2） |

种子应直接来自既有测试或文档示例；**期望表必须独立于被测实现**（如 `fuzzColumnTypeRefs` 为手写表，不由 `makeColumnTypeToGoTypeMap` 生成），避免实现自证。

## 六、辅助函数与归属

共享 helper 放进 `zcmodel/testhelpers_test.go`：

- `decodeFuzzColumns`：把 fuzz 的标量参数解码为 `[]*Column`，统一执行列数上限（≤16）；
- `truncateFuzzItem`：把列名/类型/注释各自截断到 1KB；
- `assertFuzzNoTempFiles`：失败后检查目录内无临时文件。

其余 fuzz 专用 helper 留在对应源码测试文件，不跨模块聚合：

- `columnTypeToGoType_test.go`：`fuzzColumnTypeRefs`；
- `toCase_test.go`：`fuzzSplitWordsExpectations`、`fuzzPascalExpectations`、`fuzzJSONTagExpectations`、`fuzzAssertJSONTagShape`；
- `generate_test.go`：`fuzzShouldTypeCheck`、`fuzzAssertGeneratedStruct`、`fuzzFindGeneratedStruct`、`fuzzExprString`；
- `build_test.go`：`fuzzExistingSource`、`fuzzNewGeneratedCode`、`fuzzUserDeclTexts`、`fuzzFreeComments`、`fuzzGeneratedSpans`、`fuzzImportSpecs`、`fuzzExtraFragmentExpectations`。

Fuzz 函数本身写入对应源码的既有测试文件（`generate_test.go`、`build_test.go`、`toCase_test.go`、`columnTypeToGoType_test.go`），不新建跨模块聚合文件。若后续体量过大，再按 `{source}_fuzz_test.go` 拆分（zcdb 已有先例）。

## 七、确定性与环境约束

- 每个 fuzz worker 使用独立 `t.TempDir()`；每次输入写入完整初始文件，不依赖上一个输入的状态。
- `packageNameOf` 依赖当前工作目录；fuzz 应在包目录或临时目录内运行，不依赖相对 `.` 的隐式路径。
- `goFileNameConflict` 使用 ambient `GOOS/GOARCH`；平台相关用例只断言“拒绝或产物可解析”，不锁定具体平台冲突清单。
- `format.Source` 只保证语法；`typeCheckGoSource` 受 `importer.Default()` 限制，自定义 import 场景跳过类型检查。`FuzzGenerate` 对成功输入按内容哈希抽样（约 1/16）执行编译级检查，以保持吞吐；完整类型检查由定向用例覆盖。
- 表名/文件名必须是合法 UTF-8（Windows 会把非法字节替换为 U+FFFD，导致路径不一致）；非法 UTF-8 由 `Generate` 前置拒绝，fuzz 将其归入错误路径。
- 禁止在 fuzz 目标内使用 `t.Parallel`；限制列数、字符串长度和输入规模，避免文件 I/O 放大耗时。
- 显式处理所有 `os.ReadFile`/`os.WriteFile`/`os.Stat`/`os.ReadDir` 错误；fuzz 中不允许用 `_ =` 忽略。

## 八、执行与 CI 策略

**本地/PR**

```bash
go test ./zcmodel          # 只跑 f.Add 种子
go vet ./zcmodel
```

**夜间探索**（每个目标单独跑，避免一次 `-fuzz` 匹配多个目标）

```bash
for target in FuzzColumnTypeToGoType FuzzNameConversion FuzzGenerate FuzzWriteOrReplaceStruct FuzzAssembleDDLFragment; do
  go test ./zcmodel -run '^$' -fuzz "^${target}$" -fuzztime=30s
done
```

- 建议先短跑 30s 验证目标可执行，再在夜间任务延长到 5–10 分钟；
- 失败输入自动写入 `zcmodel/testdata/fuzz/<Target>/`，应提交最小化语料并补定向回归；
- 不在 PR 中跑长时间 fuzz，避免 CI 时间不可控；
- 外部引擎目标仅夜间运行，并要求本地 MySQL/PostgreSQL 容器可达。

**实现顺序**

1. 类型映射（最快、纯函数）
2. 命名转换
3. DDL/tag 片段
4. Generate 端到端（临时目录 + parser/format）
5. 增量 AST 合并
6. 可选真库目标

## 九、风险与限制

- **oracle 可能错**：映射表/命名规则期望应以文档和既有测试为准；fuzz 失败先按“实现问题 / oracle 问题”分流。关键期望使用手写字面量表（类型映射、命名转换、EXTRA），不要用被测函数生成期望。
- **fuzz 不覆盖并发/资源泄漏/DB 语义**：并发与故障注入仍需单独测试。
- **文件 I/O 较慢**：增量 AST 目标每次输入都要读写临时文件，`-fuzztime` 不宜过长，列数/文件大小必须设上限。实测（Windows）`FuzzWriteOrReplaceStruct` 约 20 execs/10s，`FuzzGenerate` 约 73 execs/10s；短跑用于验证目标可执行，实质性探索应延长夜间运行。
- **raw 变异存活率**：合并目标约一半输入走结构化骨架，保证成功路径覆盖率；raw 变异多数直接语法错误，只用于原子性/损坏文件路径。
- **平台差异**：Windows 文件名/权限、ambient `GOOS/GOARCH` 可能导致环境相关失败；文档和用例需明确跳过/归一化策略。
- **`format.Source` 不等价于可编译**：语法通过不代表类型/import 正确；`FuzzGenerate` 的编译级检查按内容哈希抽样，其余由定向用例覆盖。

## 十、维护约定

- 新增/删除 fuzz 目标时同步更新本文目标表、种子来源和命令。
- 代码、注释、文档、测试保持四处一致：实现契约变化时同步修改本文与 `zcmodel.md`。
- 不把 fuzz 当覆盖率工具：语句覆盖率由 `go test -cover` 衡量，fuzz 关注不变量与输入空间。
- 失败语料必须可复现且最小化；修复后保留语料作为长期回归。
