# zcdb 文档与实现一致性审查

> 状态：审查完成；14 项问题已全部修复（2026-10-17），代码、文档、注释与回归测试已同步，验收通过。
>
> 审查基线：`8e98521c4b5bc32837cdb259aa3e84af0ddc2b4e`（HEAD）。修复前工作区包含上一轮问答产生的 3 个未提交修正（`docs/compile.md`、`docs/connection.md`、`grammar.go` 的 CompileRaw 占位符表述）。**以下详情中的行号与“实测”均指向修复前基线，仅用于追溯**；修复后的变更与位置见“修复执行记录”。
>
> 问题总数：14（高 1 / 中 3 / 低 10）；修复后状态：全部已修复。

## 审查范围与方法

### 文档 → 实现 → 测试映射

| 文档 | 对照实现 | 主要锁死测试 |
|---|---|---|
| [README.md](README.md) | `db_dao.go`、`pool.go`、`builder*.go`、`*_grammar.go`、`errors.go` | `docs_deviation_review_test.go`、`docs_examples_compile_test.go` |
| [connection.md](connection.md) | `pool.go`、`db_dao.go`、`slave_strategy.go`、`*_grammar.go` | `pool_unit_test.go`、`pool_sqlite_integration_test.go`、`db_dao_test.go` |
| [query-builder.md](query-builder.md) | `builder.go`、`builder_select.go`、`builder_where.go`、`builder_join.go`、`builder_group.go`、`builder_order.go` | `builder_unit_test.go`、`builder_*_unit_test.go`、`cross_dialect_integration_test.go` |
| [query-exec.md](query-exec.md) | `builder_query.go`、`builder_cursor.go`、`scanner.go` | `docs_deviation_review_test.go`、`scanner_test.go`、`builder_cursor_*` |
| [mutate.md](mutate.md) | `builder_exec.go`、`builder_compile.go`、`reflect.go`、`*_grammar.go` | `builder_exec_*_integration_test.go`、`builder_error_branches_unit_test.go` |
| [compile.md](compile.md) | `builder_compile.go`、`grammar.go`、`rawLiteral.go`、`sqlScan.go`、`expression.go` | `rawLiteral_test.go`、`builder_postgres_unit_test.go`、`cross_dialect_integration_test.go` |
| [schema.md](schema.md) | `schema_inspector.go`、`mysql_schema.go`、`postgres_schema.go`、`sqlite_schema.go` | `schema_inspector_test.go`、`schema_inspector_*_integration_test.go` |

### 方法与实测说明

1. 正向逐条读取 7 份文档，对每条行为/默认值/错误/顺序/边界定位实现分支与锁死测试；同时比对文档之间、文档与源码注释。
2. 对可疑结论使用 `go test -overlay=...` 把临时探针映射进 `zcdb` 包，在**独立进程**中执行（未写入仓库，探针文件已删除）。探针覆盖：Offset 隐式 LIMIT、Join 运算符校验、空 ON 保护绕过、嵌套 join ON 保护判定、MySQL Upsert 退化、WhereRaw 恒真、InsertGetId 切片、DeleteJoin 错误优先级、InsertUsing 静态列数、嵌入同深度冲突。
3. `go doc -all ./zcdb` 遍历导出 API，检查文档遗漏与错误清单；`git ls-files` 检查文档内测试文件引用是否存在。
4. 计入标准：可由文档声明与实现/实测直接对照、且会误导调用方或使契约不成立的问题。纯风格措辞、无行为影响的示例排版不计入。

## 问题汇总表

| 编号 | 级别 | 状态 | 一句话结论 |
|---|---|---|---|
| DOC-01 | 高 | 已修复 | 空 Raw ON 被计为“有效 JOIN 限定”，PG/SQLite 可绕过 Force 保护生成全表 UPDATE/DELETE |
| DOC-02 | 中 | 已修复 | `InsertGetId` 文档声明不支持切片，实现却执行非空切片并返回最后一行 ID |
| DOC-03 | 中 | 已修复 | `Join`/`CrossJoinOn` 不校验运算符，与 `WhereColumn`/`JoinBuilder.On` 行为割裂且可注入 |
| DOC-04 | 中 | 已修复 | MySQL Upsert 退化分支直接取 `uniqueBy[0]` 自赋值，该列未插入时生成非法 SQL |
| DOC-05 | 低 | 已修复 | `DeleteJoin` 无 JOIN 且无 WHERE 时先报 `ErrDeleteWithoutWhere`，与文档断言不符 |
| DOC-06 | 低 | 已修复 | 仅 `Offset` 无 `Limit` 时 MySQL/SQLite 隐式追加 LIMIT，文档未说明 |
| DOC-07 | 低 | 已修复 | 嵌套 join 组带 ON 但顶层无 ON 时不算有效限定，Update/DeleteJoin 被误拒 |
| DOC-08 | 低 | 已修复 | 非空 `WhereRaw("1 = 1")` 被当作依赖行数据，与“静态归约”规则及“只能用 Force”不符 |
| DOC-09 | 低 | 已修复 | 同深度嵌入字段同名列取声明顺序第一个，与文档声称的 encoding/json 语义不符 |
| DOC-10 | 低 | 已修复 | `compile.md` 链接指向不存在的 `sql_scan_engine_parity_integration_test.go` |
| DOC-11 | 低 | 已修复 | `InsertUsing` 静态列数校验还覆盖 `SelectRaw`/`SelectSub`，文档只写 `Select`/`AddSelect` |
| DOC-12 | 低 | 已修复 | `Or` 变体清单遗漏实际存在且示例已使用的 `OrWhereBetween` |
| DOC-13 | 低 | 已修复 | 10 处回归测试注释仍把已修复行为描述为“当前行为（BUG）” |
| DOC-14 | 低 | 已修复 | PostgreSQL/SQLite Grammar 头注释包含未实现的能力描述 |

