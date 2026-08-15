# zcdb 代码审查计划

## 1. 审查目标

- **SQL 安全**：标识符包裹、占位符绑定是否杜绝注入；Raw 系列方法的风险边界是否明确。
- **三方言编译正确性**：同一 Builder 状态在 MySQL / PostgreSQL / SQLite 下编译产物语义等价，方言差异点处理正确。
- **绑定参数顺序**：子查询、JOIN、WHERE、GROUP/HAVING、UNION 多来源绑定的合并顺序与占位符一一对应（PG 的 `$N` 尤其敏感）。
- **资源生命周期**：`*sql.Rows` 关闭、事务提交/回滚、连接池创建失败回滚、游标迭代中断释放。
- **并发安全**：连接池热加载从库、从库选择策略内部状态、结构体映射缓存。
- **破坏性操作保护**：无 WHERE 的 Update/Delete 拦截逻辑不可被绕过。

## 2. 审查范围与批次

模块共 26 个源文件（约 6900 行）+ 每方言的单元/集成测试，按依赖层次分 6 批：

| 批次 | 主题 | 文件 | 行数 |
|---|---|---|---|
| ① 基础设施 | 错误、表达式、子句模型、Grammar 接口 | `errors.go`、`expression.go`、`clause.go`、`grammar.go` | 218 |
| ② 构造层 | Builder 状态累积（链式 API） | `builder.go`、`builder_select.go`、`builder_where.go`、`builder_join.go`、`builder_group.go`、`builder_order.go`、`builder_query.go` | 2044 |
| ③ 编译层 | SQL 编译与三方言 Grammar | `builder_compile.go`、`mysql_grammar.go`、`postgres_grammar.go`、`sqlite_grammar.go` | 2953 |
| ④ 执行层 | 终端方法、游标、映射与扫描 | `builder_exec.go`、`builder_cursor.go`、`reflect.go`、`scanner.go` | 1148 |
| ⑤ 连接层 | 连接池、DAO、从库策略 | `pool.go`、`db_dao.go`、`slave_strategy.go` | 335 |
| ⑥ Schema 层 | 元数据查询 | `schema_inspector.go`、`mysql_schema.go`、`postgres_schema.go`、`sqlite_schema.go` | 188 |

> 建议 ①②③ 连续审查（编译正确性是本模块核心），④⑤ 其次，⑥ 独立性强可最后。

## 3. 文件级审查清单

### 批次①：基础设施

- [ ] `errors.go`：错误变量与 `docs/README.md` 错误速查表一一对应；均为哨兵错误可被 `errors.Is` 匹配。
- [ ] `expression.go`：`Expression` 直接内嵌 SQL——确认其只能由调用方显式构造（`NewExpression`），文档是否充分警示注入责任归调用方。
- [ ] `clause.go`：各子句结构体字段是否完整承载编译所需状态；Clone 时是否有字段被遗漏（对照 `Clone` 实现逐字段核对）。
- [ ] `grammar.go`：Grammar 接口方法集是否内聚；新增方言的扩展成本评估。

### 批次②：构造层（Builder 链式 API）

- [ ] `builder.go`：
    - `Clone` **深拷贝完整性**——列、子查询 Builder、JOIN（含嵌套组）、WHERE（含嵌套/切片值）、GROUP/HAVING、ORDER、UNION、锁与 force 标记逐项核对；漏拷贝任一 slice 会导致副本与原对象共享底层数组（append 相互污染）。
    - `Table`/`TableSub` 互斥语义：后调用覆盖前者时旧状态是否清理干净。
