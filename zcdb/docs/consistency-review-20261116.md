# zcdb 文档-实现一致性审查报告（2026-11-16）

> 2026-11-16 文档已整体重构：本报告中的文档路径与行号指向**重构前的扁平文档**（`compile.md`/`connection.md`/`query-builder.md`/`query-exec.md`/`mutate.md`/`schema.md`），仅作历史追溯；现行文档入口见 [README.md](README.md)。

- 审查对象：`zcdb` 模块的 7 份用户文档（`zcdb/docs/README.md`、`connection.md`、`query-builder.md`、`query-exec.md`、`mutate.md`、`compile.md`、`schema.md`）
- 基线：工作区快照 `9cad987`（`git stash create` 生成，内容 = HEAD `26a978c` + 未提交改动；其中 `zcdb` 的未提交改动是上一轮的 Pluck/PluckMap/KeyBy 拆分，其余模块的未提交改动不在本次范围）
- 阶段：**修复已完成（21 条全部按「文档/注释改到与代码一致」处理，改动留在工作区，未提交）**
- 行号约定：第 1~5 节的行号指向**审查基线快照** `9cad987`，仅用于追溯；第 6~7 节的行号指向**当前工作区**
- 重跑审查时请排除本文件自身：`--docs-dir zcdb/docs` 会自动排除 `consistency-review*`

## 审查范围与方法

### 文档清单与映射

| 文档 | 对照实现 | 锁死该文行为的测试 |
|---|---|---|
| `zcdb/docs/README.md` | `zcdb/builder.go`、`zcdb/builder_compile.go`、`zcdb/pool.go`、`zcdb/db_dao.go`、`zcdb/errors.go`、三方言 grammar | `zcdb/docs_deviation_review_test.go`、`zcdb/docs_examples_compile_test.go`、`zcdb/docs_examples_fence_test.go` |
| `zcdb/docs/connection.md` | `zcdb/pool.go`、`zcdb/db_dao.go`、`zcdb/slave_strategy.go`、`zcdb/builder_exec.go`、`zcdb/builder_query.go` | `zcdb/pool_unit_test.go`、`zcdb/slave_strategy_test.go`、`zcdb/docs_deviation_review_test.go` |
| `zcdb/docs/query-builder.md` | `zcdb/builder_select.go`、`builder_where.go`、`builder_join.go`、`builder_group.go`、`builder_order.go`、`builder_compile.go`、`builder.go`、三方言 grammar | `zcdb/builder_unit_test.go`、`zcdb/builder_where_*`、`zcdb/builder_join_*`、`zcdb/builder_order_*`、`zcdb/docs_deviation_review_test.go` |
| `zcdb/docs/query-exec.md` | `zcdb/builder_query.go`、`zcdb/builder_cursor.go`、`zcdb/scanner.go`、`zcdb/reflect.go` | `zcdb/builder_query_*`、`zcdb/builder_cursor_*`、`zcdb/scanner_test.go`、`zcdb/scanner_error_sentinel_sqlite_integration_test.go` |
| `zcdb/docs/mutate.md` | `zcdb/builder_exec.go`、`zcdb/builder_compile.go`、`zcdb/reflect.go`、三方言 grammar | `zcdb/builder_exec_{mysql,postgres,sqlite}_integration_test.go`、`zcdb/builder_error_branches_unit_test.go`、`zcdb/docs_deviation_review_test.go` |
| `zcdb/docs/compile.md` | `zcdb/builder_compile.go`、`zcdb/grammar.go`、`zcdb/rawLiteral.go`、`zcdb/sqlScan.go`、`zcdb/expression.go` | `zcdb/builder_compile_{mysql,postgres,sqlite}_unit_test.go`、`zcdb/rawLiteral_test.go`、`zcdb/sqlScan_test.go`、`zcdb/cross_dialect_integration_test.go` |
| `zcdb/docs/schema.md` | `zcdb/schema_inspector.go`、`zcdb/{mysql,postgres,sqlite}_schema.go` | `zcdb/schema_inspector_test.go`、`zcdb/schema_inspector_{mysql,postgres,sqlite}_integration_test.go`、`zcdb/cross_dialect_integration_test.go` |

范围外文档：仓库根 `README.md`（并行会话正在修改）、旧报告 `zcdb/docs/consistency-review.md`（基线 `26744e5` 的历史记录，按报告约定排除）。

### 方法

1. **正向逐声明比对**：对 7 份文档的每条可验证声明（行为、默认值、错误、SQL/args、顺序、边界、示例）定位实现分支与锁死测试，记录 `文件:行`。
2. **反向审查**：用导出 API 与实现分支反查文档遗漏（如 `WhereNotBetweenColumns`、`ErrNoFields` 的全 nil 字段路径）。
3. **运行时实测**：用 `go test -overlay` 把临时 `_test.go` 映射进 `zcdb` 包（仓库零改动），每个探针单独 `-run '^TestProbe...$' -count=1` 运行；环境为本机 MySQL 8.4 / PostgreSQL 15 容器 + modernc.org/sqlite 内存库。另用 `docker exec` 直接核对 MySQL 引擎语义。
4. **Go 示例**：`doc_go_examples.py --docs-dir zcdb/docs --strict`，并与仓库门禁 `TestDocsGoBlockSyntax` 交叉验证。
5. **行号来源**：审查期引用读取基线快照（`git show 9cad987:<path>`）；修复执行记录与验收结果读取当前工作区。