## 逐条详情

### DOC-01：空 Raw ON 条件被计为有效限定，PG/SQLite 可无 Force 触发全表更新/删除

- 级别：高；类型：实现缺陷 / 文档错误（安全保护契约不成立）
- 文档证据：`docs/mutate.md:226` 声明“或存在带 ON 条件的 JOIN（无条件 JOIN 产生笛卡尔积，不视为限定）”；`docs/mutate.md:215-220` 的保护表把 Update/DeleteJoin 的无有效 WHERE/JOIN 统一映射到 `ErrUpdateWithoutWhere`/`ErrDeleteWithoutWhere`。
- 实现证据：`builder_exec.go:450-457` 的 `hasEffectiveJoin` 只检查顶层 `len(j.Conditions) > 0`；`builder_join.go:547-555` 的 `JoinBuilder.Raw` 无条件追加条件（即使 SQL 为空串）；`mysql_grammar.go:724-727` 对非 Cross JOIN 无条件输出 `" ON " + compileJoinConditions(...)`，空条件时得到悬挂的 `ON`；`postgres_grammar.go:486-495` 与 `sqlite_grammar.go:454-463` 把为空的 ON 片段直接并入 WHERE，最终完全没有限定。
- 测试证据：`builder_unit_test.go:1689-1704` 只锁死了“完全没有条件的 JOIN 不算限定”和“带正常 ON 的 JOIN 算限定”，未覆盖“条件列表非空但编译为空”的形态。
- 实测（overlay 独立进程，`JoinOn("orders", func(j){ j.Raw("") })`）：
  - MySQL：`hasEffectiveJoin=true`，`ToUpdate` 产物为 `UPDATE `users` INNER JOIN `orders` ON  SET `name` = ?`（语法错误）；
  - PostgreSQL：`hasEffectiveJoin=true`，`ToUpdate` 产物为 `UPDATE "users" SET "name" = $1 FROM "orders"`；`ToDeleteJoin` 产物为 `DELETE FROM "users" USING "orders"`；
  - SQLite：`hasEffectiveJoin=true`，`ToDeleteJoin` 产物为 `DELETE FROM "users" WHERE "id" IN (SELECT "users"."id" FROM "users" INNER JOIN "orders" ON )`（语法错误）。
- 影响：PostgreSQL 下用户未调用 `Force()`、只写了一个空 Raw ON 条件时，`UPDATE ... FROM`/`DELETE ... USING` 在关联表非空时会命中并修改/删除目标表全部行；MySQL/SQLite 则在运行期报语法错误。保护机制在安全关键路径上失效。
- 推荐方案：让 `hasEffectiveJoin` 基于“编译后实际产生条件”判定——至少跳过空/纯空白 Raw，并按与 `hasEffectiveWhere` 相同的静态归约规则处理值条件；同时在 `compileJoin` 中仅当编译出的 ON 片段非空时输出 ` ON `。补充回归测试：PG/SQLite 的空 Raw ON 必须返回 `ErrUpdateWithoutWhere`/`ErrDeleteWithoutWhere`，MySQL 不得生成悬挂 `ON`。

### DOC-02：InsertGetId 文档声明不支持切片，实现未强制

- 级别：中；类型：文档错误 / 实现契约缺失
- 文档证据：`docs/mutate.md:58` 明确“**仅支持单个结构体/结构体指针**，不支持切片”；`builder_exec.go:44` 的同名方法注释也如此声明。
- 实现证据：`builder_exec.go:50-68` 的 `InsertGetId` 直接调用 `ToInsert(data)`，未做类型断言；`builder_compile.go:105-131` 的 `ToInsert` 接受结构体切片，`reflect.go:173-249` 会为每一行生成 VALUES。
- 测试证据：`builder_exec_mysql_integration_test.go:1014-1030` 只覆盖空切片（返回 `ErrEmptyData`），没有任何“非空切片应被拒绝”的用例。
- 实测（overlay + 内存 SQLite）：对 `[]struct{Name string}{{"a"},{"b"}}` 调用 `InsertGetId`，返回 `id=2, err=nil`，表中实际写入 2 行。
- 影响：调用方按文档认为切片会在执行前被拒绝，实际却发生多行插入，并且 `LastInsertId` 的语义（返回最后一行 ID）完全未定义；在 MySQL 上不同版本/自增模式还可能返回首行或 0，导致难以排查的数据问题。
- 推荐方案：在 `InsertGetId` 开头用反射拒绝切片（返回 `ErrInvalidStruct`），与文档和注释一致；或若决定支持切片，必须修改文档并明确返回哪个 ID。首选前者，改动小且无兼容风险。

