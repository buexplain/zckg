# zcdb 文档-实现一致性审查报告

- 审查对象：`zcdb` 模块（`zcdb/docs/` 下 7 份用户文档）
- 基线：HEAD `26744e5`（当次审查时 `zcdb` 目录工作区干净）
- 阶段：**修复阶段已完成并通过验收**。用户决策：DOC-01 改实现、DOC-02 哨兵 + 编译期校验、DOC-03 新增错误哨兵、DOC-08 启用 strict，其余按推荐方案
- 行号约定：第 2~5 节的行号指向**修复前基线** `26744e5`，仅用于追溯；第 6~7 节的行号指向当前工作区
- 复跑审查时请排除本文件自身：`--docs-dir zcdb/docs` 会自动排除 `consistency-review*`

## 审查范围与方法

### 文档清单与映射

| 文档 | 对照实现 | 锁死该文行为的测试 |
|---|---|---|
| `zcdb/docs/README.md` | `zcdb/builder.go`、`zcdb/builder_compile.go`、`zcdb/pool.go`、`zcdb/db_dao.go`、`zcdb/errors.go` | `zcdb/docs_deviation_review_test.go`、`zcdb/docs_examples_compile_test.go` |
| `zcdb/docs/connection.md` | `zcdb/pool.go`、`zcdb/db_dao.go`、`zcdb/slave_strategy.go`、`zcdb/builder_exec.go` | `zcdb/pool_unit_test.go`、`zcdb/slave_strategy_test.go`、`zcdb/docs_deviation_review_test.go` |
| `zcdb/docs/query-builder.md` | `zcdb/builder_select.go`、`zcdb/builder_where.go`、`zcdb/builder_join.go`、`zcdb/builder_group.go`、`zcdb/builder_order.go`、`zcdb/builder_compile.go`、三方言 grammar | `zcdb/builder_unit_test.go`、`zcdb/builder_where_*`、`zcdb/builder_join_*`、`zcdb/docs_deviation_review_test.go` |
| `zcdb/docs/query-exec.md` | `zcdb/builder_query.go`、`zcdb/builder_cursor.go`、`zcdb/scanner.go`、`zcdb/reflect.go` | `zcdb/builder_query_*`、`zcdb/builder_cursor_*`、`zcdb/scanner_test.go`、`zcdb/scanner_error_sentinel_sqlite_integration_test.go` |
| `zcdb/docs/mutate.md` | `zcdb/builder_exec.go`、`zcdb/builder_compile.go`、三方言 grammar | `zcdb/builder_exec_{mysql,postgres,sqlite}_integration_test.go`、`zcdb/builder_error_branches_unit_test.go` |
| `zcdb/docs/compile.md` | `zcdb/builder_compile.go`、`zcdb/grammar.go`、`zcdb/rawLiteral.go`、`zcdb/sqlScan.go`、`zcdb/expression.go` | `zcdb/builder_compile_{mysql,postgres,sqlite}_unit_test.go`、`zcdb/rawLiteral_test.go`、`zcdb/sqlScan_test.go` |
| `zcdb/docs/schema.md` | `zcdb/schema_inspector.go`、`zcdb/mysql_schema.go`、`zcdb/postgres_schema.go`、`zcdb/sqlite_schema.go` | `zcdb/schema_inspector_test.go`、`zcdb/schema_inspector_{mysql,postgres,sqlite}_integration_test.go` |

范围外的文档：仓库根 `README.md`（全仓说明）与根 `docs/` 不在本次模块审查范围，未逐条核对。

### 方法

1. **正向逐声明比对**：对 7 份文档中每条可验证声明（行为、默认值、错误变量、SQL/args、顺序、边界、示例）定位实现分支与锁死测试，记录 `文件:行`；对没有测试的声明单独标注。
2. **运行时实测**：用 `go test -overlay` 把临时 `_test.go` 映射进 `zcdb` 包（仓库零改动，目标路径不存在也可映射），每个探针单独 `-run '^TestProbe...$' -count=1` 运行；实测环境为 MySQL 8.4 / PostgreSQL 15 容器（在线，集成测试真实执行）+ modernc.org/sqlite 内存库。探针文件放在系统临时目录，审查结束已删除。
3. **反向审查**：用 `go doc -all ./zcdb` 列出全部导出 API，与 7 份文档互查，找「文档未提及但影响调用方」的项。
4. **Go 示例**：运行 `doc_go_examples.py --docs-dir zcdb/docs`（含 `--strict`），并另用一次性只读脚本按「完整文件 / 语句序列」两档复核每个 `go` 块（结果见第 5 节）。
5. 行号来源：基线 tree（`git show 26744e5:<path>`）；由于审查期 `zcdb` 无改动，基线行号与工作区一致。

### 实测过的行为（输入 → 观察结果）

- `Limit(-5).Offset(20)` → MySQL / SQLite 编译产物缺 LIMIT，执行分别报 `Error 1064` 与 `SQL logic error: near "20": syntax error`；`Limit(0)`、未调用 Limit 均正确补 LIMIT。
- `Where(...).OrderBy("id").Limit(10)` 上的 `Update`：MySQL 单表产物含 ORDER BY 与 LIMIT，按序只改 N 行；PG/SQLite 产物不含、实测 `Limit(1)` 仍改全部 3 行；MySQL 带 JOIN 时同样追加，执行报 `Error 1221`。
- `WhereRaw("name LIKE '%?%'")` 三方言原样保留；PG 代码区 `??` → 字面 `?` 且不占绑定；PG 单个 `?` → `$1`。
- 破坏性保护：空 `WhereNotIn`、空 `WhereIn`、`WhereRaw("1 = 1")`、纯空白 `WhereRaw`、普通条件 OR 空 `WhereNotIn` 嵌套组五组判定的实际结果与文档一致。
- 注释：`a*b/c\d` → `a b c d`；`/*x*/` → `x`；300 字符 → 255 rune；非法 UTF-8 → U+FFFD；结构化子查询注释不进入外层。
- `CompileRaw`：PG 重复 `$1`、`$1..$2` 空洞、最大编号小于参数数、无 `$N` 四组校验；MySQL 引号内 `?` 保留、`??` 计两个占位符；三方言 `string/bool/int/float/time/[]byte/nil/指针/Valuer` 内联产物与文档表逐项一致。
- `ToCount` 清锁、`ToExists` 的 UNION 包裹、HAVING 保留投影、`ToTruncate` 三方言注释位置。
- UPDATE JOIN 绑定顺序（MySQL：JOIN → SET → WHERE；PG/SQLite：SET → JOIN → WHERE）实测 args 顺序一致。
- 错误哨兵：`NewPool` 缺参、`Transaction` 失败、`InsertGetId`/PG 失败均**不能**被任何导出哨兵 `errors.Is` 命中。
- Expression 内嵌 `?` 在 PG 的 WHERE/IN/LIKE/NullSafe/HAVING/Raw 路径原样保留，在 UPDATE SET 与 JOIN ON 值路径被转成 `$N`。

