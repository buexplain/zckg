# zcconfig 文档与实现一致性审查报告

- 审查日期：2026-11-16
- 模块：`zcconfig`
- 基线：工作区（`HEAD 26a978c` + 未提交改动：`.env` 原文存储、数值转换安全子集、文档同步等改动均未提交）
- 行号来源：工作区文件（本报告全部引用按工作区校验，校验命令使用 `--worktree`）
- 修复状态：**已修**（9 条全部修复并验收）

## 审查范围与方法

**被审文档**

| 文档 | 定位 |
|---|---|
| `zcconfig/docs/README.md` + `env.md` / `config.md` / `cast.md` / `db-config.md` / `errors.md` / `internals.md` / `conventions.md` | zcconfig 对外契约（重构后的主题文档集） |
| `zcconfig/docs/zcconfig.md` | 保留的兼容索引入口（指向 README 与主题文档） |
| `README.md` | 仅 `zcconfig` 相关摘要行（模块总览与示例入口），核对措辞与主文档一致 |

> 2026-11-16 文档重构：`zcconfig.md` 已拆分为索引 + 7 个主题文档；本报告的文档引用已同步到新文件的对应位置，代码/测试引用不受影响。

`zcconfig/docs/env-raw-value-plan.md` 与 `zcconfig/docs/numeric-conversion-safety-plan.md` 是本次改动的**过程计划**（非用户契约、提交前会清理），不计入被审文档范围。

**对照实现与测试**

- 实现：`zcconfig/env.go`、`zcconfig/config.go`、`zcconfig/cast.go`、`zcconfig/dbConfig.go`（含导出符号的源码注释）。
- 测试：`zcconfig/env_test.go`、`zcconfig/config_test.go`、`zcconfig/config_extra_test.go`、`zcconfig/cast_test.go`、`zcconfig/dbConfig_test.go`、`zcconfig/docs_examples_compile_test.go`。
- 约束：先读 `AGENTS.md`（测试归属、文档四者一致、不变量）并按其执行。

**方法**

1. 正向逐声明比对：方法表、优先级列表、转换表、`.env` 规则、DBConfig 表、架构约定 → 源码分支与锁死测试。
2. 运行时实测（临时探针，跑完即删）：1MB 行边界（含无换行文件）、`Config` 空键/nil/点分隔、`Env` 空值/接口目标/OS 回退、`DBConfig` 驱动名大小写、类型不符消息。
3. 反向审查：`go doc -all ./zcconfig` 导出 API 逐个核对文档覆盖。
4. Go 示例：完整 `package main` 由 `zcconfig/docs_examples_compile_test.go:13` 编译；片段逐块按函数体 `gofmt -e` 校验。

**问题统计**：9 条（高 0 / 中 1 / 低 8）。全部为文档侧偏差（其中 1 条同时涉及源码注释编号），无实现缺陷。

**判定标准**：只统计会误导调用方契约、或文档内部/文档与注释互相矛盾、或关键行为未写入文档的问题；措辞润色类（不改变语义理解）不计入。

## 问题汇总

| 编号 | 级别 | 状态 | 一句话结论 |
|---|---|---|---|
| DOC-01 | 中 | 已修 | 单行 1MB 上限的边界与成因写错：方法是 token 长度上限（可加载最大 1MB-1），且与换行符无关；方法表还与格式节自相矛盾 |
| DOC-02 | 低 | 已修 | `cast` 优先级编号在源码注释（8 步）与用户文档（10 步）不一致，同一规则差一位 |
| DOC-03 | 低 | 已修 | `.env` 空值（`KEY=`）语义未写入文档：键存在且为空串，读 `string` 得 `""`、不回退默认值/OS |
| DOC-04 | 低 | 已修 | 类型不符表"值为 nil 返回 def"未限定通道，而 Env 存储已是 `map[string]string`，nil 不可达 |
| DOC-05 | 低 | 已修 | "仅 tag 不同的结构体底层类型相同"表述错误：Go 转换是比较 struct 类型时忽略 tag，而非底层类型相同 |
| DOC-06 | 低 | 已修 | 驱动名大小写敏感未写入文档：`Driver: "MySQL"` 返回空 DSN |
| DOC-07 | 低 | 已修 | `Config` 的 `.` 分隔不可转义未写入文档：字面含 `.` 的 key 无法读取；空路径段按空键查找 |
| DOC-08 | 低 | 已修 | `Env` 行"目标为 string 返回原文"对 OS 回退不准确（返回的是环境变量值，不是 `.env` 原文） |
| DOC-09 | 低 | 已修 | 数组长度不足行"panic（不返回原 def）"分层含糊：`cast` 返回未命中，读取层才 panic |

