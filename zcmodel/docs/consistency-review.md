# zcmodel 文档与实现一致性审查报告

- 审查基线：`8e98521c4b5bc32837cdb259aa3e84af0ddc2b4e`（`zcmodel/` 工作区在该基线之上无改动；工作区另有 zcdb 未提交改动，与本报告无关）
- 审查日期：2026-10-17
- 当前状态：**已修复（2026-10-17）**，4 条问题全部按推荐方案落地；第 3 节详情中的文件行号指向**修复前基线** `8e98521`，仅用于追溯，修复后的位置见第 5 节
- 问题总数：4（高 0 / 中 2 / 低 2），修复后复核全部关闭

## 1. 审查范围与方法

### 1.1 文档清单

| 文档 | 角色 | 说明 |
|---|---|---|
| `docs/zcmodel.md` | 被审文档（唯一） | 全文 598 行，逐节比对 |
| `README.md` | 交叉核对 | 仅核对 zcmodel 模块描述与链接，不计入问题范围 |
| `agents.md` | 交叉核对 | 仅核对 zcmodel 关键行为不变量与测试归属约定，不计入问题范围 |

### 1.2 对照的源码与测试

- 源码：`input.go`、`generate.go`、`build.go`、`columnTypeToGoType.go`、`toCase.go`
- 测试：`build_test.go`、`generate_test.go`、`columnTypeToGoType_test.go`、`toCase_test.go`、`input_test.go`、`integration_test.go`、`docs_examples_compile_test.go`、`testhelpers_test.go`
- 跨模块核对：`../zcdb/db_dao.go`、`../zcdb/schema_inspector.go`、`../zcdb/docs/schema.md`、`../zcdb/docs/query-builder.md`（仅核对文档中的桥接示例与引用）

### 1.3 实测过的行为

1. `go test ./zcmodel -count=1` 全绿；`go vet ./zcmodel` 无输出。
2. `TestInteg_MySQL_AllTypes`、`TestInteg_Postgres_AllTypes`、`TestInteg_SQLite_AllTypes`、`TestInteg_MySQL_SampleBridge`、`TestInteg_SQLite_IndexChangeRegeneration` 全部实跑通过（本机 MySQL/PostgreSQL 容器在线，无 SKIP）。
3. 三方言映射表与文档表格机械比对：MySQL 32/32、PostgreSQL 36/36、SQLite 30/30，键与值均无差异。
4. 用 overlay 探针实测：空 `JsonTagValueCase` + 显式 `JsonTagValue`；括号包裹的值接收者 `ToDO`；混合 `type` 块内重复生成类型；根目录包名回退；别名/点导入与默认导入共存（`go/types` 校验）；生成产物 `go build`；整段 import 移除时的 Doc 注释；用户区空行与格式变化。
5. 探针均为临时文件，未写入仓库；审查结束后已删除。

### 1.4 计入/不计入标准

- 计入：文档声明与实现/测试可观察行为不一致，且可能误导调用方、影响产物正确性或核心契约（如「原样保留」「去重」）。
- 不计入：纯内部实现细节；不影响公开契约的措辞风格；跨模块内部实现（仅核对其被 zcmodel 文档引用的公开行为）；README 覆盖率数字（由覆盖率刷新流程维护）。

## 2. 问题汇总表

| 编号 | 级别 | 状态 | 一句话结论 |
|---|---|---|---|
| DOC-01 | 低 | 已修复 | `JsonTagValueCase` 为空时文档称「不生成任何 json tag」，但显式 `StructFieldInfo.JsonTagValue` 仍会生成 tag |
| DOC-02 | 中 | 已修复 | 括号包裹的值接收者 `func (e (T)) ToDO()` 未被识别为生成方法，再生成后产生两个同名方法且 `Generate` 返回成功 |
| DOC-03 | 低 | 已修复 | 混合 `type` 块内重复的生成类型 Spec 未去重，与「其余重复槽位移除」的声明不符 |
| DOC-04 | 中 | 已修复 | 再生成会把用户区整体 gofmt 化，并在相邻顶层声明间强制插入空行，「用户代码原样留在原位 / git diff 只呈现生成区」不成立 |

## 3. 逐条详情

### DOC-01：`JsonTagValueCase` 空值语义与显式 `JsonTagValue` 冲突