### 问题统计与判定标准

- 问题总数 **8** 条：高 **0**、中 **2**、低 **6**。
- 计入：会产生非法 SQL、静默改变行为范围、文档承诺不成立、文档之间/文档与源码注释互相矛盾、公开入口无法按文档使用、示例无法机械校验。
- 不计入：纯排版、同义措辞、内部实现细节（不影响公开契约）、以及本报告在第 5 节单列的检查器包装限制。

## 问题汇总表

| 编号 | 级别 | 状态 | 一句话结论 |
|---|---|---|---|
| DOC-01 | 中 | 已修 | `Limit(n<=0)` 已置值时 Offset 不再补 LIMIT，MySQL/SQLite 生成语法错误 SQL |
| DOC-02 | 中 | 已修 | Update/Delete/Increment 的 OrderBy/Limit 仅 MySQL 单表生效；MySQL 多表(JOIN) 报 1221，PG/SQLite 静默丢弃 |
| DOC-03 | 低 | 已修 | README 称「所有错误均为可导出 errors 变量」，连接/事务/InsertGetId 错误实为临时 error |
| DOC-04 | 低 | 已修 | Expression 的 `?` 边界清单不完整：漏多个内嵌路径，且 JOIN ON 值也会转 `$N` |
| DOC-05 | 低 | 已修 | README 错误表把 ErrNotPointer/ErrNotStruct/ErrInvalidStruct 只归给游标/写入，与 query-exec.md 矛盾 |
| DOC-06 | 低 | 已修 | `Builder.Primary` 注释称 Paginate 内部克隆，与 query-builder.md 及实现（不克隆）矛盾 |
| DOC-07 | 低 | 已修 | 纯编译路径缺 Grammar 的获取方式，三个 `NewXxxGrammar` 构造函数从未在文档出现 |
| DOC-08 | 低 | 已修 | 文档 Go 示例无清单/自动校验，92/94 为语句片段，`--strict` 门禁不可用 |

## 逐条详情

### DOC-01：Limit(n<=0) 已置值时 Offset 不再补 LIMIT，MySQL/SQLite 生成非法 SQL

- 级别：中；类型：实现缺陷
- 文档证据：`zcdb/docs/query-builder.md:491` 声明 `n <= 0` 时不输出 LIMIT、`perPage <= 0` 等同不分页；`zcdb/docs/query-builder.md:493` 声明「仅设置 Offset(n)（n > 0）而未设置 Limit 时」MySQL 补 `LIMIT 18446744073709551615`、SQLite 补 `LIMIT -1`。
- 实现证据：`zcdb/mysql_grammar.go:187-194` 与 `zcdb/sqlite_grammar.go:197-204` 的补 LIMIT 条件都是 `b.limit == 0`；而 `Limit(n)` 原样保存任意整数（`zcdb/builder_order.go:50-53`），所以 `Limit(-5)` 虽按文档「不输出 LIMIT」，却让 `b.limit != 0`，补 LIMIT 分支被跳过。
- 测试证据：`zcdb/builder_unit_test.go:1579` 与 `zcdb/builder_unit_test.go:1581` 只覆盖「从未调用 Limit」的 offset-only，没有 `Limit(n<0)+Offset` 用例。
- 实测（临时 overlay 探针，MySQL 8.4 与 modernc SQLite 真实执行）：`Limit(-5).Offset(20)` 的 MySQL 产物为缺 LIMIT 的 `SELECT ... OFFSET 20`，执行报 `Error 1064 (42000): You have an error in your SQL syntax ... near '20 ...'`；同链 SQLite 执行报 `SQL logic error: near "20": syntax error (1)`；对照组 `Limit(0)+Offset(20)` 与未调用 Limit 的 `Offset(20)` 都正确补出 LIMIT。
- 影响：调用方按文档把 0/负值 Limit 当作「不限制行数」再配 Offset 时，MySQL/SQLite 在编译阶段无错、执行期才报语法错误；PostgreSQL 不报错，形成跨方言割裂。
- 推荐方案：首选改实现——两处判断改为 `b.limit <= 0`（PG 不需要补），并在 `builder_order_mysql_unit_test.go`、`builder_order_sqlite_unit_test.go` 增加 `Limit(-1)+Offset(20)` 与 `Limit(0)+Offset(20)` 的编译断言。若不改实现，则文档必须写明「Limit(n<=0) 一旦调用就不再补 LIMIT，勿与 Offset 组合」，但该形态仍产出非法 SQL，故不推荐。

### DOC-02：Update/Delete/Increment 上的 OrderBy/Limit 仅 MySQL 单表生效

- 级别：中；类型：关键行为遗漏
- 文档证据：`zcdb/docs/mutate.md:122`（Update 小节）与 `zcdb/docs/mutate.md:171`（Delete/DeleteJoin 小节）只描述 WHERE/JOIN/Force 语义，未提 OrderBy/Limit；`zcdb/docs/compile.md:125` 的 ToUpdate 小节用表格列出 MySQL 与 PG/SQLite 的 SQL 结构差异，也没有把 ORDER BY/LIMIT 列入差异项。
- 实现证据：MySQL 的 `CompileUpdate` 输出 ORDER BY 与 LIMIT（`zcdb/mysql_grammar.go:451-472`），`CompileDelete` 同样输出（`zcdb/mysql_grammar.go:514-535`）；PostgreSQL 明确不输出（注释见 `zcdb/postgres_grammar.go:438-439`），SQLite 同样不输出（注释见 `zcdb/sqlite_grammar.go:409`）。`Increment`/`Decrement` 复用 `CompileUpdate`，因此行为相同；`CompileDeleteJoin` 三方言都不输出这两个子句（`zcdb/mysql_grammar.go:479-499`、`zcdb/postgres_grammar.go:527-565`、`zcdb/sqlite_grammar.go:493-510`）。
- 测试证据：`zcdb/builder_exec_mysql_integration_test.go:529-542` 只锁死 MySQL「仅更新前 N 行」；`zcdb/builder_compile_mysql_unit_test.go:103` 只锁死 MySQL DELETE 的 ORDER BY+LIMIT；PG/SQLite 没有对应用例，也没有「被忽略」的断言。
- 实测（临时 overlay 探针 + 三方言真实引擎）：MySQL 8.4 单表 `UPDATE ... ORDER BY id DESC LIMIT 2` 被接受、`affected=2` 且确定性只改 id 4/5；不带 ORDER BY 的 `UPDATE ... LIMIT 1` 也接受（改哪行由引擎决定，实测改 id 1）；`DELETE ... ORDER BY id DESC LIMIT 2` 同样接受并删除 id 4/5。MySQL **多表** UPDATE（带 JOIN）加 ORDER BY 或 LIMIT 一律报 `Error 1221 (HY000): Incorrect usage of UPDATE and ORDER BY` / `... and LIMIT`（只加 ORDER BY 也报错），而 zcdb 的 MySQL `CompileUpdate` 在 JOIN 后照常追加，实测 `Update(JOIN+OrderBy+Limit)` 返回同一 1221；多表 DELETE 加 ORDER BY/LIMIT 是 1064 语法错误。PostgreSQL 15 对该语法直接报 `syntax error at or near "ORDER"` / `"LIMIT"`（42601）；SQLite 3.53.2（modernc）的 `PRAGMA compile_options` 不含 `SQLITE_ENABLE_UPDATE_DELETE_LIMIT`，同样报语法错误。zcdb 在 PG/SQLite 丢弃这两个子句，实测 `Limit(1)` 的 Update 返回 `affected=3`（全部命中行被改）。`Increment` 复用同一 `CompileUpdate`：单表 + ORDER BY/LIMIT 实测正常（只自增排序第一行），带 JOIN 时实测同样报 1221。另外 `ToDeleteJoin` 三方言都不输出这两个子句（MySQL 多表 DELETE 本就不支持），DeleteJoin 上设置它们等于无效。
- 影响：调用方把「按序取前 N 行更新/删除」当作跨方言能力会踩两个坑：MySQL 单表确实按序只改/删 N 行；PG/SQLite 静默执行全量命中行（实测 3 行全改）；MySQL 带 JOIN 的 Update/Increment 把子句编译进去后执行期才报 1221（Decrement 走同一编译路径）；DeleteJoin 上则任何方言都不生效。三种结果都没有编译期提示。
- 推荐方案：三选一并写清取舍——(A) 只补文档：在 `zcdb/docs/mutate.md` 与 `zcdb/docs/compile.md` 的 ToUpdate/ToDelete 处列出矩阵「MySQL 仅单表生效、多表(JOIN) 报 1221；PG/SQLite 无此语法，子句被忽略等于全量命中行；DeleteJoin 一律无效」，并补 PG/SQLite/MySQL 多表三组锁死用例（成本最低、不破坏兼容）；(B) 显式拒绝：PG/SQLite 的写操作携带 OrderBy/Limit、MySQL 多表 UPDATE 携带 OrderBy/Limit 时，编译期返回哨兵错误（符合「显式优于隐式」，但需新增哨兵并按 semver 评估，属行为变更）；(C) 统一剥离：写操作编译器一律不输出这两个子句，跨方言一致，但会让现有 MySQL 单表批量更新静默变成全量，最不推荐。建议先做 (A)；(B) 若要做，应与「写操作支持批量」这一新能力的提案一起设计。

