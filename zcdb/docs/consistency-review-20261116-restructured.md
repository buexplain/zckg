# zcdb 文档-实现一致性审查报告（分层重构后）

- 审查对象：`zcdb/docs/` 下 28 份用户文档（README、getting-started、concepts 6、reference 14、how-to 5、migration 1）
- 基线：**工作区**（HEAD `26a978c` + 未提交改动；新分层文档与 `Pluck/PluckMap/KeyBy` 拆分均在未跟踪/未提交状态）
- 行号来源：当前工作区（新文档为未跟踪文件，不能用 `git show` 读取）
- 阶段：**仅审查**。本轮只产出报告，未修改任何文档/源码/测试；修复需另行授权
- 审查方式：8 组并行逐声明比对 + 探针实测；探针均通过 `go test -overlay` 映射到不存在的 `zcdb/zz_*_test.go`，文件放在系统临时目录并在审查后删除
- 重跑审查时请排除本文件自身：`--docs-dir zcdb/docs` 会自动排除 `consistency-review*`

## 审查范围与方法

### 文档清单与映射

| 文档 | 对照实现 | 锁死该行为的测试 |
|---|---|---|
| `zcdb/docs/README.md` | `builder.go`、`builder_compile.go`、`pool.go`、`db_dao.go`、`errors.go` | `docs_structure_test.go`、`docs_examples_fence_test.go` |
| `zcdb/docs/getting-started.md` | `pool.go`、`db_dao.go`、`builder_query.go`、`builder_exec.go`、`builder_cursor.go` | `docs_deviation_review_test.go`、`docs_examples_compile_test.go` |
| `zcdb/docs/concepts/architecture.md` | 全模块（分层与调用链） | 无直接锁死；引用各层单测 |
| `zcdb/docs/concepts/builder-state.md` | `builder.go`、`builder_compile.go`、`builder_query.go`、`builder_cursor.go` | `builder_unit_test.go`、`docs_deviation_review_test.go` |
| `zcdb/docs/concepts/compile-pipeline.md` | `builder_compile.go`、三方言 grammar、`postgres_grammar.go` | `builder_compile_*_unit_test.go`、`builder_unit_test.go` |
| `zcdb/docs/concepts/mapping.md` | `reflect.go`、`scanner.go` | `reflect_test.go`、`scanner_test.go` |
| `zcdb/docs/concepts/execution-routing.md` | `pool.go`、`db_dao.go`、`slave_strategy.go` | `pool_unit_test.go`、`db_dao_test.go`、`builder_sqlite_integration_test.go` |
| `zcdb/docs/concepts/safety.md` | `builder_exec.go`、`builder_compile.go`、`expression.go` | `builder_unit_test.go`、`cross_dialect_integration_test.go`、`docs_deviation_review_test.go` |
| `zcdb/docs/reference/builder-core.md` | `builder.go`、`builder_select.go`、`db_dao.go` | `builder_unit_test.go` |
| `zcdb/docs/reference/builder-select.md` | `builder_select.go`、三方言 grammar | `builder_select_mysql_unit_test.go` |
| `zcdb/docs/reference/builder-where.md` | `builder_where.go`、`builder_compile.go`、三方言 grammar、`sqlScan.go` | `builder_where_*`、`docs_deviation_review_test.go` |
| `zcdb/docs/reference/builder-join.md` | `builder_join.go`、`builder_compile.go`、`builder_exec.go`、三方言 grammar | `builder_join_*`、`cross_dialect_*` |
| `zcdb/docs/reference/builder-order-group.md` | `builder_order.go`、`builder_group.go`、`builder_compile.go`、三方言 grammar | `builder_order_*`、`builder_group_*`、`cross_dialect_*` |
| `zcdb/docs/reference/builder-query.md` | `builder_query.go`、`builder_compile.go` | `builder_query_*`、`docs_deviation_review_test.go` |
| `zcdb/docs/reference/builder-cursor.md` | `builder_cursor.go`、`scanner.go` | `builder_cursor_*` |
| `zcdb/docs/reference/builder-exec.md` | `builder_exec.go`、`builder_compile.go`、`reflect.go`、三方言 grammar | `builder_exec_*`、`builder_error_branches_unit_test.go` |
| `zcdb/docs/reference/builder-compile.md` | `builder_compile.go`、`grammar.go`、`rawLiteral.go`、`expression.go`、`sqlScan.go` | `builder_compile_*`、`rawLiteral_test.go`、`sqlScan_test.go` |
| `zcdb/docs/reference/scanner.md` | `scanner.go`、`reflect.go` | `scanner_test.go`、`scanner_*` |
| `zcdb/docs/reference/schema.md` | `schema_inspector.go`、`{mysql,postgres,sqlite}_schema.go` | `schema_inspector_*` |
| `zcdb/docs/reference/terminal-methods.md` | `builder_query.go`、`builder_cursor.go` | 各终端方法测试（交叉引用） |
| `zcdb/docs/reference/dialect-matrix.md` | 三方言 grammar、`db_dao.go` | `cross_dialect_*`、`docs_deviation_review_test.go` |
| `zcdb/docs/reference/errors.md` | `errors.go`、各产生点 | `docs_deviation_review_test.go`（错误表集合） |
| `zcdb/docs/how-to/pagination.md` | `builder_query.go`、`builder_cursor.go`、`builder_order.go` | `builder_query_*`、`builder_cursor_*` |
| `zcdb/docs/how-to/transactions.md` | `db_dao.go`、`pool.go` | `builder_sqlite_integration_test.go`、`fault_injection_test.go` |
| `zcdb/docs/how-to/dynamic-queries.md` | `builder_where.go`、`builder_exec.go` | `builder_unit_test.go`、`cross_dialect_integration_test.go` |
| `zcdb/docs/how-to/upsert.md` | `builder_exec.go`、`builder_compile.go`、三方言 grammar | `builder_exec_*`、`docs_deviation_review_test.go` |
| `zcdb/docs/how-to/schema-inspection.md` | `schema_inspector.go`、`*_schema.go` | `schema_inspector_*` |
| `zcdb/docs/migration/breaking-changes.md` | `db_dao.go`、`errors.go`、`builder_query.go`、`builder_compile.go` | 无（版本事实用 git tag/log 只读核对） |

范围外：仓库根 `README.md`、两份历史审查报告（`consistency-review.md`、`consistency-review-20261116.md`）。

### 方法

1. **正向逐声明比对**：每份文档的每条可验证声明（行为、默认值、顺序、错误、边界、示例 SQL/args）定位到源码分支与锁死测试。
2. **运行时实测**：内存 SQLite（modernc）为主；涉及引擎语义的条目使用本机 MySQL 8.4 / PostgreSQL 15 容器，表名统一 `audit_*` 且用后清理。探针经 `go test -overlay` 注入，不落仓库。
3. **反向检查**：导出 API 与运行时路径反查文档遗漏；`TestDocsCoverExportedAPI`（156 个导出符号）与链接/旧页门禁均在本轮重跑通过。
4. **示例**：`doc_go_examples.py --docs-dir zcdb/docs --strict`（211 个 Go 块，0 error）与 `TestDocsGoBlockSyntax` 双跑；额外把 getting-started 完整程序放入临时 module 实跑。
5. **行号**：全部基于当前工作区；修复阶段若迁移文档，需要重新定位。

### 实测过的行为（输入 → 观察结果）