- 级别/类型：低；文档错误（绝对化措辞，与「显式优先」原则自相矛盾）
- 文档证据：`docs/zcmodel.md:135` 称 `JsonTagValueCase`「**空值表示不生成 json tag**」；`docs/zcmodel.md:593` 进一步写成「`JsonTagValueCase` 传空串则不生成任何 json tag」；但 `docs/zcmodel.md:157` 又说明 `StructFieldInfo.JsonTagValue` 非空时按显式值生成，`docs/zcmodel.md:168` 声明「显式优先」。
- 实现证据：`generate.go:62-71` 仅在 `JsonTagValueCase` 非空时自动推导 tag 值；`build.go:274-276` 只要 `StructFieldInfo.JsonTagValue` 非空就无条件写入 `json` tag，与全局风格是否为空无关。
- 测试证据：现有测试未覆盖「全局风格为空 + 单列显式 `JsonTagValue`」组合（`generate_test.go:83-112` 只覆盖非空风格下的特殊字符）。
- 实测：构造 `JsonTagValueCase: ""`、列 `StructFieldInfo{JsonTagValue: "explicit"}` 调用 `Generate`，产物为 `ID int64 `json:"explicit" ddl:"bigint"``，json tag 确实生成。
- 影响：按文档字面理解（尤其注意事项 8「不生成任何 json tag」）的调用方会误判显式 `JsonTagValue` 被忽略；实际行为与「显式优先」一致，属文档措辞错误而非实现错误。
- 推荐方案：首选改文档——将两处改为「空值表示**不自动生成** json tag；单列显式 `StructFieldInfo.JsonTagValue` 仍会生成 tag」。若坚持「空串绝不生成 json tag」，则需改实现并同步 `StructFieldInfo` 语义，但这会破坏「显式优先」，不建议。保持现状时，文档必须按首选改写。

### DOC-02：括号包裹的值接收者未被识别为生成方法，再生成产生重复方法

- 级别/类型：中；实现缺陷（边界）+ 文档过度承诺
- 文档证据：`docs/zcmodel.md:411` 称生成声明槽位的判定「含值接收者版本」；`docs/zcmodel.md:580` 称自定义方法「指针与值接收者均支持」。`func (e (UserOrderEntity)) ToDO()` 是合法的 Go 值接收者写法。
- 实现证据：`build.go:706-719` 的 `receiverTypeName` 只处理 `*ast.StarExpr`（指针接收者）与 `*ast.Ident`（直接值接收者）；括号接收者在 AST 中是 `*ast.ParenExpr`，返回空串，`build.go:724-734` 的 `isGeneratedMethod` 因此判为 false，旧方法按用户代码保留，随后缺失的生成方法又按配对落点补入。
- 测试证据：`build_test.go:677` 覆盖直接值接收者 `ToDO` 的替换，`build_test.go:810` 覆盖值接收者自定义方法保留；无括号接收者用例。
- 实测：存量文件含 `func (e (UserEntity)) ToDO() {}`，再生成后产物同时含 `func (e *UserEntity) ToDO() {}` 与 `func (e UserEntity) ToDO() {}`，`Generate` 返回 nil。单独验证 `func (e (E)) M() {}` 可被 `go build` 接受，说明输入合法；而重复方法名只在语义检查阶段报错，`go/format.Source` 的语法自校验拦不住。
- 影响：合法的用户文件再生成后产物不可编译，且调用方在 `Generate` 阶段看不到任何错误；属于静默产出坏代码。
- 推荐方案：首选在 `receiverTypeName` 中循环解包 `*ast.ParenExpr`（如 `func (e (T))`、`func (e ((T)))`）后再按 `Ident`/`StarExpr` 判定；在 `build_test.go` 增加「括号值接收者 ToDO 被原位替换、最终只有一份」的回归用例，并做编译级校验。备选：若明确不支持该写法，把文档改为「仅识别直接书写的 `T` / `*T` 接收者」，并明确未识别时的风险；不推荐，因为实现修复成本很低。

### DOC-03：混合 `type` 块内重复的生成类型 Spec 未去重