### DOC-03：README「所有错误均为可导出的 errors 变量」不成立

- 级别：低；类型：过度承诺
- 文档证据：`zcdb/docs/README.md:146` 写「所有错误均为可导出的 `errors` 变量，可用 `errors.Is` 精确匹配」。
- 实现证据：连接池缺参错误是临时创建（`zcdb/pool.go:54`、`zcdb/pool.go:57`），事务开启失败同样（`zcdb/db_dao.go:234`），提交/回滚失败为 `fmt.Errorf` 包装（`zcdb/db_dao.go:264`），`InsertGetId` 的 PG 限制错误是每次新建的 `errors.New`（`zcdb/builder_exec.go:65`）。这些错误都不是包级哨兵变量，无法用 `errors.Is` 与任何导出变量匹配。
- 测试证据：`zcdb/docs_deviation_review_test.go:652` 对 `NewPool` 缺参只断言 `err.Error()` 字符串，等于承认它不是哨兵。
- 实测（临时 overlay 探针）：`NewPool(PoolConfig{})` → `err="zcdb: DriverName is required"`，`errors.Is(err, ErrDialectRequired)=false`、`errors.Is(err, ErrPoolRequired)=false`；关闭池后 `Transaction` → `"zcdb: begin transaction failed: sql: database is closed"`，无哨兵；PG 上 `InsertGetId` → 内联错误，`errors.Is` 两个导出哨兵均为 false。
- 影响：调用方按文档用 `errors.Is` 分支处理连接/事务/方言限制错误会漏判，只能退化为字符串匹配。
- 推荐方案：把该句收窄为「下表中的错误是可导出哨兵，可用 `errors.Is` 匹配；连接/事务等运行时错误为带 `zcdb:` 前缀的描述性错误」。若希望这三类也可判定，再新增 `ErrInsertGetIdUnsupported` 等哨兵（新增公开 API，需按 MINOR 处理），属于可选增强。

### DOC-04：Expression 的 `?` 边界清单不完整且有一处错误

- 级别：低；类型：文档错误
- 文档证据：`zcdb/docs/compile.md:205` 的「占位符边界」段称「INSERT VALUES 与 Raw 子句中 `?` 会原样保留（PostgreSQL 方言下成为非法占位符），PostgreSQL 的 UPDATE SET 路径还会把 `?` 转为 `$N` 并占用占位符编号」。
- 实现证据：PG 下还有多处直接内嵌 Expression 而不转换 `?`：WHERE 基本比较（`zcdb/postgres_grammar.go:698-699`）、WHERE IN、LIKE、NULL 安全比较、HAVING、GROUP BY Raw；而 JOIN ON 的**值比较**会把 `?` 转成 `$N`（`zcdb/postgres_grammar.go:793-794`），不只 UPDATE SET（`zcdb/postgres_grammar.go:457-458`）。同样的清单不完整也出现在源码注释 `zcdb/expression.go:9-13`。
- 测试证据：`zcdb/docs_deviation_review_test.go:325-330` 只验证「不含 `?` 的 Expression 内嵌」，全仓无 Expression 含 `?` 的用例。
- 实测（临时 overlay 探针，PG）：`Where("id","=",NewExpression("a ? b"))` → `WHERE "id" = a ? b`（args 为空）；`WhereIn`、`WhereLike`、`WhereNullSafeEquals`、`Having`、`WhereRaw` 同样保留 `?`；`JoinOn(...).Where("orders.amt",">",expr)` → `... > a $1 b`（args 仍为空，$1 无配对参数）；`ToUpdate` → `SET "v" = a $1 b`。
- 影响：文档给出的是「边界」清单，读者会以为只有 INSERT/Raw 与 UPDATE SET 受影响；实际在 WHERE/HAVING/LIKE/IN 等常见位置写 `?` 同样产出非法 SQL，在 JOIN ON 值处还会凭空占用一个 `$N`。
- 推荐方案：把该段改成按「原样保留（所有值内嵌位置：INSERT VALUES、WHERE/HAVING/LIKE/IN/NULL 安全比较、Raw 绑定、CompileRaw 参数）」与「转成 `$N`（PostgreSQL 的 UPDATE SET 与 JOIN ON 值比较）」两类完整列举，并同步修正 `zcdb/expression.go:9-13` 的注释；无需改实现（该行为是刻意的「逃逸口」约束）。

### DOC-05：README 错误表与 query-exec.md 错误语义表互相矛盾