- `WhereExists(func(q){ q.Table("t").WhereIn("id", []any{}) })` + `Update` → `ErrUpdateWithoutWhere`（内层恒假被当作恒真）。
- `CrossJoin(...).Force().Update(...)` → 实际执行 `affected=2`（Force 跳过了「无条件 JOIN 不计入限定」）。
- `WhereNested(func(q){ q.JoinOn(...) })` 作为唯一限定 → 编译产物无任何 ON，`hasEffectiveJoin()==true`，`Update` 实际执行 `affected=2`（MySQL 真实库）。
- `SelectSub(...)` + `Pluck/PluckMap/KeyBy` → `pluck scan row failed: sql: expected N destination arguments in Scan, not M`。
- 只有 UNION、没有主表的 Builder → `ToCount` 产物 `... FROM ((SELECT c FROM ``) UNION ...)`，执行 `no such table:`；同一 Builder 的 `ToSelect` 返回 `ErrEmptyTable`。
- SQLite 的锁加在 UNION 分支上 → `ToSelect` 无错误、锁被静默丢弃；PG 同构造产物携带分支 `FOR UPDATE`，执行报 `FOR UPDATE is not allowed with UNION`。
- `LockForUpdate().Count(ctx)`（SQLite）→ `(0, nil)`；`ToCount` 在编译前清空锁；`Paginate` 仅在 `total>0` 时因数据查询报锁错误。
- `WhereNot(func(q){q.Where("status","banned").Where("age","<",18)})` 在含 NULL 行表上命中 3 行（NULL 所在组因另一条件为假使 `NOT(...)` 为真）。
- `WhereExists((*Builder)(nil))` → 条件被静默丢弃；`WhereExists((func(*Builder))(nil))` → panic；`WhereExists(nil)` → `ErrInvalidSubQuery`。
- `WhereRaw("")` 不产生条件；`WhereRaw("   ")` 生成 `WHERE    `（非法 SQL），但保护判定按缺失处理。
- 读尽 `rows` 后再 `ScanStruct` → `get columns failed: sql: Rows are closed`（不是 `sql.ErrNoRows`）。
- CursorBy 游标列 NULL：`int` 字段得零值且不报错（并可能丢行）；`*int` 与 `any` 字段报 `ErrCursorColumnNull`。
- `Transaction` 回调内 `pool.Close()` 后继续写入 → 返回 `nil` 且提交成功（database/sql 只关闭空闲连接）。
- Commit 失败用故障注入驱动实测：`Commit=1`、`Rollback=0`（`database/sql` 不会替调用方回滚）。
- getting-started 完整程序实跑输出 11 行与文档逐字一致；`SelectRaw("a, b")` + InsertUsing 两列 → `ErrInsertUsingColumnMismatch`（按 1 列计）。

### 问题统计与判定标准

- 问题总数 **37** 条：高 **1**、中 **9**、低 **27**。
- 计入：文档承诺与实现不符、写错/写漏关键行为、文档之间或文档与源码注释互相矛盾、示例无法编译或行为错误、错误触发条件/入口集合不准确。
- 不计入：纯排版与同义措辞；不影响公开契约的内部细节；测试注释陈旧但与本轮文档无因果关系（另在条目内附注）。

## 问题汇总表

| 编号 | 级别 | 状态 | 一句话结论 |
|---|---|---|---|
| DOC-01 | 高 | 待修 | WhereNested 内的 JoinOn 被计入写保护却不参与编译，可绕过无 WHERE 保护做全表更新 |
| DOC-02 | 中 | 待修 | Pluck/PluckMap/KeyBy 只换 columns 不清 selectSubs，与 SelectSub 组合必然扫描失败 |
| DOC-03 | 中 | 待修 | 文档宣称支持的「只有 UNION、没有主表」计数/聚合，产物含空表名、不可执行 |
| DOC-04 | 中 | 待修 | 锁哨兵入口集合与实现不符：Count/Exists/聚合提前清锁不报错，UNION 分支锁完全不校验 |
| DOC-05 | 中 | 待修 | ErrCursorColumnNull 的触发范围在三份文档中不一致，实际仅 nil-able 字段可识别 |
| DOC-06 | 中 | 待修 | 文档称 EXISTS 内层「恒假」也计为有效限定，实现把它当恒真拒绝 |
| DOC-07 | 中 | 待修 | 文档称 Force 不跳过 JOIN 检查，实际 Force 跳过整个「无有效限定」判定 |
| DOC-08 | 中 | 待修 | compile-pipeline 的 CompileRaw 示例按 3 个返回值赋值，实际只有 2 个，示例无法编译 |
| DOC-09 | 中 | 待修 | 文档称 `NOT (组)` 不会命中含 NULL 行，SQLite 实测命中 3 行 |
| DOC-10 | 中 | 待修 | 文档称「事务未结束就 Close 会中断事务」，实际进行中的事务仍可继续并提交 |
| DOC-11 | 低 | 待修 | architecture 的三处绝对化表述：新方言零改动、Builder 不参与路由、InsertGetId 编译前拒绝 |
| DOC-12 | 低 | 待修 | 「所有 ToXxx 都做环检测」不成立，INSERT 族/TRUNCATE 不做 |
| DOC-13 | 低 | 待修 | 文档称 Truncate 绑定来自 WHERE/JOIN，实际无任何绑定且 WHERE 被丢弃 |
| DOC-14 | 低 | 待修 | mapping 的「nil 跳过不参与该行列集合」与批量首行模板自相矛盾 |
| DOC-15 | 低 | 待修 | README 称所有 ToXxx 返回三元组，ToTruncate 只有两个返回值 |
| DOC-16 | 低 | 待修 | README 把外部审查脚本说成仓库门禁，且该脚本不递归覆盖分层文档 |
| DOC-17 | 低 | 待修 | 文档称 nil DAO 执行终端方法必然 panic，实际前置校验错误会正常返回 |
| DOC-18 | 低 | 待修 | builder-select 内部矛盾：称所有列名都过 WrapColumn，又称 SelectRaw 不过；且 Select 别名论据错误 |
| DOC-19 | 低 | 待修 | 文档称 PluckMap 列写反不会报错，类型不可互转时会在扫描期报 ErrScanConvert |
| DOC-20 | 低 | 待修 | KeyBy 的两处键边界未写：nil 嵌入键折叠零值、数值字段转 string 键得到 rune 字符 |
| DOC-21 | 低 | 待修 | terminal-methods 矩阵称 Count/Max 无条件清除 columns/selectSubs，UNION/DISTINCT 分支实际保留 |
| DOC-22 | 低 | 待修 | scanner 文档把「读尽 rows 后再扫」的错误写成 sql.ErrNoRows，实际是 Rows are closed |
| DOC-23 | 低 | 待修 | builder-join 两处边界不准确：出错后仍追加条件、JoinOn 的 nil 回调会 panic |
| DOC-24 | 低 | 待修 | HavingRaw/GroupByRaw 的空串与纯空白行为与文档描述不符 |
| DOC-25 | 低 | 待修 | 文档称 ORDER BY 支持 `column AS alias`，实际产物为非法 SQL |
| DOC-26 | 低 | 待修 | SQLite 分支子句落点的两句话自相矛盾（只有最后一个分支落在 UNION 尾部） |
| DOC-27 | 低 | 待修 | errors.md 的 ErrInvalidStruct/ErrNotPointer 返回入口漏了 Find/First/Paginate |
| DOC-28 | 低 | 待修 | ErrEmptyTable 的触发条件对写入口写成 Table/TableSub，写入口实际只认 Table |
| DOC-29 | 低 | 待修 | errors.md 把 ORDER BY/LIMIT 拒绝范围写成「PostgreSQL/SQLite 的全部写语句」 |
| DOC-30 | 低 | 待修 | 文档称三种未覆盖语法都按代码区处理，MySQL `/*! */` 与 PG `U&` 实际相反 |
| DOC-31 | 低 | 待修 | WhereNullSafeNotEquals 的语义句自相矛盾（「包括 NULL」与「NULL 与 NULL 为 false」） |
| DOC-32 | 低 | 待修 | 文档把 ErrDeleteWithJoin 挂到 DeleteJoin 名下，实际只由 Delete/ToDelete 返回 |
| DOC-33 | 低 | 待修 | WhereExists 的 typed-nil 行为未写：*Builder(nil) 静默丢条件、nil 回调 panic |
| DOC-34 | 低 | 待修 | schema 文档称权限不足会报错，MySQL 实际返回空结果、PG 目录查询可读 |
| DOC-35 | 低 | 待修 | getting-started 的 11 行输出无回归锁，测试注释仍指向旧教程示例 |
| DOC-36 | 低 | 待修 | 事务文档两处绝对化：Commit 失败并非自动回滚、跨 Pool 并非「任何入口」都先返回哨兵 |
| DOC-37 | 低 | 待修 | 文档称非唯一游标列会「重复或漏行」，实际严格 `>` 只会漏行不会重复 |

