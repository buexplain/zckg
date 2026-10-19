# zcmodel 文档-实现一致性审查报告

> 审查基线：HEAD `26744e528d4a9e9cd891018da29aede36d4924ce`。修复前 8 条问题状态为“待修”，本轮已全部修复。审查期章节（审查范围、问题汇总、逐条详情、已核对、Go 示例覆盖）中的 `文件:行` 仍指向修复前基线，仅用于追溯；“修复执行记录”与“实际验收结果”中的行号指向当前工作区。

## 审查范围与方法

### 文档全集

- `zcmodel/docs/zcmodel.md`：模块用户文档全文（审查时 600 行），覆盖 API、生成流程、类型映射、增量再生成、注释净化与注意事项。
- `README.md`：仅审查与 zcmodel 相关的契约段落——第 17 行模块表、第 162 行示例入口、第 183 行测试覆盖率、第 202 行目录说明。

### 对照源码与测试

- 源码：`zcmodel/input.go`、`zcmodel/generate.go`、`zcmodel/build.go`、`zcmodel/toCase.go`、`zcmodel/columnTypeToGoType.go`。
- 测试：`zcmodel/generate_test.go`、`zcmodel/build_test.go`、`zcmodel/toCase_test.go`、`zcmodel/columnTypeToGoType_test.go`、`zcmodel/input_test.go`、`zcmodel/integration_test.go`、`zcmodel/docs_examples_compile_test.go`、`zcmodel/testhelpers_test.go`。

### 基线与行号

- 基线提交：`26744e528d4a9e9cd891018da29aede36d4924ce`。
- 审查时 `zcmodel/` 下全部受审文件与基线一致；当时工作区仅有 `zchttp/` 的未提交改动（`zchttp/docs/*.md`、`zchttp/openapi.go`、`zchttp/openapi_test.go`），不在本次范围。审查期章节中的 `文件:行` 均相对上述基线；修复后的变更行号见“修复执行记录”。
- 审查方法：正向逐声明比对 + 反向导出 API 审查 + 运行时探针。探针以 `go test -overlay` 把临时 `_test.go` 映射进 `zcmodel` 包执行，不修改仓库文件；探针与临时缓存已清理。

### 已执行的实测

- `go test ./zcmodel -count=1`：通过。
- `go test ./zcmodel -cover -count=1`：`coverage: 98.4% of statements`，与 `README.md:183` 一致。
- `go vet ./zcmodel`：通过。
- MySQL / PostgreSQL / SQLite 集成测试实际执行通过；Windows 上 `TestWriteOrReplaceStruct_PreservePermissions`、`TestWriteOrReplaceStruct_ReadOnlyDir` 按平台条件 Skip，不影响本报告结论。
- 审查期 `doc_go_examples.py --docs-dir zcmodel/docs --strict`：4 个报错（2 个真实片段问题 + 2 个整文件示例的 `package p` 包装误报）；修复后真实片段已通过，仅剩 2 个包装误报，见“实际验收结果”。
- overlay 探针：复现 DOC-01、DOC-02、DOC-03、DOC-04、DOC-07、DOC-08 的实际产物或错误文本。

### 问题统计与计入标准

- 问题总数 8：高 0、中 4、低 4。
- 计入：导致生成产物不可编译、用户代码/注释丢失、公开契约与实现不符、文档示例不可解析、错误文本契约不实。
- 不计入：与公开契约无关的内部实现细节、纯风格差异、本次未改动的 `zchttp` 工作区内容。

## 问题汇总

