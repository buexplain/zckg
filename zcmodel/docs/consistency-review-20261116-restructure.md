# zcmodel 文档-实现一致性审查报告（2026-11-16，文档重构后）

> 审查基线：**工作区**（HEAD `26a978c7310f39892cf9761087257b607e23e056` + 未提交改动：zcmodel 文档拆分重构、`docs_examples_compile_test.go` 注释、上一轮 `build.go`/`build_test.go` 修复、根 README/AGENTS 链接更新）。审查对象是重构后的新文档集，行号均来自当前工作区；机械校验使用 `--worktree`。
>
> 本轮 6 条问题已按报告修复、1 条（DOC-05）复核为误报，未修改代码。审查期章节（审查范围、问题汇总、逐条详情、已核对、Go 示例覆盖）沿用审查时的工作区行号；修复采用等行数定点替换，行号不变，因此当前工作区同位置已是修复后文本，“文档证据”中的引用原文以详情概括与「修复执行记录」为准。

## 审查范围与方法

### 文档全集

模块文档（`zcmodel/docs/`，共 14 篇，审查报告自身排除）：

- `zcmodel/docs/README.md`：入口页（总览、30 秒数据流、核心概念、文档地图、阅读路径）。
- `zcmodel/docs/zcmodel.md`：兼容入口（旧链接）。
- `zcmodel/docs/quickstart.md`：快速开始与三类示例。
- `zcmodel/docs/api.md`：对外 API、Input/Column 等类型、错误表。
- `zcmodel/docs/generated-code.md`：产物契约。
- `zcmodel/docs/tags-ddl-index.md`：tag、ddl、索引块、注释净化。
- `zcmodel/docs/types-and-naming.md`：类型映射与命名转换。
- `zcmodel/docs/regeneration.md`：增量再生成契约。
- `zcmodel/docs/architecture.md`：文件职责、数据流、设计取舍。
- `zcmodel/docs/pipeline.md`：Generate 阶段与落盘安全。
- `zcmodel/docs/skeleton-merge.md`：骨架保持式合并算法。
- `zcmodel/docs/testing.md`：测试分层与排障。
- `zcmodel/docs/glossary.md`：术语与不变量。
- `zcmodel/docs/code-map.md`：文档 ↔ 源码符号 ↔ 测试追溯表。

`README.md`（仓库根）的 zcmodel 相关段落也纳入核对：模块表链接（第 17 行）、示例入口（第 154/162 行）、覆盖率行（第 183 行）。

### 对照源码与测试

- 源码：`zcmodel/input.go`、`zcmodel/generate.go`、`zcmodel/build.go`、`zcmodel/toCase.go`、`zcmodel/columnTypeToGoType.go`。
- 测试：`zcmodel/generate_test.go`、`zcmodel/build_test.go`、`zcmodel/toCase_test.go`、`zcmodel/columnTypeToGoType_test.go`、`zcmodel/input_test.go`、`zcmodel/integration_test.go`、`zcmodel/docs_examples_compile_test.go`、`zcmodel/testhelpers_test.go`。
- 交叉核对：`zcdb/schema_inspector.go`、`zcdb/db_dao.go`、`zcdb/pool.go`（仅用于 quickstart 的 Schema 桥接示例）。

### 基线与行号

- 基线：工作区（HEAD `26a978c7310f39892cf9761087257b607e23e056` + 未提交改动）。
- 行号来源：当前工作区文件；报告「审查范围/详情/已核对」中的 `文件:行` 均按工作区读取。
- 方法：正向逐声明比对 + 反向导出 API/错误分支审查 + 机械校验（链接、表格、示例解析、测试名/符号存在性、映射表差分、错误文本比对）+ 运行时探针（CRLF 行尾、文件读取错误文本）。

### 已执行的实测

