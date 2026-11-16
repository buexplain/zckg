# zcmodel 文档-实现一致性审查报告（2026-11-16）

> 审查基线：HEAD `26a978c7310f39892cf9761087257b607e23e056`。审查时 `zcmodel/` 下受审文件与基线一致；工作区仅有其他模块（zcconfig / zcdb / zchttp）与 README 的未提交改动，不在本次范围。本轮 5 条问题修复前状态为“待修”，现已全部修复；审查期章节（审查范围、问题汇总、逐条详情、已核对、Go 示例覆盖）中的 `文件:行` 指向修复前基线，仅用于追溯；“修复执行记录”与“实际验收结果”指向当前工作区。
>
> 后续变更（2026-11-16）：`zcmodel/docs/zcmodel.md` 已拆分重构为 `README.md` + 分主题文档；修复执行记录中的文档位置已更新为重构后的对应文件，审查期章节的行号仍按重构前基线，验收表中的文档示例统计也按重构前文档计算。

## 审查范围与方法

### 文档全集

- `zcmodel/docs/zcmodel.md`：模块用户文档全文（606 行），覆盖 API、生成流程、类型映射、增量再生成、注释净化与注意事项。
- `README.md`：仅审查与 zcmodel 相关的段落——第 17 行模块表、第 162 行示例入口、第 183 行测试覆盖率、第 202 行目录说明。工作区对 README 的未提交改动只涉及 zcdb 覆盖率行与实测日期，zcmodel 行未变。

### 对照源码与测试

- 源码：`zcmodel/input.go`、`zcmodel/generate.go`、`zcmodel/build.go`、`zcmodel/toCase.go`、`zcmodel/columnTypeToGoType.go`。
- 测试：`zcmodel/generate_test.go`、`zcmodel/build_test.go`、`zcmodel/toCase_test.go`、`zcmodel/columnTypeToGoType_test.go`、`zcmodel/input_test.go`、`zcmodel/integration_test.go`、`zcmodel/docs_examples_compile_test.go`、`zcmodel/testhelpers_test.go`。
- 交叉核对（仅用于核对文档中的桥接示例）：`zcdb/schema_inspector.go`、`zcdb/db_dao.go`、`zcdb/pool.go`。

### 基线与行号

- 基线提交：`26a978c7310f39892cf9761087257b607e23e056`。
- 审查时 `zcmodel/` 全部受审文件与基线逐字节一致；审查期章节的 `文件:行` 均相对该基线。
- 审查方法：正向逐声明比对 + 反向导出 API 审查 + 运行时探针。探针以 `go test -overlay` 把临时 `_test.go`（必要时连同基线版 `build.go`）映射进 `zcmodel` 包执行，不修改仓库文件；探针与临时文件已清理。

### 已执行的实测

- `go test ./zcmodel -count=1`：通过（MySQL / PostgreSQL / SQLite 集成用例实际执行）。
- `go test ./zcmodel -count=1 -race`：通过；`go test ./zcmodel -shuffle=on -count=5`：通过。
- `go vet ./zcmodel`：通过；`go test ./zcmodel -cover -count=1`：`coverage: 98.3% of statements`，与 `README.md:183` 一致。
- `doc_go_examples.py --docs-dir zcmodel/docs --strict`：8 个 Go 块中 6 个解析通过；2 个整文件示例（含 `package main`）在脚本强制前置 `package p` 的包装下报错，独立 `gofmt -e` 均通过，属工具包装误报。
- overlay 探针（基线代码）：复现 DOC-01（分号分隔声明报错）、DOC-02（块内 Spec 注释同名替换失败）、DOC-03（同一落点插入顺序）、DOC-04（`(`/`)` 同行注释触发保留块）；DOC-05 用 `go doc -all ./zcmodel` 核对导出清单。
- 逐条核对了三方言映射表键集、错误表全部错误文本与触发条件、tag 顺序与转义、ddl 片段、索引块排序与净化、import 增删、单 Spec 块展开、再生成幂等性与两个使用示例。

### 问题统计与计入标准

- 问题总数 5：高 0、中 1、低 4。
- 计入：合法输入被拒绝、文档承诺的生成行为不成立、文档对公开 API 的表述不实、文档遗漏会改变产物的保留条件。
- 不计入：纯风格差异、内部实现细节、其他模块工作区改动、工具包装导致的整文件示例误报。