| 编号 | 级别 | 状态 | 一句话结论 |
|---|---|---|---|
| DOC-01 | 中 | 已修 | 同名别名导入（如 `time "time"`）被当作“未导入”，再生成会追加默认导入，产物因 `time redeclared` 无法编译。 |
| DOC-02 | 中 | 已修 | import 使用检测只看选择器名字：局部变量遮蔽包名会保留未使用 import，产物无法编译。 |
| DOC-03 | 中 | 已修 | 同一个 type 块内的结构体缺少配对方法时，方法按块边界插入，不紧跟各自结构体，违反文档的标准顺序。 |
| DOC-04 | 中 | 已修 | 仅含一个生成 Spec 的 type 块展开为独立声明时，块内游离注释被静默丢弃。 |
| DOC-05 | 低 | 已修 | 两个 `go` 示例块不是合法 Go：索引块片段缺右花括号，显式字段信息片段是顶层语句。 |
| DOC-06 | 低 | 已修 | `StructFieldInfo.Import` 文档称留空不引入包，但 `time.Time` 会自动补 `time`。 |
| DOC-07 | 低 | 已修 | 错误文本格式声明称表名/文件名原样输出，非法 UTF-8 分支实际用 `%q` 输出。 |
| DOC-08 | 低 | 已修 | 含反引号的列名在列映射 tag 中被替换为单引号，文档未说明这会导致 zcdb 列映射失配。 |

## 逐条详情

### DOC-01：同名别名导入被重复追加，再生成产物无法编译

- 级别：中；类型：实现缺陷 / 过度承诺
- 文档证据：`zcmodel/docs/zcmodel.md:418` 称存量文件以别名导入所需包时“仍会补充默认导入（生成代码引用默认包名，两种导入可合法共存）”；`zcmodel/docs/zcmodel.md:583` 进一步承诺再生成后“产物保持可编译”。
- 实现证据：`zcmodel/build.go:872` 在统计 `existingImports` 时跳过所有 `impSpec.Name != nil` 的导入；`zcmodel/build.go:967-970` 因此把已由 `time "time"` 绑定默认本地名 `time` 的包再次列为缺失；`zcmodel/build.go:975-1018` 把默认导入追加进同一 import 块。`zcmodel/build.go:499` 的注释还明确写着“保证生成代码可编译”。
- 测试证据：`zcmodel/build_test.go:496-547` 只覆盖 `mytime "time"` 与 `_ "time"` 两种别名，断言“补充标准导入”并“完整保留”；没有覆盖别名恰好等于默认包名的 `time "time"`。
- 实测：overlay 探针以存量文件 `import time "time"` 再生成，产物 import 块同时含 `"time"` 与 `time "time"`：

```text
import (
    "time"
    time "time"
)

go/types: time redeclared in this block
```

- 影响：合法存量文件再生成后无法编译，而 `Generate` 返回 nil；用户只能在后续 `go build`/测试阶段发现，违反“产物保持可编译”的公开承诺。
- 推荐方案：统计现有导入绑定名时保留别名（`importLocalName` 已能返回别名），当所需包的默认本地名已被某个现有 spec 绑定时不再追加；若同名绑定指向不同路径，返回明确错误而不是写出坏文件。补一个 `import time "time"` 的再生成用例，并用 `go/types` 做编译级断言。若决定不修，文档必须写明同名别名不受支持，但这与“两种导入可合法共存”直接矛盾。

### DOC-02：局部变量遮蔽包名时 import 使用判定误判

- 级别：中；类型：实现缺陷 / 过度承诺
- 文档证据：`zcmodel/docs/zcmodel.md:418` 描述移除规则为“旧生成代码引用过、而新生成代码与保留的用户代码都不再引用的 import 会被移除”；`zcmodel/docs/zcmodel.md:583` 承诺“产物保持可编译”。
- 实现证据：`zcmodel/build.go:420-430` 的 `collectSelectorRefs` 只把 `sel.X.(*ast.Ident)` 的名字收集为包引用候选，不区分该标识符是包名还是同名局部变量；`zcmodel/build.go:760-786` 据此分别构造 `oldGenRefs`/`usedRefs`；`zcmodel/build.go:812` 仅按名字决定是否移除 import。
- 测试证据：`zcmodel/build_test.go:1013-1101` 覆盖普通“不再引用则移除”“用户仍引用则保留”路径，但没有覆盖局部变量遮蔽包名的场景。
- 实测：overlay 探针构造函数内 `time := struct{ Now func() }{}; _ = time.Now`，旧生成代码含 `time.Time`、新生成代码不再需要 `time`；产物保留 `import "time"`，`go/types` 报 `"time" imported and not used`。
- 影响：遮蔽导致未使用 import 残留，再生成产物无法编译，且 `Generate` 不报错。
- 推荐方案：用 parser 的对象解析信息（`ast.Ident.Obj == nil` 表示未绑定到文件内声明）或 `go/types` 区分包引用与局部声明；补一条遮蔽回归用例并断言产物通过 `go/types`。仅改文档不能消除“产物不可编译”的后果，因此优先改实现。