- `go test ./zcmodel -count=1`、`-race`、`-shuffle=on -count=5`：通过；`go vet ./zcmodel`：通过；覆盖率 98.3%。
- `doc_go_examples.py --docs-dir zcmodel/docs --strict`：10 个 Go 块，8 个解析通过；2 个整文件示例因脚本强制前置 `package p` 报错，独立 `gofmt -e` 均通过。
- 链接与锚点检查：14 篇模块文档 + 2 份历史审查报告（报告自身排除），相对链接全部可解析。
- Markdown 表格单元一致性检查：通过。
- 文档中引用的全部 `Test*`/`Fuzz*` 名称存在性检查：通过；全部小写驼峰源码符号存在性检查：通过（测试 helper 除外，均在测试文件中）。
- 三方言映射表逐键差分：文档表与源码 map 完全一致（MySQL 32、PostgreSQL 36、SQLite 30 键）。
- 源码中 37 条 `fmt.Errorf`/`errors.New` 错误字面量的静态前缀均出现在 `api.md` 错误表。
- `go list` 确认 `zcmodel` 包只依赖标准库。
- overlay 探针：CRLF 存量文件再生成的行尾；目标路径为目录时的读取错误文本；无效显式 Type 导致的输出目录残留（仅用于排除误报，见下）。

### 问题统计与计入标准

- 问题总数 7：高 0、中 0、低 7；其中 6 条已修复，1 条（DOC-05）复核为误报。
- 计入：文档事实错误、文档内部术语/数字矛盾、对错误文本或测试覆盖的过度承诺、会改变产物字节但未说明的关键行为遗漏。
- 不计入：纯风格与措辞偏好；工具包装导致的整文件示例误报；历史审查报告（`consistency-review*.md`）中的基线行号。

## 问题汇总

| 编号 | 级别 | 状态 | 一句话结论 |
|---|---|---|---|
| DOC-01 | 低 | 已修 | `testing.md` 改以 `f.Add` 种子与 `testdata/fuzz` 失败语料区分，与同文一致。 |
| DOC-02 | 低 | 已修 | 兼容入口改为“阶段 0—5（六段）”，与 pipeline 一致。 |
| DOC-03 | 低 | 已修 | `api.md` 区分“校验类错误文本稳定”与“写文件类错误附带底层原文”，不再承诺全部文本。 |
| DOC-04 | 低 | 已修 | `generated-code.md` 改为“生成标记/生成器声明”，术语不再冲突。 |
| DOC-05 | 低 | 误报 | 复核后：`fuzzColumnTypeRefs` 覆盖全部映射键并全部注册为 fuzz 种子，普通 `go test` 会执行校验，原句成立。 |
| DOC-06 | 低 | 已修 | `testing.md` 改为“能发现 imported and not used 等错误”，与 helper 注释一致。 |
| DOC-07 | 低 | 已修 | `regeneration.md` 与 `skeleton-merge.md` 补充行尾统一为 LF（CRLF 输入会转换）。 |

## 逐条详情

### DOC-01：fuzz 种子位置描述错误且与同文矛盾

- 级别：低；类型：文档内部矛盾 / 文档错误
- 文档证据（修复前）：`zcmodel/docs/testing.md:27` 称“种子在 `testdata/fuzz/`，普通 `go test` 只跑种子”；同文 `zcmodel/docs/testing.md:70` 又称“失败的语料会写入 `testdata/fuzz/<Target>/`，作为永久回归种子保留”。
- 实现证据：不适用（纯文档问题：两处对 `testdata/fuzz` 的角色描述互斥）
- 测试证据：`zcmodel/build_test.go:2488`、`zcmodel/generate_test.go:1450`、`zcmodel/columnTypeToGoType_test.go:275`、`zcmodel/toCase_test.go:274` 均通过 `f.Add` 在测试函数内注册种子；`zcmodel/testdata/fuzz/FuzzGenerate/ec1badae5e0262d5:1` 是失败语料文件，且 `testdata/fuzz` 下只有 `FuzzColumnTypeToGoType`、`FuzzGenerate` 两个目录。
- 实测：列出 `testdata/fuzz` 目录，仅存在两个失败语料目录；五个 fuzz 目标的种子来自测试函数中的 `f.Add`，其中三个目标没有任何 `testdata/fuzz` 目录。
- 影响：维护者会去 `testdata/fuzz` 找“种子”，或把失败语料误当种子；新增 fuzz 目标没有语料目录时会被误判为配置缺失。
- 推荐方案：把 `testing.md` 第 27 行改为“`f.Add` 种子写在测试函数内；发现的失败语料写入 `testdata/fuzz/<Target>/`，作为永久回归语料”，与第 70 行统一。