### DOC-03：Join / CrossJoinOn 不校验运算符，行为与其它公开条件 API 割裂

- 级别：中；类型：实现缺陷 / 安全（文档遗漏）
- 文档证据：`docs/query-builder.md:84` 声明基本比较“运算符经白名单校验，非法时编译阶段返回 `ErrInvalidOperator`”；`docs/query-builder.md:323-347` 的 JOIN 章节只说明“条件为列到列比较”，未提示运算符会原样拼接。
- 实现证据：`builder_join.go:15-28`（`Join`）、`builder_join.go:92-105`（`CrossJoinOn`）、`builder_join.go:526-539`（`JoinBuilder.CrossJoinOn`）均直接把 `op` 拼进 SQL，无 `validateOperator`；而 `builder_join.go:251-255` 的 `JoinBuilder.On` 和 `builder_where.go:331-344` 的 `WhereColumn` 会校验。
- 测试证据：现有测试中的 `Join` 只使用 `"="`，没有非法运算符用例；`builder_error_branches_unit_test.go` 覆盖的是 `Where`/`On` 的校验路径。
- 实测（overlay，MySQL 方言，`op = "= 1 OR 1=1 --"`）：
  - `Join` 产物：`SELECT * FROM `users` INNER JOIN `orders` ON `users`.`id` = 1 OR 1=1 -- `orders`.`user_id``，`err=nil`；
  - `CrossJoinOn` 产物同样注入成功，`err=nil`；
  - 相同非法运算符传给 `JoinBuilder.On` 时返回 `zcdb: invalid operator`。
- 影响：同一套公开 API 对运算符的信任边界不一致；若调用方把动态运算符（如前端排序/筛选参数映射）传入 `Join`/`CrossJoinOn`，会形成 SQL 注入点。文档对 `Where` 的白名单承诺容易让调用方误以为所有运算符入口都受保护。
- 推荐方案：在上述 4 个入口统一调用 `validateOperator(op)`，错误累积到 Builder/JoinBuilder 并由编译入口返回 `ErrInvalidOperator`；补充非法运算符的单元测试。若坚持保留逃生口，必须在文档中显式标注“运算符原样拼接、调用方自负注入责任”，但首选校验。

### DOC-04：MySQL Upsert 空 updateColumns 退化分支可能引用未插入列

- 级别：中；类型：实现缺陷 / 文档错误
- 文档证据：`docs/compile.md:108` 声明“`updateColumns` 为空时默认更新所有插入列（排除 uniqueBy 列；全部插入列均为 uniqueBy 时退化为「冲突时不更新」：PG/SQLite 编译为 `DO NOTHING`，MySQL 编译为自赋值 no-op）”；`docs/mutate.md:85` 对 `updateColumns` 的描述相同。
- 实现证据：`builder_compile.go:222-243` 在 `updateColumns` 为空时先用“插入列 − uniqueBy”推导，结果为空则交给方言处理；`mysql_grammar.go:338-346` 在 `updateColumns` 为空时直接取 `uniqueBy[:1]` 作为自赋值列，未校验该列是否在插入列集合中。
- 测试证据：`docs_deviation_review_test.go:252-263` 只覆盖 `uniqueBy={"email"}` 且插入列同为 `email` 的情形，未覆盖 `uniqueBy` 含额外列的顺序。
- 实测（overlay，MySQL）：插入数据仅含 `email`，`uniqueBy={"tenant","email"}`、`updateColumns=nil`，产物为 `INSERT INTO `users` (`email`) VALUES (?) ON DUPLICATE KEY UPDATE `tenant` = VALUES(`tenant`)`，`err=nil`；`tenant` 未出现在插入列中，执行时数据库会报未知列/列不存在。
- 影响：文档承诺的“自赋值 no-op”在合法入参下变成非法 SQL；若 `uniqueBy` 顺序恰好把额外列放首位就会触发，且错误发生在执行期而非编译期。
- 推荐方案：退化分支优先使用 `columns[0]`（必定是插入列）做自赋值；或从 `uniqueBy` 中挑选第一个同时存在于 `columns` 的列，找不到则使用 `columns[0]`。补充“uniqueBy 含额外列/顺序不同”的单测。文档无需改动。

### DOC-05：DeleteJoin 无 JOIN 时的错误优先级与文档不符