## 逐条详情

### DOC-01：WhereNested 内的 JoinOn 被计入保护却不参与编译

- 级别：高；类型：实现缺陷
- 文档证据：`zcdb/docs/reference/builder-join.md:69`、`zcdb/docs/reference/builder-join.md:385`
- 实现证据：`zcdb/builder_join.go:502`（嵌套组含 Joins 时仍追加条件）；`zcdb/mysql_grammar.go:815-821`（嵌套分支只编译 Conditions，忽略 Joins）；`zcdb/builder_exec.go:492-496`（hasEffectiveJoinConditions 递归 Nested.Joins）；`zcdb/builder_exec.go:173-176`（Update 守卫）
- 测试证据：无（`zcdb/builder_unit_test.go:1755` 只覆盖顶层 join；`zcdb/builder_exec_fuzz_test.go:222-225` 的参考模型也只认顶层）
- 实测：`JoinOn("no", func(j){ j.WhereNested(func(q){ q.JoinOn("ni", ...) }) })` → `ToSelect` = `SELECT * FROM "nu" INNER JOIN "no"`（无 ON），`hasEffectiveJoin=true`，`Update` 实际执行 `affected=2`（MySQL 同构产物亦无 ON）
- 影响：`Update/Increment/Decrement/DeleteJoin` 的「无有效 WHERE/JOIN 拒绝」可被这一形态绕过，产生无条件多表全表更新/删除
- 推荐方案：让 `hasEffectiveJoinConditions` 的 nested 分支只递归 `Nested.Conditions`（与编译、绑定收集对齐），补「嵌套 JoinOn 不计入 + Update 返回 ErrUpdateWithoutWhere」回归

### DOC-02：Pluck/PluckMap/KeyBy 没清 selectSubs

- 级别：中；类型：实现缺陷
- 文档证据：`zcdb/docs/reference/builder-query.md:287`、`zcdb/docs/reference/builder-query.md:321`、`zcdb/docs/reference/terminal-methods.md:20`
- 实现证据：`zcdb/builder_query.go:94`（Pluck 只 Select）、`zcdb/builder_query.go:152`、`zcdb/builder_query.go:241`；`zcdb/builder_select.go:47`（Select 只替换 columns）；`zcdb/builder_compile.go:715`（Count 会清 selectSubs 作对照）
- 测试证据：无（三个 Pluck 族用例 `zcdb/builder_query_sqlite_integration_test.go:381`、`:433`、`:465` 均未设置 SelectSub）
- 实测：`SelectSub(...)` + `Pluck/PluckMap/KeyBy` 分别报 `expected 2/3/3 destination arguments in Scan, not 1/2/2`；编译产物仍带子查询列
- 影响：链式构造里用过 SelectSub 的调用方，三个方法 100% 失败，且与文档「只查该列/两列/字段列」矛盾
- 推荐方案：三处 `Select(...)` 后补 `b.selectSubs = nil`（与 Count/Exists/聚合的策略一致），补 SelectSub 组合回归；文档补「连同 SELECT 子查询列一起清空」

### DOC-03：只有 UNION、没有主表的计数/聚合产出非法 SQL

- 级别：中；类型：实现缺陷
- 文档证据：`zcdb/docs/reference/builder-compile.md:116`、`zcdb/docs/reference/builder-compile.md:135`、`zcdb/docs/reference/errors.md:43`
- 实现证据：`zcdb/builder_compile.go:645`、`zcdb/builder_compile.go:738`、`zcdb/builder_compile.go:779`（union-only 放行）；`zcdb/mysql_grammar.go:117-124`（CompileSelect 无条件输出 FROM）
- 测试证据：无（现有 UNION 用例都先设置 Table，如 `zcdb/builder_unit_test.go:464`）
- 实测：MySQL `SELECT COUNT(*) FROM ((SELECT c FROM ``) UNION (SELECT `c` FROM `b`)) AS `t``；SQLite 执行报 `no such table:`；同一 Builder 的 `ToSelect()` 返回 `ErrEmptyTable`
- 影响：文档宣称受支持的形态编译成功但不可执行；`Find` 报 `ErrEmptyTable`、`Count` 却在数据库侧报错，错误类型割裂
- 推荐方案：去掉 `len(unions)==0` 豁免，与 ToSelect 一样要求 Table/TableSub；同步 `zcdb/docs/reference/builder-compile.md:116`、`zcdb/docs/reference/builder-compile.md:135` 与 `zcdb/docs/reference/errors.md:43`

### DOC-04：锁哨兵的入口集合与校验范围不成立

- 级别：中；类型：文档错误 + 实现行为不一致
- 文档证据：`zcdb/docs/reference/errors.md:54`、`zcdb/docs/reference/errors.md:55`、`zcdb/docs/reference/builder-order-group.md:456`、`zcdb/docs/reference/builder-order-group.md:457`
- 实现证据：`zcdb/builder_compile.go:75-84`（只有 ToSelect 检查锁）；`zcdb/builder_compile.go:653-659`、`:675`、`:699`、`:713`、`:747-751`、`:798-804`（计数/存在/聚合编译前清空锁）；`zcdb/sqlite_grammar.go:208-230`、`zcdb/postgres_grammar.go:273-293`（UNION 分支锁不校验）
- 测试证据：`zcdb/builder_query_sqlite_integration_test.go:15-33`（只覆盖 ToSelect/First）；分支锁无测试
- 实测：SQLite `LockForUpdate().Count(ctx)` → `(0,nil)`；`Paginate` 在 total>0 时才因数据 SQL 报错；SQLite 分支锁被静默丢弃，PG 分支锁执行期才报错
- 影响：用 errors.Is 判断「锁被拒绝」会漏判；SQLite 分支锁「以为加了、实际没有」
- 推荐方案：统一在 ToCount/ToExists/ToAggregate 与 UNION 分支复用锁校验；或把 errors.md 的入口收窄为走 ToSelect 的终端，并明确计数/聚合忽略锁

### DOC-05：ErrCursorColumnNull 的触发范围在三份文档中不一致

- 级别：中；类型：文档内部矛盾 + 关键行为遗漏
- 文档证据：`zcdb/docs/reference/builder-cursor.md:136`、`zcdb/docs/reference/errors.md:87`、`zcdb/docs/how-to/pagination.md:220`
- 实现证据：`zcdb/builder_cursor.go:231`（isNilValue 判定）、`zcdb/builder_cursor.go:266-277`（仅 nil-able 类型返回 true）、`zcdb/scanner.go:193-199`（非指针 NULL 置零值）
- 测试证据：`zcdb/builder_cursor_sqlite_integration_test.go:194`（*int）、`zcdb/builder_cursor_sqlite_integration_test.go:574`（any）；无标量字段用例
- 实测：int 字段 NULL → 值为 0、无错误、可能静默丢行；*int 与 any 字段 → `ErrCursorColumnNull`
- 影响：三份文档对同一错误给出不同触发条件；标量字段场景会把截断误当正常读完
- 推荐方案：统一表述为「指针/interface 等 nil-able 字段可识别 NULL 并报错；非指针基础类型退化为零值」，并修正 pagination 的「只有指针字段」