## 问题汇总

| 编号 | 级别 | 状态 | 一句话结论 |
|---|---|---|---|
| DOC-01 | 中 | 已修 | 显式分号分隔的合法存量文件（如 `package p; var X = 1`）再生成时报语法错误，违背“只有语法错误才失败”的契约。 |
| DOC-02 | 低 | 已修 | 块内 Spec 形态转换按子串搜索 `type <结构体名>`，Doc 注释或 tag 值含同样文字时替换错位、报语法错误。 |
| DOC-03 | 低 | 已修 | 配对插入与无配对追加落在同一位置时，实际顺序是“配对方法在前”，与文档承诺的标准顺序不符。 |
| DOC-04 | 低 | 已修 | 文档称单 Spec 括号块仅在“块级 Doc / 块内游离注释”时保留，实际 `(`/`)` 同行注释也会保留括号块。 |
| DOC-05 | 低 | 已修 | 文档称 `toCase.go` 的命名转换函数均为导出 API，但 `formatJSONTag` 是内部函数。 |

## 逐条详情

### DOC-01：显式分号分隔的顶层声明导致再生成报错

- 级别：中；类型：实现缺陷 / 兼容性缺陷
- 文档证据：`zcmodel/docs/zcmodel.md:411` 声明目标文件已存在时“用户声明的文本内容与顺序不变”，`zcmodel/docs/zcmodel.md:599` 只把“用户代码存在语法错误”列为失败条件，即语法正确的存量文件应能再生成。
- 实现证据：`zcmodel/build.go:886` 以 package 行结束处作为第一个声明的 lead 起点，`zcmodel/build.go:898` 以每个声明的 `End()` 作为下一个声明的 lead 起点，二者都不跳过显式分号；`zcmodel/build.go:636` 判定行尾注释时也只允许空格/制表符，因此 `type A struct{}; var X = 1` 会把 `;` 带进下一个声明前缀，重建出孤立分号。
- 测试证据：`zcmodel/build_test.go:1042` 覆盖声明间游离注释、`//go:generate`、同行尾注释与文件尾注释，但所有用例都以换行分隔声明，没有显式分号用例。
- 实测：以基线 `build.go` 执行 overlay 探针，输入 `package model\n\ntype A struct{}; var X = 1\n` 返回 `生成代码存在语法错误: 18:1: expected declaration, found ';'`；同一探针下 `package model; var X = 1`、同一行 `import "fmt"; import "strings"` 也以 `expected declaration, found ';'` 失败（孤立分号所在行号随重建内容不同）。
- 影响：Go 语法正确的存量文件无法再生成，`Generate` 直接返回错误且不写文件；用户只能手工删除分号后重试，违背“语法正确即可增量再生成”的公开契约。
- 推荐方案：把声明分隔符（空格、制表符、显式分号）统一跳过，声明区间不含分号；行尾注释的同行判定允许分隔符中间有分号。补回归用例覆盖 package 与声明同行、用户声明同行、生成声明同行、同行 import 与文件尾分号。若保持现状，文档必须写明“显式分号分隔的存量文件不受支持”，但这与上述契约直接矛盾。

### DOC-02：块内 Spec 形态转换按子串定位 type 关键字，注释同名时替换错位

- 级别：低；类型：实现缺陷
- 文档证据：`zcmodel/docs/zcmodel.md:419` 承诺“块内生成 Spec 就地替换为新结构体”，即保留块形态时生成声明应被正确替换。
- 实现证据：`zcmodel/build.go:583-595` 的 `specTextFor` 用 `strings.Index(declText, "type "+name)` 找第一次出现的字样并删除其后的 `type `；当结构体的 Doc 注释（来自 `TableComment`/`Database`）或字段 tag 值先出现同样文字时，删除的是注释里的字样，真正的 `type` 关键字被留在 `type (...)` 块内。
- 测试证据：`zcmodel/build_test.go:1553` 与 `zcmodel/build_test.go:1585` 覆盖单 Spec 块的展开/保留，但块内生成结构体的 Doc 注释与 tag 均为常规文本，没有“注释含同名字样”的用例。
- 实测：以基线 `build.go` 执行 overlay 探针，混合块内生成 `UserEntity`、其 doc 注释含 `type UserEntity` 字样，返回 `生成代码存在语法错误: 7:1: expected 'IDENT', found 'type' (and 2 more errors)`；原文件保持不变。修复后用同一探针输入输出块内 `UserEntity struct` 且注释完整。
- 影响：合法输入（表注释/数据库名恰好含 `type <结构体名>`）在保留 type 块形态的存量文件上再生成失败；虽不写坏文件，但用户无法理解失败根因。
- 推荐方案：用 AST 定位 `type` 关键字与类型名（`GenDecl.Pos()` 与 `TypeSpec.Pos()` 的源码偏移差），按偏移删除关键字，不依赖子串位置；补直接单测与端到端回归。若坚持字符串搜索，至少锚定“行首的 `type <name>`”，但块注释中的行首同样字仍会误判。