### DOC-02：兼容入口的阶段数量与 pipeline 不一致

- 级别：低；类型：文档内部矛盾
- 文档证据（修复前）：`zcmodel/docs/zcmodel.md:17` 把 `pipeline.md` 描述为“`Generate` 五阶段”；`zcmodel/docs/pipeline.md:18` 到 `zcmodel/docs/pipeline.md:67` 依次是阶段 0、1、2、3、4、5，共六段。
- 实现证据：不适用（纯文档问题：阶段编号由 pipeline 文档自身定义）
- 测试证据：无（纯文档问题：阶段划分不涉及运行时行为）
- 实测：无需运行时；数 `pipeline.md` 的 `## 阶段 N` 标题为 0—5 共 6 个。
- 影响：读者按“五阶段”理解会漏掉一个阶段（输入校验/复制补全/字段名检查/组装/输出路径/合并落盘中的一段），或误判阶段编号。
- 推荐方案：改为“阶段 0—5（六段）”或“六阶段”；若坚持“五阶段”，需把阶段 0 的输入校验并入阶段 1 并重排编号，代价更大且与现文冲突，不推荐。

### DOC-03：“错误文本即契约”对写文件类错误过度承诺

- 级别：低；类型：过度承诺
- 文档证据（修复前）：`zcmodel/docs/api.md:14` 称“错误文本即契约，见下方错误表”。
- 实现证据：`zcmodel/build.go:599-601` 读取失败直接返回 `os.ReadFile` 的错误；`zcmodel/generate.go:162` 用 `%v` 包裹为 `生成结构体失败: ...`；`zcmodel/build.go:369`、`zcmodel/build.go:387` 的写临时文件/rename 错误同样附带底层错误文本。这些文本随 OS、语言环境与文件系统错误变化。
- 测试证据：`zcmodel/build_test.go:1325` 的 `TestWriteOrReplaceStruct_ReadFileFails` 只断言 `err != nil`，没有锁定具体文本。
- 实测：让目标路径指向一个目录，返回的文本为 `生成结构体失败: read C:\...\adir.go: Incorrect function.`（Windows 下的底层错误），换平台/语言会不同；校验类错误（表名、字段名、方言等）文本才是固定的。
- 影响：调用方若按完整错误文本匹配，会在不同平台或环境失效；文档给出了“全部错误文本都是契约”的预期。
- 推荐方案：改为“校验类错误文本是稳定契约；写文件类错误在固定前缀 `生成结构体失败:` 之后附带底层错误原文，随 OS/环境变化，不应按完整文本匹配”。

### DOC-04：“生成声明”一词在正文与术语表中含义冲突

- 级别：低；类型：文档内部矛盾
- 文档证据（修复前）：`zcmodel/docs/generated-code.md:26` 称“生成文件不写入任何生成声明（既不用 Go 官方的 `// Code generated ... DO NOT EDIT.` 标记，也不自拟分区声明）”；`zcmodel/docs/glossary.md:9` 定义“生成声明”为“Entity 结构体、`ToDO` 方法、DO 结构体、`ToEntity` 方法”。
- 实现证据：不适用（纯文档问题：同一术语被用于“生成器标记/声明”与“四个生成声明”两种含义）
- 测试证据：无（纯文档问题：术语使用冲突，无行为可测）
- 实测：无需运行时；对照正文与术语表即可判定。
- 影响：读者会误以为产物不应包含 Entity/DO/ToDO/ToEntity，与产物契约直接冲突；也会削弱术语表的权威性。
- 推荐方案：把正文改为“生成文件不写入任何生成标记/生成器声明（既不用官方标记，也不自拟分区声明）”，保留“生成声明”专指四个顶层声明。