### DOC-06：EXISTS 内层恒假被当作恒真

- 级别：中；类型：文档错误
- 文档证据：`zcdb/docs/concepts/safety.md:40`
- 实现证据：`zcdb/builder_exec.go:407-415`（仅 `truthUnknown` 返回 unknown，其余一律 truthTrue）
- 测试证据：`zcdb/builder_unit_test.go:1961`（exists_empty 期望 truthTrue）；无「EXISTS 内层恒假」用例
- 实测：`WhereExists(func(q){ q.Table("t").WhereIn("id", []any{}) })` + `Update` → `ErrUpdateWithoutWhere`（内层 `0 = 1` 恒假，按文档应放行）
- 影响：文档的四值归约模型与实现不一致；方向是保守拒绝，不会放大范围，但契约错误
- 推荐方案：实现内层 `truthFalse` 时返回 truthFalse（NOT EXISTS 取反），或把文档改为「只检查内层是否含未知条件；恒假也按恒真处理」

### DOC-07：Force 会跳过 JOIN 检查

- 级别：中；类型：文档错误
- 文档证据：`zcdb/docs/concepts/safety.md:62`
- 实现证据：`zcdb/builder_exec.go:174`、`:207`、`:237`、`:328`（守卫为 `!force && !hasEffectiveWhere() && !hasEffectiveJoin()`）
- 测试证据：`zcdb/builder_unit_test.go:1755`（只测无条件 JOIN 不计入，未测 Force+JOIN）
- 实测：`CrossJoin(...).Update(...)` 无 Force → `ErrUpdateWithoutWhere`；同一链加 `.Force()` → `affected=2`（全表更新）
- 影响：相信文档的调用方会以为 Force 只跳过 WHERE；实际它是整个「无有效限定」判定的逃逸口
- 推荐方案：改为「Force 跳过整个无有效限定判定（WHERE 或 JOIN），不改变 SQL；唯一例外是 Delete 带 JOIN 的 ErrDeleteWithJoin」

### DOC-08：compile-pipeline 的 CompileRaw 示例返回值个数错误

- 级别：中；类型：文档错误
- 文档证据：`zcdb/docs/concepts/compile-pipeline.md:95`
- 实现证据：`zcdb/grammar.go:54`（`CompileRaw(sqlStr string, args []any) (string, error)`）
- 测试证据：无（concepts 示例未纳入 `zcdb/docs_examples_compile_test.go`，门禁只做语法解析）
- 实测：按示例原样编译报 `assignment mismatch: 3 variables but ... returns 2 values`
- 影响：复制示例直接编译失败；同一示例前三行用三元组变量，补一个变量也会继续类型不符
- 推荐方案：改为 `rawSQL, err := db.Grammar().CompileRaw(...)`，并把该示例纳入编译级文档测试

### DOC-09：`NOT (组)` 对含 NULL 行的结果说明错误

- 级别：中；类型：文档错误
- 文档证据：`zcdb/docs/reference/builder-where.md:807`
- 实现证据：`zcdb/builder_where.go:698-715`（只置 Not 标记）；`zcdb/mysql_grammar.go:574-582`（编译为 NOT (...)）
- 测试证据：`zcdb/builder_unit_test.go:3213`、`zcdb/docs_deviation_review_test.go:402-406`（只断言 SQL/args，不覆盖 NULL 行求值）
- 实测：`NOT(status='banned' AND age<18)` 在 (NULL,20)、(ok,30)、(x,NULL) 三行上命中，仅 (banned,10) 被排除
- 影响：按文档会以为 NOT 组天然排除 NULL 行；实际只要组内另一条件为假，含 NULL 行就会命中
- 推荐方案：改为「组内结果为未知时 NOT 后仍为未知；组内其它条件为假时 NOT 为真，含 NULL 行可能命中」

### DOC-10：事务未结束就 Close 并不会中断事务

- 级别：中；类型：文档错误
- 文档证据：`zcdb/docs/how-to/transactions.md:318`
- 实现证据：`zcdb/pool.go:200-221`（Close 只调 *sql.DB.Close）；标准库 `GOROOT/src/database/sql/sql.go:926-951`（只关闭空闲连接）
- 测试证据：无（现有事务用例都在事务结束后才 Close 池）
- 实测：SQLite 文件库、MaxOpenConns=1，回调内 `pool.Close()` 后继续 INSERT → 返回 nil，事务提交成功，重开文件可见该行
- 影响：关机/清理路径以为会中止或回滚，实际写入可能落库；只有 Close 之后的新语句才报 database is closed
- 推荐方案：改为「进行中的事务不会被中断，仍可继续并可能提交；Close 后新语句报错；必须先结束事务再关池」，补 close-in-tx 回归

### DOC-11：architecture 的三处绝对化表述

- 级别：低；类型：过度承诺
- 文档证据：`zcdb/docs/concepts/architecture.md:42`、`zcdb/docs/concepts/architecture.md:45`、`zcdb/docs/concepts/architecture.md:55`
- 实现证据：`zcdb/db_dao.go:41-55`（方言字符串硬编码）；`zcdb/builder_compile.go:36-45`、`:268-271`（Builder 内的方言分支）；`zcdb/builder_query.go:18-23`（Builder.query 决定 Query/QueryPrimary）；`zcdb/builder_exec.go:58-66`（InsertGetId 先编译后拒绝）
- 测试证据：无（无自定义 Grammar 扩展用例；InsertGetId 顺序由 `zcdb/builder_compile_postgres_unit_test.go:164-170` 反证）
- 实测：自引用 Builder 的 `ToInsert` 正常编译；PG `ToInsert` 正常、`InsertGetId` 在编译后拒绝；`Builder` 参与主库入口选择
- 影响：贡献者按「实现一个 Grammar 即可」交付会遇到方言注册、Builder 特例、Schema 限制
- 推荐方案：分别改为「新增内置方言 = Grammar + dialectGrammar/SchemaInspector 注册 + 补齐 Builder 特例」「Builder 只选择查询入口，连接由 DBDao/Pool 决定」「InsertGetId 编译后、执行前拒绝」

### DOC-12：「所有 ToXxx」都做环检测不成立

- 级别：低；类型：过度承诺
- 文档证据：`zcdb/docs/concepts/compile-pipeline.md:11`、`zcdb/docs/concepts/builder-state.md:80`、`zcdb/docs/reference/errors.md:50`
- 实现证据：`zcdb/builder_compile.go:70`、`:456`、`:518`、`:561`、`:648`、`:741`、`:782`、`:860`（调用点）；`:134-160`、`:181-207`、`:238-289`、`:599-607`（不调用）
- 测试证据：`zcdb/builder_compile_cyclic_unit_test.go:11`（覆盖 9 个入口，不含 Insert/Truncate）
- 实测：自引用 Builder：`ToSelect/ToCount/ToUpdate` 报 `ErrCyclicQuery`；`ToInsert/ToTruncate/ToUpsert` 返回 nil
- 影响：读者会误以为任何编译入口都会拒绝环图；实际只有遍历子查询图的入口保证
- 推荐方案：改为「需要递归子查询图的 ToXxx 与 Clone 先做环检测；INSERT 族/TRUNCATE 不涉及」

### DOC-13：Truncate 没有绑定参数