### DOC-03：同一落点上配对插入先于无配对声明，与文档的标准顺序不符

- 级别：低；类型：实现缺陷 / 误导性契约
- 文档证据：`zcmodel/docs/zcmodel.md:420` 声明缺失声明“按标准顺序 Entity 结构体 → ToDO → DO 结构体 → ToEntity 排列”，并称块内结构体场景下“标准顺序只在独立声明之间保证”。
- 实现证据：`zcmodel/build.go:1126-1159` 先按配对关系把结构体/方法放进 `beforeMap`，再把无配对声明按种类排序后追加到同一位置；`zcmodel/build.go:1161-1175` 直接按切片顺序输出，因此同一落点上配对方法总在无配对的 Entity/ToDO 之前。
- 测试证据：`zcmodel/build_test.go:1527` 覆盖“无任何生成声明”与“追加到生成区末尾”的无配对落点，`zcmodel/build_test.go:1499` 覆盖混合块内两个方法都缺失，但没有覆盖“块内只有 DO、Entity 与两个方法都缺失”的组合。
- 实测：以基线 `build.go` 执行 overlay 探针，存量文件为仅含 `UserDO` 的 `type (...)` 块，新生成代码包含全部四个声明，产物顺序为 `UserDO`（块）→ `ToEntity` → `UserEntity` → `ToDO`；三个独立插入声明未按文档的标准顺序排列。
- 影响：仅影响首次补入缺失声明时的声明顺序，产物仍可编译；但文档把顺序写入契约，依赖 golden/diff 或按文档阅读生成文件的用户会被误导。
- 推荐方案：同一落点上的插入统一按 `genKind` 排序后再输出（配对关系只决定插入位置，不决定同位置内的先后）；补“块内只有 DO、其余三个声明缺失”的回归用例。若保持现状，文档应改为“配对插入先于无配对追加”，但这样同一文件在不同缺失组合下会出现两套顺序规则，不推荐。

### DOC-04：单 Spec 括号块的保留条件遗漏 `(`/`)` 同行注释

- 级别：低；类型：文档错误
- 文档证据：`zcmodel/docs/zcmodel.md:419` 称“仅含一个生成 Spec 且块内没有游离注释、也没有块级 Doc 的声明以独立声明形态输出（不保留括号块），存在这些注释时保留括号块”。
- 实现证据：`zcmodel/build.go:968-982` 在单 Spec 块上扫描整个声明区间：块级 Doc 之外，凡是位于生成 Spec 自身区间之外的注释（`type (` 同行注释、`)` 行尾注释、块内游离注释）都会使 `keepBlock` 为真，保留括号块。
- 测试证据：`zcmodel/build_test.go:1585` 覆盖块级 Doc + 块内游离注释，`zcmodel/build_test.go:1653` 覆盖块前游离注释（区间之外、不触发保留），但没有 `(`/`)` 同行注释用例。
- 实测：overlay 探针构造 `type ( // AFTER-LPAREN` 与 `) // AFTER-RPAREN` 两种存量文件，再生成产物均保留 `type (` 与对应注释；按文档给出的条件（无块内游离注释、无块级 Doc）应展开为独立声明。
- 影响：文档对“何时保留括号块”的行为描述不完整，用户无法从文档预测再生成后的形态；行为本身（保留注释）是合理的，问题在文档。
- 推荐方案：把保留条件改为“生成 Spec 区间外存在任何其他注释（块级 Doc、块内游离注释、`(`/`)` 同行注释）”；同步源码注释，并补 `(`/`)` 同行注释的回归用例锁定该行为。

### DOC-05：文档把内部函数 formatJSONTag 表述为导出 API