### 实测过的行为（输入 → 观察结果）

- 非相关 `WhereExists`（内层 `WHERE id = 1`）+ 无外层 WHERE 的 `Update` → `affected=3`、`err=nil`；内层无 WHERE 的 EXISTS → `ErrUpdateWithoutWhere`（保护语义为句法判定，不做相关性分析）。
- `SelectRaw("a, b")` + `ToInsertUsing([]string{"a","b"}, ...)` → `ErrInsertUsingColumnMismatch`，错误消息 `subquery select columns=1`。`Select("a")+SelectRaw("b, c")` 对 1 个目标列同样计数为 2。
- `WhereBetween("age", NewExpression("min_age"), 30)` → SQL `BETWEEN ? AND ?`，args 含 `zcdb.Expression`，执行报 `sql: converting argument $1 type: unsupported type zcdb.Expression, a struct`；`HavingBetween` 同型；`JoinBuilder.WhereIn` 的 Expression 元素进入 args。
- MySQL `InsertOrIgnore` 向 `VARCHAR(2)` 写 `abcdef` → `affected=1`、`err=nil`，回读 `ab`；SQLite `InsertOrIgnore` 触发 NOT NULL 违例 → `affected=0`、`err=nil`（同数据普通 `Insert` 报 `constraint failed: NOT NULL constraint failed`）。
- 三方言括号嵌套 join 组（`JOIN ((...) y JOIN (...) z ON ...) ON ...`）均可执行；PostgreSQL 的 `HAVING` 引用 SELECT 别名报 `42703 column "total" does not exist`；MySQL `BINARY ... LIKE` 区分大小写、SQLite `GLOB` 区分大小写。
- MySQL 8.4 的 `AUTO_INCREMENT` 列 `COLUMN_DEFAULT` 为 NULL（`schema.md` 的「nil 表示无默认值」成立）。

### 问题统计与判定标准

- 问题总数 **21** 条：高 **0**、中 **5**、低 **16**。
- 计入：文档承诺与实现不符、文档写错/写漏关键行为、文档之间或文档与源码注释互相矛盾、示例逻辑不可成立。
- 不计入：纯排版与同义措辞；不影响公开契约的内部实现细节；测试文件里的过期注释（本次另记为观察项，不单列 DOC）。

## 问题汇总表

| 编号 | 级别 | 状态 | 一句话结论 |
|---|---|---|---|
| DOC-01 | 低 | 已修 | connection.md 把 Pluck 列入受标签影响的「结构体映射」方法，实际 Pluck 不做标签映射 |
| DOC-02 | 低 | 已修 | query-builder.md 只列 Or 变体，未描述基方法 `WhereNotBetweenColumns` |
| DOC-03 | 低 | 已修 | query-builder.md 称原始片段占位符识别「与引擎一致」，未列 `sqlScan.go` 的已知未覆盖语法 |
| DOC-04 | 低 | 已修 | JoinBuilder.Where 的绑定列只写「值」，未提 nil→IS NULL 与 Expression 内联不占绑定 |
| DOC-05 | 低 | 已修 | `Builder.Clone` 注释称 Paginate 内部用 Clone，实际 Paginate 不克隆 |
| DOC-06 | 低 | 已修 | `Builder.Offset` 注释称「未设置 Limit」才补 LIMIT，实际 `Limit(0)/负数` 同样补位 |
| DOC-07 | 低 | 已修 | PluckMap/KeyBy 对结构体（指针）键、值的拒绝边界未完整写入文档 |
| DOC-08 | 低 | 已修 | ScanStruct 单行示例复用已读完的 rows，按示例写必然得到 ErrNoRows |
| DOC-09 | 低 | 已修 | query-exec.md 称时间列扫入 string 一律格式化为 RFC3339，未限定驱动返回 time.Time |
| DOC-10 | 低 | 已修 | mutate.md 概览称所有写操作返回受影响行数，InsertGetId 返回自增 ID |
| DOC-11 | 低 | 已修 | 字段规则未写「全部可写字段被跳过时返回 ErrNoFields」 |
| DOC-12 | 中 | 已修 | InsertOrIgnore 称只忽略唯一键冲突，MySQL/SQLite 的忽略范围更宽 |
| DOC-13 | 中 | 已修 | Upsert 称 uniqueBy 用于判定冲突，MySQL 下它不参与冲突目标 |
| DOC-14 | 低 | 已修 | updateColumns 排除后为空的退化形态（DO NOTHING / no-op）未写入 mutate.md |
| DOC-15 | 中 | 已修 | 列数静态校验把小括号内多列 SelectRaw 当成 1 列，文档承诺的「1:1 校验」不成立 |
| DOC-16 | 低 | 已修 | 称「PostgreSQL/SQLite 的任何写语句」带 ORDER BY/LIMIT 都会编译期拒绝，Insert 族与 Truncate 实际静默忽略 |
| DOC-17 | 低 | 已修 | 保护表把 Delete 的拒绝原因写成「无有效 WHERE/JOIN」，与下行「带 JOIN 报 ErrDeleteWithJoin」矛盾 |
| DOC-18 | 中 | 已修 | 保护规则称只认「依赖行数据」的条件，EXISTS 判定实为句法归约，非相关子查询也会放行 |
| DOC-19 | 低 | 已修 | compile.md 的 ToInsertUsing 小节未提 `ErrInsertUsingColumnMismatch` |
| DOC-20 | 中 | 已修 | compile.md 称 Expression「一律内嵌、可用于所有值位置」，Between/HavingBetween/JoinBuilder IN 实际走绑定 |
| DOC-21 | 低 | 已修 | schema.md 的 PG Columns SQL 块多出 `AS is_primary`，与实现及测试逐字不一致 |