### DOC-05：“测试会校验键集”的覆盖声明不实（误报，撤回）

- 级别：低；类型：文档错误（复核为误报）
- 文档证据：`zcmodel/docs/types-and-naming.md:49` 称三张表“与源码映射表逐键一致，测试会校验键集”。
- 实现证据：不适用（复核后撤回：该声明成立，文档无需修改）
- 测试证据：`zcmodel/columnTypeToGoType_test.go:164-248` 的独立期望表覆盖三方言全部映射键；`zcmodel/columnTypeToGoType_test.go:277-281` 把这些键全部注册为 fuzz 种子；`zcmodel/columnTypeToGoType_test.go:316-322` 对每个命中键断言归一化结果与 Go 类型。
- 实测：overlay 探针比对 `fuzzColumnTypeRefs()` 与 `makeColumnTypeToGoTypeMap()`，三方言 `missing_keys=[]`（mysql 32/refs 43、postgres 36/refs 42、sqlite 30/refs 37）；`go test ./zcmodel -run '^FuzzColumnTypeToGoType$' -count=1` 实际执行种子（`seed#326` 等 PASS），普通 `go test` 会跑这些种子。因此“测试会校验键集”成立，原判定为误报撤回。
- 影响：无；文档原句保留。
- 推荐方案：不修改文档。可选改进：在 `FuzzColumnTypeToGoType` 的注释中注明“映射键集校验来自 fuzz 种子”，避免读者只查 `TestNormalizeColumnTypeMatchesMap` 时误判（该注释变更不属本报告问题）。


### DOC-06：typeCheckGoSource 的 helper 描述与实际行为相反

- 级别：低；类型：文档错误
- 文档证据（修复前）：`zcmodel/docs/testing.md:77` 称 `typeCheckGoSource` “把产物放入临时 package 做 `go/types` 编译级检查（排除 “imported and not used”）”。
- 实现证据：不适用（纯文档问题：对 helper 行为的描述反了）
- 测试证据：`zcmodel/testhelpers_test.go:76-89` 的 helper 注释明确写着“能发现 imported and not used、字段与方法同名、未定义标识符等 go/parser 层面发现不了的错误”，且 `types.Config.Check` 返回任何错误都会 `t.Fatalf`，并没有过滤或排除该类错误。
- 实测：无需运行时；读 helper 注释与实现即可判定。
- 影响：维护者会误以为该 helper 会忽略未使用 import，进而在依赖它的用例里漏判该类编译错误；与测试质量要求中的“排除 imported and not used”误读叠加。
- 推荐方案：改为“能发现 imported and not used、字段与方法同名、未定义标识符等 parser 层发现不了的错误”，与 `testhelpers_test.go` 注释一致。

### DOC-07：CRLF 行尾被规范化为 LF 未在文档中说明

- 级别：低；类型：关键行为遗漏
- 文档证据（修复前）：`zcmodel/docs/regeneration.md:7` 称“用户声明与注释保留在原位置，整个文件最终经 gofmt 统一格式化”；`zcmodel/docs/skeleton-merge.md:17` 称“用户槽位原样输出”。
- 实现证据：`zcmodel/build.go:395-401` 的 `writeGeneratedFile` 对完整内容执行 `go/format.Source`，gofmt 输出 LF；因此 CRLF 存量文件重建后整体变为 LF，用户区行尾也变化。
- 测试证据：`zcmodel/build_test.go:1200-1203` 的“CRLF 文件移除整行不留空行”用例只断言 import 结果与不留空行，没有断言行尾；`zcmodel/docs/testing.md:25` 也只把 CRLF 列为已覆盖场景，未说明行尾转换。
- 实测：构造 CRLF 的存量文件（含用户方法 `// USER-CRLF`）再生成，产物统计为 `crlf_count=0 lf_count=16`；用户方法保留，但所有行尾变为 LF。
- 影响：Windows 用户首次再生成会看到整文件行尾 diff，或误以为用户代码被改写；文档的“原样保留”未限定行尾，容易造成预期偏差。
- 推荐方案：在 `regeneration.md` 的“边界与例外”中加一条“行尾：产物最终经 gofmt，CRLF 输入会规范为 LF（用户区行尾也会变化）”；同时把“文本内容不变”限定为“除 gofmt 格式（含行尾）外”。