- 级别：低；类型：文档内部矛盾
- 文档证据：`zcdb/docs/README.md:151` 把 `ErrInvalidStruct` 的触发场景写成「Insert/Update 传入了非结构体类型」；`zcdb/docs/README.md:172` 把 `ErrNotPointer` / `ErrNotStruct` 写成「游标迭代目标不是结构体指针」。
- 实现证据：`ScanStruct` 对非指针 dest 返回 `ErrNotPointer`（`zcdb/scanner.go:78-80`），对非法切片元素返回 `ErrInvalidStruct`（`zcdb/scanner.go:132-134`）；`InsertGetId` 传入非空切片也返回 `ErrInvalidStruct`（`zcdb/builder_exec.go:54-56`）。
- 测试证据：`zcdb/scanner_error_sentinel_sqlite_integration_test.go:13-60` 锁死 ScanStruct 的 `ErrNotPointer` / `ErrScanDest` / `ErrInvalidStruct` 三个哨兵。
- 实测：无需额外实测——`zcdb/docs/query-exec.md:244-246` 的「错误语义」表已把 `ErrNotPointer`、`ErrScanDest`、`ErrInvalidStruct` 列为 ScanStruct 可能返回的错误，与 README 的归属直接冲突；测试文件逐条断言可复现。
- 影响：调用方只看 README 会以为 `ScanStruct` 不会返回这些错误，错误分支写不全；维护者也容易在改 README 时把语义写偏。
- 推荐方案：README 表格的「触发场景」补全为跨入口描述（`ErrNotPointer`：ScanStruct/Cursor/CursorBy 的 dest 不是指针；`ErrInvalidStruct`：Insert/Update/InsertGetId 数据非法，或 ScanStruct 切片元素非法），或改为「详见 query-exec.md 错误语义」，只保留一句话索引，避免两处重复维护。

### DOC-06：Builder.Primary 注释与文档关于 Paginate 的描述矛盾

- 级别：低；类型：代码注释与文档矛盾
- 文档证据：`zcdb/docs/query-builder.md:572` 明确「First/Value/CursorBy 等终端方法内部用 Clone 避免污染调用方的 Builder；Paginate 不克隆，直接复用原 Builder」。
- 实现证据：`zcdb/builder.go:144` 的 `Primary` 注释写「First/Value/Paginate 等内部克隆的终端方法同样生效」；而 `Paginate` 的实现是直接调用 `b.Count` 与 `b.ToSelect`（`zcdb/builder_query.go:249-252`），不做 Clone。
- 测试证据：无（现有 `zcdb/docs_deviation_review_test.go:832-837` 只断言 Paginate 的 total/数据行数，没有断言「是否克隆」）。
- 实测：无需实测——行为由 `zcdb/builder_query.go:249-252` 的调用路径直接确定，`Primary` 标记本身在 Paginate 上依然生效（不经克隆也用得上），注释错在「内部克隆」这一描述。
- 影响：注释会误导维护者以为 Paginate 会克隆 Builder（例如据此推断 Paginate 不会改动原 Builder 的 limit/offset），与文档和实现相反；属于四者一致性中对「注释」的违约。
- 推荐方案：把 `zcdb/builder.go:144` 改为「该标记经 Clone 保留；First/Value 等内部克隆的终端方法与不克隆的 Paginate 均生效」，与 `zcdb/docs/query-builder.md:572` 的措辞对齐。

### DOC-07：纯编译路径缺少 Grammar 的获取方式

- 级别：低；类型：关键行为遗漏
- 文档证据：`zcdb/docs/query-builder.md:22` 给出「`NewBuilder(grammar, nil)` 可纯编译，初始无注释」；但全部 7 份文档中只有这一处提到 `NewBuilder`，从未出现任何构造 Grammar 的入口。
- 实现证据：无 DAO 时取得 Grammar 的唯一途径是导出的构造函数 `NewMySQLGrammar`（`zcdb/mysql_grammar.go:11`）、`NewPostgresGrammar`（`zcdb/postgres_grammar.go:65`）、`NewSQLiteGrammar`（`zcdb/sqlite_grammar.go:21`）；另一个来源是 `DBDao.Grammar()`，但文档把 `NewBuilder(..., nil)` 定位为「独立构造器」，此时调用方通常还没有 DAO。
- 测试证据：无（`zcdb/docs_deviation_review_test.go:19-22` 用 `&MySQLGrammar{}` 等字面量做纯编译，绕开了文档入口问题，说明内部知道怎么拿，但用户文档没写）。
- 实测：在 `zcdb/docs` 全量检索 `NewMySQLGrammar` / `NewPostgresGrammar` / `NewSQLiteGrammar` 零命中，仅 `NewBuilder` 1 处；照文档无法在无数据库场景下构造出 `NewBuilder` 的第一个参数（除非改读源码或 `go doc`）。
- 影响：想「只编译不执行」（打印 SQL、单元测试、交给别的组件执行，正是 `zcdb/docs/compile.md:3-7` 列出的场景）的调用方必须自行发现构造函数；文档能力声明不可自洽。
- 推荐方案：在 `zcdb/docs/compile.md` 或 `zcdb/docs/query-builder.md` 的 Comment 小节补一行示例：`b := zcdb.NewBuilder(zcdb.NewMySQLGrammar(), nil)`，并列出三方言构造函数与 `db.Grammar()` 两种取得方式（后者不追加 DAO 默认注释，与现有 `zcdb/docs/connection.md:75` 说明一致）。

### DOC-08：文档 Go 示例缺少清单与自动校验

- 级别：低；类型：示例可验证性缺失
- 文档证据：`zcdb/docs/README.md:72` 的快速上手是完整 `package main` 文件；其余示例绝大多数是语句片段（如 `zcdb/docs/query-builder.md:12`），仅 `zcdb/docs/mutate.md:113` 是纯注释块，语法正确但不是可独立解析的 Go 文件。
- 实现证据：`zcdb/docs_examples_compile_test.go:6` 自述「本文件不会自动读取 Markdown；新增/修改示例须显式同步」，即示例与文档之间没有任何自动比对。
- 测试证据：无（仓库内没有从 Markdown 重新提取示例的测试；`doc_go_examples.py` 的 `--strict` 也不能作为门禁，见下）。
- 实测：`doc_go_examples.py --docs-dir zcdb/docs` 共提取 94 个 `go` 块，`--strict` 报 93 条 issue（唯一通过的是 `zcdb/docs/mutate.md:113` 的纯注释块）。逐块复核后：整文件 1 个（README 快速上手，单独 `gofmt -e` 通过）、声明列表 1 个（同上的纯注释块）、语句片段 92 个（包进 `func example()` 后 `gofmt -e` 全部通过）、不可解析 0 个、`...`/`{ ... }` 占位符 0 个。即严格模式的失败全部来自检查器包装方式（只加 `package p`，既不能包语句、也不能处理自带的 `package main`），不是示例语法错误。
- 影响：示例本身目前是好的，但没有任何机制在 API 变更时强制更新 `docs_examples_compile_test.go`——文档里改了调用而编译测试没跟上时，测试仍然全绿；同时 `--strict` 无法作为 CI 门禁，后续审查只能重复人工复核。
- 推荐方案：保留片段风格（可读性优先，不建议把 92 个块批量包进 `func example()`），新增 `zcdb/docs/docs-examples.json` 清单（记录块 ID 与内容哈希）+ 一个测试从 Markdown 重新提取并比对清单，块增删/内容变化时测试失败，提示同步 `docs_examples_compile_test.go`；若维护者更希望直接启用 `doc_go_examples.py --strict` 门禁，则需把全部块改写为 `func example()` 形态，成本与可读性取舍由维护者决定。