## 逐条详情

### DOC-01：connection.md 把 Pluck 列入受列映射标签影响的「结构体映射」方法

- 级别：低；类型：文档错误
- 文档证据：`zcdb/docs/connection.md:104`
- 实现证据：`zcdb/builder_query.go:83-114`（Pluck 只把单列扫进切片元素，不经 `parseStruct`/`tagName`）、`zcdb/builder_query.go:192-208`（KeyBy 才按标签做整行映射）
- 测试证据：无（该清单没有测试断言；Pluck/KeyBy 的行为由各自集成用例锁定，但与「标签是否生效」无关）
- 实测：无需实测——Pluck 实现不引用 `tagName`，映射标签只影响 KeyBy/Find/First 等结构体映射入口
- 影响：使用自定义标签（`NewDBDao` 第四参数）的调用方会误以为 Pluck 也按标签映射结构体字段；实际 Pluck 只做单列扫描
- 推荐方案：从该清单删除 Pluck，保留 KeyBy（已按此修复）

### DOC-02：query-builder.md 缺少基方法 WhereNotBetweenColumns

- 级别：低；类型：关键行为遗漏
- 文档证据：`zcdb/docs/query-builder.md:193`
- 实现证据：`zcdb/builder_where.go:238-246`（公开方法 `WhereNotBetweenColumns`，编译为无绑定的两列 NOT BETWEEN）
- 测试证据：无（该方法没有文档级断言；编译形态与 `WhereBetweenColumns` 同族）
- 实测：无需实测——方法签名与 SQL 形态由 `zcdb/builder_where.go:238-246` 直接确定
- 影响：文档列出了 `OrWhereNotBetweenColumns` 却找不到对应的基方法，调用方只能猜或读源码
- 推荐方案：在 Or 变体说明处补基方法的存在与 SQL 形态（已按此修复）

### DOC-03：query-builder.md 的「占位符识别与引擎一致」未列已知例外

- 级别：低；类型：过度承诺
- 文档证据：`zcdb/docs/query-builder.md:208`
- 实现证据：`zcdb/sqlScan.go:8-9`（SQLite `[...]`、MySQL `/*! ... */`、PostgreSQL `U&'...'` 被当作代码区处理）；`zcdb/docs/compile.md:292` 已列同一例外清单
- 测试证据：`zcdb/cross_dialect_integration_test.go:1970`（扫描器计数与引擎参数个数的一致性用例，只覆盖已支持语法）
- 实测：无需实测——例外清单由 `zcdb/sqlScan.go:8-9` 明示，compile.md 也已声明
- 影响：在上述边缘语法里写 `?` 时，扫描器与引擎判定不同，绑定参数可能静默错位（如 SQLite `[...]` 内的 `?` 被当作占位符）
- 推荐方案：在 query-builder.md 补例外与 compile.md/sqlScan.go 的指引（已按此修复）

### DOC-04：JoinBuilder.Where 的绑定列未写 nil/Expression 特例

- 级别：低；类型：关键行为遗漏
- 文档证据：`zcdb/docs/query-builder.md:414`
- 实现证据：`zcdb/mysql_grammar.go:756-769`（nil 且 `=`/`!=`/`<>` 编译为 `IS [NOT] NULL`、Expression 直接内联）；`zcdb/builder_compile.go:1019-1027`（这两种情形不收集绑定）
- 测试证据：无（该表格「绑定」列没有断言；对应分支由 grammar 代码确定）
- 实测：无需实测——两条分支由 `zcdb/mysql_grammar.go:759-766` 直接确定
- 影响：按「值」推算 args 个数的调用方在 nil/Expression 条件时会多算一个，混排多个 ON 条件时绑定顺序与预期错位
- 推荐方案：在表格该行补「nil→IS [NOT] NULL、Expression→内联，两者不占绑定」（已按此修复）

### DOC-05：Builder.Clone 注释称 Paginate 内部用 Clone

- 级别：低；类型：代码注释与实现矛盾
- 文档证据：`zcdb/docs/query-builder.md:636`（文档正确：Paginate 不克隆）
- 实现证据：`zcdb/builder.go:160`（注释称「First/Value/Paginate/CursorBy 等终端方法内部即用 Clone」）；`zcdb/builder_query.go:296-299`（Paginate 直接 `Count`+`ToSelect`）；`zcdb/builder_compile.go:609-625`（ToCount 的临时状态保存/恢复）
- 测试证据：无（没有测试断言 Paginate 是否克隆；`zcdb/docs_deviation_review_test.go:834-839` 只断言分页结果）
- 实测：无需实测——Paginate 调用路径不含 `Clone`
- 影响：维护者据注释可能误判 Paginate 期间原 Builder 的 limit/order 不被读取，甚至反向「修」成克隆而改变语义
- 推荐方案：改注释为「First/Value/CursorBy 等内部克隆；Paginate 不克隆，经 ToCount 临时状态恢复」（已按此修复）