- [ ] `builder_where.go`（649 行，重点）：
    - 运算符白名单校验时机（构造期记录、编译期报 `ErrInvalidOperator`）；大小写归一化。
    - `Where("col", "=", nil)` → `IS NULL` 特判；`!=`/`<>` → `IS NOT NULL`；其它运算符遇 nil 的行为。
    - `WhereIn` 空切片 → `0 = 1`、`WhereNotIn` 空切片 → `1 = 1`；切片元素含 `Expression` / 子 Builder 的处理。
    - 嵌套组（`WhereNested`/`WhereNot`/`WhereAny`/`WhereNone`）：空回调组被忽略——该逻辑与"破坏性保护"联动（空组不得计为有效 WHERE）。
    - `WhereAny` 顶层 Boolean 覆写为 OR 的实现是否只影响顶层、不影响组内再嵌套。
    - `WhereExists`/`WhereSub`/`WhereInSub` 入参类型校验（回调 / `*Builder` / 非法类型 → `ErrInvalidSubQuery`）。
- [ ] `builder_join.go`：
    - `JoinBuilder` 条件方法与 Builder 的 Where 系语义对齐（IN 空切片、NULL 展开、Raw 绑定顺序）。
    - 嵌套 join 组（括号 join）递归编译的括号配对与 ON 归属。
    - `CrossJoinOn` 在 PG 下转 `INNER JOIN` 的等价性。
- [ ] `builder_group.go` / `builder_order.go`：
    - `OrderByRaw` 不包裹、不支持绑定——文档已警示注入风险，确认代码注释同样警示。
    - 方向参数仅 `DESC`（大小写不敏感）为降序，其余全部 ASC 的容错逻辑。
    - `Limit(n<=0)`/`Offset(n<=0)` 不输出子句；`ForPage` page<1 修正为 1。
- [ ] `builder_query.go` / `builder_select.go`：
    - `Select` 替换 vs `AddSelect` 追加去重（等价列判定标准）；`SelectSub` 绑定参数排序位置。
    - `Distinct` 与 `Count` 的联动（包裹子查询计数）。

### 批次③：编译层（核心，逐方言横向对照）

- [ ] `builder_compile.go`（765 行，最高优先级）：
    - **绑定参数总顺序**：SELECT 子查询 → FROM 子查询 → JOIN（派生表绑定先于 ON 值绑定）→ WHERE → GROUP BY Raw → HAVING → UNION 各查询，逐条对照编译输出的占位符位置。
    - `ToSelect`/`ToCount`/`ToInsert`/`ToUpdate`/`ToDelete` 各编译入口的前置校验（`ErrEmptyTable` 等）是否完备。
    - `ToCount` 对 UNION / GROUP BY / DISTINCT 包裹子查询计数的判定条件。
- [ ] 三方言 Grammar（逐项做**方言一致性矩阵**核对）：

  | 检查项 | MySQL | PostgreSQL | SQLite |
    |---|---|---|---|
  | 标识符包裹（含 `a.b` 点分、别名、`*`） | 反引号 | 双引号 | 双引号 |
  | 标识符内含包裹符本身的转义（`` a`b ``、`a"b`） | ☐ | ☐ | ☐ |
  | 占位符生成 | `?` | `$N` 计数连续无跳号 | `?` |
  | 共享锁 | `LOCK IN SHARE MODE` | `FOR SHARE` | 报 `ErrSQLiteLockNotSupported` |
  | UNION + 锁 | 允许 | 报 `ErrPgUnionLockNotSupported` | 报锁错误 |
  | Upsert | `ON DUPLICATE KEY`（uniqueBy 可省） | `ON CONFLICT`（缺 uniqueBy 报错） | 同 PG |
  | DeleteJoin | 多表 DELETE 直译 | `USING` | `IN (子查询)` |
  | Truncate | `TRUNCATE TABLE` | `TRUNCATE TABLE` | `DELETE FROM` + 清 `sqlite_sequence` |
  | WhereDate / WhereLike / NullSafe | `date()`/`BINARY`/`<=>` | `::date`/`ILIKE`/`IS DISTINCT FROM` | `strftime`/`GLOB`/`IS` |
  | 随机排序 | `RAND()` | `RANDOM()` | `RANDOM()` |