> 修订说明：审查初稿曾把 `var _ = (time).Now` 列为“括号包裹的包名被误删”。修复时实测 `go/types` 报 `use of package time not in selector`，该写法本身不是合法 Go，不能出现在可编译的存量文件里；因此作为误报从本条剔除，DOC-02 只保留已证实的局部变量遮蔽问题。

### DOC-03：混合 type 块内缺失生成方法不紧跟对应结构体

- 级别：中；类型：实现缺陷 / 误导性契约
- 文档证据：`zcmodel/docs/zcmodel.md:417` 明确“结构体缺失补在其方法前、方法缺失补在其结构体后”，并列出标准顺序“Entity 结构体 → ToDO → DO 结构体 → ToEntity”；`zcmodel/docs/zcmodel.md:416` 又声明 type 块按 Spec 粒度处理。
- 实现证据：`zcmodel/build.go:921-931` 对混合 type 块只把 `carrierIdx[k]` 指向整个块槽位，不记录 Spec 在块内的位置；`zcmodel/build.go:1036-1058` 的配对落点因此只能以块槽位为基准；`zcmodel/build.go:1073-1084` 按槽位输出。
- 测试证据：`zcmodel/build_test.go:1342-1389` 只覆盖独立声明之间的配对落点；`zcmodel/build_test.go:1420-1448` 只覆盖“单 Spec 块展开”，没有覆盖“同一 type 块含 Entity 与 DO、且两个方法都缺失”的组合。
- 实测：存量文件为同块内的 `UserEntity` 与 `UserDO`，新生成代码包含两个方法；产物顺序为“type 块（Entity、DO）→ ToDO → ToEntity”：

```text
type (
    UserEntity struct { ... }
    UserDO struct { ... }
)

func (e *UserEntity) ToDO() {}

func (d *UserDO) ToEntity() {}
```

ToDO 落在 DO 之后，而不是紧跟 Entity；文档的标准顺序不成立。
- 影响：生成声明的位置契约与 golden/diff 预期不一致；代码仍可编译，但“方法紧跟其结构体”的承诺失效，用户按文档预期阅读或做增量 diff 时会被误导。
- 推荐方案：有两条路，需要用户拍板。方案 A（推荐，代价小且不破坏用户块结构）：收窄文档，明确“当结构体位于同一个 type 块内时，缺失的配对方法落在整个 type 块之后；标准顺序只在独立声明之间保证”，并补混合块回归用例。方案 B（严格满足现文档）：跟踪每个生成 Spec 的块内偏移，必要时把块拆成独立声明以便在对应结构体后插入方法；这会改变用户原有 type 块形态，与“块形态保留”存在张力，需明确取舍。

### DOC-04：单 Spec type 块展开时丢失块内游离注释

- 级别：中；类型：实现缺陷 / 关键行为遗漏
- 文档证据：`zcmodel/docs/zcmodel.md:416` 声明“用户类型（含各自注释）与块内不附着于任何 Spec 的游离注释内容与位置保留”；`zcmodel/docs/zcmodel.md:415` 也承诺“注释随声明原位保留”。
- 实现证据：`zcmodel/build.go:906-916` 在“仅含一个生成 Spec”时直接用 `takeLead(lead) + genText[k]` 输出独立声明，完全跳过块体；`zcmodel/build.go:693-712` 的 `renderTypeBlock` 本可按源码区间保留游离注释，但该分支没有调用它。
- 测试证据：`zcmodel/build_test.go:899-937` 覆盖多 Spec 混合块中的游离注释保留；`zcmodel/build_test.go:1420-1448` 覆盖单 Spec 块展开，但块内没有游离注释，因此未锁死该场景。
- 实测：构造 `type (` 后跟“与 Spec 之间有空行”的注释；AST 检查显示 `specDoc=nil`，确认它是游离注释而不是 Spec 的 Doc；再生成产物不含该注释。
- 影响：用户放在单 Spec 括号块内的游离注释在再生成时被静默删除，且没有任何错误提示，违反“用户代码/注释原位保留”的核心承诺。
- 推荐方案：在单 Spec 展开分支保留块内不附着于 Spec 的注释区间（可复用 `renderTypeBlock` 的区间拼接思路，把游离注释前置到新独立声明之前），并补一条“单 Spec 块 + 游离注释 + 再生成”的回归用例；若确实决定不保留，必须在文档中明确该例外并说明注释会被丢弃。