## 逐条详情

### DOC-01：单行 1MB 上限的边界与成因写错

- 级别：中；类型：文档错误
- 文档证据：`zcconfig/docs/env.md:51` 方法表原句"单行上限 1MB（超出返回错误）"；`zcconfig/docs/env.md:51` 原句"扫描缓冲区需容纳行结束符，单行恰好达到 1MB 时也会报错，实际可加载的单行长度略小于 1MB"。两处对"恰好 1MB 是否可加载"的口径不一致，且成因归因于换行符。
- 实现证据：`zcconfig/env.go:48` 用 `scanner.Buffer(64KB, 1MB)`，上限是 `bufio.Scanner` 的 token 长度；`zcconfig/env.go:82` 把 `bufio.ErrTooLong` 包装进"读取 .env 文件失败"错误。
- 测试证据：`zcconfig/env_test.go:312` 新增 `TestLoadEnv_LineLimitBoundary` 锁死边界；`zcconfig/env_test.go:293` 保留超限用例。
- 实测（临时探针，逐字节构造）：行内容 1,048,575 字节（带换行）加载成功；行内容 1,048,576 字节（带换行）返回 `bufio.ErrTooLong`；行内容 1,048,576 字节且**文件末尾无换行**同样返回 `bufio.ErrTooLong`。结论：可加载上限为 1MB-1 字节，与文件是否以换行符结尾无关。
- 影响：把内联证书等值按"1MB 恰好可加载"生成的调用方会在启动期失败；错误原因的描述会误导排查（误以为是行结束符占位）。
- 推荐方案：把两处改成"token 上限 1MB，实际最大单行 1MB-1 字节，恰好 1MB 也报错，可用 `errors.Is` 判定"；新增边界测试（已实施）。

### DOC-02：`cast` 优先级编号两套（源码注释 vs 用户文档）

- 级别：低；类型：文档内部矛盾
- 文档证据：`zcconfig/docs/cast.md:52` 文档把 reflect 列为第 9 步、`zcconfig/docs/cast.md:87` 把数值族子集称为"第 8 步"（文档编号从 `v == nil` 起算）。
- 实现证据：`zcconfig/cast.go:33` 修复前源码注释的编号从"直接类型断言"起算（8 步），同一"数值族"在注释中是第 7 步；交叉引用"见第 7 步"在文档指数值→Duration、在注释指数值族。
- 测试证据：无（纯注释/文档编号问题，不涉及运行时行为）。
- 实测：核对两处列表逐项对应关系，规则集合一致、仅编号偏移 1；文档内部与注释内部各自自洽，跨载体对照时错位。
- 影响：维护者按编号跨文档/源码核对时指向错误规则，评审与后续改动的沟通成本增加。
- 推荐方案：源码注释改为与文档一致的 10 步并注明"编号与 docs/zcconfig.md 一致"，同时把"complex 仅参与第 8 步"改为新编号（已实施）。

### DOC-03：`.env` 空值语义未文档化

- 级别：低；类型：关键行为遗漏
- 文档证据：`zcconfig/docs/env.md:50` 的 `.env` 示例包含 `EMPTY_KEY=`，但修复前"解析规则"未说明空值语义；`zcconfig/docs/env.md:86` 的 `Env` 行只写"key 不存在返回 def"。
- 实现证据：`zcconfig/env.go:79` 空值照常存入（`unquote("")`），`zcconfig/env.go:124` 仅当 key 不在存储中才回退 OS。
- 测试证据：`zcconfig/env_test.go:56` 断言 `EMPTY_KEY` 读取为 `""`。
- 实测：`.env` 写 `EMPTY=` 后，`Env[string]("EMPTY", "def")` 返回 `""`（不是 `def`）；`EnvAll` 中可见该键；`Env[int]("EMPTY", 1)` panic（key 存在、解析失败）；同 key 的 OS 环境变量不参与回退。
- 影响：调用方按"空值=未配置"设计时，默认值会被空串覆盖；多环境覆盖场景行为与预期相反且难以察觉。
- 推荐方案：`.env` 解析规则增加"空值视为存在"条目，`Env` 行补空值返回 `""` 的说明（已实施）。

### DOC-04：nil 行未区分 Env/Config 通道