## 已核对且未发现偏差的关键行为

- **文档结构与链接**：14 篇模块文档加根 README 的 zcmodel 链接全部可解析；`README.md` 入口、`zcmodel.md` 兼容入口与 AGENTS 的模块主文档条目一致，见 `zcmodel/docs/README.md:1`、`zcmodel/docs/zcmodel.md:1`、`README.md:17`。
- **三方言映射表**：逐键差分与源码 map 完全一致（MySQL 32、PostgreSQL 36、SQLite 30），归一化规则与示例一致，见 `zcmodel/docs/types-and-naming.md:9`、`zcmodel/columnTypeToGoType.go:18-123`、`zcmodel/columnTypeToGoType_test.go:13-152`。
- **错误表**：源码 37 条错误字面量的静态前缀全部出现在 `api.md` 错误表；错误文本 `%q` 说明与实现一致，见 `zcmodel/docs/api.md:17-46`、`zcmodel/generate.go:30-283`。
- **tag 顺序/转义**：json → 列映射 → primary_key → ddl → description；含反引号整条 tag 改用解释型字符串，见 `zcmodel/docs/generated-code.md:28-46`、`zcmodel/docs/tags-ddl-index.md:5-17`、`zcmodel/build.go:83-105`、`zcmodel/build_test.go:139`。
- **ddl 片段**：Type 原样、Nullable 三态、Default 指针与空串 `DEFAULT ''`、EXTRA 白名单/精度、`DEFAULT_GENERATED` 过滤，见 `zcmodel/docs/tags-ddl-index.md:18-38`、`zcmodel/build.go:140-207`、`zcmodel/build_test.go:1854`。
- **索引块**：PRIMARY → UNIQUE（名字典序）→ 普通（名字典序）、主键行隐藏物理索引名、三空格清单、换行/成对引号净化，见 `zcmodel/docs/tags-ddl-index.md:40-66`、`zcmodel/build.go:229-258`、`zcmodel/build_test.go:1947`。
- **再生成契约**：生成声明原位替换、用户声明与注释保留、重复声明去重、缺失声明落点、单 Spec 块展开条件、同落点排序、分号分隔处理，均有实现与测试对应，见 `zcmodel/docs/regeneration.md:9-24`、`zcmodel/docs/skeleton-merge.md:79-147`、`zcmodel/build.go:590-1258`。
- **import 增删/别名/冲突**：不同名别名与 `_`/`.` 导入会补默认导入、同名别名不重复、本地名冲突报错、不再引用的旧生成 import 移除，见 `zcmodel/docs/skeleton-merge.md:100-120`、`zcmodel/build.go:436-464`、`zcmodel/build_test.go:529、`zcmodel/build_test.go:585`、`zcmodel/build_test.go:610`、`zcmodel/build_test.go:1145``。
- **落盘安全**：gofmt 自校验、同目录临时文件 + rename、权限位保留、解析失败不覆盖，见 `zcmodel/docs/pipeline.md:67-94`、`zcmodel/build.go:359-401`、`zcmodel/build_test.go:456`。
- **代码地图可追溯性**：`code-map.md` 引用的全部 `Test*`/`Fuzz*` 名称在测试文件中真实存在；源码符号存在性检查通过（测试 helper 除外），见 `zcmodel/docs/code-map.md:5-36`。
- **依赖与导出面**：`go list` 确认包只依赖标准库；`go doc -all ./zcmodel` 的导出符号均已在 `api.md` 覆盖（Generate、Input/Column/StructFieldInfo/IndexInfo、NameCase/IsValid、Dialect、命名转换函数），见 `zcmodel/docs/api.md:5-14`、`zcmodel/input.go:3-89`、`zcmodel/toCase.go:9-188`。
- **quickstart 示例**：三个示例与 `docs_examples_compile_test.go` 镜像一致；整文件示例独立 `gofmt -e` 通过，见 `zcmodel/docs/quickstart.md:9`、`zcmodel/docs/quickstart.md:87`、`zcmodel/docs_examples_compile_test.go:21-166`。