- 级别：低；类型：文档错误
- 文档证据：`zcmodel/docs/zcmodel.md:384` 称“`toCase.go` 中的命名转换函数均为导出 API，可直接复用”，随后在 `zcmodel/docs/zcmodel.md:405` 单独以 `### formatJSONTag` 介绍该函数。
- 实现证据：`zcmodel/toCase.go:11` 的 `formatJSONTag` 为小写开头、不导出；`zcmodel/toCase.go:140` 的 `lowerFirst` 同样不导出。`go doc -all ./zcmodel` 仅列出 `Generate`、`SplitWords`、`ToPascalCase` 与六个风格函数。
- 测试证据：无（纯文档问题：导出性由 `go doc` 直接反映，不需要行为测试）。
- 实测：`go doc -all ./zcmodel` 的 FUNCTIONS 段没有 `FormatJSONTag`；尝试以调用方身份使用该名字无法编译。
- 影响：读者可能尝试调用不存在的导出函数；也弱化了“导出 API 清单”的准确性。
- 推荐方案：把表述收窄为“`toCase.go` 中导出的命名转换函数可直接复用”，并在 `formatJSONTag` 小节标注“内部函数，不对外导出”。

## 已核对且未发现偏差的关键行为

- **三方言映射表完整性**：探针打印三方言 map 键集，与文档三张表逐键一致（MySQL 32、PostgreSQL 36、SQLite 30）；归一化规则与示例一致，见 `zcmodel/docs/zcmodel.md:352-377`、`zcmodel/columnTypeToGoType.go:18-123`、`zcmodel/columnTypeToGoType_test.go:13-152`。
- **Generate 主流程与 tag 顺序**：Entity/DO 字段、`json → 列映射 → primary_key → ddl → description` 顺序、DO 的 `any` 类型、ToDO/ToEntity 签名与赋值/断言语句均有行级断言，见 `zcmodel/docs/zcmodel.md:224-233`、`zcmodel/build.go:260-318`、`zcmodel/generate_test.go:23`。
- **错误表与触发条件**：逐条比对错误文本（方言、表名/文件名校验、Go 文件名规则、tag 名、nil 列、字段名四类校验、输出目录/路径逃逸、import 别名冲突、写文件错误）与实现，格式与场景一致，见 `zcmodel/docs/zcmodel.md:94-125`、`zcmodel/generate.go:30-283`。
- **错误文本 `%q` 契约**：列名/字段名/结构体名前缀/tag 名用 `%q`，非法 UTF-8 表格名/文件名用 `%q`，其余原样输出，与实现一致，见 `zcmodel/docs/zcmodel.md:125`、`zcmodel/generate.go:171-256`、`zcmodel/generate_test.go:613`。
- **ddl 片段**：Type 原样、Nullable 三态、Default 指针语义与空串 `DEFAULT ''`、EXTRA 白名单与 `(n)` 精度、`DEFAULT_GENERATED` 过滤，见 `zcmodel/docs/zcmodel.md:275-294`、`zcmodel/build.go:117-207`、`zcmodel/build_test.go:1755`。
- **索引块**：PRIMARY → UNIQUE（名字典序）→ 普通（名字典序）、主键行隐藏物理索引名、三空格清单、换行/成对引号净化，见 `zcmodel/docs/zcmodel.md:296-320`、`zcmodel/build.go:209-258`、`zcmodel/build_test.go:1848`。
- **tag 转义与反引号保留**：反引号使整个 tag 改用解释型字符串字面量，`reflect.StructTag` 可还原原值，见 `zcmodel/docs/zcmodel.md:434-440`、`zcmodel/build.go:83-105`、`zcmodel/build_test.go:139`。
- **import 增删**：不同名别名/`_`/`.` 导入不视为已存在、同名别名不重复追加、同名冲突报错不写文件、旧生成 import 移除，见 `zcmodel/docs/zcmodel.md:421`、`zcmodel/build.go:417-464`、`zcmodel/build_test.go:585`。
- **单 Spec 块展开、重复声明去重与配对落点**：常规单 Spec 块展开为独立声明、块级 Doc/游离注释保留块形态、重复生成声明只保留首个，见 `zcmodel/build.go:732-751`、`zcmodel/build.go:956-1058`、`zcmodel/build_test.go:1553`、`zcmodel/build_test.go:1499`。
- **落盘安全**：`go/format.Source` 自校验、同目录临时文件 + rename、解析失败不覆盖、权限位保留，见 `zcmodel/docs/zcmodel.md:428-430`、`zcmodel/build.go:359-401`、`zcmodel/build_test.go:456`。
- **再生成幂等性**：独立声明、混合 type 块、含块级 Doc 的单 Spec 块、无生成声明四类存量文件连续再生成两次/三次产物逐字节一致（overlay 探针）。
- **文档示例可编译性**：`zcmodel/docs_examples_compile_test.go:141` 实际执行手工示例并核对索引块、主键 tag、NULL 标记与默认值片段；两个整文件示例经外部测试包编译级核对，见 `zcmodel/docs_examples_compile_test.go:52`、`zcmodel/docs_examples_compile_test.go:79`。
- **README 覆盖率**：`README.md:183` 的 zcmodel 98.3% 与本次 `go test ./zcmodel -cover -count=1` 实测一致。