### DOC-05：两个 go 示例块不可解析

- 级别：低；类型：文档错误
- 文档证据：`zcmodel/docs/zcmodel.md:301` 起的“索引块”示例只有注释与 `type UserOrderDO struct {`，没有右花括号；`zcmodel/docs/zcmodel.md:563` 起的“显式指定字段信息”示例是包级语句 `columns := []*zcmodel.Column{...}`，不在函数内。
- 实现证据：不适用（纯文档问题：示例片段本身不构成可解析的 Go 文件）。
- 测试证据：`zcmodel/docs_examples_compile_test.go:3-7` 明确说明示例是手工收录、不自动读取 Markdown；`zcmodel/docs_examples_compile_test.go:141` 只执行“手工构造 Input”示例，没有覆盖这两个片段。
- 实测：`doc_go_examples.py --docs-dir zcmodel/docs --strict` 报：

```text
zcmodel.md#索引块#1: parse error: <standard input>:10:26: expected ';', found 'EOF'
zcmodel.md#显式指定字段信息-覆盖自动推导#1: parse error: <standard input>:4:1: expected declaration, found 'columns'
```

同一次检查中，第 445 行与第 487 行的 `package main` 整文件示例在脚本外独立执行 `gofmt -e` 均通过；脚本强制前置 `package p` 才报错，属工具包装限制，不计为文档问题。
- 影响：读者复制这两个标记为 `go` 的片段会得到语法错误；文档严格校验无法归零。
- 推荐方案：把“索引块”片段补成完整类型声明，或改用 `text` 围栏；把“显式指定字段信息”片段包进 `func example() { ... }`（这是语法正确的 Go）；保留两个整文件示例的 `go` 标记。若希望长期锁死示例，扩展 `docs_examples_compile_test.go` 从 Markdown 提取，或登记块清单与内容哈希。

### DOC-06：Import 留空语义文档与自动补 time 矛盾

- 级别：低；类型：文档内部矛盾
- 文档证据：`zcmodel/docs/zcmodel.md:158` 称 `Import` “留空不引入包”；同一文档 `zcmodel/docs/zcmodel.md:377` 又说 `time.Time` 会自动引入 `time` 包。
- 实现证据：`zcmodel/generate.go:86-89` 在 `StructFieldInfo.Type == "time.Time"` 且 `Import == ""` 时自动填充 `"time"`；`zcmodel/input.go:56` 的字段注释也写“空值不引入任何包”。
- 测试证据：`zcmodel/generate_test.go:314-340` 锁定“datetime 列自动补 time import，且不修改调用方数据”。
- 实测：`Generate` 传入 `datetime` 列且不填 `Import`，产物包含 `import "time"`；文档第 445 行的手工示例同样得到该结果。
- 影响：调用方可能误以为留空就绝不会引入包，或误以为必须显式填 `Import` 才能使用 `time.Time`；属于低风险的 API 文档矛盾。
- 推荐方案：把两处改为“留空时仅 `time.Time` 自动补 `time`，其余类型不引入包”，并同步 `zcmodel/input.go:56` 注释；无需新增行为测试，现有自动补全用例保留。

### DOC-07：错误文本格式声明与非法 UTF-8 分支不符