### DOC-06：Builder.Offset 注释的补 LIMIT 条件过期

- 级别：低；类型：代码注释与实现矛盾
- 文档证据：`zcdb/docs/query-builder.md:545`（文档已写「Limit 未调用或为 n<=0」）
- 实现证据：`zcdb/builder_order.go:55`（注释仍写「未设置 Limit」）；`zcdb/mysql_grammar.go:190` 与 `zcdb/sqlite_grammar.go:200` 的判定均为 `b.limit <= 0`
- 测试证据：`zcdb/builder_order_mysql_unit_test.go:43-51`（limit_unset / limit_zero / limit_negative 三情形）
- 实测：已有测试实测——三种情形都补出 `LIMIT 18446744073709551615`
- 影响：按注释把实现改回 `== 0` 会重新引入「只剩 OFFSET」的语法错误（MySQL 1064、SQLite near "20"）
- 推荐方案：注释改为「Limit 未设置或 <= 0」（已按此修复）

### DOC-07：PluckMap/KeyBy 的结构体键/值边界未完整文档化

- 级别：低；类型：关键行为遗漏
- 文档证据：`zcdb/docs/query-exec.md:138`、`zcdb/docs/README.md:163`、`zcdb/docs/README.md:164`
- 实现证据：`zcdb/builder_query.go:134-149`（PluckMap 拒绝结构体/结构体指针键与值）；`zcdb/builder_query.go:192-196`（KeyBy 拒绝结构体/结构体指针键）
- 测试证据：`zcdb/builder_unit_test.go:2235-2276`（PluckMap/KeyBy 的结构体键与结构体指针键用例）
- 实测：单元测试已锁定——`map[User]X`、`map[*User]X` 分别得到 `ErrPluckMapDest`/`ErrKeyByDest`
- 影响：`map[SomeStruct]V` 目标会被拒，但文档只描述值类型约束，调用方无法从错误表推断键的限制
- 推荐方案：query-exec.md 与 README 错误表补齐「键不能是结构体/结构体指针」（已按此修复）

### DOC-08：ScanStruct 单行示例复用了已读完的 rows

- 级别：低；类型：文档错误
- 文档证据：`zcdb/docs/query-exec.md:245-249`
- 实现证据：`zcdb/scanner.go:126-150`（`ScanStruct(&users)` 顺序消费 rows 至结束）；`zcdb/scanner.go:110-115`（再取不到行时返回 `sql.ErrNoRows`）
- 测试证据：无（示例逻辑无测试断言）
- 实测：无需实测——rows 已耗尽时 `Next()` 返回 false，单行扫描必然返回 `sql.ErrNoRows`
- 影响：照示例写「先扫切片再扫单行」会永远拿到 `ErrNoRows`，读者会误判为查询条件问题
- 推荐方案：示例改为重新执行一次 Query 再扫单行（已按此修复）

### DOC-09：时间列→string 的 RFC3339 格式化缺少驱动前提

- 级别：低；类型：过度承诺
- 文档证据：`zcdb/docs/query-exec.md:271`
- 实现证据：`zcdb/scanner.go:302-308`（仅当源值为 `time.Time` 时格式化为 RFC3339）；`zcdb/scanner.go:330-370`（源值为 []byte/string 时按原文本转换）
- 测试证据：`zcdb/scanner_test.go:379-383`（`time.Time → string` 应为 RFC3339）
- 实测：无需实测——分支条件由源码确定；MySQL 未开 `parseTime=true` 时驱动返回 []byte 属驱动契约
- 影响：未开 parseTime 的 MySQL 用户会得到 `2006-01-02 15:04:05` 原文本而非 RFC3339，与文档承诺不符
- 推荐方案：文档补「驱动以 time.Time 返回时（MySQL 需 parseTime=true）」（已按此修复）

### DOC-10：概览句称所有写操作返回受影响行数

- 级别：低；类型：文档内部矛盾
- 文档证据：`zcdb/docs/mutate.md:5` 与 `zcdb/docs/mutate.md:62-73`（InsertGetId 自称返回自增 ID）
- 实现证据：`zcdb/builder_exec.go:51-74`（InsertGetId 返回 `LastInsertId`）
- 测试证据：`zcdb/docs_deviation_review_test.go:644-650`（PG 下 InsertGetId 执行前报错，锁死该入口语义）
- 实测：无需实测——返回值由 `zcdb/builder_exec.go:61-70` 直接确定
- 影响：按概览把 InsertGetId 的 int64 当受影响行数做幂等/影响面判断会出错
- 推荐方案：概览区分受影响行数、自增 ID、只返回 error 三类（已按此修复）

### DOC-11：字段规则未写「全部字段被跳过 → ErrNoFields」