## Go 示例覆盖

| 文档#章节#序号 | 语法/结构 | 证据/原因 |
|---|---|---|
| `zcmodel.md#Generate#1` | 通过 | `gofmt -e` 在 `package p` 包装下解析通过；见 `zcmodel/docs/zcmodel.md:90`。 |
| `zcmodel.md#Column-StructFieldInfo-IndexInfo#1` | 通过 | 完整类型声明可解析；见 `zcmodel/docs/zcmodel.md:144`。 |
| `zcmodel.md#Entity-与-DO-结构体#1` | 通过 | 完整结构体声明可解析；见 `zcmodel/docs/zcmodel.md:237`。 |
| `zcmodel.md#索引块#1` | 通过 | 完整结构体声明（含占位字段）可解析；见 `zcmodel/docs/zcmodel.md:302`。 |
| `zcmodel.md#ToDO-ToEntity-互转方法#1` | 通过 | 两个无函数体的方法声明是语法合法 Go；见 `zcmodel/docs/zcmodel.md:326`。 |
| `zcmodel.md#手工构造-Input-生成#1` | 通过（独立文件） | 独立 `gofmt -e` 通过；脚本强制前置 `package p` 才报错，属工具包装误报；见 `zcmodel/docs/zcmodel.md:448`。 |
| `zcmodel.md#配合-zcdb-Schema-从真实数据库生成#1` | 通过（独立文件） | 独立 `gofmt -e` 通过；脚本包装误报；编译级镜像在 `zcmodel/docs_examples_compile_test.go:79`；见 `zcmodel/docs/zcmodel.md:490`。 |
| `zcmodel.md#显式指定字段信息-覆盖自动推导#1` | 通过 | 已包在 `func example()` 中，`gofmt -e` 解析通过；见 `zcmodel/docs/zcmodel.md:566`。 |

## 修复执行记录

- **DOC-01（已修）**
  - 实现：新增 `skipDeclSeparators` 跳过空格/制表符/显式分号（`zcmodel/build.go:682`），`prev` 初始化与每个声明之后均经它推进（`zcmodel/build.go:936`、`zcmodel/build.go:948`）；`lineCommentEnd` 判定同行注释时允许中间有分号（`zcmodel/build.go:665`）。
  - 测试：新增 `TestWriteOrReplaceStruct_SemicolonSeparatedDecls`（`zcmodel/build_test.go:1111`），覆盖 package 同行、用户声明同行、生成声明同行、同行 import、分号后行尾注释与文件尾分号，并做编译级检查。
  - 文档：`writeOrReplaceStruct` 注释补充“显式分号只作分隔符、不进入重建产物”（`zcmodel/build.go:545`）；用户文档无需改动（契约本就如此）。
- **DOC-02（已修）**
  - 实现：以 `structSpecText` 替换闭包 `specTextFor`（`zcmodel/build.go:509`），按 `GenDecl.Pos()` 与 `TypeSpec.Pos()` 的源码偏移删除 `type` 关键字，不再做子串搜索。
  - 测试：新增 `TestStructSpecText`（`zcmodel/build_test.go:2275`，覆盖 Doc/tag 同名字样、空文本、语法非法与类型名不匹配）与端到端 `TestWriteOrReplaceStruct_BlockSpecTextWithKeywordInComment`（`zcmodel/build_test.go:1761`）。
  - 文档：无需改动（文档承诺的“块内 Spec 就地替换”现已成立）。