- 级别：低；类型：文档错误
- 文档证据：`zcmodel/docs/zcmodel.md:124` 称“表名与文件名以原样输出”；`zcmodel/docs/zcmodel.md:101` 与 `zcmodel/docs/zcmodel.md:107` 分别列出表名、文件名的非法 UTF-8 错误。
- 实现证据：`zcmodel/generate.go:176` 对非法 UTF-8 表名使用 `%q`；`zcmodel/generate.go:199-200` 与 `zcmodel/generate.go:207` 对表名/文件名同样使用 `%q`。
- 测试证据：`zcmodel/generate_test.go:577-609` 只做 `strings.Contains` 子串断言，未锁定 `%q` 格式。
- 实测：探针得到 `表名不是合法的 UTF-8: "user\x8e"` 与 `文件名不是合法的 UTF-8: "user\x8e"`（错误信息中带引号），与“原样输出”不符。
- 影响：文档对错误文本的格式契约不准确；依赖错误文本做日志解析或精确断言的调用方会偏差。
- 推荐方案：修改文档第 124 行，明确“非法 UTF-8 名称以 `%q`（带引号、转义非法字节）输出，其余表名/文件名错误原样输出”；保留实现现状，因为直接输出非法字节不利于日志与终端。若坚持“原样”，则需改实现并承担错误信息出现非法字节的代价。

### DOC-08：含反引号列名的列映射 tag 值被改写，文档未说明映射后果

- 级别：低；类型：关键行为遗漏
- 文档证据：`zcmodel/docs/zcmodel.md:16` 把反引号处理归入“tag 安全”；`zcmodel/docs/zcmodel.md:227` 称列映射 tag 的值是列名并经 `sanitizeTagValue` 净化；`zcmodel/docs/zcmodel.md:431` 仅说明反引号被替换为单引号。
- 实现证据：`zcmodel/build.go:79-82` 的 `sanitizeTagValue` 对 `strconv.Quote` 结果执行反引号到单引号的替换；`zcmodel/build.go:278` 把净化后的列名写入列映射 tag。
- 测试证据：`zcmodel/build_test.go:265-289` 明确锁死 ``a`b`` → `a'b` 的替换结果，并只断言 tag 可被 `reflect` 读出，不校验它是否仍等于真实列名。
- 实测：以 PostgreSQL 合法列名 ``a`b`` 生成模型，`reflect.StructTag.Get("db")` 得到 `a'b`，与列名 ``a`b`` 不相等。
- 影响：zcdb 的列映射按 tag 值匹配真实列名；反引号列名会被静默映射到错误列，读写行为不正确。文档只讲了语法安全，没有提示这一语义损失。
- 推荐方案：二选一。方案 A：当 tag 值含反引号时，用双引号解释型字符串字面量承载整个 tag（Go 允许，`gofmt` 可保留），从而完整保留列名；同步修改 `zcmodel/build_test.go:265-289` 的期望并增加“tag 值等于原列名”的断言。方案 B：在“已知局限”中明确写“含反引号的列名无法在列映射 tag 中精确保留，会被替换为单引号，zcdb 映射会失配”，并建议改名或显式处理。优先 A，因为它消除的是核心映射能力的损失。

## 已核对且未发现偏差的关键行为