- 级别：低；类型：文档错误
- 文档证据：`zcdb/docs/concepts/compile-pipeline.md:62`
- 实现证据：`zcdb/builder_compile.go:599-607`（ToTruncate 只读 err/table）；`zcdb/mysql_grammar.go:542-544`（无 WHERE/JOIN 输出）
- 测试证据：`zcdb/builder_compile_mysql_unit_test.go:107`（truncate 期望 args=nil）
- 实测：`Where(...).OrderBy(...).Limit(1).ToTruncate()` → `TRUNCATE TABLE ...`，args 为空、WHERE/ORDER/LIMIT 全部丢弃
- 影响：读者会以为 Truncate 可带条件或绑定
- 推荐方案：拆开表述为「Delete/DeleteJoin 的绑定来自 WHERE（DeleteJoin 另含 ON/派生表）；Truncate 无绑定且 WHERE 不参与」

### DOC-14：批量插入的 nil 跳过规则自相矛盾

- 级别：低；类型：文档内部矛盾
- 文档证据：`zcdb/docs/concepts/mapping.md:75`
- 实现证据：`zcdb/reflect.go:192-215`（列集合由首行决定）、`zcdb/reflect.go:237-243`（后续行 nil → SQL NULL）
- 测试证据：`zcdb/reflect_test.go:541`（第二行 nil 指针 → rows[1][1]==nil，列仍在）
- 实测：首行只有 Name、第二行只有 Age → SQL `INSERT (\`name\`) VALUES (?), (?)`，第二行该列写 NULL
- 影响：读者会以为每行可以各自省略 nil 列，实际列集合全局唯一
- 推荐方案：改为「首行确定列集合：首行 nil 字段整列跳过；其余行 nil 字段在该列写 SQL NULL」

### DOC-15：README 称所有 ToXxx 返回三元组

- 级别：低；类型：文档错误
- 文档证据：`zcdb/docs/README.md:120`、`zcdb/docs/README.md:129`
- 实现证据：`zcdb/builder_compile.go:599`（ToTruncate 返回 `(string, error)`）
- 测试证据：`zcdb/docs_examples_compile_test.go:815`（以两个返回值调用 ToTruncate）
- 实测：把 `(*Builder)(nil).ToTruncate` 赋给三元组函数类型无法编译
- 影响：按统一三元组写通用辅助会在 ToTruncate 处编译失败
- 推荐方案：README 补「除 ToTruncate 返回 (sql, error) 外，其余 ToXxx 返回 (sql, args, err)」

### DOC-16：README 把外部审查脚本说成仓库门禁

- 级别：低；类型：文档错误
- 文档证据：`zcdb/docs/README.md:138`
- 实现证据：仓库不存在 `scripts/` 与 `doc_go_examples.py`；实际门禁是 `zcdb/docs_examples_fence_test.go:29`（递归遍历）
- 测试证据：`zcdb/docs_examples_fence_test.go:29`
- 实测：该脚本只扫顶层 `*.md`（3 块），分层文档 211 个 Go 块里 208 个不在其覆盖内
- 影响：维护者会误判门禁覆盖面或去找不存在的脚本
- 推荐方案：删掉脚本名，或注明「等价的外部审查工具，不作为仓库门禁」

### DOC-17：nil DAO 执行终端方法并非必然 panic

- 级别：低；类型：文档错误
- 文档证据：`zcdb/docs/reference/builder-core.md:42`
- 实现证据：`zcdb/builder_exec.go:174-176`、`:286-288`、`:323-325`（保护错误先于 nil DAO 解引用）
- 测试证据：无（现有测试未覆盖 nil DAO + 执行终端方法）
- 实测：nil DAO Builder 的 `Update` 无 WHERE → `ErrUpdateWithoutWhere`；带 JOIN 的 `Delete` → `ErrDeleteWithJoin`；带 WHERE 的 `Find` 才 panic
- 影响：把「必然 panic」当诊断依据会误判
- 推荐方案：改为「通过 ToXxx 与保护判定后，执行阶段才会因解引用 nil DAO panic；前置校验错误照常返回」

### DOC-18：builder-select 的 WrapColumn 表述与 SelectRaw 论据

- 级别：低；类型：文档内部矛盾
- 文档证据：`zcdb/docs/reference/builder-select.md:15`、`zcdb/docs/reference/builder-select.md:92`、`zcdb/docs/reference/builder-select.md:116`
- 实现证据：`zcdb/builder_select.go:47-63`（Select 替换、SelectRaw 追加且 Raw=true）；`zcdb/mysql_grammar.go:100-104`（Raw 列不包裹）；`zcdb/mysql_grammar.go:39-42`（含 `(` 与 AS 原样输出）
- 测试证据：`zcdb/builder_select_mysql_unit_test.go:11`（同一 SQL 中 Raw 列未包裹、普通列包裹）
- 实测：`Select("SUM(id) AS total")` 可表达别名；`SelectRaw("name")` 不包裹
- 影响：读者会对 Select/SelectRaw 的能力边界产生错误预期
- 推荐方案：第 5 条限定为「普通列（Select/AddSelect）」；SelectRaw 的推荐理由改为「追加语义、不参与去重」，删掉「无法表达别名」

### DOC-19：PluckMap 列写反并非总是不报错

- 级别：低；类型：文档错误
- 文档证据：`zcdb/docs/reference/builder-query.md:338`
- 实现证据：`zcdb/builder_query.go:164-171`（按位置扫描，不做类型预校验）
- 测试证据：无（`zcdb/builder_query_sqlite_integration_test.go:433` 只测正常顺序）
- 实测：`PluckMap(ctx,&map[int64]string,"name","id")` → `pluck scan row failed: ... cannot convert "a" to int64`
- 影响：读者以为写反只是静默错位，实际常见组合直接报错且错误指向转换失败
- 推荐方案：改为「写反后类型不可互转会在扫描期报 ErrScanConvert；可互转时才会静默错位」

### DOC-20：KeyBy 的两处键边界未写

- 级别：低；类型：关键行为遗漏
- 文档证据：`zcdb/docs/reference/builder-query.md:353`、`zcdb/docs/reference/builder-query.md:375`
- 实现证据：`zcdb/builder_query.go:266-272`（nil 嵌入字段 key 保持零值；ConvertibleTo+Convert）
- 测试证据：无（`zcdb/builder_query_sqlite_integration_test.go:465` 只用扁平结构体）
- 实测：nil 嵌入指针承载键列时所有行折叠到零值键；`map[string]X` + int 字段键得到 `"\x01"` 这类 rune 字符键
- 影响：静默产生不可读/折叠的键；文档的「让两者类型兼容」会误导 int→string
- 推荐方案：补两句边界；数值→string 键建议显式转成字符串字段，或考虑实现侧拒绝

### DOC-21：Count/Max 并非无条件清除 columns/selectSubs

- 级别：低；类型：文档错误
- 文档证据：`zcdb/docs/reference/terminal-methods.md:14`、`zcdb/docs/reference/terminal-methods.md:16`
- 实现证据：`zcdb/builder_compile.go:653-666`、`zcdb/builder_compile.go:669-689`、`zcdb/builder_compile.go:691-706`、`zcdb/builder_compile.go:714-715`（按分支保留/替换）
- 测试证据：无（现有用例只覆盖非 UNION 分支）
- 实测：`Count + SelectSub + UnionAll` 产物保留每个分支的子查询列；`Max` 同样
- 影响：按矩阵推断 UNION 计数会丢子查询列，与实际 SQL 不符
- 推荐方案：两格改为「按分支：普通分支替换为 COUNT(*)/1 并清空；UNION/DISTINCT 保留；GROUP+HAVING+显式列保留」

### DOC-22：读尽 rows 后再扫得到的是 Rows are closed

