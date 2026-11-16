# zcconfig 文档与实现一致性审查报告（重构后复审）

- 审查日期：2026-11-16
- 模块：`zcconfig`
- 审查对象：**文档重构后**的 zcconfig 文档集（`zcconfig/docs/README.md` + 7 个主题文档 + 兼容索引）
- 基线：工作区（`HEAD 26a978c` + 未提交改动：文档重构、`.env` 原文存储、数值转换安全子集等）
- 行号来源：工作区文件（本报告全部引用按工作区校验，校验命令使用 `--worktree`）
- 修复状态：**已修**（5 条全部修复并验收；修复记录与验收结果见文末）

> 与旧报告的关系：`consistency-review.md` 记录的是重构前单文件文档的审查结果（9 条已修）；本报告针对重构后的文档集重新执行全量审查。

## 审查范围与方法

**被审文档**（`zcconfig/docs/` 下的契约文档与过程文档）

| 文档 | 定位 |
|---|---|
| `zcconfig/docs/README.md` | 总览、30 秒模型、5 分钟上手、概念模型、设计公理、文档地图、术语表 |
| `zcconfig/docs/env.md` | Env 通道：装载、解析规则、存储语义、1MB 边界、`Env` 查找 |
| `zcconfig/docs/config.md` | Config 通道：`Register` 合并、`.` 路径、nil 三态、零拷贝 |
| `zcconfig/docs/cast.md` | 转换引擎：10 步判定树、字符串转换表、数值安全子集、reflect 兜底 |
| `zcconfig/docs/db-config.md` | `DBConfig` 字段、三种驱动 DSN 格式与转义、从库 |
| `zcconfig/docs/errors.md` | panic/error 二分、`TypeMismatchError`、诊断与排查表 |
| `zcconfig/docs/internals.md` | 数据结构、锁、调用链、原子性、零拷贝取舍、性能 |
| `zcconfig/docs/conventions.md` | 不变量、被否决方案、破坏性变更迁移、文档维护规则 |
| `zcconfig/docs/zcconfig.md` | 兼容索引入口（旧链接仍有效） |
| `README.md` | 根 README 的 zcconfig 摘要行（模块总览、示例入口） |
| 过程计划文档（`env-raw-value-plan.md`、`numeric-conversion-safety-plan.md`） | 审查时存在的过程产物；修复阶段已删除，不入库（处置见 DOC-04） |

**对照实现与测试**

| 文档主题 | 实现 | 锁死行为的测试 |
|---|---|---|
| Env 装载/读取 | `zcconfig/env.go` | `zcconfig/env_test.go` |
| Config 注册/读取 | `zcconfig/config.go` | `zcconfig/config_test.go`、`zcconfig/config_extra_test.go` |
| 转换引擎 | `zcconfig/cast.go` | `zcconfig/cast_test.go` |
| DBConfig | `zcconfig/dbConfig.go` | `zcconfig/dbConfig_test.go` |
| 文档示例 | — | `zcconfig/docs_examples_compile_test.go` |

**基线说明**：工作区不干净（本次文档重构与代码改动均未提交），因此行号以工作区文件为准，机械校验使用 `--worktree`。

**方法**

1. 正向逐声明比对：文档规则表、转换表、判定树、字段表、迁移表 → 源码分支与测试名。
2. 机械一致性：文档引用的全部 `Test*` 名与测试文件比对；Go 代码块按“完整 main / 片段”分类做语法检查；内部链接存在性检查。
3. 运行时实测（临时探针，跑完即删）：空值优先于 OS、切片→数组/数组指针、`Register(fn panic)` 传播、跨族加宽（`uint8→int16`）、bool 文本边界。
4. 反向审查：`go doc -all ./zcconfig` 导出 API 逐个核对文档覆盖；panic 分支与边界逐条找归属。

**问题统计**：5 条（高 0 / 中 0 / 低 5）。均为文档侧偏差，无实现缺陷。

**判定标准**：只计入会误导调用方契约、或文档内部/文档与实现矛盾、或关键行为缺失的问题；纯风格润色不计入。过程计划文档的过期引用单独计一条（DOC-04）。

## 问题汇总