- **方言与命名枚举**：`Dialect` 三个值、`NameCase` 六个值及 `IsValid()` 与文档表格一致，见 `zcmodel/docs/zcmodel.md:177-196`、`zcmodel/input.go:3-37`、`zcmodel/input_test.go:6-69`。
- **Generate 主流程与 tag 顺序**：Entity/DO 字段、`json → column → primary_key → ddl → description` 顺序、DO 的 `any` 类型、ToDO/ToEntity 签名与赋值/断言语句均有行级断言，见 `zcmodel/docs/zcmodel.md:223-232`、`zcmodel/build.go:272-288`、`zcmodel/generate_test.go:23-82`。
- **表名/文件名校验**：空名、`.`/`..`、路径分隔符、Windows 非法字符、首字符、导出结构名前缀、Go 文件名规则（`_`/`.` 开头、`_test`、GOOS/GOARCH）均与文档错误表一致，见 `zcmodel/docs/zcmodel.md:98-111`、`zcmodel/generate.go:171-244`、`zcmodel/generate_test.go:393-639`。
- **字段名校验**：空字段名、非 Go 标识符、未导出标识符、与 ToDO/ToEntity 同名、重复字段名均前置报错且不落盘，见 `zcmodel/docs/zcmodel.md:113-118`、`zcmodel/generate.go:92-117`、`zcmodel/generate_test.go:662-695`、`zcmodel/generate_test.go:838-955`。
- **ddl 片段**：类型原样、Nullable 三态、Default 指针语义与空串 `''`、EXTRA 白名单顺序与 `(n)` 精度、`DEFAULT_GENERATED` 过滤，见 `zcmodel/docs/zcmodel.md:276-293`、`zcmodel/build.go:117-184`、`zcmodel/build_test.go:1524-1616`。
- **索引块**：PRIMARY → UNIQUE（名字典序）→ 普通（名字典序）、主键行隐藏物理索引名、三空格清单、换行/成对引号净化，见 `zcmodel/docs/zcmodel.md:295-317`、`zcmodel/build.go:186-235`、`zcmodel/build_test.go:1617-1686`、`zcmodel/generate_test.go:1219-1281`。
- **列类型映射表完整性**：探针打印三方言 map 键集，与文档三张映射表逐键一致（MySQL 32 键、PostgreSQL 36 键、SQLite 31 键）；归一化规则的 MySQL unsigned/zerofill、PostgreSQL 时区后缀、SQLite 去引号均有覆盖，见 `zcmodel/docs/zcmodel.md:349-377`、`zcmodel/columnTypeToGoType.go:18-123`、`zcmodel/columnTypeToGoType_test.go:13-152`。
- **命名转换**：`SplitWords`、`ToPascalCase`、六个风格函数及 `id → ID` 特例与文档示例一致，见 `zcmodel/docs/zcmodel.md:379-404`、`zcmodel/toCase.go:9-188`、`zcmodel/toCase_test.go:11-179`。
- **增量再生成核心路径（非块边界特例）**：用户声明原位保留、生成声明原位替换、build tags/文件级注释/package 行保留、混合 type 块内用户类型与游离注释保留、缺失 import 补齐、普通未使用 import 移除，见 `zcmodel/docs/zcmodel.md:406-427`、`zcmodel/build.go:509-1115`、`zcmodel/build_test.go:1302-1517`。
- **落盘安全**：`go/format.Source` 自校验、同目录临时文件 + rename、解析失败不覆盖、权限保留逻辑，见 `zcmodel/docs/zcmodel.md:423-427`、`zcmodel/build.go:336-380`、`zcmodel/build_test.go:423-440`、`zcmodel/build_test.go:761-833`（后两项权限用例在 Windows 上 Skip）。
- **README 覆盖率**：`README.md:183` 的 zcmodel 98.4% 与本次 `go test ./zcmodel -cover -count=1` 实测一致。
- **文档主示例**：`zcmodel/docs_examples_compile_test.go:141` 实际执行手工示例并核对索引块、主键 tag、NULL 标记与 `DEFAULT CURRENT_TIMESTAMP` 片段。

## Go 示例覆盖

| 文档#章节#序号 | 语法/结构 | 证据/原因 |
|---|---|---|
| `zcmodel.md#Generate#1` | 通过 | `gofmt -e` 在 `package p` 包装下解析通过；见 `zcmodel/docs/zcmodel.md:90`。 |
| `zcmodel.md#Column-StructFieldInfo-IndexInfo#1` | 通过 | 完整类型声明可解析；见 `zcmodel/docs/zcmodel.md:143`。 |
| `zcmodel.md#Entity-与-DO-结构体#1` | 通过 | 完整结构体声明可解析；见 `zcmodel/docs/zcmodel.md:236`。 |
| `zcmodel.md#索引块#1` | 失败 | 片段只有 `type UserOrderDO struct {`，缺右花括号；见 DOC-05 与 `zcmodel/docs/zcmodel.md:301`。 |
| `zcmodel.md#ToDO-ToEntity-互转方法#1` | 通过 | 两个无函数体的方法声明是语法合法 Go；见 `zcmodel/docs/zcmodel.md:323`。 |
| `zcmodel.md#手工构造-Input-生成#1` | 通过（独立文件） | 独立 `gofmt -e` 通过；脚本强制前置 `package p` 才报错，属工具包装误报；`zcmodel/docs_examples_compile_test.go:141` 有执行级覆盖；见 `zcmodel/docs/zcmodel.md:445`。 |
| `zcmodel.md#配合-zcdb-Schema-从真实数据库生成#1` | 通过（独立文件） | 独立 `gofmt -e` 通过；脚本包装误报；编译级镜像在 `zcmodel/docs_examples_compile_test.go:79`；见 `zcmodel/docs/zcmodel.md:487`。 |
| `zcmodel.md#显式指定字段信息-覆盖自动推导#1` | 失败 | 包级 `columns :=` 语句不能构成 Go 文件；见 DOC-05 与 `zcmodel/docs/zcmodel.md:563`。 |