- 级别：低；类型：文档错误
- 文档证据：`zcdb/docs/reference/scanner.md:21`、`zcdb/docs/reference/scanner.md:37`
- 实现证据：`zcdb/scanner.go:83-86`（先取列名）、`:110-115`（Next 未命中才返回 ErrNoRows）
- 测试证据：无（`zcdb/docs_examples_compile_test.go:661` 只做编译级核对）
- 实测：先整表 ScanStruct，再单行 ScanStruct → `zcdb: get columns failed: sql: Rows are closed`，`errors.Is(sql.ErrNoRows)=false`
- 影响：按文档用 ErrNoRows 识别会误判为连接/取列故障
- 推荐方案：改为「读尽后 rows 已被 database/sql 自动关闭，再扫在 Columns() 处报 Rows are closed；要重扫必须重新 Query」

### DOC-23：builder-join 的两处边界说明不准确

- 级别：低；类型：文档错误
- 文档证据：`zcdb/docs/reference/builder-join.md:162`、`zcdb/docs/reference/builder-join.md:494`
- 实现证据：`zcdb/builder_join.go:267-271`（出错只 return j）、`zcdb/builder_join.go:326-330`、`zcdb/builder_join.go:178-180`（JoinOn 无条件调用 callback）、`zcdb/builder_join.go:195-199`（JoinSub 有 nil 判断）
- 测试证据：`zcdb/builder_join_mysql_unit_test.go:158`（只断言整组丢弃）
- 实测：`On(ok)→On(bad)→Where(ok)` 后 Conditions=3（后续仍追加），整组在回调结束后被丢弃；`JoinOn("t", nil)` panic，`JoinSub(..., nil)` 正常
- 影响：复用 JoinBuilder 的代码会误判状态；把回调变量化可能直接崩溃
- 推荐方案：改为「该条不追加，但后续仍会收集，整组在回调结束后丢弃」；注明 JoinOn 回调不可为 nil（或实现兼容）

### DOC-24：HavingRaw/GroupByRaw 的空片段行为

- 级别：低；类型：文档错误
- 文档证据：`zcdb/docs/reference/builder-order-group.md:26`、`zcdb/docs/reference/builder-order-group.md:142`
- 实现证据：`zcdb/mysql_grammar.go:147-151`（GroupByRaw 空串走空列名包裹）、`:866-888`（HavingRaw 空串被跳过、纯空白保留）、`:156-159`（有 havings 就写 HAVING）
- 测试证据：`zcdb/builder_exec_mysql_integration_test.go:1362-1370`（HavingRaw("") 后接有效 Having 等同无操作）
- 实测：`HavingRaw("")` 留下悬空 `HAVING`；`HavingRaw("   ")` 生成 `HAVING    `；`GroupByRaw("", 7)` 生成空列名并保留 args=[7]
- 影响：动态拼装时生成非法 SQL 或参数泄漏，排查方向错误
- 推荐方案：分别写明空串/纯空白/空列名的实际形态，或实现统一按「空片段跳过」处理

### DOC-25：ORDER BY 不支持 `column AS alias`

- 级别：低；类型：文档错误
- 文档证据：`zcdb/docs/reference/builder-order-group.md:304`
- 实现证据：`zcdb/mysql_grammar.go:43-48`（AS 分支包裹别名后直接用于 ORDER BY）
- 测试证据：无
- 实测：`OrderBy("users.name AS n")` → `ORDER BY \`users\`.\`name\` AS \`n\` ASC`；MySQL 报 1064、PG 报 syntax error
- 影响：按文档写法生成非法 SQL
- 推荐方案：删掉括注或改为「ORDER BY 只接受列名或裸别名；`col AS alias` 会生成非法 SQL」

### DOC-26：SQLite 分支子句落点的自相矛盾

- 级别：低；类型：文档内部矛盾
- 文档证据：`zcdb/docs/reference/builder-order-group.md:455`
- 实现证据：`zcdb/sqlite_grammar.go:209-223`（主查询 + 分支完整 SQL + 尾部子句）
- 测试证据：无
- 实测：最后分支的 LIMIT 落到整条 UNION 尾部（作用于全集）；非最后分支的 ORDER BY 出现在下一个 UNION 之前，SQLite 直接语法错误
- 影响：首句「因而落在尾部」只对最后一个分支成立
- 推荐方案：改为「最后分支的子句落在整条 UNION 尾部；非最后分支落在下一个 UNION 之前并直接报错」

### DOC-27：ErrInvalidStruct/ErrNotPointer 的返回入口不完整

- 级别：低；类型：文档错误
- 文档证据：`zcdb/docs/reference/errors.md:44`、`zcdb/docs/reference/errors.md:83`
- 实现证据：`zcdb/scanner.go:133`（切片元素分支包装 ErrInvalidStruct）、`zcdb/builder_query.go:319`（Paginate 走 ScanStructClose）
- 测试证据：`zcdb/scanner_error_sentinel_sqlite_integration_test.go:14-72`（只直连 ScanStruct）
- 实测：`Find/First/Paginate(ctx,&[]int{})` 返回包装后的 ErrInvalidStruct；`Paginate(ctx,123)` 返回包装后的 ErrNotPointer
- 影响：只在 ScanStruct 上处理这两个哨兵会漏掉终端方法入口
- 推荐方案：入口集合补 Find/First/Paginate（经扫描层包装）

### DOC-28：ErrEmptyTable 对写入口的触发条件写宽了

- 级别：低；类型：文档错误
- 文档证据：`zcdb/docs/reference/errors.md:43`
- 实现证据：`zcdb/builder_compile.go:65`（SELECT 族 Table 或 TableSub）、`zcdb/builder_compile.go:454`、`zcdb/builder_compile.go:516`、`zcdb/builder_compile.go:559`、`zcdb/builder_compile.go:604`（写入口只判 Table）
- 测试证据：无（无 TableSub + 写入口用例）
- 实测：只设 TableSub（无 Table）时 ToSelect 正常、ToUpdate/ToInsert 返回 ErrEmptyTable
- 影响：读者会以为设置 TableSub 后写语句可用
- 推荐方案：按入口拆分触发条件（写语句只认 Table）

### DOC-29：errors.md 把写语句 ORDER BY/LIMIT 拒绝范围写成「全部」

- 级别：低；类型：文档内部矛盾
- 文档证据：`zcdb/docs/reference/errors.md:56`
- 实现证据：`zcdb/builder_compile.go:32-47`、调用点 `zcdb/builder_compile.go:459`、`zcdb/builder_compile.go:524`、`zcdb/builder_compile.go:567`、`zcdb/builder_compile.go:866`；Insert 族/Truncate 不调用
- 测试证据：`zcdb/builder_compile_postgres_unit_test.go:107`（只覆盖已接入的入口）
- 实测：PG/SQLite 的 ToInsert/ToTruncate 带 OrderBy/Limit → err=nil
- 影响：读者会预期 INSERT/UPSERT/TRUNCATE 也报错
- 推荐方案：改为「UPDATE/DELETE/DeleteJoin/Increment/Decrement」，并注明 Insert 族与 Truncate 忽略

### DOC-30：三种「未覆盖语法」的处理方向写反了两项

- 级别：低；类型：文档错误
- 文档证据：`zcdb/docs/reference/builder-compile.md:577`
- 实现证据：`zcdb/sqlScan.go:8-9`、`:79-84`（字符串）、`:98-100`、`:143`（块注释，MySQL 不嵌套）
- 测试证据：`zcdb/sqlScan_test.go:52-54`（块注释用例，无 `/*!`）
- 实测：MySQL `/*! ? */` 被当注释（占位符不识别）；PG `U&'a$1b'` 被当普通字符串（不识别）；SQLite `[a?b]` 才按代码区处理
- 影响：按文档在 `/*! */` 里写占位符会得到相反的计数/匹配结果
- 推荐方案：按实际方向重写三项，并同步 `zcdb/sqlScan.go:8-9` 的源码注释