- 级别：低；类型：关键行为遗漏
- 文档证据：`zcdb/docs/mutate.md:34-35`
- 实现证据：`zcdb/reflect.go:339`、`zcdb/reflect.go:369`（可写列为空时返回 `ErrNoFields`）
- 测试证据：`zcdb/builder_exec_mysql_integration_test.go:123`（全 nil 指针插入返回 ErrNoFields）
- 实测：无需实测——分支由 `zcdb/reflect.go:339` 与 `zcdb/reflect.go:369` 直接确定
- 影响：主推的「nil 指针 = 部分更新」惯用法在全部字段为 nil 时直接报错，按文档会以为生成空 SET/静默无操作
- 推荐方案：字段规则补一句全跳过时返回 `ErrNoFields`（已按此修复）

### DOC-12：InsertOrIgnore 的忽略范围被写成只限唯一键冲突

- 级别：中；类型：过度承诺
- 文档证据：`zcdb/docs/mutate.md:77`
- 实现证据：`zcdb/mysql_grammar.go:271`（`INSERT IGNORE`）、`zcdb/sqlite_grammar.go:274`（`INSERT OR IGNORE`）、`zcdb/postgres_grammar.go:344-346`（`ON CONFLICT DO NOTHING`）
- 测试证据：无（引擎级忽略范围没有测试；本报告以探针实测）
- 实测：MySQL `VARCHAR(2)` 写 `abcdef` → `affected=1`、`err=nil`，回读 `ab`；SQLite NOT NULL 违例 → `affected=0`、`err=nil`，同数据普通 Insert 报 `constraint failed: NOT NULL constraint failed`
- 影响：读者以为只有唯一键冲突被忽略，实际 MySQL 可能静默写入被截断的脏数据、SQLite 会静默跳过其它约束违例
- 推荐方案：文档写明三方言忽略范围差异（已按此修复）

### DOC-13：Upsert 的 uniqueBy 在 MySQL 下不用于判定冲突

- 级别：中；类型：文档错误
- 文档证据：`zcdb/docs/mutate.md:94`
- 实现证据：`zcdb/mysql_grammar.go:305-375`（`ON DUPLICATE KEY UPDATE` 不使用 uniqueBy 生成冲突目标；uniqueBy 只用于 `zcdb/mysql_grammar.go:342-361` 的退化列选择与 `zcdb/builder_compile.go:251-263` 的默认更新列排除）
- 测试证据：`zcdb/docs_deviation_review_test.go:247-254`（三方言 Upsert SQL 形态）
- 实测：无需实测——MySQL 产物不含冲突目标，冲突判定由引擎按任意唯一键/主键完成
- 影响：读者会以为传入 uniqueBy 就限定了「只有该键冲突才更新」，实际主键或其它唯一键冲突同样触发 UPDATE
- 推荐方案：文档区分「PG/SQLite 用它生成冲突目标；MySQL 不参与冲突判定」（已按此修复）

### DOC-14：updateColumns 全被排除时的退化形态未写入 mutate.md

- 级别：低；类型：关键行为遗漏
- 文档证据：`zcdb/docs/mutate.md:95`
- 实现证据：`zcdb/builder_compile.go:251-263`（排除 uniqueBy 后可能为空）；`zcdb/postgres_grammar.go:392-396`、`zcdb/sqlite_grammar.go:351-355`（`DO NOTHING`）；`zcdb/mysql_grammar.go:340-361`（单列自赋值 no-op）
- 测试证据：`zcdb/docs_deviation_review_test.go:257-267`（Upsert 退化形态：PG DO NOTHING、MySQL no-op）
- 实测：已有测试实测——退化形态与 compile.md 的描述一致
- 影响：文档承诺「冲突时更新」，该形态下 PG/SQLite 变成「冲突时不更新」、MySQL 变成 no-op（0 行受影响）
- 推荐方案：mutate.md 补退化说明，与 compile.md 对齐（已按此修复）

### DOC-15：列数静态校验把多列 SelectRaw 当作 1 列

- 级别：中；类型：过度承诺
- 文档证据：`zcdb/docs/mutate.md:132-135`
- 实现证据：`zcdb/builder_compile.go:398-408`（按 `len(columns)+len(selectSubs)` 计数；`SelectRaw` 每次只追加一个 SelectColumn，见 `zcdb/builder_select.go:60-63`）
- 测试证据：`zcdb/docs_deviation_review_test.go:281-292`（列数不一致返回 ErrInsertUsingColumnMismatch）
- 实测：`SelectRaw("a, b")` 对 2 个目标列 → `ErrInsertUsingColumnMismatch`（消息含 `subquery select columns=1`）；`Select("a")+SelectRaw("b, c")` 对 1 个目标列计数为 2
- 影响：合法的多列原始投影被编译期误拒；反向（`SelectRaw` 多列 + 目标列数多于 1）可能漏报
- 推荐方案：文档补「单条 SelectRaw 按 1 列计」的例外并建议用 Select/AddSelect 逐列书写（已按此修复）；若要根治需在实现里把含逗号的 Raw 视为不可静态判定

### DOC-16：ORDER BY/LIMIT 的拒绝范围被写成「任何写语句」