- 级别/类型：低；实现缺陷（边界）/ 文档声明过宽
- 文档证据：`docs/zcmodel.md:415` 称「同类生成声明出现多份时（如用户手写的值接收者 ToDO 与生成的指针接收者 ToDO 共存），首个槽位原位替换、其余重复槽位移除」。
- 实现证据：`build.go:687-702` 的 `renderTypeBlock` 对块内每个生成 Spec 都无条件写入新文本，没有按 `genKind` 去重；`build.go:896-906` 的混合块分支只记录首个承载槽位，不跳过后续重复 Spec。顶层独立声明路径的去重由 `build.go:918-926` 完成，两者行为不一致。
- 测试证据：`build_test.go:1996-2004` 只覆盖两个独立的顶层 `type UserDO struct` 声明；没有覆盖单个 `type (...)` 块内重复 Spec。
- 实测：存量文件含一个 `type (...)` 块，其中两个 `UserEntity struct`（语义非法但可被 `go/parser` 解析）、一个用户类型；再生成后块内仍有两个 `UserEntity struct`，`Generate` 返回 nil。
- 影响：触发前提是存量文件本身已含重复同名类型（不可编译），因此实际风险低；但文档承诺的去重在该形态下不成立，且产物依旧不可编译。
- 推荐方案：首选在 `renderTypeBlock` 中按 `genKind` 跳过已出现的生成 Spec（保留首个、其余输出空串，并按既有规则顺延游离注释），补一个混合块重复 Spec 的回归用例并用 `typeCheckGoSource` 校验；备选是把文档收窄为「顶层多份生成声明会被去重；同一 type 块内的重复 Spec 不在支持范围」。前者与现有 `TestWriteOrReplaceStruct_DropAndDedupeGeneratedDecls` 的意图更一致。

### DOC-04：用户区并非「原样留在原位」，再生成会重排格式并插入空行

- 级别/类型：中；文档过度承诺（核心契约描述与实现不符）
- 文档证据：`docs/zcmodel.md:15` 称用户自定义代码「**原样留在原位**」，并称「表结构变化时 git diff 只呈现生成区那几处变更」；`docs/zcmodel.md:406` 称「排列结构原封不动」；`docs/zcmodel.md:413` 称注释「原位保留」。
- 实现证据：`build.go:825-829` 的 `takeLead` 会丢弃声明前的原始空白；`build.go:1074-1080` 用 `"\n\n"` 连接所有槽位，强制在每个顶层声明之间放一个空行；`build.go:935` 对文件尾做 `TrimSpace`；最后 `build.go:374-379` 对整个文件执行 `go/format.Source`，会统一缩进、对齐并排序 import。
- 测试证据：`build_test.go:1278-1314` 只断言声明顺序和方法体内容，`build_test.go:311-395` 只断言用户代码片段存在；没有断言用户区逐字节不变或空行保持。
- 实测：输入一个已通过 `gofmt` 的文件（`var a = 1` 与 `var b = 2` 相邻、两个函数相邻，确认 `gofmt` 不会插入空行），再生成后产物在每对相邻声明之间插入了一个空行；另一用例中未 gofmt 的用户代码被整体重排、import 被排序。即使用户区内容与顺序不变，格式/空行仍会出现在 diff 中。
- 影响：文档主打的「diff 只呈现生成区」在首次再生成时不成立，会给存量仓库带来额外的格式噪音；不会破坏代码语义。若用户误以为可以逐字节依赖用户区，可能据此设计代码审查流程而失望。
- 推荐方案：首选改文档——把「原样留在原位」限定为「声明的**内容、顺序与位置**保留；整个文件最终经 gofmt 规范化，声明间统一为一个空行，import 会被排序，因此首次再生成可能包含格式类 diff，之后稳定」。备选是改实现：让用户槽位携带原始声明间空白（不再统一 `"\n\n"`），减少格式噪音；但只要仍使用 `go/format.Source`，非 gofmt 存量文件的缩进/对齐/import 排序仍会变化，所以文档仍需限定。若实现保持不变，文档必须按首选方案改写。

## 4. 已核对且未发现偏差的关键行为

以下行为经源码、测试与实测核对，未发现偏差；修复时应避免改动：