- [ ] **PG `$N` 占位符重编号**：WhereRaw / HavingRaw 等用户写 `?` 的片段转换为 `$N` 时序号是否与绑定数组严格对应；多来源合并后是否会出现重复或跳号。
- [ ] SQLite `Truncate` 清 `sqlite_sequence` 失败时"表从未用 AUTOINCREMENT 则忽略错误"的判定是否精确（不能吞掉其它错误）。

### 批次④：执行层

- [ ] `builder_exec.go`：
    - **破坏性保护**：`hasEffectiveWhere` 判定——空嵌套回调不算、无 ON 条件的 JOIN 不算；`Force()` 是显式唯一豁免通道；确认不存在其它绕过路径（如 WhereRaw 空字符串）。
    - `InsertGetId` 依赖 `LastInsertId`：**lib/pq 不支持 LastInsertId**（PG 需 `RETURNING`），确认 PG 下的行为（报错？文档说明？）——重点核实项。
    - Insert 批量以首行为模板：后续行缺列传 NULL 的实现；首行 nil 列被跳过后其余行该列有值的场景。
    - `Increment` extra 变参奇偶校验与列名类型校验（`ErrIncrementColumns`）。
    - `Paginate`：COUNT 前克隆并清 orders/limit/offset/columns；total==0 短路不查数据。
    - `First`/`Value` 内部克隆并附加 LIMIT 1，不污染原 Builder。
    - 聚合空集语义：Max/Min 返回 `sql.ErrNoRows`、Sum/Avg 返回 `(0, nil)` 的 NULL 归一化实现。
- [ ] `builder_cursor.go`：
    - `Cursor`：`iter.Seq` 中 break 提前退出时 `rows.Close` 是否必然执行（defer 位置）；`rows.Err()` 是否在迭代结束后检查。
    - `CursorBy`：每批独立查询间连接不持有；游标列值 NULL → `ErrCursorColumnNull`（防死循环）；字段查找失败 → `ErrCursorFieldNotFound`；`chunkSize==0` 直接返回、`<0` 用默认 100；强制覆盖用户 ORDER BY。
    - 批次末行游标值提取：嵌入结构体 / 指针字段（`ErrCursorFieldUnavailable`）路径。
- [ ] `reflect.go`：
    - 结构体映射缓存：缓存 key 是否包含**标签名**（不同 DAO 用不同标签时不得串缓存）；缓存并发读写安全（`sync.Map` 或等价机制）。
    - snake_case 转换、`db:"-"` 跳过、嵌入结构体展开、未导出字段跳过。
    - any 字段 nil 跳过列、指针 nil 跳过 / 非 nil 解引用、`Expression` 内嵌——Insert 与 Update 两处规则一致。
- [ ] `scanner.go`：
    - `ScanStruct`（不关 rows）与 `ScanStructClose`（自动关）职责边界；后者在 scan 出错时也必须关闭。
    - 列在结构体中无对应字段时忽略；NULL 扫描到值类型的处理（零值 or 报错，与文档核对）。
    - dest 类型分派：`*struct` / `*[]struct` / `*[]*struct`；`*struct` 无行返回 `sql.ErrNoRows`。

### 批次⑤：连接层

- [ ] `pool.go`：
    - `NewPool`：主库 Ping 失败返回错误；**任一从库失败整体回滚关闭**——确认已创建的 `*sql.DB` 全部 Close，无泄漏。
    - `AddSlave` 并发安全：slaves 切片的替换/追加是否加锁或 copy-on-write；与读路径 `PickReadDB` 的竞争。
    - 池参数默认值（50/50/600s）与文档一致；参数为 0 时的取默认逻辑。
- [ ] `slave_strategy.go`：
    - `RandomStrategy` / `RoundRobinStrategy` 的内部状态（计数器）并发安全（原子操作？）；`Pick` 返回 nil 时上层降级主库。