### DOC-31：WhereNullSafeNotEquals 的语义句自相矛盾

- 级别：低；类型：文档内部矛盾
- 文档证据：`zcdb/docs/reference/builder-where.md:990`
- 实现证据：`zcdb/mysql_grammar.go:634-639`、`zcdb/postgres_grammar.go:661-666`、`zcdb/sqlite_grammar.go:608-613`
- 测试证据：`zcdb/builder_where_sqlite_integration_test.go:1160-1165`（NULL 与 NULL 不命中，期望 4 行）
- 实测：`WhereNullSafeNotEquals("age", nil)` 在 SQLite 返回 4 行（NULL 行不命中）
- 影响：同句前半「包括 NULL 为不等」与后半「NULL 与 NULL 为 false」冲突
- 推荐方案：删去「（包括 NULL）」或改为「NULL 与非 NULL 值比较为不等；NULL 与 NULL 为 false」

### DOC-32：ErrDeleteWithJoin 被错挂到 DeleteJoin 名下

- 级别：低；类型：文档错误
- 文档证据：`zcdb/docs/reference/builder-where.md:1238`
- 实现证据：`zcdb/builder_exec.go:286-288`（Delete 带 JOIN）、`zcdb/builder_compile.go:521-523`（ToDelete）；DeleteJoin 只返回 ErrDeleteJoinNoJoin/ErrDeleteWithoutWhere
- 测试证据：`zcdb/cross_dialect_integration_test.go:1392`（Delete 带 JOIN → ErrDeleteWithJoin）
- 实测：`ToDelete()` 带 JOIN → ErrDeleteWithJoin；`ToDeleteJoin()` 无 JOIN → ErrDeleteJoinNoJoin
- 影响：读者会以为 DeleteJoin 可能返回 ErrDeleteWithJoin
- 推荐方案：改为「Delete 带 JOIN 返回 ErrDeleteWithJoin；DeleteJoin 无 JOIN 为 ErrDeleteJoinNoJoin」

### DOC-33：WhereExists 的 typed-nil 行为未写

- 级别：低；类型：关键行为遗漏
- 文档证据：`zcdb/docs/reference/builder-where.md:1028`
- 实现证据：`zcdb/builder_where.go:446-464`（`*Builder` 直接赋值、`func` 直接调用）；`zcdb/mysql_grammar.go:585-589`（nil 子句跳过）
- 测试证据：`zcdb/builder_unit_test.go:2654`（只覆盖 42/untyped nil）
- 实测：`WhereExists((*Builder)(nil))` 静默丢条件；`WhereExists((func(*Builder))(nil))` panic；`WhereExists(nil)` → ErrInvalidSubQuery
- 影响：条件构造可能为 nil 时，查询范围被静默放大或直接崩溃
- 推荐方案：补两句边界；或实现统一拒绝 nil 入参（行为变更需拍板）

### DOC-34：schema 权限说明与引擎实际行为不符

- 级别：低；类型：文档错误
- 文档证据：`zcdb/docs/reference/schema.md:229`、`zcdb/docs/reference/schema.md:231`
- 实现证据：`zcdb/mysql_schema.go:16`、`:39`、`:74`（information_schema 查询）；`zcdb/postgres_schema.go:15`、`:40`、`:69`（pg_catalog 查询）
- 测试证据：无（无权限场景用例）
- 实测：MySQL 仅 USAGE 权限账号查 information_schema → 空结果、无错误；PG 目录查询默认可读，表级权限不足不报错
- 影响：读者会等一个不会出现的错误；MySQL 空结果还会与「表不存在」混淆
- 推荐方案：改为「MySQL 只返回可见对象（不可见→空结果）；PG 的 pg_catalog 默认可读；连接/普通 SQL 失败才原样返回驱动错误」

### DOC-35：getting-started 的预期输出没有回归锁

- 级别：低；类型：测试缺口 / 文档-测试失同步
- 文档证据：`zcdb/docs/getting-started.md:332-348`
- 实现证据：教程步骤对应的实现行为正确（`zcdb/builder_query.go:296`、`zcdb/builder_cursor.go:30`、`zcdb/db_dao.go:223`）
- 测试证据：`zcdb/docs_deviation_review_test.go:54-78`（注释标注 getting-started，断言仍是旧 MySQL 示例）；`zcdb/docs_examples_compile_test.go:24`（节标题同名，镜像旧示例）
- 实测：把教程完整程序放入临时 module 运行，11 行输出与文档一致；但测试集不执行它
- 影响：Paginate/Cursor/事务/扫描任何行为变化都不会被 CI 发现；测试注释指向已不存在的内容
- 推荐方案：增加内存 SQLite 端到端用例断言 11 行输出；同步修正两处测试注释/节标题

### DOC-36：事务文档的两处绝对化表述

- 级别：低；类型：文档错误
- 文档证据：`zcdb/docs/how-to/transactions.md:33`、`zcdb/docs/how-to/transactions.md:10`
- 实现证据：`zcdb/db_dao.go:260-268`（Commit 失败后不再 Rollback）；标准库 `GOROOT/src/database/sql/sql.go:2287-2319`（Commit 失败只释放连接，不调用 Rollback）；`zcdb/db_dao.go:29-38`（txFor 只在执行层）
- 测试证据：`zcdb/fault_injection_test.go:278-285`（只断言 ErrTxCommit，未断言 Rollback 次数）
- 实测：假驱动 Commit 失败 → `Commit=1, Rollback=0`；跨 Pool ctx 下未设表/非法运算符/无 WHERE 分别返回 ErrEmptyTable/ErrInvalidOperator/ErrUpdateWithoutWhere
- 影响：读者会以为 Commit 失败必然回滚；也会把 ErrTxOwnerMismatch 当作跨库误用的唯一判据
- 推荐方案：Commit 失败改为「事务已结束，实际结果由驱动决定」；跨 Pool 改为「SQL 可编译且通过保护时返回 ErrTxOwnerMismatch」

### DOC-37：非唯一游标列只会漏行，不会重复

- 级别：低；类型：文档错误
- 文档证据：`zcdb/docs/how-to/pagination.md:219`
- 实现证据：`zcdb/builder_cursor.go:163-168`（固定 `>`/`<`）、`:177-187`（按游标列排序续取）
- 测试证据：无（现有 CursorBy 用例都用唯一列）
- 实测：`grp` 值 `x,x,x,y`、chunk=2 → 读到 id `[1 2 4]`，id=3 被跳过、无重复
- 影响：调用方会为「可能重复」做多余去重，而真正风险是静默漏行
- 推荐方案：改为「批次边界上相同游标值的行会被跳过（漏行）；严格 >/< 不会重复」

## 已核对且未发现偏差的关键行为

以下行为已逐条对照源码与测试（多数另经探针实测），修复其他条目时不应顺手改动：