1. **三方言映射表**：文档表格与 `columnTypeToGoType.go:15-127` 的键/值集合完全一致（脚本比对：MySQL 32/32、PostgreSQL 36/36、SQLite 30/30），并由 `columnTypeToGoType_test.go:67-147` 锁死。
2. **类型归一化**：大小写、括号及内容剔除、连续空格压缩、MySQL `unsigned`/`zerofill`、PostgreSQL 时区后缀、SQLite 引号剥离均与 `columnTypeToGoType.go:136-174` 及 `docs/zcmodel.md:337-345` 一致；`timestamp(6) with time zone` → `timestamptz` 有用例。
3. **错误表与错误文本**：`docs/zcmodel.md:96-122` 的错误行与 `generate.go:28-163`、`generate.go:169-233` 的实际返回一一对应；`%q` 引用约定由 `generate_test.go:844-848` 精确锁定。
4. **表名/文件名安全**：空名、`.`/`..`、路径分隔符与 Windows 非法字符、首字符 ASCII、结构体名导出性、Go 文件名规则（`_`/`.` 前缀、`_test`、GOOS/GOARCH 后缀）均与实现一致，`generate_test.go:365-575` 覆盖。
5. **tag 顺序与净化**：`json → 列 tag → primary_key → ddl → description` 的生成条件与顺序、`sanitizeTagValue` 的转义与反射还原均一致（`build.go:245-297`、`build_test.go:134-288`）。
6. **`ddl` 片段**：类型原样、`Nullable` 三态、`Default` 指针判定与空串补引号、EXTRA 白名单/固定顺序/精度后缀均一致（`build.go:117-184`、`build_test.go:1500-1587`）。
7. **索引块**：排序（PRIMARY → UNIQUE 名字典序 → 普通名字典序）、主键行隐藏物理名、三空格清单前缀、换行净化与零宽空格防弯引号均一致（`build.go:186-235`、`build_test.go:1593-1741`）；真实 MySQL 样板由 `integration_test.go:647-722` 锁死。
8. **骨架保持式合并的主干**：用户声明原位不搬动、生成声明原位替换、缺失声明配对插入、单独生成 Spec 去掉括号块、type 块内用户类型/游离注释保留均有测试（`build_test.go:1278-1500`、`build_test.go:1965-2005`）。
9. **import 补齐/移除**：缺包合并进首个 import 块、旧生成代码独占的 import 移除、`_`/`.`/`"C"`/版本后缀不误删、别名与默认导入共存均有实现与用例（`build.go:396-418`、`build.go:736-800`、`build.go:940-1004`、`build_test.go:439-575`、`build_test.go:984-1077`）；本次额外用 `go/types` 验证了 `"time"` 与 `mytime "time"` / `. "time"` 同文件共存可编译。
10. **包名推导**：新建文件取输出目录绝对路径末级目录名，相对路径 `.` 解析为实际目录名，根目录回退 `main`，存量文件尊重原 package（`build.go:382-394`、`build_test.go:1111-1159`）；根目录回退已实测。
11. **落盘三重防线**：存量文件解析失败不覆盖、`go/format.Source` 语法自校验、同目录临时文件 + rename、权限位保留（`build.go:336-380`、`build_test.go:420-438`、`generate_test.go:736-769`）。
12. **生成产物可直接编译**：用文档样板（含 `time.Time`、decimal、enum、索引、自增主键）与空列两种输入实测 `go build` 通过；`docs_examples_compile_test.go:141-167` 亦覆盖文档手工示例。
13. **zcdb 桥接示例与跨文档引用**：`docs_examples_compile_test.go:79-137` 编译级验证示例；`../zcdb/db_dao.go:85-103` 的五参数签名/空 tag 默认 `db`、`../zcdb/schema_inspector.go:20-46` 的字段类型（`Nullable bool`、`Default *string`）与文档一致；`../zcdb/docs/schema.md` 的索引特殊形态（`col(n)`、`#expr`、INCLUDE 不输出）与 zcmodel 文档引用一致。
14. **不写官方生成标记**：产物以 `package` 起始且不匹配 `// Code generated ... DO NOT EDIT.`，由 `generate_test.go:1261-1283` 锁死。
15. **注释净化**：表注释与索引文本换行→空格、`''`/``` `` ``` 间插零宽空格均与实现一致（`build.go:84-96`、`build_test.go:1688-1741`）。