- 级别：低；类型：过度承诺
- 文档证据：`zcdb/docs/mutate.md:167`、`zcdb/docs/mutate.md:174`
- 实现证据：`zcdb/builder_compile.go:32-47`（`validateWriteOrderLimit`）仅在 `zcdb/builder_compile.go:459`、`zcdb/builder_compile.go:524`、`zcdb/builder_compile.go:567`、`zcdb/builder_compile.go:866` 调用；Insert 族与 Truncate 不读取 orders/limit
- 测试证据：`zcdb/builder_compile_postgres_unit_test.go:107`（PG 写入口拒绝 ORDER BY/LIMIT，覆盖已接入校验的入口）
- 实测：无需实测——校验入口清单由调用点确定
- 影响：`.OrderBy(...).Limit(n).Insert(...)` 不会返回 `ErrWriteOrderLimitUnsupported`，而是静默忽略，与「任何写语句都拒绝」的承诺不符
- 推荐方案：文档把范围限定为 UPDATE/DELETE/DeleteJoin/Increment/Decrement，并注明 Insert 族与 Truncate 直接忽略（已按此修复）

### DOC-17：保护表把 Delete 的拒绝原因写成「无有效 WHERE/JOIN」

- 级别：低；类型：文档内部矛盾
- 文档证据：`zcdb/docs/mutate.md:262` 与 `zcdb/docs/mutate.md:263`、`zcdb/docs/mutate.md:271`
- 实现证据：`zcdb/builder_exec.go:282-292`（Delete 在 WHERE 判定前先判 `len(b.joins) > 0` → `ErrDeleteWithJoin`）
- 测试证据：`zcdb/docs_deviation_review_test.go:619-642`（保护判定矩阵）
- 实测：无需实测——`zcdb/builder_exec.go:285-287` 的判定顺序直接确定
- 影响：读者会认为 Delete 带 JOIN 即可通过保护，实际拿到另一个错误；也误以为 JOIN 是 Delete 的逃逸口
- 推荐方案：拆成 Delete（JOIN 不参与）与 DeleteJoin（WHERE/JOIN）两行（已按此修复）

### DOC-18：破坏性保护对 EXISTS 的判定是句法的

- 级别：中；类型：过度承诺
- 文档证据：`zcdb/docs/mutate.md:267`
- 实现证据：`zcdb/builder_exec.go:405-412`（EXISTS/NOT EXISTS 只检查内层归约是否 `truthUnknown`，不分析子查询是否引用外层列）
- 测试证据：无（现有保护矩阵 `zcdb/docs_deviation_review_test.go:619-642` 不含非相关 EXISTS 用例）
- 实测：非相关 EXISTS（内层 `WHERE id = 1`）+ 无外层 WHERE 的 Update → `affected=3`、`err=nil`；内层无 WHERE 的 EXISTS → `ErrUpdateWithoutWhere`
- 影响：文档把判定描述为「依赖行数据的条件才有效」，实际不依赖行数据的非相关 EXISTS 也会放行全表 Update/Delete，破坏性保护可被无意绕过
- 推荐方案：文档明确「句法判定、不做相关性分析」并给出关联子查询建议（已按此修复）；若要根治需在实现里做子查询列引用分析

### DOC-19：compile.md 的 ToInsertUsing 小节缺少列数校验错误

- 级别：低；类型：关键行为遗漏
- 文档证据：`zcdb/docs/compile.md:130`
- 实现证据：`zcdb/builder_compile.go:330-335`（列数可静态判定且不一致时返回 `ErrInsertUsingColumnMismatch`）
- 测试证据：`zcdb/docs_deviation_review_test.go:281-292`
- 实测：同一行为已在 DOC-15 的探针中复现
- 影响：只读 compile.md 的调用方会把该错误当成意外；影响有限，因为 mutate.md 与 README 已覆盖
- 推荐方案：本节补一句列数校验与链接（已按此修复）

### DOC-20：Expression「一律内嵌、可用于所有值位置」不成立

- 级别：中；类型：过度承诺
- 文档证据：`zcdb/docs/compile.md:211`、`zcdb/docs/compile.md:215`
- 实现证据：`zcdb/builder_compile.go:1077-1078`（Between/NotBetween 的 min/max 无条件收集绑定）、`zcdb/mysql_grammar.go:566-569`（`BETWEEN ? AND ?`）、`zcdb/builder_compile.go:1034-1035`（JoinBuilder IN 的元素直接进 args）
- 测试证据：无（Between/HavingBetween 传 Expression 没有测试）
- 实测：`WhereBetween("age", NewExpression("min_age"), 30)` → `BETWEEN ? AND ?`、args 含 Expression，执行报 `sql: converting argument $1 type: unsupported type zcdb.Expression, a struct`；`HavingBetween` 同型；`JoinBuilder.WhereIn` 的 Expression 元素进入 args
- 影响：按「所有值位置可用」在 Between 系列写 Expression，编译不报错、执行期才失败
- 推荐方案：文档把承诺收窄为实际支持清单，并注明列间区间用 `WhereBetweenColumns`/`WhereRaw`（已按此修复）

### DOC-21：schema.md 的 PG Columns SQL 字符串多出别名

- 级别：低；类型：文档错误
- 文档证据：`zcdb/docs/schema.md:92`
- 实现证据：`zcdb/postgres_schema.go:40`（EXISTS 子查询后没有 `AS is_primary`）
- 测试证据：`zcdb/cross_dialect_integration_test.go:646`（逐字锁死该 SQL，同样不含别名）
- 实测：无需实测——实现与测试的 SQL 文本直接可比
- 影响：文档声称是「底层执行的 SQL」，读者复制比对会看到不一致；功能无影响
- 推荐方案：删除文档中的 `AS is_primary`（已按此修复）

