# zcmodel 代码审查计划

## 1. 审查目标

- **生成代码的语法正确性**：任何输入（含恶意/畸形注释、特殊列名）下生成的 `.go` 文件必须可通过 `go/parser` 解析并可编译。
- **用户代码零丢失**：增量再生成时用户自定义方法、import、其他声明必须完整保留——这是本模块最不可妥协的属性。
- **列类型映射准确性**：三方言归一化与映射表的正确性、未命中兜底。
- **命名转换确定性**：任意风格输入产出合法且稳定的 Go 标识符。
- **文件写入安全**：解析失败绝不覆盖原文件；写入过程的原子性评估。

## 2. 审查范围与批次

模块共 5 个源文件（约 743 行）+ 单元测试 + 集成测试，按数据流顺序分 4 批：

| 批次 | 主题 | 文件 | 行数 |
|---|---|---|---|
| ① 输入模型 | 数据结构与枚举 | `input.go` | 50 |
| ② 转换基础 | 命名转换、类型映射 | `toCase.go`、`columnTypeToGoType.go` | 318 |
| ③ 生成入口 | 校验、补全、组装 | `generate.go` | 87 |
| ④ 代码拼装与写入 | 字符串生成、AST 增量写入 | `build.go` | 288 |

## 3. 文件级审查清单

### 批次①：input.go

- [ ] `NameCase.IsValid()` 覆盖全部六个枚举值；`Dialect` 三个枚举值。
- [ ] `Column`/`StructFieldInfo` 字段注释与文档表格一致；"显式优先"约定（留空才自动推导）在结构体注释中写明。
- [ ] Input 各字段的零值语义：`JsonTagValueCase` 空 = 不生成 json tag；`TableComment` 空 = 用"表"。

### 批次②：转换基础

- [ ] `toCase.go`：
    - `splitWords` 三条拆分规则：分隔符（`_`/`-`/空格）、小写→大写边界、连续大写后接小写在最后一个大写前拆分（`HTTPServer` → `HTTP|Server`）。
    - 边界输入：空串、纯分隔符（`"__"`）、数字开头（`"1st_place"` → 生成的字段名 `1stPlace` 非法标识符？）、纯数字列名、Unicode/中文列名——每种输入产出是否为**合法 Go 标识符**，非法时的处理策略（报错 or 兜底）。
    - `toPascalCase`：`id` → `ID` 特例仅对完整单词生效（`identity` 不得变 `IDentity`）；连续大写词（`HTTPServer` → `HttpServer`）与文档示例一致。
    - `formatJSONTag` 六种风格输出对照文档表格（`user_id` 列逐一验证）；未识别风格返回空串。
    - 同名冲突：两个不同列（如 `user_id` 与 `user-id`、`UserId`）转换后字段名相同——是否检测重复字段（生成代码编译报错），评估是否需要注册期冲突检测。
- [ ] `columnTypeToGoType.go`：
    - `normalizeColumnType` 归一化顺序：小写 → 去括号及内容 → 压缩空格 → 方言特有处理；`timestamp(6) with time zone` → `timestamptz` 的"括号后内容保留"逻辑。
    - MySQL：`unsigned`/`zerofill` 去除；**`bigint unsigned` 映射为 `int64` 是否溢出**（最大值超 int64 范围，记录为已知取舍或建议映射 `uint64`）。
    - PostgreSQL：时区后缀规范化两个方向（with → timestamptz、without → timestamp）；`character varying` 含空格类型的匹配。
    - SQLite：类型名引号去除（`"VARCHAR"(255)`）。
    - 三张映射表逐行与文档核对；未命中兜底 `string` 不 panic。
    - 映射为 `time.Time` 时 Import 自动补 `"time"` 的触发条件（调用方未显式指定时）。

### 批次③：generate.go

- [ ] 校验顺序与错误信息：Dialect 非法 → "不支持的数据库方言"；JsonTagValueCase 非空且非法 → "invalid json tag value case"。
- [ ] 字段补全四步（JsonTagValue → Name → Type → Import）仅对**留空字段**执行，显式值不被覆盖。
- [ ] `OutputDir` 创建（`MkdirAll`）失败的错误包装；目录名 → 包名推导，无法推导回退 `main` 的条件。
- [ ] `TableName` 直接用作文件名：Windows 非法字符（`<>:"|?*`）由 OS 报错——错误信息是否可理解；路径穿越风险（TableName 含 `../`）评估——生成文件逃逸 OutputDir 的可能性（重点安全项）。
- [ ] Columns 为空切片/nil 时生成空结构体的行为是否合理。

### 批次④：build.go（核心）