- 级别：低；类型：关键行为遗漏
- 文档证据：`zcconfig/docs/errors.md:10` 修复前为"key 存在且值为 nil → 返回 def（视为未配置）"，未说明仅 Config 可能存 nil。
- 实现证据：`zcconfig/env.go:13` 的 `envData` 为 `map[string]string`，Env 存储值不可能是 nil；nil 分支由 `cast.go` 的 `castOrPanic` 服务 Config。
- 测试证据：`zcconfig/config_test.go:125` 验证 Config 的 nil 叶子/中间节点返回 def。
- 实测：`Config` 注册 `{"leaf": nil}` 读取得 `def`；Env 侧无法构造 nil 值（`EnvAll` 类型为 `map[string]string`）。
- 影响：读者可能以为 Env 也存在 nil 状态并依赖其回退 def；实际 Env 只有"不存在 / 空串 / 文本"三态。
- 推荐方案：表中注明"仅 Config 通道；Env 存储值恒为 string"（已实施）。

### DOC-05：结构体 tag 与"底层类型相同"的表述错误

- 级别：低；类型：文档错误
- 文档证据：`zcconfig/docs/cast.md:126` 修复前写"结构体 B（含 tag 不同；底层类型相同）"。
- 实现证据：`zcconfig/cast.go:146` 走 `reflect.Value.CanConvert`；Go 规则是"比较 struct 类型是否可转换时忽略 tag"，tag 不同的两个具名结构体底层类型并不相同。
- 测试证据：`zcconfig/cast_test.go:734` 断言 A→Tagged（仅 tag 不同）转换成功。
- 实测：`castProbeStructA{X:1,Y:"a"}` 转 `castProbeStructTagged` 成功；字段不同（A→Diff）失败。
- 影响：读者会误以为 tag 参与底层类型判定，进而错误推断"tag 不同是否可转换"的边界。
- 推荐方案：改述为"底层类型相同，或仅 struct tag 不同——Go 转换规则比较 struct 类型时忽略 tag"；测试注释同步（已实施）。

### DOC-06：驱动名大小写敏感未文档化

- 级别：低；类型：关键行为遗漏
- 文档证据：`zcconfig/docs/db-config.md:34` 修复前只列 `sqlite3`/`postgresql`/`pgsql` 别名与"未知驱动返回空字符串"，未说明大小写。
- 实现证据：`zcconfig/dbConfig.go:107` 的 `switch driver` 为精确小写匹配。
- 测试证据：`zcconfig/dbConfig_test.go:548` 新增 `{"MySQL", "mysql", ""}` 用例（`TestDBConfig_DriverFormatOnly` 位于 `zcconfig/dbConfig_test.go:538`）。
- 实测：`Driver: "mysql"` 生成正常 DSN；`Driver: "MySQL"`、`"SQLITE"` 均返回空字符串。
- 影响：调用方传 `"MySQL"` 时得到空 DSN，错误要到建池阶段才暴露，且文档没有提示排查方向。
- 推荐方案：文档补"驱动名大小写敏感"；`GetMasterDSN` 注释同步；测试表补一行（已实施）。

### DOC-07：`Config` 的点分隔限制未文档化

- 级别：低；类型：关键行为遗漏
- 文档证据：`zcconfig/docs/config.md:109` 修复前只说明"按 `.` 分隔的 key 递归查找"，未说明不可转义与空路径段。
- 实现证据：`zcconfig/config.go:94` 用 `strings.Split(key, ".")` 后逐层查找，字面点无转义通道。
- 测试证据：`zcconfig/config_test.go:140` 新增 `TestConfig_DotSeparatorNotEscapable`。
- 实测：注册 `{"a.b": 42, "": "empty-segment"}` 后，`Config("app.a.b", "def")` 返回 `def`（无法读到字面 `a.b`）；`Config("app.", "def")` 返回 `empty-segment`（空路径段按空键查找）。
- 影响：注册在 map 中、字面含 `.` 的 key 永远读不到且静默回退 `def`；末尾点号会被当作空键。
- 推荐方案：`Config` 方法行与源码注释补该限制；新增测试锁死（已实施）。

### DOC-08：`Env` 行"返回原文"对 OS 回退不准确

- 级别：低；类型：文档错误
- 文档证据：`zcconfig/docs/env.md:86` 修复前写"目标为 `string` / 具名 `string` 时恒成功并返回原文"。
- 实现证据：`zcconfig/env.go:124` 的 OS 回退返回 `os.LookupEnv` 的值，此时不存在 `.env` 原文。
- 测试证据：`zcconfig/env_test.go:180` 断言 OS 回退返回 `"os_value"`。
- 实测：`.env` 无该 key、OS 设置 `MY_TEST_OS_ENV=os_value` 时，`Env(key, "default")` 返回 `"os_value"`。
- 影响：读者把"原文"理解为 `.env` 内容，跨环境排查时混淆值来源。
- 推荐方案：改为"返回文本原值（`.env` 为文件原文，OS 回退为环境变量值）"（已实施）。