## 已核对且未发现偏差的关键行为

以下行为已逐条对照实现与测试（部分另经探针实测），修复其他条目时**不应顺手改动**：

- **注释规范化与生命周期**（`zcdb/docs/query-builder.md:24`、`zcdb/docs/README.md:71` ↔ `zcdb/builder.go:59-70`、`zcdb/db_dao.go:85-108`）：替换字符集、非法 UTF-8 → U+FFFD、255 rune 截断、空值清空不回退、Clone 保留，逐项一致。
- **注释只在最终编译出口追加一次**（`zcdb/docs/compile.md:15` ↔ `zcdb/builder_compile.go:13-18`）：15 个 `ToXxx` 入口；结构化子查询不泄漏注释；SQLite Truncate 仅主语句带注释。
- **方言差异总览表**（`zcdb/docs/README.md:40-53` ↔ 三方言 grammar）：标识符/占位符/共享锁/UNION+锁/UNION 括号/CROSS JOIN ON/RIGHT JOIN/Truncate/随机排序，MySQL/SQLite/PG 实现与实测一致；三方言括号嵌套 join 组实测均可执行。
- **UPSERT 三方言形态与退化**（`zcdb/docs/mutate.md:101-108`、`zcdb/docs/compile.md:126` ↔ `zcdb/mysql_grammar.go:305-375`、`zcdb/postgres_grammar.go:349-410`、`zcdb/sqlite_grammar.go:309-369`）：`ON DUPLICATE KEY` / `ON CONFLICT`、缺 uniqueBy 的方言差异、全部列为 uniqueBy 的退化，测试与实现一致。
- **破坏性保护的归约规则**（`zcdb/docs/mutate.md:257-271` ↔ `zcdb/builder_exec.go:358-455`）：AND/OR 优先级、空嵌套/空 Raw 不计、空 NOT IN 恒真、空 IN 恒假、`WhereRaw("1 = 1")` 逃生口、带 ON 的 JOIN 有效、Delete 带 JOIN 拒绝且 Force 不放行（EXISTS 的句法判定已在 DOC-18 单列）。
- **Cursor/CursorBy 分批规则**（`zcdb/docs/query-exec.md:189-224` ↔ `zcdb/builder_cursor.go:114-260`）：chunkSize 0 直接返回、负数取 100、`LIMIT chunkSize+1` 探测、忽略外层 ORDER BY、desc 条件与排序、三类游标错误，均有测试与实现对应。
- **ScanStruct 错误与转换语义**（`zcdb/docs/query-exec.md:273-283` ↔ `zcdb/scanner.go:75-150`、`zcdb/scanner.go:302-370`）：`ErrNotPointer`/`ErrScanDest`/`ErrInvalidStruct`/`ErrScanConvert`、NULL→零值/nil、数值溢出报错、time.Time→RFC3339，一致。
- **PG 的 HAVING 别名限制**（`zcdb/docs/query-builder.md:517`）：实测 PG 15 报 `42703 column "total" does not exist`，MySQL 允许，文档正确。
- **MySQL BINARY LIKE 与 SQLite GLOB 大小写**（`zcdb/docs/query-builder.md:283-301`）：实测 MySQL `BINARY 'Alice' LIKE 'ali%' = 0`、`LIKE 'Ali%' = 1`，SQLite `'Alice' GLOB 'ali*' = 0`，与文档一致。
- **CompileRaw 契约**（`zcdb/docs/compile.md:274-294` ↔ `zcdb/grammar.go:140-205`、`zcdb/rawLiteral.go:57-181`）：占位符数量/编号校验、只识别代码区、值内联表、错误位置，一致。
- **Schema 元数据 SQL 文本**（`zcdb/docs/schema.md:22-46`、`zcdb/docs/schema.md:69-102`、`zcdb/docs/schema.md:130-164` ↔ 三个 `*_schema.go`）：除 DOC-21 的 PG 别名外逐字一致；PG INCLUDE 过滤、SQLite `origin='pk'` 跳过、`#expr` 占位等说明与实现一致。
- **schema.md 的 Default nil 语义**（`zcdb/docs/schema.md:112` ↔ `zcdb/mysql_schema.go:52-61`）：实测 MySQL 8.4 `AUTO_INCREMENT` 列 `COLUMN_DEFAULT` 为 NULL，nil 语义成立。

## Go 示例覆盖

提取方式：`doc_go_examples.py --docs-dir zcdb/docs --strict`（基线快照与修复后工作区各运行一次，均为 93 个块、0 error）；判定只看 Go 语法/结构，不要求编译。

| 文档 | Go 块数 | 语法/结构 | 证据/原因 |
|---|---|---|---|
| `zcdb/docs/compile.md` | 15 | 通过 | 全部块 `parse=ok`，strict 0 error |
| `zcdb/docs/connection.md` | 12 | 通过 | 全部块 `parse=ok`，strict 0 error |
| `zcdb/docs/mutate.md` | 15 | 通过 | 全部块 `parse=ok`，strict 0 error |
| `zcdb/docs/query-builder.md` | 31 | 通过 | 全部块 `parse=ok`，strict 0 error |
| `zcdb/docs/query-exec.md` | 14 | 通过 | 全部块 `parse=ok`，strict 0 error（DOC-08 修复后仍通过） |
| `zcdb/docs/README.md` | 1 | 通过 | 快速上手完整程序解析通过，strict 0 error |
| `zcdb/docs/schema.md` | 5 | 通过 | 全部块 `parse=ok`，strict 0 error |