- 级别：低；类型：文档错误（错误契约不精确）
- 文档证据：`docs/mutate.md:194` 明确“没有任何 JOIN 时调用 `DeleteJoin` 返回 `ErrDeleteJoinNoJoin`”；`docs/README.md:169` 的错误表也把该错误归因为“DeleteJoin 没有任何 JOIN”。
- 实现证据：`builder_exec.go:313-326` 先调用 `validateWriteBuilder`，再执行无 WHERE 保护，最后才在 `ToDeleteJoin` 中检查 JOIN；`builder_compile.go:529-531` 的前置检查在保护之后。
- 测试证据：`docs_deviation_review_test.go:595-598` 只直接调用 `ToDeleteJoin()`，没有覆盖执行方法 `DeleteJoin` 的错误优先级。
- 实测（overlay，MySQL 方言）：
  - 无 JOIN、无 WHERE：`err=zcdb: DELETE without WHERE is not allowed...`，`errors.Is(err, ErrDeleteJoinNoJoin)=false`；
  - 无 JOIN、有 WHERE：`err=zcdb: DeleteJoin requires at least one JOIN`，才命中 `ErrDeleteJoinNoJoin`。
- 影响：调用方按文档用 `errors.Is(err, ErrDeleteJoinNoJoin)` 处理“忘写 JOIN”时，在同样忘写 WHERE 的情况下会进入错误分支，诊断信息也会误导（提示补 WHERE 而不是补 JOIN）。
- 推荐方案：在 `DeleteJoin` 保护检查之前先判断 `len(b.joins)==0` 并返回 `ErrDeleteJoinNoJoin`（更具体、与文档一致）；或在文档中明确“无 JOIN 且无有效 WHERE 时优先返回 `ErrDeleteWithoutWhere`”。首选前者并补执行路径测试。

### DOC-06：仅 Offset 无 Limit 时隐式 LIMIT 未文档化

- 级别：低；类型：关键行为遗漏
- 文档证据：`docs/query-builder.md:491-499` 只说明 `n <= 0` 不输出对应子句，示例为 `Limit(10).Offset(20)`；未提及只设 `Offset` 时的产物。
- 实现证据：`mysql_grammar.go:186-194` 在 `offset>0 && limit==0` 时输出 `LIMIT 18446744073709551615`；`sqlite_grammar.go:196-204` 输出 `LIMIT -1`；`postgres_grammar.go:257-261` 直接输出 `OFFSET`。
- 测试证据：`builder_unit_test.go:1525-1535` 已锁死三方言产物，说明这是既定行为，只是文档未描述。
- 实测（overlay）：
  - MySQL：`SELECT * FROM `users` LIMIT 18446744073709551615 OFFSET 20`；
  - PostgreSQL：`SELECT * FROM "users" OFFSET 20`；
  - SQLite：`SELECT * FROM "users" LIMIT -1 OFFSET 20`。
- 影响：调用方做 SQL 断言、审计或迁移到其它 ORM 时会因多出的 LIMIT 产生困惑；不影响执行语义。
- 推荐方案：在 `query-builder.md` 的 Limit/Offset 小节补充上述隐式 LIMIT 规则，并同步 `builder_order.go:55-66` 的方法注释；保持现有行为不变。

### DOC-07：嵌套 join 组带 ON 但不被计为有效限定

- 级别：低；类型：实现缺陷（与文档判定规则不一致）
- 文档证据：`docs/mutate.md:226` 规定“存在带 ON 条件的 JOIN”即视为有效限定，且该规则适用于 `DeleteJoin`/Update 系列。
- 实现证据：`builder_exec.go:450-457` 的 `hasEffectiveJoin` 只遍历顶层 `b.joins` 的 `Conditions`；`builder_join.go:505-518` 的 `JoinBuilder.JoinOn` 把带 ON 的嵌套 join 放入 `Joins`，不计入顶层 `Conditions`。
- 测试证据：`builder_unit_test.go:1689-1704` 未覆盖“仅嵌套组带 ON”的形态。
- 实测（overlay，MySQL）：构造 `JoinOn("orders", func(j){ j.JoinOn("items", func(q){ q.On("items.order_id","=","orders.id") }) })` 后，`hasEffectiveWhere=false`、`hasEffectiveJoin=false`；`Update` 返回 `ErrUpdateWithoutWhere`，`DeleteJoin` 返回 `ErrDeleteWithoutWhere`。
- 影响：该 Builder 形态确实通过内层 ON 收窄了 `orders × items` 的笛卡尔积，但调用方被迫额外调用 `Force()` 或补一个外层条件；文档的判定规则与实际不一致。
- 推荐方案：`hasEffectiveJoin` 递归检查 `JoinClause.Joins`，只要任意层级存在编译后非空的 ON 条件即视为有效；与 DOC-01 的空条件判定一起修复，并补嵌套组用例。