- **DOC-03（已修）**
  - 实现：同一落点上的插入在输出前按 `genKind` 排序（`zcmodel/build.go:1211`），配对关系只决定插入位置。
  - 测试：新增 `TestWriteOrReplaceStruct_SharedInsertionOrder`（`zcmodel/build_test.go:1587`），锁定“块内只有 DO、其余三个声明缺失”时 `UserDO → UserEntity → ToDO → ToEntity` 的顺序。
  - 文档：改写 `zcmodel/docs/regeneration.md:115`（重构后位置）与 `zcmodel/docs/skeleton-merge.md:132`，明确“同一落点按标准顺序排列、配对只决定位置”，并删除不可达的“结构体缺失补在块前”表述；源码注释同步（`zcmodel/build.go:558`）。
- **DOC-04（已修）**
  - 文档：改写 `zcmodel/docs/regeneration.md:98` 与 `zcmodel/docs/skeleton-merge.md:94`（重构后位置）的保留条件为“生成 Spec 区间外存在其他注释（块级 Doc、块内游离注释、`(`/`)` 同行注释）”；源码注释同步（`zcmodel/build.go:1009`）。
  - 测试：新增 `TestWriteOrReplaceStruct_SingleSpecTypeBlockTrailingComment`（`zcmodel/build_test.go:1741`），锁定 `(`/`)` 同行注释保留括号块。
- **DOC-05（已修）**
  - 文档：`zcmodel/docs/types-and-naming.md:61` 改为“导出的命名转换函数”，并补充 `formatJSONTag` 为内部函数；`zcmodel/docs/types-and-naming.md:84` 标题标注“（内部函数）”。

## 实际验收结果

| 验收项 | 命令/用例 | 结果 |
|---|---|---|
| 全量测试 | `go test ./zcmodel -count=1` | 通过；集成用例实际执行。 |
| 竞态 | `go test ./zcmodel -count=1 -race` | 通过。 |
| 乱序重复 | `go test ./zcmodel -shuffle=on -count=5` | 通过。 |
| 静态检查 | `go vet ./zcmodel` | 通过，无输出。 |
| 格式 | 本次改动的 `build.go`、`build_test.go` 执行 `gofmt -l` | 无输出。 |
| 覆盖率 | `go test ./zcmodel -cover -count=1` | 98.3%，与 `README.md:183` 一致。 |
| 工作区检查 | `git diff --check` | 通过；仅有 Git 的 LF→CRLF autocrlf 提示。 |
| 文档片段 | `doc_go_examples.py --docs-dir zcmodel/docs --strict` | 按重构前的 `zcmodel.md` 统计：6/8 块解析通过；其余 2 个整文件示例独立 `gofmt -e` 通过，属脚本包装误报。 |
| DOC-01 回归 | 基线 vs 修复后 overlay 探针；`TestWriteOrReplaceStruct_SemicolonSeparatedDecls` | 修复前报 `expected declaration, found ';'`，修复后成功且产物通过编译级检查。 |
| DOC-02 回归 | 基线 vs 修复后 overlay 探针；`TestStructSpecText`、`TestWriteOrReplaceStruct_BlockSpecTextWithKeywordInComment` | 修复前报 `expected 'IDENT', found 'type'`，修复后块内 Spec 正确替换、注释原样保留。 |
| DOC-03 回归 | 基线 vs 修复后 overlay 探针；`TestWriteOrReplaceStruct_SharedInsertionOrder` | 修复前 `UserDO → ToEntity → UserEntity → ToDO`，修复后 `UserDO → UserEntity → ToDO → ToEntity`。 |
| DOC-04 回归 | `TestWriteOrReplaceStruct_SingleSpecTypeBlockTrailingComment` | `(`/`)` 同行注释均保留括号块与注释。 |
| DOC-05 回归 | `go doc -all ./zcmodel` 与文档措辞核对 | 导出清单与文档一致，`formatJSONTag` 已标注内部函数。 |
| 变异自检 | 以基线 `build.go` 与基线测试 overlay 运行探针，再以变异实现运行新增用例 | DOC-01/02/03 的新增用例在旧实现/变异实现下均失败，可真实拦住回归。 |

> 本轮改动仅涉及 `zcmodel/`，未提交；其他模块与 README 的未提交改动未触碰。