| 编号 | 级别 | 状态 | 一句话结论 |
|---|---|---|---|
| DOC-01 | 低 | 已修 | README 术语表“跨族转换默认不安全”与 R2（无符号→更宽有符号属跨族但允许）矛盾 |
| DOC-02 | 低 | 已修 | conventions“EnvAll 返回拷贝”的否决理由写的是 `map[string]any` 值类型，那是 ConfigAll 的对象，与 EnvAll（`map[string]string`）不匹配 |
| DOC-03 | 低 | 已修 | errors.md“Register 不返回 error，也不会 panic”绝对化：用户 `fn` panic 会原样向上传播 |
| DOC-04 | 低 | 已修 | 两份过程计划文档引用已不存在的测试名（11 处）；已按计划在提交前删除，过程产物不入库 |
| DOC-05 | 低 | 已修 | 2 个 ` ```go ` 片段不是可独立解析的 Go 文件（语句片段），不满足技能的 Go 块语法承诺 |

## 逐条详情

### DOC-01：README 术语表“跨族转换默认不安全”与 R2 矛盾

- 级别：低；类型：文档错误
- 文档证据：`zcconfig/docs/README.md:139` 术语表把数值族描述为“跨族转换默认不安全”。
- 实现证据：`zcconfig/cast.go:230` 的 R2 分支允许“无符号 → 有符号”的跨族加宽（目标位宽严格更宽时），例如 `uint8→int16`。
- 测试证据：`zcconfig/cast_test.go:401` 的 `TestCast_NumericSafeSubsetAllowed` 含 `uint8_to_int16` 用例。
- 实测：`Config` 注册 `uint8(255)` 读取 `int16` 得到 `255`（跨族、无损、允许）。
- 影响：读者会把“跨族”理解为一律拒绝，从而误判 `uint8→int16`、`uint32→int64` 等合法转换不可用。
- 推荐方案：改为“跨族仅在静态无损时允许（如无符号→更宽有符号），其余拒绝，详见 `cast.md` 安全子集”；或直接删掉该半句，把细节交给 cast.md。

### DOC-02：“EnvAll 返回拷贝”的否决理由主体错位

- 级别：低；类型：文档错误
- 文档证据：`zcconfig/docs/conventions.md:26` 的行标题是“`EnvAll` 返回拷贝”，理由却是“任何浅/深拷贝都无法覆盖 `map[string]any` 中的 slice/指针/chan/func”。
- 实现证据：`zcconfig/env.go:138` 的 `EnvAll` 返回 `map[string]string`，值是不可变 string，不存在 slice/chan/func；`map[string]any` 是 `ConfigAll` 的对象。
- 测试证据：`zcconfig/env_test.go:220` 的 `TestEnvAll` 断言返回 `map[string]string`。
- 实测：无需运行时探针；`EnvAll` 的签名与存储类型可直接证明（`go doc -all ./zcconfig` 与源码一致）。
- 影响：读者会把 ConfigAll 的“any 值无法部分拷贝”理由套到 EnvAll 上，得出“EnvAll 也必须零拷贝”的错误推论依据。
- 推荐方案：行标题改为“`EnvAll` / `ConfigAll` 返回拷贝”，理由拆两句：ConfigAll 的 any 值无法部分隔离；EnvAll 是 string map，拷贝虽可行但会破坏统一零拷贝契约。

### DOC-03：“Register 不会 panic”绝对化

- 级别：低；类型：文档错误
- 文档证据：`zcconfig/docs/errors.md:16` 写“`Register` 不返回 error，也不会 panic（`fn` 返回 nil 时静默忽略）”。
- 实现证据：`zcconfig/config.go:29` 直接调用 `fn()` 且没有 recover，用户 fn 内的 panic 会原样向上传播。
- 测试证据：修复前无（仅覆盖 `fn` 返回 nil 的 `TestRegister_NilDataNoop`）；修复后新增 `zcconfig/config_extra_test.go:26` 的 `TestRegister_FnPanicPropagates` 锁死 panic 传播。
- 实测：临时探针调用 `Register("boom", func() map[string]any { panic("fn-boom") })`，观察到 panic 原样传播（`PANIC fn-boom`）。
- 影响：调用方可能误以为 `Register` 是 panic 边界、不需要在外层保护自己的 `fn`；启动期 panic 的定位会被误导。
- 推荐方案：改为“`Register` 自身不返回 error、不主动 panic；`fn` 的 panic 会原样向上传播（zcconfig 不 recover）”；可补一条锁死 fn panic 传播的测试（`proposed:zcconfig/config_extra_test.go:1`）。

### DOC-04：过程计划文档引用已不存在的测试名

- 级别：低；类型：文档错误（过期引用）
- 文档证据：`zcconfig/docs/consistency-review.md:21` 记录两份过程计划是“提交前会清理”的非契约文档（修复阶段已按此删除）。
- 实现证据：不适用（纯文档问题：这些名字是计划期目标，实际实现改用了其他测试名或删除）。
- 测试证据：无（引用目标不存在，按名搜索 `_test.go` 无结果）。
- 实测：脚本提取文档中的全部 `Test*` 名并与 `zcconfig/*_test.go` 比对，发现 11 处不存在：`env-raw-value-plan.md` 4 处（`TestParseValue_LargeInteger`、`TestParseValue_IntBoundary`、`TestEnv_BoolFromIntString`、`TestCast_Float64ToIntTruncation`），`numeric-conversion-safety-plan.md` 7 处（`TestCast_IntToFloat64`、`TestCast_Float64ToFloat32`、`TestCast_FloatToIntRejected`、`TestCast_NumericAllowMatrix`、`TestConfig_TypedValueLossyConversionUnaffected` 等）。
- 影响：过程文档若随文档集交付，读者按名找不到测试，会误以为测试缺失；也违反“文档引用可追溯”的约定。
- 推荐方案：提交前删除两份过程计划（已实施；删除前用 `grep` 确认仅审查报告引用，无其他文档依赖）。

### DOC-05：2 个 Go 代码块不是可独立解析的 Go 文件

- 级别：低；类型：文档错误
- 文档证据：`zcconfig/docs/db-config.md:80` 的示例片段与 `zcconfig/docs/errors.md:51` 的 recover 片段均以 ` ```go ` 标注，但内容只是语句列表（原第 3 个同类片段随过程文档删除）。
- 实现证据：不适用（纯文档问题：内容本身是正确的 Go 语句，只是缺少包裹结构）。
- 测试证据：`zcconfig/docs_examples_compile_test.go:13` 只提取并编译完整 `package main` 块，不校验片段语法。
- 实测：`doc_go_examples.py --strict` 对这 2 块报 `expected declaration`（`db-config.md#从库#1: found cfg`、`errors.md#捕获与诊断#1: found 'defer'`）；同一次运行中 2 个完整 main 也报 `found 'package'`，属脚本“前置 package p”对完整程序的限制，不计入本条。
- 影响：技能的 Go 块语法承诺（块内是可解析的 Go）不成立；外部工具/读者无法直接把它们当文件或函数体校验。
- 推荐方案：把 2 个片段包进 `func example() { ... }`（保留 Go 高亮且可通过语法检查），或改为 ` ```text `；完整 main 保持 ` ```go ` 以继续被 `TestDocExamples_Compile` 编译。

## 已核对且未发现偏差的关键行为

- Env 存储值恒为 string、空值视为存在且不回退 OS：`zcconfig/docs/env.md:50`、`zcconfig/env.go:79`、`zcconfig/env_test.go:56`。
- 1MB token 边界（最大单行 1MB-1，与换行符无关）：`zcconfig/docs/env.md:51`、`zcconfig/env.go:48`、`zcconfig/env_test.go:312`。
- Config 的 `.` 分隔不可转义、空路径段按空字符串键：`zcconfig/docs/config.md:109`、`zcconfig/config.go:94`、`zcconfig/config_test.go:140`。
- Config 的 nil 三态与中间层非 map panic：`zcconfig/docs/errors.md:10`、`zcconfig/config.go:110`、`zcconfig/config_test.go:125`。
- 数值安全子集（同族加宽、无符号→更宽有符号、整型→浮点按尾数位）：`zcconfig/docs/cast.md:87`、`zcconfig/cast.go:221`、`zcconfig/cast_test.go:428`。
- 数值→Duration 拒绝与数值→bool C 风格：`zcconfig/docs/cast.md:85`、`zcconfig/cast.go:129`、`zcconfig/cast_test.go:481`。
- 类型不符消息三要素与 `map[string]interface {}` 显示：`zcconfig/docs/errors.md:38`、`zcconfig/cast.go:26`、`zcconfig/cast_test.go:138`。
- DBConfig 驱动名大小写敏感、别名与空 DSN：`zcconfig/docs/db-config.md:34`、`zcconfig/dbConfig.go:107`、`zcconfig/dbConfig_test.go:548`。
- 导出 API 全覆盖（`go doc -all ./zcconfig` 对照）：`zcconfig/docs/README.md:117`。
- 文档内部链接与术语表：11 份文档相对链接全部可达；`env`/`config`/`cast`（数值子集）/`db-config` 的重复描述一致（单一事实来源章节未被复述规则）。

## Go 示例覆盖

| 文档#章节#序号 | 语法/结构 | 证据/原因 |
|---|---|---|
| README.md#5-分钟上手#1 | 完整 `package main`，编译通过 | `zcconfig/docs_examples_compile_test.go:13` 扫描并编译；脚本因前置 `package p` 报错属工具限制 |
| config.md#5-分钟上手-完整示例-编译级校验#1 | 完整 `package main`，编译通过 | 同上 |
| errors.md#TypeMismatchError#1 | 通过（类型声明可作为局部声明解析） | `doc_go_examples.py` parse=ok |
| db-config.md#从库#1 | 修复前失败；修复后通过（片段已包进 `func example`） | DOC-05；修复后 `doc_go_examples.py` parse=ok |
| errors.md#捕获与诊断#1 | 修复前失败；修复后通过（片段已包进 `func example`） | DOC-05；修复后 parse=ok |

## 修复执行记录

| 编号 | 修改内容 | 修复后锚点 |
|---|---|---|
| DOC-05 | `db-config.md`、`errors.md` 的语句片段包进 `func example() { ... }` | `zcconfig/docs/db-config.md:80`、`zcconfig/docs/errors.md:51` |
| DOC-04 | 按计划在提交前删除两份过程计划（过程产物不入库）；删除前确认仅审查报告引用 | `zcconfig/docs/consistency-review.md:21` |
| DOC-03 | 改述为“`Register` 自身不返回 error、不主动 panic；`fn` 的 panic 原样向上传播”；新增回归测试锁死传播行为 | `zcconfig/docs/errors.md:16`、`zcconfig/config_extra_test.go:26` |
| DOC-02 | 否决方案行改为 `EnvAll` / `ConfigAll` 并分别给出理由（any 值无法隔离 vs string map 统一契约） | `zcconfig/docs/conventions.md:26` |
| DOC-01 | 术语表改为“跨族仅在静态无损时允许（如无符号→更宽有符号），其余拒绝” | `zcconfig/docs/README.md:139` |

说明：

- DOC-04 选择删除而非标注：过程产物不入库；删除前已确认仅两份审查报告引用它们，代码与主题文档无依赖。计划中的设计推导仍可从提交前的本地副本或对话记录追溯。
- DOC-03 的新测试做了变异自检：临时在 `Register` 里加 `defer func(){ _ = recover() }()` 吞掉 panic 后，用例立即失败（`期望 panic 原样传播 fn-boom，实际 <nil>`），已还原。
- DOC-05 修复后 `doc_go_examples.py --strict` 的片段错误归零，仅剩 2 个“完整 `package main` 因脚本前置 `package p` 报错”的已知工具限制（非文档缺陷，完整示例由 `TestDocExamples_Compile` 编译）。

## 实际验收结果

| 验收项 | 结果 |
|---|---|
| `git diff --check` | 通过（退出码 0；仅有 Git autocrlf 的 LF→CRLF 提示） |
| `gofmt -l zcconfig` | 空 |
| `go vet ./...` | 通过（退出码 0） |
| `go test ./zcconfig -count=1` | ok（6.6s） |
| `go test ./zcconfig -race -count=1` | ok（6.4s） |
| `go test ./zcconfig -shuffle=on -count=5` | ok（20.0s） |
| `go test ./zcconfig -cover -count=1` | ok，coverage 99.5% |
| `go test ./zcconfig -run 'TestDocExamples_Compile'` | 通过（2 个完整示例编译成功） |
| `doc_go_examples.py --docs-dir zcconfig/docs --strict` | 片段 0 错误；仅 2 个完整 main 报 `expected declaration, found 'package'`（脚本限制） |
| DOC-03 变异自检 | 通过（吞掉 panic 后用例变红，已还原） |
| 过程文档清理 | 两份计划已删除，工作区与文档目录不再包含它们 |
| 报告机械校验 | 结构 + 引用校验通过；锚点逐条核对与所述语句一致 |

结论：5 条低优先级问题全部修复；文档集与实现一致，未改动任何运行时行为（仅新增一条测试锁死 `fn` panic 传播）。