### DOC-08：非空 WhereRaw 恒真条件绕过“静态归约”描述

- 级别：低；类型：文档错误（与实现判定规则不一致）
- 文档证据：`docs/mutate.md:225` 声明有效限定需“按 SQL 优先级把条件归约为恒真/恒假/依赖行数据，只有‘依赖行数据’或‘恒假’计为有效”，并强调空 `WhereRaw` 会被跳过。
- 实现证据：`builder_exec.go:403-406` 对非空 Raw 直接返回 `truthUnknown`，不做内容分析；`builder.go:130` 的 `Force` 注释明确把 `WhereRaw("1=1")` 列为“逃生口”。
- 测试证据：`builder_exec_mysql_integration_test.go:1337`、`builder_exec_postgres_integration_test.go:1243`、`builder_exec_sqlite_integration_test.go:1388` 都以 `WhereRaw("1 = 1").Delete(ctx)` 成功执行，锁死了该逃生口。
- 实测（overlay）：`WhereRaw("1=1")` 与 `WhereRaw("1 = 1")` 的 `hasEffectiveWhere` 均为 `true`；纯空白与空串为 `false`。
- 影响：调用方可能以为只有 `Force()` 能放行全表操作，实际任意非空 Raw（含恒真条件）都能绕过；文档的“静态归约”承诺只适用于结构化条件，未说明 Raw 是例外。
- 推荐方案：在文档中明确“非空 `WhereRaw` 一律视为依赖行数据（有意的逃生口），不做内容归约”，并给出手写 `1=1` 的示例与风险提示；若希望严格化，可对纯空白/简单 `1=1`/`1 = 1` 做窄匹配，但这会破坏现有逃生口测试，需先确认取舍。

### DOC-09：同深度嵌入字段冲突的行为与 encoding/json 声明不符

- 级别：低；类型：文档过度承诺 / 语义不规则
- 文档证据：`docs/README.md:9` 声明嵌入字段展开“外层优先（同名列浅层遮蔽深层，对齐 encoding/json 语义）”；`docs/query-exec.md:199` 进一步写“外层字段遮蔽嵌入层字段（浅层优先，与 encoding/json 语义一致），与字段声明顺序无关”。
- 实现证据：`reflect.go:63-141` 的 BFS 展开用 `seen` 保留第一个同名列；同深度两个匿名嵌入结构体同名列时，先声明者胜出（`reflect.go:129-137`），而同深度冲突在 encoding/json 中是**双方都被丢弃**。
- 测试证据：`reflect_test.go:230-271` 只覆盖“外层 vs 内层”的遮蔽，且已锁死“外层在前/嵌入在前都取外层”，未覆盖同深度冲突。
- 实测（overlay）：`type C struct{ A; B }`，A、B 均含 `db:"name"`，`parseStruct(C{})` 只得到 `name → [0 0]`（A 的字段）；同样输入用 `encoding/json` 反序列化后 A.Name、B.Name 均为空串。
- 影响：用户按文档认为映射与 encoding/json 完全一致时，同深度同名列会静默选中先声明的字段，可能把值写入错误的列；文档中“与字段声明顺序无关”的表述在该场景下也不成立。
- 推荐方案：将文档收窄为“外层优先；内层同名列被外层遮蔽；同深度冲突取声明顺序第一个（与 encoding/json 的冲突丢弃语义不同）”，并补一条同深度冲突测试锁死；若要完全对齐 encoding/json，需在 `collectStructFields` 中实现同深度冲突消除。

### DOC-10：compile.md 引用不存在的测试文件

- 级别：低；类型：文档错误（失效引用）
- 文档证据：`docs/compile.md:253` 链接到 `../sql_scan_engine_parity_integration_test.go`，并称由该文件锁死扫描器与引擎的一致性。
- 实现证据：仓库中不存在该文件；实际实现位于 `cross_dialect_integration_test.go:1963-1966` 的 `crossDialectTestScanEngineParity`，由 `cross_dialect_mysql_integration_test.go:46`、`cross_dialect_postgres_integration_test.go:45`、`cross_dialect_sqlite_integration_test.go:46` 三方言入口调用。
- 实测（文件系统/版本库检查）：`ls zcdb/*parity*` 无结果，`git ls-files zcdb | grep -i parity` 无结果。
- 影响：读者点击链接得到 404，无法定位锁死用例；文档的“有测试锁死”声明失去可追溯性。
- 推荐方案：把链接改为 `../cross_dialect_integration_test.go`（可附文字说明“三方言入口见 cross_dialect_{mysql,postgres,sqlite}_integration_test.go”）。

### DOC-11：InsertUsing 静态列数校验范围文档不完整