补充观察（不计入问题）：整段 import 声明因不再被引用而整体移除时，其自身 Doc 注释也会消失。`docs/zcmodel.md:416` 明确「整个 import 声明一并移除」，将该注释视为声明的一部分，故不判为偏差；若后续想保护此类注释，可另行设计。

## 5. 修复执行记录

按 P1 → P2 顺序全部落地（本节行号均指修复后的当前工作区）：

1. **DOC-02（P1，实现 + 测试 + 文档）**
   - `build.go:717` 新增 `unwrapParen`，循环解包 `*ast.ParenExpr`；`receiverTypeName` 对 `(T)`、`((T))`、`*(T)` 均能还原出 `T`。
   - `build_test.go:718` 新增 `TestWriteOrReplaceStruct_RemoveParenthesizedValueReceiverToDO`（三种接收者形态，断言只剩一份 `ToDO` 并通过 `typeCheckGoSource` 编译级校验）。
   - `docs/zcmodel.md:411`、`docs/zcmodel.md:412` 补充「括号包裹的接收者」。
   - 可失败性：修复前运行该用例实际输出两份 `ToDO`（红），修复后绿。
2. **DOC-04（P1，文档 + 源码注释）**
   - `docs/zcmodel.md:15`、`docs/zcmodel.md:406`、`docs/zcmodel.md:412`、`docs/zcmodel.md:413`、`docs/zcmodel.md:414`、`docs/zcmodel.md:580`、`docs/zcmodel.md:582` 统一改为「内容/顺序/位置保留；整个产物经 gofmt 规范化（缩进、对齐、import 排序，槽位间一个空行），首次再生成可能带格式 diff」。
   - `build.go:470` 的 `writeOrReplaceStruct` 注释、`generate.go:25` 的 `Generate` 注释同步该限定。
3. **DOC-01（P2，文档 + 测试）**
   - `docs/zcmodel.md:135`、`docs/zcmodel.md:157`、`docs/zcmodel.md:593` 改为「空值不自动生成；显式 `JsonTagValue` 仍生效」。
   - `generate_test.go:117` 新增 `TestGenerate_ExplicitJsonTagWithEmptyCase` 锁定该契约。
4. **DOC-03（P2，实现 + 测试 + 文档）**
   - `build.go:693` 的 `renderTypeBlock` 增加 `alreadySeen`/`emitted` 去重；混合块按实际写入种类更新 `seenGen`/`carrierIdx`，全部为前序重复且无用户类型时整块移除。
   - `build_test.go:2030`、`build_test.go:2041`、`build_test.go:2052` 新增块内重复、跨槽位重复、全重复块移除三个子用例。
   - `docs/zcmodel.md:415` 明确「含同一 type 块内重复的生成 Spec」。
   - 可失败性：修复前两个重复场景均输出 2 份 `UserEntity`（红），修复后绿。

## 6. 实际验收结果

| 验收项 | 命令 | 结果 |
|---|---|---|
| 空白检查 | `git diff --check` | 通过（仅 LF→CRLF 提示，无空白错误） |
| 仓库级全量测试 | `go test ./... -count=1` | 通过：zcconfig / zcdb / zchttp / zcmodel / zcquit 全 ok |
| 静态检查 | `go vet ./...` | 通过（无输出） |
| race | `go test -race ./zcmodel -count=1` | ok |
| shuffle | `go test ./zcmodel -shuffle=on -count=5` | ok |
| 覆盖率 | `go test ./zcmodel -cover -count=1` | 98.5% |
| 集成（真实 DB） | `go test ./zcmodel -run 'TestInteg' -v -count=1` | 5 个用例全部 PASS，无 SKIP（MySQL/PostgreSQL/SQLite 实跑） |
| 定向回归（DOC-02/03） | 括号接收者与去重定向用例 | 修复前红、修复后绿；产物通过 `typeCheckGoSource` 编译级校验 |
| 文档示例 | `go test ./zcmodel -run 'TestDocExamples' -v -count=1` | 通过 |
| 格式 | `gofmt -l`（必要时剥离 `\r`） | build.go / generate.go / build_test.go 无输出；generate_test.go 为 CRLF 假告警，剥离后无差异 |
| 文档自检 | 本报告机械校验脚本 | 结构/引用/锚点 0 问题（第 5、6 节引用按当前工作区校验） |

结论：4 条问题全部修复并验收通过；未发现回归。