## 已核对且未发现偏差的关键行为

以下行为已逐条对照实现与测试（部分另经探针实测），修复其他条目时**不应顺手改动**：

- **方言差异总览表 10 行**（`zcdb/docs/README.md:40-51`）：标识符/占位符/共享锁/UNION+锁/UNION 括号/CROSS JOIN ON/RIGHT JOIN/Truncate/随机排序，与三方言实现及实测产物一致（MySQL 8.4 还实测确认 `UNION ... FOR UPDATE` 可执行）。
- **注释规范化**（`zcdb/docs/query-builder.md:20` ↔ `zcdb/builder.go:59-70`）：`*`、`/`、`\`、控制字符替换、非法 UTF-8 → U+FFFD、255 rune 截断、清空不回退默认值，实测符合。
- **注释只在最终编译出口追加一次**（`zcdb/docs/compile.md:13-18` ↔ `zcdb/builder_compile.go:13-18`）：15 个 `ToXxx` 全部命中；结构化子查询不泄漏注释；SQLite Truncate 仅主语句带注释（`zcdb/builder_exec.go:529-545`）。
- **破坏性操作保护**（`zcdb/docs/mutate.md:223-229` ↔ `zcdb/builder_exec.go:353-427`）：AND/OR 归约、空 WhereNotIn 恒真、空 WhereIn 恒假、空 Raw 跳过、`WhereRaw("1 = 1")` 逃生口、JOIN ON 计入、普通 Delete 拒绝 JOIN，五组探针结果与文档一致。
- **SELECT 绑定顺序** `SELECT_SUB → FROM_SUB → JOIN → WHERE → GROUP BY → HAVING → UNION`（`zcdb/docs/compile.md:175-181` ↔ `zcdb/builder_compile.go:855-888`）：全链路探针 args 顺序一致。
- **UPDATE 方言绑定顺序**（`zcdb/docs/compile.md:127-132` ↔ `zcdb/builder_compile.go:450-460`）：MySQL JOIN→SET→WHERE、PG/SQLite SET→JOIN→WHERE，实测 args 分别为 `[paid n 3]` 与 `[n paid 3]`。
- **DeleteJoin 三方言形态与绑定顺序**（`zcdb/docs/mutate.md:183-196`、`zcdb/docs/compile.md:155-160`）：与 `zcdb/builder_compile.go:519-548` 及各 grammar 一致。
- **CompileRaw 契约**（`zcdb/docs/compile.md:237-254` ↔ `zcdb/grammar.go:140-205`、`zcdb/rawLiteral.go:57-147`）：数量/编号校验、PG 允许重复编号与空洞、只识别代码区、`??` 计数、值内联表，逐项实测一致。
- **聚合空集语义**（`zcdb/docs/query-exec.md:78-83` ↔ `zcdb/builder_query.go:379-390`）：Max/Min → `(0, sql.ErrNoRows)`，Sum/Avg → `(0, nil)`。
- **CursorBy 分批规则**（`zcdb/docs/query-exec.md:181-182` ↔ `zcdb/builder_cursor.go:114-120`、`zcdb/builder_cursor.go:171-183`）：chunkSize 0 直接返回、负数取 100、`LIMIT chunkSize+1` 探测行，已有集成测试锁死。
- **Pluck 三模式与覆盖 SELECT**（`zcdb/docs/query-exec.md:85-118` ↔ `zcdb/builder_query.go:92-238`）：切片/keyBy 需 1 列、标量 map 需 2 列、keyBy 追加键列，错误哨兵 `ErrPluckDest`/`ErrPluckColumns` 一致。
- **ScanStruct 转换语义**（`zcdb/docs/query-exec.md:236` ↔ `zcdb/scanner.go:193-423`）：位宽溢出报 `ErrScanConvert`、NULL → 零值/nil、time → RFC3339，均有实现分支与测试。
- **事务所有权与嵌套复用**（`zcdb/docs/connection.md:116` ↔ `zcdb/db_dao.go:29-38`、`zcdb/db_dao.go:223-229`）：跨 Pool 返回 `ErrTxOwnerMismatch`，同 Pool 复用。
- **Schema 元数据 SQL 文本**（`zcdb/docs/schema.md:29-46`、`zcdb/docs/schema.md:69-94`、`zcdb/docs/schema.md:133-154` ↔ 三个 `*_schema.go`）：逐字一致；`#expr` 占位、`origin='pk'` 跳过、INCLUDE 过滤、Nullable 的 rowid 局限等说明与实现一致。
- **内嵌结构体展开**（`zcdb/docs/README.md:9`、`zcdb/docs/query-exec.md:199` ↔ `zcdb/reflect.go:63-142`）：外层优先、浅层遮蔽、同深度取声明序在前，与实现 BFS 语义一致。

## Go 示例覆盖

提取方式：`doc_go_examples.py --docs-dir zcdb/docs`（块 ID 与内容哈希由脚本生成）；语法判定另用只读脚本复核——先按完整文件解析，失败再包进 `func example()` 解析。判定只看语法/结构，不要求编译。