### DOC-09：数组长度不足行的分层表述含糊

- 级别：低；类型：文档错误
- 文档证据：`zcconfig/docs/cast.md:127` 修复前写"值长度不足，不可转换 | panic（不返回原 `def`）"。
- 实现证据：`zcconfig/cast.go:146` 的 `CanConvert` 对短切片返回 false → `zcconfig/cast.go:156` 的 `castOrPanic` panic。
- 测试证据：`zcconfig/cast_test.go:680` 断言短切片返回 `(def,false)` 而非 panic（panic 发生在读取层）。
- 实测：`cast([]int{1}, [2]int{})` 返回未命中；`Config`/`Env` 读同一值抛 `*TypeMismatchError`，`def` 不会返回给调用方。
- 影响：读者无法判断"不返回 def"是 `cast` 层行为还是读取层行为，定位断言位置时误解。
- 推荐方案：明确"`cast` 返回未命中，Env/Config 抛 `*TypeMismatchError`，不会把 def 返回给调用方"（已实施）。

## 已核对且未发现偏差的关键行为

- `.env` 值按原文存储、`EnvAll` 为 `map[string]string`：`zcconfig/env.go:13`、`zcconfig/env_test.go:220`。
- 原始文本不归一化（`007`/`1e3`/`1.50`/`TRUE`/`+5`）：`zcconfig/env_test.go:615`。
- 数值转换安全子集（同族加宽、无符号→更宽有符号、整型→浮点按尾数位）与拒绝矩阵：`zcconfig/cast_test.go:401`、`zcconfig/cast_test.go:428`。
- 数值→`time.Duration` 拒绝、`Duration` 自读/转 `int64` 正常：`zcconfig/cast_test.go:481`。
- Config 通道数值拒绝与允许（含 `numeric→bool` C 风格保留）：`zcconfig/config_test.go:590`。
- `.env` 加载全有或全无、失败不污染已有数据：`zcconfig/env_test.go:348`。
- 无 `=` 行/空 key 行跳过、首行 BOM 剥离：`zcconfig/env_test.go:372`、`zcconfig/env_test.go:392`。
- Duration 文本解析（`10s`/`1h30m`/`500ms`/零值例外）：`zcconfig/env_test.go:509`。
- OS 环境变量回退与宽松转换：`zcconfig/env_test.go:165`。
- DBConfig 各驱动 DSN、PostgreSQL 转义、MySQL 库名/时区回读、别名与大小写行为：`zcconfig/dbConfig_test.go:19`、`zcconfig/dbConfig_test.go:173`、`zcconfig/dbConfig_test.go:432`、`zcconfig/dbConfig_test.go:538`。
- `Register` 深度合并、nil 源/目标、标量路径覆盖：`zcconfig/config_extra_test.go:82`、`zcconfig/config_extra_test.go:204`。
- 导出 API 在文档集中逐一覆盖（`go doc -all ./zcconfig` 对照，入口见 `zcconfig/docs/README.md:117` 文档地图）。

## Go 示例覆盖

| 文档#章节#序号 | 语法/结构 | 证据/原因 |
|---|---|---|
| zcconfig.md#数据库连接配置-DBConfig#1 | 通过（片段可作为函数体） | 片段按函数体包裹后 `gofmt -e` 解析通过；该示例依赖外部驱动与 `zcdb`，编译级校验不在本文档范围 |
| zcconfig.md#类型不符-TypeMismatchError#1 | 通过（片段可作为函数体） | `defer`/`recover` 片段包裹后解析通过 |
| zcconfig.md#架构约定#1 | 通过（片段可作为函数体） | 缩进代码块，去缩进后语句合法（含 `❌` 注释，不影响解析） |
| zcconfig.md#读取-.env-文件#1 | 通过（完整 `package main`） | 由 `zcconfig/docs_examples_compile_test.go:13` 原样提取并 `go build` 验证 |
| zcconfig.md#注册与读取业务配置#1 | 通过（完整 `package main`） | 同上，`go build` 通过 |
| zcconfig.md#time.Duration-支持#1 | 通过（片段可作为函数体） | 语句片段包裹后解析通过 |
| zcconfig.md#多次注册深度合并#1 | 通过（片段可作为函数体） | 语句片段包裹后解析通过 |