- 级别：低；类型：文档错误（遗漏实现分支）
- 文档证据：`docs/mutate.md:117-120` 声明“子查询通过 `Select`/`AddSelect` 显式指定列且不含通配符（`*`）时”才做编译期列数校验，无法静态判定的场景（如 `SELECT *` 或未显式指定列）交由数据库校验。
- 实现证据：`builder_compile.go:304-306` 调用 `countSubSelectColumns`；`builder_compile.go:366-379` 统计 `sub.columns`（含 `SelectRaw`）与 `sub.selectSubs`（`SelectSub`），只有列值为空（默认 `*`）或任一列值含 `*` 时才放弃校验。
- 测试证据：现有列数校验测试（`docs_deviation_review_test.go:275-287`）只使用 `Select`。
- 实测（overlay，MySQL）：
  - 目标列 2 个、子查询 `SelectRaw("name")`：返回 `ErrInsertUsingColumnMismatch`（“insert columns=2, subquery select columns=1”）；
  - 目标列 2 个、子查询 `SelectSub(...)`：同样返回该错误。
- 影响：按文档认为 `SelectRaw`/`SelectSub` 不做编译期校验的调用方，会收到意外的 `ErrInsertUsingColumnMismatch`；错误本身是合理的，但文档范围描述不准确。
- 推荐方案：把文档改为“通过 `Select`/`AddSelect`/`SelectRaw`/`SelectSub` 指定且不含通配符时均会静态计数”，并说明 `*` 的判定是列值字符串包含 `*`；或把 `countSubSelectColumns` 限制为只统计 `Select`/`AddSelect` 以匹配文档。首选改文档（现有实现更安全）。

### DOC-12：Or 变体清单遗漏 OrWhereBetween

- 级别：低；类型：文档内部矛盾
- 文档证据：`docs/query-builder.md:173` 声称“以上每个方法均有对应的 Or 前缀变体”，但列出的只有 `OrWhereNotBetween`、`OrWhereBetweenColumns`、`OrWhereNotBetweenColumns`、`OrWhereValueBetween`，漏掉 `OrWhereBetween`；而同一节 `docs/query-builder.md:157-160` 的示例正在使用 `OrWhereBetween`。
- 实现证据：`builder_where.go:193-208` 定义了 `OrWhereBetween`，`go doc -all ./zcdb` 可见其公开签名。
- 测试证据：`docs_deviation_review_test.go:381-390` 覆盖了非 Or 形态，未直接断言该清单。
- 实测（go doc）：`func (b *Builder) OrWhereBetween(column string, min, max any) *Builder` 存在且可用。
- 影响：仅文档完整性问题，调用方按清单查找可能误以为缺少 Or 变体。
- 推荐方案：把 `OrWhereBetween` 加入清单，并核对 `OrWhereValueBetween` 等方法命名的一致性。

### DOC-13：回归测试注释仍把已修复行为描述为“当前行为（BUG）”

- 级别：低；类型：注释与实现不一致
- 文档证据：用户文档已经描述修复后的行为，例如 `docs/compile.md:42-50`（ToCount 包裹）、`docs/query-builder.md:491-499`（Offset 隐式 LIMIT）、`docs/query-builder.md:557-570`（Clone 深拷贝）、`docs/query-exec.md:236`（数值文本按位宽解析）。按项目“四者一致性”约定，测试注释也应同步。
- 实现证据：以下注释仍称“当前行为（BUG）”，但其断言已经锁定修复后的正确行为：
  - `builder_unit_test.go:1427-1428`（Having Expression 已内嵌）
  - `builder_unit_test.go:1455`（Raw 绑定中的 Expression 已内嵌）
  - `builder_unit_test.go:1499`（Distinct + Count 已包裹子查询）
  - `builder_unit_test.go:1526`（Offset 已补隐式 LIMIT）
  - `builder_unit_test.go:1551-1552`（Clone 已深拷贝 havings Bindings）
  - `builder_unit_test.go:1568-1569`（nil 指针已返回 ErrInvalidStruct）
  - `builder_unit_test.go:1603-1604`（数值→string 已格式化为数字）
  - `builder_unit_test.go:1617`（[]byte→非字节切片已走 JSON 反序列化）
  - `builder_group_sqlite_integration_test.go:134-136`（GROUP BY Count 已包裹计数，断言为 4）
  - `reflect_test.go:193-194`（嵌入指针结构体已展开，断言 3 个字段）
- 测试证据：上述用例全部通过，且断言的是修复后的预期值；注释与断言相互矛盾。
- 实测（定向运行）：`go test ./zcdb -run 'TestBug_|TestSQLiteInteg_CountWithGroupBy|TestParseStruct_EmbeddedPtrStruct' -count=1` 全绿，进一步证明“BUG”描述已过时。
- 影响：后续维护者阅读测试注释会误判当前行为，甚至在“修复”已修复的问题时引入回退风险。
- 推荐方案：把“当前行为（BUG）：……”改写为“回归背景：修复前……；本用例锁死修复后行为”，并保留触发条件与断言语义。

### DOC-14：PostgreSQL/SQLite Grammar 头注释包含未实现的能力描述