- [ ] `db_dao.go`：
    - `Transaction`：ctx 传播事务、嵌套检测复用、回调 error 回滚、**panic 时 defer 兜底回滚后是否重新抛出**。
    - 提交/回滚错误的处理（Commit 失败是否返回给调用方）。
    - 读写路由：带锁查询强制主库的判定位置；事务连接优先于主从路由。
    - 慢 SQL 回调：nil 时零开销（不 `time.Now()`）；回调 panic 是否会影响主流程（建议 recover，记录为改进项）。
    - `Close` 关闭主库与全部从库。

### 批次⑥：Schema 层

- [ ] 三方言 `Columns` 输出字段语义对齐（Name/Type/Comment/Nullable/Default）；SQLite `PRAGMA table_info` 的表名拼接是否安全（表名来自调用方，PRAGMA 无法用占位符——确认转义方式，防注入）。
- [ ] PG 查询过滤 `attisdropped`、`attnum > 0`；Default 为 `*string` nil 语义。
- [ ] `NewSchemaInspector` 非内置 Grammar 报错路径。

## 4. 专项审查

### 4.1 SQL 注入面梳理

- [ ] 列名/表名进入 SQL 的所有路径必经标识符包裹函数；列出**不包裹**的入口清单（`SelectRaw`/`WhereRaw`/`HavingRaw`/`GroupByRaw`/`OrderByRaw`/`Raw`/`Expression`），核对每处的文档警示。
- [ ] 值永远走绑定参数，唯一例外是 `Expression`——搜索编译层确认无字符串拼接值的遗漏路径。

### 4.2 并发与资源

- [ ] `go test -race` 全量（单元测试）；重点：并发 Builder 独立性（Builder 本身非并发安全是否已文档化）、Pool 并发读 + AddSlave。
- [ ] 泄漏排查：所有 `Query` 返回的 rows 在错误分支的 Close；事务两阶段的连接归还。

### 4.3 性能（非阻塞项）

- [ ] 编译热路径的字符串拼接方式（`strings.Builder` 预估容量）；反射缓存命中率；占位符生成的分配次数。

## 5. 测试审查

- [ ] 测试文件组织：`*_unit_test.go`（纯编译断言，无 DB）与 `*_integration_test.go`（真实 MySQL/PG/SQLite）——确认集成测试的**门控方式**（环境变量/DSN 可达性跳过），保证无数据库环境下 `go test ./zcdb/...` 不误报失败。
- [ ] 单元测试按"构造 → 期望 SQL + 期望 args"逐条断言：抽查每方言的 WHERE 嵌套、JOIN 派生表、UNION、Upsert 用例是否同时断言 SQL 与 args 顺序。
- [ ] 三方言测试对称性：每个 builder 特性在三套方言测试中均有对应用例（用文件名配对核查）。
- [ ] 集成测试覆盖：事务回滚/嵌套、读写路由（从库命中）、CursorBy 大数据分批、破坏性保护 Force。
- [ ] 执行：
    - 单元：`go test -race -count=1 -run 'Unit' ./zcdb/...`（按实际命名调整）。
    - 集成：配置三库 DSN 后全量执行一次并记录结果。

## 6. 文档一致性核对

- [ ] `docs/` 六篇文档中的每段示例 SQL 与实际编译输出抽样比对（每篇至少 5 例，覆盖三方言差异表）。
- [ ] 错误变量速查表、方言差异总览表逐行核对。
- [ ] `InsertGetId` 在 PG 下的行为如与文档不符（见批次④），同步修订文档。

## 7. 产出物与完成标准

- **问题清单**：`文件:行号` + 方言标注 + 复现 Builder 链 + 期望/实际 SQL + 严重级别。
- **方言一致性矩阵**（3.③ 表格）填写完成并归档。
- **严重级别**：
    - Blocker：SQL 注入、绑定参数错位、破坏性保护可绕过、连接/rows 泄漏、事务未回滚。
    - Major：方言编译语义偏差、Clone 漏字段、缓存串号、并发竞争。
    - Minor：错误信息、性能建议、回调缺 recover。
    - Nit：命名、注释、文档修订。
- **完成标准**：六批清单全部勾选；`-race` 单元测试通过；三方言集成测试通过；Blocker/Major 建立修复项。