| 块 ID | 位置 | 语法/结构 | 证据/原因 |
|---|---|---|---|
| `compile.md#ToSelect#1` | `zcdb/docs/compile.md:28` | 语句片段 | 语句序列可解析（非完整文件） |
| `compile.md#ToCount#1` | `zcdb/docs/compile.md:36` | 语句片段 | 语句序列可解析（非完整文件） |
| `compile.md#ToCount#2` | `zcdb/docs/compile.md:44` | 语句片段 | 语句序列可解析（非完整文件） |
| `compile.md#ToExists#1` | `zcdb/docs/compile.md:56` | 语句片段 | 语句序列可解析（非完整文件） |
| `compile.md#ToAggregate#1` | `zcdb/docs/compile.md:66` | 语句片段 | 语句序列可解析（非完整文件） |
| `compile.md#ToInsert#1` | `zcdb/docs/compile.md:78` | 语句片段 | 语句序列可解析（非完整文件） |
| `compile.md#ToInsertOrIgnore#1` | `zcdb/docs/compile.md:91` | 语句片段 | 语句序列可解析（非完整文件） |
| `compile.md#ToUpsert#1` | `zcdb/docs/compile.md:100` | 语句片段 | 语句序列可解析（非完整文件） |
| `compile.md#ToInsertUsing-ToInsertOrIgnoreUsing#1` | `zcdb/docs/compile.md:114` | 语句片段 | 语句序列可解析（非完整文件） |
| `compile.md#ToUpdate#1` | `zcdb/docs/compile.md:134` | 语句片段 | 语句序列可解析（非完整文件） |
| `compile.md#ToDelete-ToDeleteJoin-ToTruncate#1` | `zcdb/docs/compile.md:142` | 语句片段 | 语句序列可解析（非完整文件） |
| `compile.md#ToIncrement-ToDecrement#1` | `zcdb/docs/compile.md:166` | 语句片段 | 语句序列可解析（非完整文件） |
| `compile.md#Expression-原始表达式#1` | `zcdb/docs/compile.md:189` | 语句片段 | 语句序列可解析（非完整文件） |
| `compile.md#原始-SQL-内联编译-CompileRaw#1` | `zcdb/docs/compile.md:211` | 语句片段 | 语句序列可解析（非完整文件） |
| `compile.md#原始-SQL-内联编译-CompileRaw#2` | `zcdb/docs/compile.md:228` | 语句片段 | 语句序列可解析（非完整文件） |
| `connection.md#创建连接池#1` | `zcdb/docs/connection.md:9` | 语句片段 | 语句序列可解析（非完整文件） |
| `connection.md#创建连接池#2` | `zcdb/docs/connection.md:33` | 语句片段 | 语句序列可解析（非完整文件） |
| `connection.md#创建连接池#3` | `zcdb/docs/connection.md:44` | 语句片段 | 语句序列可解析（非完整文件） |
| `connection.md#创建-DAO#1` | `zcdb/docs/connection.md:58` | 语句片段 | 语句序列可解析（非完整文件） |
| `connection.md#自定义列映射标签#1` | `zcdb/docs/connection.md:81` | 语句片段 | 语句序列可解析（非完整文件） |
| `connection.md#自定义列映射标签#2` | `zcdb/docs/connection.md:97` | 语句片段 | 语句序列可解析（非完整文件） |
| `connection.md#事务#1` | `zcdb/docs/connection.md:122` | 语句片段 | 语句序列可解析（非完整文件） |
| `connection.md#事务#2` | `zcdb/docs/connection.md:147` | 语句片段 | 语句序列可解析（非完整文件） |
| `connection.md#原始-SQL#1` | `zcdb/docs/connection.md:160` | 语句片段 | 语句序列可解析（非完整文件） |
| `connection.md#原始-SQL#2` | `zcdb/docs/connection.md:181` | 语句片段 | 语句序列可解析（非完整文件） |
| `connection.md#慢-SQL-回调#1` | `zcdb/docs/connection.md:192` | 语句片段 | 语句序列可解析（非完整文件） |
| `connection.md#连接探活#1` | `zcdb/docs/connection.md:207` | 语句片段 | 语句序列可解析（非完整文件） |
| `mutate.md#Insert#1` | `zcdb/docs/mutate.md:13` | 语句片段 | 语句序列可解析（非完整文件） |
| `mutate.md#字段映射与值处理规则#1` | `zcdb/docs/mutate.md:35` | 语句片段 | 语句序列可解析（非完整文件） |
| `mutate.md#InsertGetId#1` | `zcdb/docs/mutate.md:60` | 语句片段 | 语句序列可解析（非完整文件） |
| `mutate.md#InsertOrIgnore#1` | `zcdb/docs/mutate.md:71` | 语句片段 | 语句序列可解析（非完整文件） |
| `mutate.md#Upsert#1` | `zcdb/docs/mutate.md:87` | 语句片段 | 语句序列可解析（非完整文件） |
| `mutate.md#InsertUsing-InsertOrIgnoreUsing#1` | `zcdb/docs/mutate.md:102` | 语句片段 | 语句序列可解析（非完整文件） |
| `mutate.md#InsertUsing-InsertOrIgnoreUsing#2` | `zcdb/docs/mutate.md:113` | 声明列表 | 仅注释行，声明列表为空但可解析 |
| `mutate.md#Update#1` | `zcdb/docs/mutate.md:126` | 语句片段 | 语句序列可解析（非完整文件） |
| `mutate.md#Update#2` | `zcdb/docs/mutate.md:136` | 语句片段 | 语句序列可解析（非完整文件） |
| `mutate.md#Increment-Decrement#1` | `zcdb/docs/mutate.md:150` | 语句片段 | 语句序列可解析（非完整文件） |
| `mutate.md#Increment-Decrement#2` | `zcdb/docs/mutate.md:162` | 语句片段 | 语句序列可解析（非完整文件） |
| `mutate.md#Delete-DeleteJoin#1` | `zcdb/docs/mutate.md:173` | 语句片段 | 语句序列可解析（非完整文件） |
| `mutate.md#Delete-DeleteJoin#2` | `zcdb/docs/mutate.md:183` | 语句片段 | 语句序列可解析（非完整文件） |
| `mutate.md#Truncate#1` | `zcdb/docs/mutate.md:202` | 语句片段 | 语句序列可解析（非完整文件） |
| `mutate.md#破坏性操作保护#1` | `zcdb/docs/mutate.md:233` | 语句片段 | 语句序列可解析（非完整文件） |
| `mutate.md#破坏性操作保护#2` | `zcdb/docs/mutate.md:243` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#Comment-短业务标识#1` | `zcdb/docs/query-builder.md:11` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#Table-TableSub#1` | `zcdb/docs/query-builder.md:34` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#Select-AddSelect-SelectRaw-SelectSub#1` | `zcdb/docs/query-builder.md:55` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#Distinct#1` | `zcdb/docs/query-builder.md:73` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#Where-OrWhere-基本比较#1` | `zcdb/docs/query-builder.md:88` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#Where-OrWhere-基本比较#2` | `zcdb/docs/query-builder.md:107` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#WhereIn-WhereNotIn#1` | `zcdb/docs/query-builder.md:118` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#WhereNull-WhereNotNull#1` | `zcdb/docs/query-builder.md:134` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#WhereBetween-系列-区间比较#1` | `zcdb/docs/query-builder.md:146` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#WhereRaw-OrWhereRaw#1` | `zcdb/docs/query-builder.md:179` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#WhereColumn-两列比较#1` | `zcdb/docs/query-builder.md:192` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#嵌套逻辑组-WhereNested-WhereNot-WhereAll-WhereAny-WhereNone#1` | `zcdb/docs/query-builder.md:201` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#WhereDate-日期比较-方言分支#1` | `zcdb/docs/query-builder.md:246` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#WhereLike-系列-模糊匹配-方言分支#1` | `zcdb/docs/query-builder.md:257` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#WhereNullSafe-系列-空安全比较-方言分支#1` | `zcdb/docs/query-builder.md:280` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#子查询条件-WhereExists-WhereSub-WhereInSub#1` | `zcdb/docs/query-builder.md:296` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#单条件简写-Join-LeftJoin-RightJoin-CrossJoin#1` | `zcdb/docs/query-builder.md:325` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#单条件简写-Join-LeftJoin-RightJoin-CrossJoin#2` | `zcdb/docs/query-builder.md:342` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#多条件回调-JoinOn-LeftJoinOn-RightJoinOn#1` | `zcdb/docs/query-builder.md:353` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#多条件回调-JoinOn-LeftJoinOn-RightJoinOn#2` | `zcdb/docs/query-builder.md:381` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#嵌套-join-组-括号-join#1` | `zcdb/docs/query-builder.md:399` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#派生表-JOIN-JoinSub-LeftJoinSub-RightJoinSub-CrossJoinSub#1` | `zcdb/docs/query-builder.md:412` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#GroupBy-GroupByRaw#1` | `zcdb/docs/query-builder.md:433` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#Having-系列#1` | `zcdb/docs/query-builder.md:451` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#OrderBy-OrderByRaw-InRandomOrder#1` | `zcdb/docs/query-builder.md:475` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#Limit-Offset-ForPage#1` | `zcdb/docs/query-builder.md:495` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#UNION#1` | `zcdb/docs/query-builder.md:507` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#行锁#1` | `zcdb/docs/query-builder.md:522` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#行锁#2` | `zcdb/docs/query-builder.md:538` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#Primary-强制主库读#1` | `zcdb/docs/query-builder.md:549` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-builder.md#Clone-查询复用#1` | `zcdb/docs/query-builder.md:561` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-exec.md#Find-First-Value#1` | `zcdb/docs/query-exec.md:21` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-exec.md#Find-First-Value#2` | `zcdb/docs/query-exec.md:43` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-exec.md#Count-Exists#1` | `zcdb/docs/query-exec.md:52` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-exec.md#Max-Min-Sum-Avg#1` | `zcdb/docs/query-exec.md:69` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-exec.md#Pluck#1` | `zcdb/docs/query-exec.md:91` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-exec.md#Pluck#2` | `zcdb/docs/query-exec.md:100` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-exec.md#Pluck#3` | `zcdb/docs/query-exec.md:108` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-exec.md#Paginate#1` | `zcdb/docs/query-exec.md:124` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-exec.md#Cursor-流式迭代#1` | `zcdb/docs/query-exec.md:146` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-exec.md#CursorBy-游标分批迭代#1` | `zcdb/docs/query-exec.md:164` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-exec.md#CursorBy-游标分批迭代#2` | `zcdb/docs/query-exec.md:186` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-exec.md#ScanStruct-ScanStructClose#1` | `zcdb/docs/query-exec.md:201` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-exec.md#ScanStruct-ScanStructClose#2` | `zcdb/docs/query-exec.md:222` | 语句片段 | 语句序列可解析（非完整文件） |
| `query-exec.md#错误语义#1` | `zcdb/docs/query-exec.md:252` | 语句片段 | 语句序列可解析（非完整文件） |
| `README.md#快速上手#1` | `zcdb/docs/README.md:71` | 整文件 | 完整 Go 文件（含 `package main`）解析通过 |
| `schema.md#_#1` | `zcdb/docs/schema.md:7` | 语句片段 | 语句序列可解析（非完整文件） |
| `schema.md#Tables-查询所有用户表#1` | `zcdb/docs/schema.md:20` | 语句片段 | 语句序列可解析（非完整文件） |
| `schema.md#Columns-查询表的字段信息#1` | `zcdb/docs/schema.md:59` | 语句片段 | 语句序列可解析（非完整文件） |
| `schema.md#Indexes-查询表的索引信息#1` | `zcdb/docs/schema.md:124` | 语句片段 | 语句序列可解析（非完整文件） |
| `schema.md#完整示例#1` | `zcdb/docs/schema.md:191` | 语句片段 | 语句序列可解析（非完整文件） |