合计 93 个 `go` 块，全部通过 `--strict`；仓库门禁 `TestDocsGoBlockSyntax` 同样通过。

## 修复执行记录

21 条全部按「文档/注释改到与代码一致」处理，改动留在工作区，未提交。按文档归类（行号为修复后工作区）：

- **connection.md**（DOC-01）：从 `zcdb/docs/connection.md:104` 的结构体映射方法清单移除 Pluck。
- **query-builder.md**（DOC-02/03/04）：`zcdb/docs/query-builder.md:193` 补 `WhereNotBetweenColumns`；`zcdb/docs/query-builder.md:208` 补扫描器已知例外；`zcdb/docs/query-builder.md:414` 补 JoinBuilder.Where 的 nil/Expression 绑定特例。
- **源码注释**（DOC-05/06）：`zcdb/builder.go:160-161` 改为「First/Value/CursorBy 内部克隆；Paginate 不克隆」；`zcdb/builder_order.go:55` 的补位条件改为「Limit 未设置或 <=0」。
- **query-exec.md**（DOC-07/08/09）：`zcdb/docs/query-exec.md:138` 补结构体/结构体指针键值边界；`zcdb/docs/query-exec.md:245-253` 的单行扫描示例改为重新 Query；`zcdb/docs/query-exec.md:277` 补 `parseTime=true` 前提。
- **mutate.md**（DOC-10~18）：`zcdb/docs/mutate.md:5` 区分受影响行数/自增 ID/error；`zcdb/docs/mutate.md:36` 补全字段跳过时 `ErrNoFields`；`zcdb/docs/mutate.md:78` 补 InsertOrIgnore 三方言忽略范围；`zcdb/docs/mutate.md:95` 与 `zcdb/docs/mutate.md:96` 区分 MySQL 的 uniqueBy 语义与退化形态；`zcdb/docs/mutate.md:136` 补单条 SelectRaw 按 1 列计数；`zcdb/docs/mutate.md:168` 与 `zcdb/docs/mutate.md:175` 收窄 ORDER BY/LIMIT 拒绝范围，`zcdb/docs/mutate.md:177` 补 Insert 族/Truncate 忽略；`zcdb/docs/mutate.md:265` 与 `zcdb/docs/mutate.md:266` 拆分 Delete 与 DeleteJoin 的拒绝原因；`zcdb/docs/mutate.md:272` 补 EXISTS 句法判定与非相关子查询风险。
- **compile.md**（DOC-19/20）：`zcdb/docs/compile.md:130` 补列数校验错误；`zcdb/docs/compile.md:211` 与 `zcdb/docs/compile.md:215` 收窄 Expression 的适用位置。
- **schema.md**（DOC-21）：`zcdb/docs/schema.md:92` 删除 PG SQL 中的 `AS is_primary`。
- **历史报告导航**：在 `zcdb/docs/consistency-review.md:3` 顶部补一行指向本报告的后续审查说明，原内容与 DOC 编号保持不变。

## 实际验收结果

| 验收项 | 命令/做法 | 结果 |
|---|---|---|
| 空白与行尾 | `git diff --check` | 退出码 0（autocrlf 的 `LF will be replaced by CRLF` 是提示，不是失败） |
| 静态检查 | `go vet ./...` | 退出码 0 |
| 全仓测试 | `go test ./... -count=1` | 5 个模块全部 `ok`（zcdb 47.8s，本机 MySQL/PostgreSQL 容器在线，集成测试真实执行） |
| 文档锁定用例 | `go test ./zcdb -run 'TestDocsGoBlockSyntax\|TestDocReview\|DocsExamples' -count=1` | `ok` |
| 并发 | `go test ./zcdb -race -count=1` | `ok`（50.7s） |
| 乱序 | `go test ./zcdb -shuffle=on -count=2` | `ok`（84.7s） |
| 格式 | 对 `git diff --name-only -- '*.go'` 逐个剥 CR 后 `gofmt -l` | 无输出 |
| Go 示例 strict | `python <skill>/scripts/doc_go_examples.py --docs-dir zcdb/docs --strict` | 93 个块，0 error |
| 仓库示例门禁 | `go test ./zcdb -run '^TestDocsGoBlockSyntax$' -count=1` | PASS |
| 报告机械校验 | `validate_report.py --report ... --baseline 9cad987 --module zcdb --docs-dir zcdb/docs --require-examples --anchors` | `OK: 21 issue(s), 21 detail(s), 138 reference(s), 0 warning(s)` |

**结论**：21 条偏差全部修复，均为文档/源码注释修正（无行为变更）；上表全部通过。另记一条不属于用户文档的观察：`zcdb/schema_inspector_mysql_integration_test.go:112-114` 的注释称 `id` 默认值为 `''`，实测 MySQL 8.4 为 NULL，测试结果不受影响，未纳入本次修复范围。验收期间另一次全量测试出现过 `zcmodel` 失败，单包与随后全量重跑均通过，属并行会话并发改动期间的偶发失败，与本次 zcdb 文档修正无关。