- [ ] **buildStruct 拼装**：
    - tag 顺序固定 json → ColumnTagName → description；json tag 仅 JsonTagValue 非空时生成；description 仅注释非空时生成。
    - gofmt 对齐（字段名与类型按最长宽度对齐）的计算是否含 DO 的 `any` 类型场景。
    - `ColumnTagName` 为空串时的 tag 生成行为（跳过还是生成空名 tag）。
- [ ] **sanitizeTagValue 注释净化**（安全关键）：
    - `strconv.Quote` 转义 + 反引号替换为单引号的顺序；构造对抗输入验证：注释含 `` ` ``、`"`、换行、`\r`、tag 结构字符（`json:"x"`）——生成文件必须仍可解析且 `reflect.StructTag.Lookup` 能还原。
- [ ] **ToDO/ToEntity 方法生成**：
    - 可变参数复用实例的分支（传 nil、不传、传实例）；receiver 命名。
    - ToEntity 类型断言失败跳过语义的生成代码正确性（断言目标类型与 Entity 字段类型一致，含 `time.Time`、`[]byte`）。
- [ ] **writeOrReplaceStruct AST 增量写入**（最高优先级）：
    - 文件不存在/空白 → 新建路径：package 行、import 块、生成代码的组装。
    - `parser.ParseComments` 解析失败 → **报错返回不覆盖**——确认无任何先清空后写入的中间态。
    - `isGenerated` 识别精度：仅移除 `{EntityName}`/`{DOName}` type 声明、Entity 上的 `ToDO`、DO 上的 `ToEntity`——同名但 receiver 不同的方法、用户写的 `ToDO2` 不得误删。
    - 用户代码分类归位：Entity 方法紧随 Entity、DO 方法紧随 DO、其他声明放末尾——**注释/文档注释是否随声明一起保留**（AST 重组时 comment 关联极易丢，重点验证）。
    - import 补齐：已有 import 去重、分组格式；生成代码新增依赖（`time`）缺失时补充。
    - 写文件：是否先写临时文件再 rename（原子性）；直接 truncate 写入在进程中断时会损坏用户文件——若无原子写，记录为 Major 改进项。
    - 最终输出是否经 `go/format`（gofmt 等价）格式化。

## 4. 专项审查

### 4.1 幂等性与鲁棒性

- [ ] **幂等性**：同一 Input 连续 Generate 两次，第二次输出与第一次逐字节一致（无重复代码、无 import 重排抖动）。
- [ ] **再生成循环**：生成 → 手工添加自定义方法/import/常量 → 修改列再生成 → 校验用户内容完整保留且新列生效——设计为标准回归场景。
- [ ] 对抗输入集：含反引号/换行/双引号的表注释与列注释、保留字列名（`type`、`func`）、`id` 各种大小写、超长列名。

### 4.2 生成产物编译验证

- [ ] 审查期间对每类场景的生成文件执行 `go build`（或 `go vet`）验证可编译；确认集成测试（`integration_test.go`）已包含该验证，若无则补。

## 5. 测试审查

- [ ] 单元测试逐文件核对：`toCase_test.go`（拆词/PascalCase/六风格 json tag）、`columnTypeToGoType_test.go`（三方言归一化+映射）、`build_test.go`（tag 净化、AST 保留）、`generate_test.go`（校验/补全）、`input_test.go`。
- [ ] `integration_test.go` 对接真实三库：确认环境门控（无库跳过不失败）；从 zcdb Schema 读结构 → 生成 → 编译的端到端链路。
- [ ] 补测建议（若缺失）：幂等性断言（两次生成 diff 为空）、用户代码带注释的保留、解析失败不覆盖（写入语法错误文件后 Generate 报错且文件未变）、路径穿越 TableName。
- [ ] 执行：`go test -race -count=1 ./zcmodel/...`，覆盖率目标 ≥ 85%（`build.go` ≥ 90%）。

## 6. 文档一致性核对

- [ ] `docs/zcmodel.md` 的生成流程四步、三张映射表、命名转换示例表、增量再生成五步、八条注意事项逐条与实现比对。
- [ ] 示例代码（手工构造 / 配合 zcdb / 显式指定）可直接运行。

## 7. 产出物与完成标准

- **问题清单**：`文件:行号` + 复现 Input（最小化）+ 生成产物片段 + 严重级别。
- **严重级别**：
    - Blocker：用户代码丢失、生成非法语法文件、解析失败仍覆盖、路径穿越。
    - Major：类型映射错误、命名转换产出非法标识符、非原子写入、注释丢失。
    - Minor：错误信息、幂等性抖动（import 顺序）、格式不一致。
    - Nit：命名、注释、文档修订。
- **完成标准**：四批清单全部勾选；幂等性与再生成循环回归通过；生成产物编译验证通过；Blocker/Major 建立修复项。