合计 94 个块：整文件 1、声明列表 1（纯注释）、语句片段 92、不可解析 0、占位符 0。`doc_go_examples.py --strict` 报 93 条 issue，全部是包装器限制（详见 DOC-08），不是示例语法错误。

> 以上为**修复前快照**（基线 `26744e5`）；DOC-08 修复后文档 Go 块统一包装为 `func example()`（README 快速上手去掉 `package` 子句并加说明），另有 1 个纯 SQL 注释块改为 `sql` 围栏，当前 93 个 Go 块全部满足 `--strict`（见第 6 节）。

## 修复执行记录

按用户决策执行，8 条全部修复（改动留在工作区，**未提交**）：

### DOC-01（改实现）

- 改动：`zcdb/mysql_grammar.go:190`、`zcdb/sqlite_grammar.go:200` 的补 LIMIT 判断由 `b.limit == 0` 改为 `b.limit <= 0`；`zcdb/docs/query-builder.md:545` 同步为「Limit 未调用或 `n <= 0`」。
- 回归：`zcdb/builder_order_mysql_unit_test.go:43`、`zcdb/builder_order_sqlite_unit_test.go:41` 覆盖 Limit 未调用 / 0 / 负数三种情形，断言补位为 `LIMIT 18446744073709551615` / `LIMIT -1`。

### DOC-02（哨兵 + 编译期校验）

- 新增哨兵 `ErrWriteOrderLimitUnsupported`（`zcdb/errors.go:105`）。
- 新增 `hasWriteOrderLimit` / `validateWriteOrderLimit`（`zcdb/builder_compile.go:22`、`zcdb/builder_compile.go:32`），在 `ToUpdate`、`ToDelete`、`ToDeleteJoin`、`ToIncrement`/`ToDecrement` 四个编译入口调用（`zcdb/builder_compile.go:459`、`zcdb/builder_compile.go:524`、`zcdb/builder_compile.go:567`、`zcdb/builder_compile.go:866`）：MySQL 单表放行；MySQL 多表 UPDATE/DeleteJoin、PostgreSQL/SQLite 写语句在编译期拒绝。
- 共享注释夹具的 PG/SQLite delete 场景改用不带 ORDER BY/LIMIT 的 Builder（`zcdb/builder_unit_test.go:2336`），MySQL 单表 DELETE 的 ORDER BY/LIMIT 注释覆盖保持不变。
- 回归：`zcdb/builder_compile_mysql_unit_test.go:257`（单表放行 + 多表/DeleteJoin 拒绝）、`zcdb/builder_compile_postgres_unit_test.go:107`、`zcdb/builder_compile_sqlite_unit_test.go:175`（五种写入口全部拒绝）。
- 文档：支持矩阵（`zcdb/docs/mutate.md:165`）、编译入口说明（`zcdb/docs/compile.md:154`）、错误表（`zcdb/docs/README.md:166`）。

### DOC-03（新增错误哨兵）