## Go 示例覆盖

| 文档#章节#序号 | 语法/结构 | 证据/原因 |
|---|---|---|
| `api.md#Generate#1` | 通过 | 脚本包装下 `gofmt -e` 解析通过；见 `zcmodel/docs/api.md:7`。 |
| `api.md#Column-StructFieldInfo-IndexInfo#1` | 通过 | 完整类型声明可解析；见 `zcmodel/docs/api.md:67`。 |
| `generated-code.md#Entity-与-DO-结构体#1` | 通过 | 完整结构体声明可解析；见 `zcmodel/docs/generated-code.md:41`。 |
| `generated-code.md#ToDO-ToEntity-互转方法#1` | 通过 | 无函数体的方法声明语法合法；见 `zcmodel/docs/generated-code.md:85`。 |
| `quickstart.md#1.-最小可运行示例#1` | 通过（独立文件） | 整文件示例；脚本强制前置 `package p` 才报错，独立 `gofmt -e` 通过；见 `zcmodel/docs/quickstart.md:9`。 |
| `quickstart.md#2.-生成出来长什么样#1` | 通过 | 结构体片段可解析；见 `zcmodel/docs/quickstart.md:51`。 |
| `quickstart.md#4.-配合-zcdb-Schema-从真实数据库生成#1` | 通过（独立文件） | 整文件示例；脚本包装误报，独立 `gofmt -e` 通过；见 `zcmodel/docs/quickstart.md:87`。 |
| `quickstart.md#5.-显式指定字段信息-覆盖自动推导#1` | 通过 | 已包进 `func example()`；见 `zcmodel/docs/quickstart.md:165`。 |
| `README.md#最短示例#1` | 通过 | 函数片段可解析；见 `zcmodel/docs/README.md:66`。 |
| `tags-ddl-index.md#索引块#1` | 通过 | 完整结构体声明（含占位字段注释）可解析；见 `zcmodel/docs/tags-ddl-index.md:46`。 |

> 说明：脚本对两个整文件示例的失败是工具包装导致的（强制前置 `package p` 与示例自带的 `package main` 冲突），历史审查报告采用同一判定；独立 `gofmt -e` 均通过。

## 修复执行记录

- **DOC-01（已修）**
  - 文档：`zcmodel/docs/testing.md:27` 与 `zcmodel/docs/testing.md:70` 统一为“`f.Add` 种子写在测试函数内；发现的失败语料写入 `testdata/fuzz/<Target>/`，普通 `go test` 会重放”，不再把失败语料称为种子。
- **DOC-02（已修）**
  - 文档：`zcmodel/docs/zcmodel.md:17` 改为“`Generate` 阶段 0—5（六段）、校验顺序、落盘安全”。
- **DOC-03（已修）**
  - 文档：`zcmodel/docs/api.md:14` 改为“校验类错误文本稳定（见下方错误表）；写文件类错误在固定前缀 `生成结构体失败:` 后附带底层错误原文，随 OS/环境变化，请勿按完整文本匹配”。
- **DOC-04（已修）**
  - 文档：`zcmodel/docs/generated-code.md:26` 改为“生成文件不写入任何生成标记/生成器声明（既不用 Go 官方 ... 标记，也不自拟分区声明）”，保留“生成声明”专指四个顶层声明。