说明：技能自带的 `doc_go_examples.py --strict` 会无条件前置 `package p`，因此对包含完整 `package main` 的块一律报 `expected declaration, found 'package'`（7 块全报）；这是脚本对完整程序的包装限制，不是文档语法缺陷。完整示例的权威校验以仓库的编译级测试为准（当前通过），片段则按"包裹为函数体后可解析"逐块实测通过。

## 修复执行记录

| 编号 | 修改载体与要点 | 修复后锚点 |
|---|---|---|
| DOC-01 | `zcconfig/docs/zcconfig.md` 方法表与 `.env` 规则改为 token 上限口径、注明 1MB-1 与 `errors.Is`；`env.go` 注释同步；新增边界测试（含无换行文件） | `zcconfig/docs/env.md:51`、`zcconfig/docs/env.md:51`、`zcconfig/env_test.go:312` |
| DOC-02 | `cast.go` 优先级注释改为与文档一致的 10 步并注明编号对齐 | `zcconfig/cast.go:33` |
| DOC-03 | `.env` 规则新增"空值视为存在"；`Env` 行补空值返回 `""` | `zcconfig/docs/env.md:50`、`zcconfig/docs/env.md:86` |
| DOC-04 | 类型不符表 nil 行限定"仅 Config 通道" | `zcconfig/docs/errors.md:10` |
| DOC-05 | 结构体转换行改为"仅 tag 不同时转换忽略 tag"；`cast_test.go` 探针注释同步 | `zcconfig/docs/cast.md:126`、`zcconfig/cast_test.go:19` |
| DOC-06 | DBConfig 别名段补"大小写敏感"；`dbConfig.go` 注释同步；测试表补 `MySQL` 行 | `zcconfig/docs/db-config.md:34`、`zcconfig/dbConfig.go:67`、`zcconfig/dbConfig_test.go:548` |
| DOC-07 | `Config` 方法行与 `config.go` 注释补点分隔限制；新增测试 | `zcconfig/docs/config.md:109`、`zcconfig/config.go:80`、`zcconfig/config_test.go:140` |
| DOC-08 | `Env` 行改为"文本原值（`.env` 原文 / OS 环境变量值）" | `zcconfig/docs/env.md:86` |
| DOC-09 | 数组行改为"`cast` 返回未命中，Env/Config 抛 `*TypeMismatchError`，不返回 def" | `zcconfig/docs/cast.md:127` |

新增/变更测试均为"文档声明 → 源码分支 → 可失败测试"闭环：

- `TestLoadEnv_LineLimitBoundary`（`zcconfig/env_test.go:312`）做过变异自检：临时把 scanner 上限改成 2MB，两条"恰好 1MB"子用例立即失败（`单行达到 1MB 期望返回错误`），已还原。
- `TestConfig_DotSeparatorNotEscapable`（`zcconfig/config_test.go:140`）锁死字面点不可读、空路径段按空键查找。
- `TestDBConfig_DriverFormatOnly` 新增 `MySQL` 行（`zcconfig/dbConfig_test.go:548`）锁死大小写敏感。

## 实际验收结果

| 验收项 | 结果 |
|---|---|
| `git diff --check` | 通过（退出码 0；仅有 Git autocrlf 的 LF→CRLF 提示，不是失败） |
| `gofmt -l zcconfig` | 空（仓库其余既有 CRLF 文件与本次无关，未计入） |
| `go vet ./...` | 通过（退出码 0） |
| `go test ./zcconfig -race -count=1` | ok（6.4s） |
| `go test ./zcconfig -cover -count=1` | ok，coverage 99.5% |
| `go test ./... -count=1` | zcconfig / zchttp / zcmodel / zcquit 全绿；zcdb 的 `TestDocsGoBlockSyntax`、`TestDocReview_ErrorTableMatchesExportedSentinels` 失败，原因是工作区未提交的 **zcdb 文档改动**（`zcdb/docs/getting-started.md` 代码块语法、`zcdb/docs/README.md` 错误表），与 zcconfig 无关，按约定不顺手修复 |
| 文档示例编译（`TestDocExamples_Compile`） | 通过（重构后该测试扫描 `docs/*.md` 的全部完整示例） |
| 逐块 Go 示例语法检查 | 2 个完整 main 编译通过、5 个片段 + 1 个缩进片段包裹后 `gofmt -e` 解析通过 |
| 新增边界用例变异自检 | 通过（放宽 buffer 上限后用例变红，已还原源码） |

结论：文档与实现一致；9 条偏差全部按"以代码/实测为准"修正到文档与注释，未改任何运行时行为。