> 上表为修复前基线状态；DOC-05 修复后“索引块”与“显式指定字段信息”两个块均已解析通过，整文件示例剩余 2 个报错是脚本包装误报，见“实际验收结果”。

## 修复执行记录

- **DOC-01（已修）**
  - 实现：新增 `importPathLocalName` 并重构 `importLocalName`（`zcmodel/build.go:420-449`）；重建时记录所有导入绑定的本地名（`zcmodel/build.go:884`）；同名别名已满足默认包名时不再追加，绑定到其他路径时返回 `import 别名冲突`（`zcmodel/build.go:1044-1061`）。
  - 测试：新增同名别名再生成用例（`zcmodel/build_test.go:585`）与别名冲突不覆盖原文件用例（`zcmodel/build_test.go:610`），均做编译级类型检查。
  - 文档：错误表新增别名冲突行（`zcmodel/docs/zcmodel.md:122`），import 段落改为区分“不同名别名 / 同名别名 / 冲突”（`zcmodel/docs/zcmodel.md:421`）。
- **DOC-02（已修）**
  - 实现：`collectSelectorRefs` 只收集未绑定到文件内声明的标识符（`id.Obj == nil`），避免局部变量遮蔽包名时误保留 import（`zcmodel/build.go:453-461`）。
  - 测试：`TestWriteOrReplaceStruct_RemoveUnusedImports` 新增“局部变量遮蔽包名时不保留 import”子用例（`zcmodel/build_test.go:1112`），产物通过 `go/types`。
  - 修订：`(time).Now` 经实测不是合法 Go（`use of package time not in selector`），已作为误报从 DOC-02 剔除，不作实现改动。
- **DOC-03（已修，采用报告推荐的文档收窄）**
  - 文档：明确混合 type 块内缺失结构体补在块前、缺失方法补在块后，标准顺序只在独立声明之间保证（`zcmodel/docs/zcmodel.md:419-420`）。
  - 测试：新增同块 Entity+DO、两个方法缺失的顺序锁死用例（`zcmodel/build_test.go:1499`）。
  - 源码注释同步：`zcmodel/build.go:519-521`。
- **DOC-04（已修）**
  - 实现：单 Spec 块仅在存在块级 Doc 或块内游离注释时保留括号块，否则维持原有“展开为独立声明”的行为（`zcmodel/build.go:965-995`）。
  - 测试：新增块级 Doc + 游离注释用例（`zcmodel/build_test.go:1585`）、无块级 Doc 但 Spec 带 Doc 且块内另有游离注释的用例（`zcmodel/build_test.go:1621`）、块前游离注释仍展开用例（`zcmodel/build_test.go:1653`）。
  - 文档：改写单 Spec 展开的例外条件（`zcmodel/docs/zcmodel.md:419`）。
- **DOC-05（已修）**
  - 文档：索引块示例补齐右花括号（`zcmodel/docs/zcmodel.md:302-313`）；显式字段信息片段包进 `func example()`（`zcmodel/docs/zcmodel.md:566-581`）。
  - 结果：两个块均通过 `gofmt -e`；整文件示例保留 `go` 标记，脚本包装误报见“实际验收结果”。
- **DOC-06（已修）**
  - 文档与注释：`zcmodel/docs/zcmodel.md:159` 与 `zcmodel/input.go:56` 改为“留空时仅 `time.Time` 自动补 `time`，其余不引入包”。
  - 测试：`TestGenerate_InvalidUTF8Name` 精确断言（`zcmodel/generate_test.go:613`）保持自动补全行为不变。