- 级别：低；类型：注释与实现不一致
- 文档证据：`postgres_grammar.go:13` 在“与 MySQL 的主要差异”中写“INSERT 支持 RETURNING 子句”；`sqlite_grammar.go:16` 写“DELETE 不生成 ORDER BY 和 LIMIT（除非编译时启用）”。
- 实现证据：`postgres_grammar.go:305-341` 的 `CompileInsert` 不生成 `RETURNING`，`Grammar` 接口也没有相关入口；`builder_exec.go:57-60` 反而明确提示 PG 的 `InsertGetId` 不支持，需要改用“带 `RETURNING` 的原始 SQL”。`sqlite_grammar.go:475-488` 的 `CompileDelete` 永远不生成 ORDER BY/LIMIT，仓库中不存在“编译时启用”的开关或选项。
- 测试证据：无对应能力测试；`grep RETURNING zcdb` 只命中错误提示、用户文档与这行注释。
- 实测（静态核对）：`go doc -all ./zcdb` 未暴露任何 RETURNING 相关 API；SQLite 选项结构体/构建参数中无 ORDER BY/LIMIT 开关。
- 影响：维护者可能误以为 Grammar 已支持 RETURNING 或 SQLite DELETE 排序/限量能力，从而设计出不存在的调用路径。
- 推荐方案：删除或改写这两行注释——PG 应写成“数据库支持 RETURNING，但本 Grammar 暂不生成该子句”；SQLite 应写成“DELETE 不生成 ORDER BY/LIMIT”。

## 已核对且未发现偏差的关键行为

以下内容逐条对照源码与测试后未发现偏差，修复时请勿误改：