- 注释规范化与生命周期：`*`/`/`/`\`/控制字符替换、非法 UTF-8 → U+FFFD、255 rune 截断、空值清空不回退、Clone 保留、子查询不泄漏（`zcdb/builder.go:56-76`、`zcdb/builder_compile.go:13-18`）。
- 终端方法克隆策略：Find/Count/Exists/聚合/Pluck 族不克隆，First/Value 按需克隆，Paginate 不克隆但恢复临时状态，CursorBy 每批克隆（`zcdb/builder_query.go`、`zcdb/builder_cursor.go:177`）。
- 绑定顺序：SELECT_SUB → FROM_SUB → JOIN → WHERE → GROUP BY → HAVING → UNION；UPDATE 的 MySQL JOIN→SET→WHERE 与 PG/SQLite SET→JOIN→WHERE（`zcdb/builder_compile.go:899-929`、`:474-494`）。
- 三方言差异矩阵：标识符、占位符、锁、UNION 括号、RIGHT JOIN、CROSS JOIN ON、Truncate、随机排序、UPSERT、INSERT IGNORE、DeleteJoin、多表 UPDATE、WhereDate（`zcdb/docs/reference/dialect-matrix.md` 全表，实测与测试一致）。
- 破坏性保护归约：空嵌套/空串 Raw 不计入、空 NOT IN 恒真、空 IN 恒假、非空 Raw 逃生口、带 ON 的顶层 JOIN 计入、Delete+JOIN 拒绝（DOC-01/06/07 列出的例外除外）。
- CursorBy 分批：chunkSize 0/负数、`LIMIT chunkSize+1` 探测、忽略 ORDER BY、desc、offset 叠加、探测行不丢不重（`zcdb/builder_cursor.go:114-261`）。
- 扫描映射：标签/snake_case、BFS 外层优先、同深度取先声明、未匹配列丢弃、NULL→零值/nil、数值溢出报错、JSON、time→RFC3339（parseTime 前提）（`zcdb/scanner.go:75-175`、`:302-370`、`zcdb/reflect.go:68-142`）。
- 执行路由：事务 ctx 归属、锁/Primary 走主库、无从库降级、慢 SQL 回调（执行后、panic 隔离）、rows 关闭责任（`zcdb/db_dao.go:29-38`、`:133-215`、`zcdb/pool.go:225-244`）。
- 连接池：默认 50/50/600/5s、`<=0` 取默认、从库独立 Open+Ping、Close 幂等、AddSlave 热加载（`zcdb/pool.go:48-221`）。
- 错误参考：45 个哨兵集合与 errors.go 完全一致；包装形态（裸/%w、ErrPoolConnect 多层、ErrTxRollback 双错误、ErrWriteOrderLimitUnsupported 文案）逐条核对通过（DOC-04/12/27/28/29 列出的入口/条件例外除外）。
- Schema：三方言 SQL 与测试逐字一致；Tables 可见范围、Columns/Indexes 字段语义、Default/Extra 形态、INCLUDE/rowid/#expr 局限（`zcdb/schema_inspector.go`、`*_schema.go`）。
- 迁移对照：NewDBDao 五参数、SlowSQLCallback grammar 参数、Pluck 拆分、写语句 ORDER BY/LIMIT、默认注释的版本归属与旧/新写法（git tag/log 只读核对）。

## Go 示例覆盖

提取方式：`doc_go_examples.py --docs-dir zcdb/docs --strict` 与仓库门禁 `TestDocsGoBlockSyntax`；均按 Go 语法解析，不要求编译。211 个块全部 `parse=ok`，0 error。

| 文档 | Go 块数 | 语法/结构 | 证据/原因 |
|---|---|---|---|
| `zcdb/docs/README.md` | 1 | 通过 | strict 0 error |
| `zcdb/docs/getting-started.md` | 2 | 通过 | strict 0 error；完整程序另行实跑 |
| `zcdb/docs/concepts/architecture.md` | 0 | 不适用 | 无 Go 块 |
| `zcdb/docs/concepts/builder-state.md` | 1 | 通过 | strict 0 error |
| `zcdb/docs/concepts/compile-pipeline.md` | 1 | 通过 | 语法通过，但 DOC-08 为编译错误 |
| `zcdb/docs/concepts/execution-routing.md` | 1 | 通过 | strict 0 error |
| `zcdb/docs/concepts/mapping.md` | 2 | 通过 | strict 0 error |
| `zcdb/docs/concepts/safety.md` | 1 | 通过 | strict 0 error |
| `zcdb/docs/reference/builder-core.md` | 9 | 通过 | strict 0 error |
| `zcdb/docs/reference/builder-select.md` | 5 | 通过 | strict 0 error |
| `zcdb/docs/reference/builder-where.md` | 43 | 通过 | strict 0 error |
| `zcdb/docs/reference/builder-join.md` | 14 | 通过 | strict 0 error |
| `zcdb/docs/reference/builder-order-group.md` | 15 | 通过 | strict 0 error |
| `zcdb/docs/reference/builder-query.md` | 10 | 通过 | strict 0 error |
| `zcdb/docs/reference/builder-cursor.md` | 4 | 通过 | strict 0 error |
| `zcdb/docs/reference/builder-exec.md` | 16 | 通过 | strict 0 error |
| `zcdb/docs/reference/builder-compile.md` | 17 | 通过 | strict 0 error |
| `zcdb/docs/reference/scanner.md` | 4 | 通过 | strict 0 error |
| `zcdb/docs/reference/schema.md` | 2 | 通过 | strict 0 error |
| `zcdb/docs/reference/terminal-methods.md` | 1 | 通过 | strict 0 error |
| `zcdb/docs/reference/dialect-matrix.md` | 1 | 通过 | strict 0 error |
| `zcdb/docs/reference/errors.md` | 0 | 不适用 | 无 Go 块 |
| `zcdb/docs/how-to/pagination.md` | 8 | 通过 | strict 0 error；完整示例实跑 |
| `zcdb/docs/how-to/transactions.md` | 11 | 通过 | strict 0 error |
| `zcdb/docs/how-to/dynamic-queries.md` | 17 | 通过 | strict 0 error |
| `zcdb/docs/how-to/upsert.md` | 10 | 通过 | strict 0 error |
| `zcdb/docs/how-to/schema-inspection.md` | 2 | 通过 | strict 0 error |
| `zcdb/docs/migration/breaking-changes.md` | 13 | 通过 | strict 0 error |

## 建议处理顺序

### P0（先修）

- **DOC-01**（安全守卫绕过）：修实现（`hasEffectiveJoinConditions` 只递归 Conditions）+ 回归用例。

### P1（中）

- 实现类：DOC-02（Pluck 族清 selectSubs）、DOC-03（union-only 拒绝）、DOC-04（锁校验统一）。
- 文档类：DOC-05（CursorBy NULL 统一口径）、DOC-06（EXISTS 恒假）、DOC-07（Force 作用域）、DOC-08（CompileRaw 示例）、DOC-09（NOT NULL）、DOC-10（Close 与事务）。

### P2（低）

- 先改「会导致示例编译失败/非法 SQL」的：DOC-15、DOC-25、DOC-30。
- 再改契约/入口集合类：DOC-11、DOC-12、DOC-13、DOC-27、DOC-28、DOC-29、DOC-36。
- 最后改语义措辞与遗漏：DOC-14、DOC-16、DOC-17、DOC-18、DOC-19、DOC-20、DOC-21、DOC-22、DOC-23、DOC-24、DOC-26、DOC-31、DOC-32、DOC-33、DOC-34、DOC-35、DOC-37。
- 需要产品决策的三项：DOC-06（改实现还是改文档）、DOC-05（是否统一 NULL 检测）、DOC-01（守卫修复方式）。

## 验收建议

修复后至少执行：

```bash
git diff --check
go test ./zcdb -count=1
go test ./zcdb -run 'TestDocs|TestDocReview' -count=1
go test ./... -count=1
go vet ./...
python "<skill-dir>/scripts/doc_go_examples.py" --docs-dir zcdb/docs --strict
```

定向回归建议：新增「嵌套 WhereNested 内 JoinOn 不计入 + Update 拒绝」「Pluck 族 + SelectSub」「union-only 返回 ErrEmptyTable」「Count/Exists/聚合/UNION 分支锁」「CursorBy 非指针 NULL」「Force + 无条件 JOIN」六组可失败用例；文档侧对上述 DOC 逐条做 SQL/args 或行为断言。