- **DOC-07（已修）**
  - 文档：错误文本格式说明改为“表名/文件名通常原样输出，非法 UTF-8 名称以 `%q` 输出”（`zcmodel/docs/zcmodel.md:125`）。
  - 测试：`TestGenerate_InvalidUTF8Name` 增加精确错误文本断言（`zcmodel/generate_test.go:613`），锁定 `%q` 格式。
- **DOC-08（已修）**
  - 实现：新增 `formatStructTag`（`zcmodel/build.go:83-110`）；值先 `strconv.Quote`，任一键/值含反引号时整个 tag 改用解释型字符串字面量；`buildStruct` 改用键值对组装（`zcmodel/build.go:269-305`）。
  - 测试：`TestFormatStructTag` 与反射还原（`zcmodel/build_test.go:139`、`zcmodel/build_test.go:183`）；端到端反引号列名生成（`zcmodel/generate_test.go:119`）；列映射 tag 反引号完整保留（`zcmodel/build_test.go:298`）。
  - 文档：tag 安全段落与注释净化章节改为“反引号完整保留 / 解释型字符串字面量”（`zcmodel/docs/zcmodel.md:16`、`zcmodel/docs/zcmodel.md:432-440`）。

## 实际验收结果

| 验收项 | 命令/用例 | 结果 |
|---|---|---|
| 全量测试 | `go test ./zcmodel -count=1` | 通过；MySQL / PostgreSQL / SQLite 集成用例实际执行，Windows 上两个权限用例按平台 Skip。 |
| 竞态 | `go test ./zcmodel -count=1 -race` | 通过。 |
| 乱序重复 | `go test ./zcmodel -shuffle=on -count=5` | 通过。 |
| 静态检查 | `go vet ./zcmodel` | 通过，无输出。 |
| 格式 | 本次改动的 Go 文件逐个剥 `\r` 后 `gofmt -l` | 无输出；`input_test.go`、`testhelpers_test.go` 的既有 CRLF 假告警未改动，不在本次范围。 |
| 覆盖率 | `go test ./zcmodel -cover -count=1` | 98.3%（修复前 98.4%）；下降来自新增的别名冲突、注释保留与防御性分支，新增可达分支均有对应用例。README 的 98.4% 是 2026-10-17 基线快照，本次未刷新覆盖率表。 |
| 工作区检查 | `git diff --check` | 通过；仅有 Git 的 LF→CRLF autocrlf 提示。 |
| 文档片段 | `doc_go_examples.py --docs-dir zcmodel/docs --strict` | 6/8 块解析通过；剩余 2 个报错是整文件示例被脚本强制前置 `package p` 的包装误报，独立 `gofmt -e` 通过，不计为文档问题。 |
| DOC-01 回归 | 同名别名再生成 + `go/types`；别名冲突错误 | 通过：不再追加默认导入；冲突时返回明确错误且原文件逐字节不变。 |
| DOC-02 回归 | 局部变量遮蔽包名再生成 + `go/types` | 通过：未使用 import 被移除，产物可编译。 |
| DOC-03 回归 | 同块 Entity+DO、两个方法缺失 | 通过：方法落在块后，文档已明确该例外。 |
| DOC-04 回归 | 单 Spec 块 + 块级 Doc/游离注释 | 通过：注释保留且括号块保留；无注释时仍展开为独立声明。 |
| DOC-05 回归 | 两个修正后的示例块 | 通过：`索引块#1` 与 `显式指定字段信息#1` 均可解析。 |
| DOC-06/07 回归 | 文档/注释核对；非法 UTF-8 精确错误断言 | 通过：文档语义与错误文本一致。 |
| DOC-08 回归 | 反引号列名生成 + `reflect.StructTag.Get` | 通过：`db` 与 `json` tag 均等于真实列名 ``a`b``，产物通过编译级检查。 |

> 剩余未完成事项：本次未提交、未推送；README 覆盖率表（2026-10-17 快照）未按本次 98.3% 刷新，如需保持“四者一致”可另跑覆盖率刷新任务。