- 方言差异总览（标识符包裹、占位符、共享锁、UNION+锁、RIGHT JOIN、CROSS JOIN ON、UNION 括号、Truncate、随机排序）与三方言 Grammar 一致，且有 `docs_deviation_review_test.go:76-153` 锁死。
- 连接池默认值（50/50/600/5s）、ConnectTimeout<=0 取默认、Close 幂等、Close 后 AddSlave 拒绝、从库失败整体回滚，与 `pool.go:52-225` 一致。
- 读写分离路由、`Primary()`/带锁查询强制主库、事务 context 绑定 Pool、跨 Pool 返回 `ErrTxOwnerMismatch`、嵌套事务复用，与 `db_dao.go:29-38`、`db_dao.go:133-268` 一致。
- 注释规范化（`*`/`/`/`\`/控制字符替换、非法 UTF-8、255 rune 截断）与 Comment/Clone 生命周期，与 `builder.go:56-76`、`builder.go:169-276` 一致。
- First/Find/Value 无结果语义、First/Value 的 LIMIT 1、Pluck 三模式、Paginate 先 COUNT 后数据、CursorBy 每批 chunkSize+1，与 `builder_query.go`、`builder_cursor.go` 一致。
- 聚合空集语义（Max/Min → `(0, sql.ErrNoRows)`，Sum/Avg → `(0, nil)`）已由 `docs_deviation_review_test.go:745-765` 实测锁死。
- ScanStruct 的 NULL/零值、数值位宽溢出、时间→RFC3339、嵌入展开（外层优先）与错误哨兵，与 `scanner.go` 一致。
- CompileRaw 的方言占位符形态（MySQL/SQLite `?`、PostgreSQL `$N`）、代码区扫描、`??` 语义、PG 编号契约与内联字面量规则，与 `grammar.go:105-206`、`rawLiteral.go`、`sqlScan.go` 一致；上一轮修正后的 compile.md 示例现已正确。
- ToCount/ToExists/ToAggregate 的包裹规则、注释追加位置与 15 个编译入口清单，与 `builder_compile.go` 一致。
- Schema 三方言的表/列/索引 SQL、类型与 Default/Extra 呈现、SQLite 主键合成与 origin 规则，与 `*_schema.go` 一致。

## 修复执行记录

按 P0 → P2 顺序完成，代码、源码注释、用户文档与回归测试同步修改。

### P0

- **DOC-01**：`builder_exec.go` 的 `hasEffectiveJoin` 改为递归判定 `hasEffectiveJoinClauses`/`hasEffectiveJoinConditions`：空/纯空白 Raw、空列表 NOT IN（`1 = 1`）不计入，任意层级嵌套 join 组的有效 ON 计入；三方言 `compileJoin` 仅在编译出的 ON 片段非空时输出 `ON`，`compileJoinConditions` 跳过空白条件。新增 `TestBug_HasEffectiveJoin`、`TestBug_EmptyJoinOnNoDanglingOn`。变异验证：临时回退 `hasEffectiveJoin` 后 `TestBug_HasEffectiveJoin` 的 4 个子用例失败。

### P1

- **DOC-02**：`builder_exec.go` 的 `InsertGetId` 对非空切片返回 `ErrInvalidStruct`；空切片仍交给 `ToInsert` 返回 `ErrEmptyData`，保持既有契约。新增 `TestBug_InsertGetIdRejectsSlice`，既有三方言空切片用例保持通过。
- **DOC-03**：`builder_join.go` 的 `Join`/`LeftJoin`/`RightJoin`/`CrossJoinOn` 与 `JoinBuilder.CrossJoinOn` 统一调用 `validateOperator`，非法时返回 `ErrInvalidOperator`。新增 `TestBug_OperatorInjection_JoinShorthand`（覆盖 5 个入口）。
- **DOC-04**：`mysql_grammar.go` 的退化自赋值改为“优先取同时出现在插入列中的 uniqueBy 列，否则回退插入首列”。新增 `TestMySQLCompile_UpsertFallbackUsesInsertedColumn`。

### P2

- **DOC-05**：`builder_exec.go` 的 `DeleteJoin` 在保护检查前先判断 `len(b.joins)==0` 并返回 `ErrDeleteJoinNoJoin`；同步更新 MySQL/PostgreSQL/SQLite 的 `NewApi_DeleteJoin` 与 `cross_dialect` 的 `ErrorsAndGuards` 期望。新增 `TestBug_DeleteJoinNoJoinPrecedence`。
- **DOC-06**：`docs/query-builder.md` 补充仅 `Offset` 时 MySQL/SQLite 的隐式 LIMIT 规则；`builder_order.go` 的 `Offset` 注释同步。
- **DOC-07**：随 DOC-01 的递归判定一并修复；`TestBug_HasEffectiveJoin` 新增“仅嵌套 join 组带 ON 计入”子用例。
- **DOC-08**：`docs/mutate.md` 明确“非空 `WhereRaw` 一律视为依赖行数据（刻意逃生口）”，并说明空 Raw/空 NOT IN/嵌套 ON 的判定；保留既有逃生口行为与测试。
- **DOC-09**：`docs/README.md`、`docs/query-exec.md`、`reflect.go`、`reflect_test.go` 的措辞收窄为“外层优先；同深度冲突取声明顺序靠前者，与 encoding/json 的冲突丢弃语义不同”；新增 `TestParseStruct_EmbeddedSameDepthFirstDeclared`。
- **DOC-10**：`docs/compile.md` 的失效链接改为指向 `cross_dialect_integration_test.go`（`crossDialectTestScanEngineParity`），并注明三方言入口文件。
- **DOC-11**：`docs/mutate.md` 的列数校验范围补充 `SelectRaw`/`SelectSub` 与通配符判定规则。
- **DOC-12**：`docs/query-builder.md` 的 Or 变体清单补上 `OrWhereBetween`。
- **DOC-13**：10 处回归测试注释由“当前行为（BUG）”改写为“回归背景：修复前……；本用例锁死……”，涉及 `builder_unit_test.go`、`builder_group_sqlite_integration_test.go`、`reflect_test.go`。
- **DOC-14**：`postgres_grammar.go` 改为“数据库支持 RETURNING，但本 Grammar 暂不生成”；`sqlite_grammar.go` 删除不存在的“编译时启用”描述。

## 实际验收结果

| 验收项 | 执行方式 | 结果 |
|---|---|---|
| 补丁格式 | `git diff --check` | 通过（仅 Git 的 LF→CRLF 提示，无空白错误） |
| 静态检查 | `go vet ./...` | 通过 |
| 全模块测试 | `go test ./... -count=1` | zcconfig、zcdb、zcmodel、zcquit 通过；zchttp 失败于 `TestGenerateOpenAPI_JSONNamedEmbedRequired`。该失败位于本审查未修改的 zchttp 文件，工作区存在其它并发改动，按技能约定如实记录、不计入本次验收失败 |
| 竞态检测 | `go test -race ./zcdb -count=1` | 通过（93.6s，无 DATA RACE） |
| 随机顺序回归 | `go test ./zcdb -shuffle=on -count=5` | 通过（331.0s） |
| 覆盖率 | `go test ./zcdb -cover -count=1` | 98.7% statements |
| 格式检查 | 对全部修改的 `.go` 文件执行 gofmt 并剥离 CRLF 后比较 | 通过 |
| 集成测试执行 | 定向 `-v` 运行三方言 `NewApi_DeleteJoin`、`InsertGetIdEmptyData` | 全部 PASS，无 SKIP（本机 MySQL/PostgreSQL 容器在线） |
| 回归有效性 | 变异回退 DOC-01 的 `hasEffectiveJoin` 后运行 `TestBug_HasEffectiveJoin` | 4 个子用例按预期失败，确认用例可失败 |
| 报告机械校验 | 编号/字段/引用/锚点脚本 | 14 条连续、7/7 文档覆盖、引用全部有效（修复记录新增引用随最终校验复核） |

结论：14 项问题全部修复；实现、源码注释、用户文档与回归测试一致；zcdb 全量测试、竞态与随机顺序回归通过。`zchttp` 的失败与本次改动无关（该模块存在本审查未修改的并发工作区改动），已按技能要求单独记录。