- 新增 `ErrDriverNameRequired` / `ErrDSNRequired` / `ErrPoolClosed` / `ErrPoolConnect` / `ErrTxBegin` / `ErrTxCommit` / `ErrTxRollback` / `ErrInsertGetIdUnsupported`（`zcdb/errors.go:26`、`zcdb/errors.go:41` 及同组声明）。
- `zcdb/pool.go:77` 起连接失败统一用 `%w` 包装 `ErrPoolConnect`，配置缺失直接返回哨兵、导出 `ErrPoolClosed`；`zcdb/db_dao.go:234`、`zcdb/db_dao.go:254`、`zcdb/db_dao.go:265` 三个事务错误改为哨兵包装（回滚路径的回滚错误与回调原始错误都用 `%w`）；`zcdb/builder_exec.go:65` 用哨兵前缀包装，消息与旧文案逐字一致。
- 回归：`zcdb/pool_unit_test.go:420`（配置/连接/关闭五情形）、`zcdb/db_dao_test.go:123`（Begin 哨兵）、`zcdb/fault_injection_test.go:28`（新增 `failRollback`）及 rollback 子测试、`zcdb/builder_compile_postgres_unit_test.go:167`（InsertGetId 哨兵）。
- 文档：错误表改为「自身哨兵 + 驱动错误透传」并补 8 个新行（`zcdb/docs/README.md:148`、`zcdb/docs/README.md:186`）、事务说明（`zcdb/docs/connection.md:174`）、InsertGetId 限制（`zcdb/docs/mutate.md:73`）。

### DOC-04（文档 + 注释同步）

- `zcdb/docs/compile.md:235` 改为「原样保留 / 转 `$N`」两类完整列举，并注明 MySQL/SQLite 无 `$N` 转换；`zcdb/expression.go:9` 注释同步。
- 回归锁死：三方言各新增 `ExpressionPlaceholderBoundary`（12 条路径 × 3 方言，见 `zcdb/builder_compile_mysql_unit_test.go:304`、`zcdb/builder_compile_postgres_unit_test.go:185`、`zcdb/builder_compile_sqlite_unit_test.go:234`）；已用「把 PG WHERE 的 Expression 改为转 `$N`」变异确认该用例会变红。

### DOC-05（README 错误表跨入口补全）

- `zcdb/docs/README.md:153`（`ErrInvalidStruct`：Insert/Update/InsertGetId + ScanStruct 切片元素）、`zcdb/docs/README.md:175`（`ErrNotPointer`/`ErrNotStruct`：ScanStruct/Cursor/CursorBy）。

### DOC-06（注释修正）

- `zcdb/builder.go:144` 改为「First/Value 等内部克隆的终端方法与不克隆的 Paginate 均生效」，与文档及实现一致。

### DOC-07（补 Grammar 获取方式）

- `zcdb/docs/query-builder.md:26` 补 `NewMySQLGrammar` / `NewPostgresGrammar` / `NewSQLiteGrammar` 与 `db.Grammar()` 两种来源。

### DOC-08（启用 strict）

- 7 份文档共 93 个 `go` 块统一包装为 `func example()`，每份文档补一行示例约定（如 `zcdb/docs/query-builder.md:7`）；README 快速上手去掉 `package` 子句并补说明（`zcdb/docs/README.md:14`）；纯 SQL 注释块改为 `sql` 围栏（`zcdb/docs/mutate.md:129`）。
- 新增仓库门禁 `TestDocsGoBlockSyntax`（`zcdb/docs_examples_fence_test.go:28`），提取规则与 `doc_go_examples.py --strict` 等价（`package p` + 块内容解析 + 占位符检查），并用 `minDocsGoBlocks`（`zcdb/docs_examples_fence_test.go:19`）防止提取规则失效导致零提取假通过。

### 补充加固（审查后追加）

- 哨兵与错误表一致性：新增 `TestDocReview_ErrorTableMatchesExportedSentinels`（`zcdb/docs_deviation_review_test.go:913`），用 `go/ast` 提取 `errors.go` 的导出 `Err*` 变量，与 README「错误变量速查」表首列比对，两侧集合必须相等；新增哨兵漏写表、表里残留已删除/改名的哨兵都会失败。已用「删除 `ErrPoolClosed` 行」变异确认会报红。
- Expression `?`边界回归：见 DOC-04 的补充说明（三方言 12 路径，已变异验证会红）。

## 实际验收结果

| 验收项 | 命令/做法 | 结果 |
|---|---|---|
| 空白与行尾 | `git diff --check` | 退出码 0；autocrlf 的 `LF will be replaced by CRLF` 是提示，不算失败 |
| 构建 | `go build ./...` | 退出码 0 |
| 定向回归 | `go test ./zcdb -run 'TestMySQLGrammar_WriteOrderLimit\|TestPostgresGrammar_WriteOrderLimitRejected\|TestSQLiteGrammar_WriteOrderLimitRejected\|TestMySQLGrammar_OffsetWithoutLimit\|TestSQLiteGrammar_OffsetWithoutLimit\|TestPool_ErrorSentinels\|TestDBDao_TransactionBeginSentinel\|TestFaultInjection_QueryExecPrepareBegin\|TestPostgresGrammar_InsertGetIdUnsupported\|TestDocsGoBlockSyntax' -count=1` | `ok`（3.552s） |
| 全量测试 | `go test ./zcdb/... -count=1 -v` | `ok ... 46.262s`；0 FAIL，全部 PASS；仅 2 个 SKIP，均为 `ZCDB_ENGINE_FUZZ` 外部引擎 fuzz 门控，不是数据库不可达 |
| 静态检查 | `go vet ./...` | 退出码 0 |
| 格式 | 对 20 个改动的 `.go` 文件执行 `gofmt -l` | 17 个 LF 文件无输出；`errors.go`/`expression.go`/`sqlite_grammar.go` 是既有 CRLF 误报，剥 `\r` 后复核无输出 |
| 并发 | `go test ./zcdb -race -count=1` | `ok ... 47.177s` |
| 乱序 | `go test ./zcdb -shuffle=on -count=5` | `ok ... 170.129s` |
| 覆盖率 | `go test ./zcdb -cover -count=1` | `coverage: 98.8% of statements` |
| 全仓回归 | `go test ./... -count=1` | 5 个模块全部 `ok`（zcconfig / zcdb / zchttp / zcmodel / zcquit） |
| Go 示例 strict | `python <skill>/scripts/doc_go_examples.py --docs-dir zcdb/docs --strict` | 93 个块，0 error（修复前为 94 块 / 93 issue） |
| 仓库示例门禁 | `go test ./zcdb -run '^TestDocsGoBlockSyntax$' -count=1` | PASS |
| 哨兵 ↔ README 一致性 | `go test ./zcdb -run '^TestDocReview_ErrorTableMatchesExportedSentinels$' -count=1` | PASS（删除任一行会 FAIL，已变异验证） |
| Expression `?` 边界 | `go test ./zcdb -run 'ExpressionPlaceholderBoundary' -count=1 -v` | 3 方言 × 12 路径全部 PASS（改坏 PG WHERE 会 FAIL，已变异验证） |

**结论**：8 条问题全部修复并通过上述验收，另追加两组回归（哨兵↔README 一致性、Expression `?` 边界）；DOC-01/02/03 的实现与测试为行为变更，DOC-08 的文档示例形态为可读性取舍（统一 `func example()` 包装）；改动尚未提交，提交分组与提交信息待确认。