- **DOC-05（误报，无改动）**
  - 复核：`fuzzColumnTypeRefs()` 覆盖全部映射键（探针 `missing_keys=[]`），且这些键全部作为 fuzz 种子注册，普通 `go test` 会执行并校验；原句成立，撤回。详见详情。
- **DOC-06（已修）**
  - 文档：`zcmodel/docs/testing.md:77` 改为“能发现 imported and not used、字段与方法同名、未定义标识符等 parser 层发现不了的错误”，与 `zcmodel/testhelpers_test.go:76` 注释一致。
- **DOC-07（已修）**
  - 文档：`zcmodel/docs/regeneration.md:7` 补充“行尾统一为 LF，CRLF 输入再生成后变为 LF”；`zcmodel/docs/regeneration.md:113` 的边界条目补充“行尾 CRLF 规范为 LF”；`zcmodel/docs/skeleton-merge.md:17` 补充“最终经 gofmt，行尾统一为 LF”。
  - 说明：未新增行尾锁死测试；若要锁死该行为，可另行新增 CRLF→LF 断言（现有用例仅覆盖 CRLF 输入下的行删除，见 `zcmodel/build_test.go:1200-1203`）。

## 实际验收结果

| 验收项 | 结果 |
|---|---|
| 测试 | `go test ./zcmodel -count=1` 通过 |
| 竞态/乱序 | `-race`、`-shuffle=on -count=5` 通过 |
| 静态与格式 | `go vet ./zcmodel` 通过；本次未改动 Go 文件 |
| 覆盖率 | 98.3% |
| 文档示例 | 10 个 Go 块中 8 个 `gofmt -e` 通过；2 个整文件示例仍为脚本包装误报，独立 `gofmt -e` 通过 |
| 链接/表格 | 14 篇模块文档 + 2 份历史报告链接全部可解析；表格一致 |
| 映射键集 | 文档表与源码一致；`fuzzColumnTypeRefs` 覆盖全部键（`missing_keys=[]`），DOC-05 撤回 |
| 报告校验 | `validate_report.py --worktree`：OK，7 个问题、7 条详情、0 warning |

### 逐条修复验收

| 编号 | 修复落点 | 验收结果 |
|---|---|---|
| DOC-01 | `zcmodel/docs/testing.md:27`、`zcmodel/docs/testing.md:70` | 两处均以 `f.Add` 种子 / `testdata/fuzz` 失败语料表述，语义一致；旧句“种子在 testdata/fuzz”已不存在。 |
| DOC-02 | `zcmodel/docs/zcmodel.md:17` | 改为“阶段 0—5（六段）”，与 `zcmodel/docs/pipeline.md:18` 至 `zcmodel/docs/pipeline.md:67` 的六个阶段标题一致。 |
| DOC-03 | `zcmodel/docs/api.md:14` | 校验类错误文本稳定与写文件类错误附带底层原文已分开表述；不再承诺全部错误文本可按完整文本匹配。 |
| DOC-04 | `zcmodel/docs/generated-code.md:26` | 改为“生成标记/生成器声明”，与 `zcmodel/docs/glossary.md:9` 的“生成声明=四个顶层声明”不再冲突。 |
| DOC-05（误报） | 无改动 | 探针 `missing_keys=[]`；`go test ./zcmodel -run '^FuzzColumnTypeToGoType$' -count=1` 执行全部种子并通过，原句“测试会校验键集”成立。 |
| DOC-06 | `zcmodel/docs/testing.md:77` | 改为“能发现 imported and not used 等错误”，与 `zcmodel/testhelpers_test.go:76` 的 helper 注释一致。 |
| DOC-07 | `zcmodel/docs/regeneration.md:7`、`zcmodel/docs/regeneration.md:113`、`zcmodel/docs/skeleton-merge.md:17` | 三处均说明最终经 gofmt、行尾统一为 LF（CRLF 输入会转换）。 |

