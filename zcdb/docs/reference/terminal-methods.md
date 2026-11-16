# 终端方法语义矩阵

终端方法是「编译 + 执行 + 扫描」的最后一步。它们读同一份 Builder 状态，但在**目标容器、结果基数、空结果语义、是否克隆、是否改状态、SQL 条数**上各不相同。这张表是横向对比入口；每个方法的完整签名、示例与边界见 [builder-query](builder-query.md) 与 [builder-cursor](builder-cursor.md)。

表中 SQL 以 MySQL 形态给出；PostgreSQL/SQLite 仅标识符与占位符变化（见 [dialect-matrix](dialect-matrix.md)）。

## 矩阵

| 方法 | 目标 / 返回 | 结果基数 | 空结果语义 | 是否 Clone | 是否改 Builder 状态 | SQL 形态 | 逐行 / SQL 条数 |
|---|---|---|---|---|---|---|---|
| `Find` | `dest *[]struct` 或 `*[]*struct`，返回 `error` | 0..N 行 | 不写入任何元素，`err == nil`（`dest` 保持调用前内容） | 否 | 否 | `SELECT <列> FROM ...`（Builder 状态原样，不附加 LIMIT/OFFSET） | 逐行扫描；1 条 |
| `First` | `dest *struct`，返回 `error` | 0..1 行 | `sql.ErrNoRows` | 按需：`limit == 1` 时复用原对象，否则 `Clone` | 否（只改副本的 `limit`） | `SELECT <列> FROM ... LIMIT 1`（保留 offset/排序/锁/注释） | 扫描首行；1 条 |
| `Value` | 标量指针（`*string`、`*int64`…）、二级指针或 `sql.Scanner`，返回 `error` | 0..1 行 1 列 | `sql.ErrNoRows` | 按需（同 `First`） | 否 | `SELECT <列> FROM ... LIMIT 1` | `Scan` 一次；1 条 |
| `Count` | `(int, error)` | 标量 | `(0, nil)`（SQL 恒返回一行；驱动返回空行集时也是 0） | 否 | 是：`ToCount` 经 `saveTransientState` 临时清除 `limit`/`offset`/`orders`/`lockClause`/`columns`/`selectSubs`，`defer` 恢复 | `SELECT COUNT(*) FROM ...`；带 UNION、GROUP BY、HAVING 或 DISTINCT 时包裹为 `SELECT COUNT(*) FROM (<子查询>) AS t` | 读一行；1 条 |
| `Exists` | `(bool, error)` | 布尔 | `(false, nil)` | 否 | 是：`ToExists` 临时清除分页/排序/锁；UNION/HAVING 时保留原投影 | `SELECT 1 FROM ... LIMIT 1`；UNION/HAVING 时 `SELECT 1 FROM (<子查询>) AS t LIMIT 1` | 读一行即停；1 条 |
| `Max` | `(float64, error)` | 标量 | `(0, sql.ErrNoRows)`（聚合结果为 NULL） | 否 | 是：`ToAggregate` 临时替换列为聚合表达式并清除分页/排序/锁/`selectSubs` | `SELECT MAX(<col>) AS aggregate FROM ...`；UNION 时包裹子查询后聚合 | 读一行；1 条 |
| `Min` | `(float64, error)` | 标量 | `(0, sql.ErrNoRows)` | 否 | 是（同 `Max`） | `SELECT MIN(<col>) AS aggregate FROM ...` | 读一行；1 条 |
| `Sum` | `(float64, error)` | 标量 | `(0, nil)`（空集聚合的 NULL 归一为 0） | 否 | 是（同 `Max`） | `SELECT SUM(<col>) AS aggregate FROM ...` | 读一行；1 条 |
| `Avg` | `(float64, error)` | 标量 | `(0, nil)`（同 `Sum`） | 否 | 是（同 `Max`） | `SELECT AVG(<col>) AS aggregate FROM ...` | 读一行；1 条 |
| `Pluck` | `dest *[]T`（T 为标量），`column string`，返回 `error` | 0..N 行 1 列 | 不追加元素，`err == nil` | 否 | **永久**覆盖 `columns`（只查该列） | `SELECT <column> FROM ...` | 逐行追加；1 条 |
| `PluckMap` | `dest *map[K]V`（键/值均非结构体），两个列名，返回 `error` | 0..N 行 2 列 | nil map 被初始化为空 map，`err == nil` | 否 | **永久**覆盖 `columns`（键列、值列） | `SELECT <key>, <value> FROM ...` | 逐行 `SetMapIndex`；1 条 |
| `KeyBy` | `dest *map[K]V`（V 为结构体或结构体指针），`keyColumn string`，返回 `error` | 0..N 行 | nil map 被初始化为空 map，`err == nil` | 否 | **永久**覆盖 `columns`（结构体字段列 + 键列，键列已在字段中时不重复） | `SELECT <结构体字段列...> FROM ...` | 逐行扫描整行；1 条 |
| `Paginate` | `dest *[]struct` 或 `*[]*struct`，返回 `(int, error)` | 总数 + 0..N 行 | 总数为 0 时返回 `(0, nil)`，且**不执行**数据查询 | 否 | COUNT 期间临时改（`ToCount` 自恢复）；数据查询用调用方状态 | 先 `SELECT COUNT(*) ...`，再 `SELECT <列> FROM ... [ORDER BY ...] [LIMIT/OFFSET]`（分页范围需预先 `ForPage`/`Limit`+`Offset`） | 逐行扫描；1 或 2 条（总数为 0 时 1 条） |
| `Cursor` | `dest *struct`，返回 `iter.Seq[error]` | 0..N 行（流式） | 不产出任何值（序列直接结束） | 否 | 否 | `SELECT <列> FROM ...`（状态原样，保留 LIMIT/排序/锁） | 逐行；1 条，迭代期间持有连接 |
| `CursorBy` | `dest *struct`，`chunkSize int`、`cursorColumn string`、`desc ...bool`，返回 `iter.Seq[error]` | 0..N 行（分批） | 首批无行即结束 | 每批 `Clone` | 否（在副本上覆盖 `limit`、清空 `orders`；不改原对象） | 首批 `SELECT ... ORDER BY <游标列> ASC LIMIT chunkSize+1`；后续追加 `WHERE ... AND <游标列> > ?` | 逐行；批数条（每批 1 条） |

## 解读

**两类隔离手段，针对不同对象。** `First`/`Value` 必须改 `limit` 才能拿到结果，因此用克隆（`limit` 已为 1 时省掉克隆）；`Count`/`Exists`/`Max`/`Min`/`Sum`/`Avg` 只需要「暂时不看分页、排序、锁」，因此用 `ToXxx` 内的临时状态保存/恢复。前者隔离的是**新对象**，后者隔离的是**一次编译内的字段**。`Pluck`/`PluckMap`/`KeyBy` 不属于任何一类：它们**永久覆盖** `columns`，复用同一个 Builder 会污染后续查询（需要复用先 `Clone`）。机制细节见 [builder-state](../concepts/builder-state.md)。

**空结果有三种语义，别混用。** ①返回 `sql.ErrNoRows`：`First`、`Value`、`Max`、`Min`——「没有这一行/没有值」是错误信号；②返回空容器 + `nil`：`Find`、`Pluck`、`PluckMap`、`KeyBy`、`Cursor`、`CursorBy`；③返回标量零值 + `nil`：`Count`（0）、`Exists`（false）、`Sum`/`Avg`（0）、`Paginate` 的 total（0）。聚合的归一化只发生在 `Sum`/`Avg`（SQL 对空集返回 NULL，这里归一为 0），`Max`/`Min` 保留 NULL → `sql.ErrNoRows`。

**容器不会被清空。** `Find`/`Pluck` 把行**追加**到 `dest` 现有元素之后；`PluckMap`/`KeyBy` 只对 nil map 做初始化，已有键保留、重复键后者覆盖。因此「复用一个切片/ map 变量连续调用」会累积结果；需要干净结果时先显式清空或换新变量。

**SQL 条数不等于一次调用。** `Paginate` 是 COUNT + 数据两条（总数为 0 时只有 COUNT）；`CursorBy` 每批一条；只有 `Cursor`、`Find`、`First`/`Value`、`Count`/`Exists`、聚合、`Pluck` 族是单条。慢 SQL 回调按**实际执行的 SQL** 触发，不能把一次方法调用当成一条 SQL（见 [execution-routing](../concepts/execution-routing.md)）。

**路由看 Builder 标记，不看最终 SQL。** `Count`/`Exists`/聚合会临时清掉锁子句，但执行时仍按 Builder 上的 `lockClause`/`primary` 选主库；`First`/`Value` 的克隆副本保留锁与 `Primary` 标记，因此同样强制主库。

**`Find` 的 dest 契约是切片，但实现不强制。** 扫描层按 `ScanStruct` 分派：`*struct` 会走「只扫第一行」分支（未命中返回 `sql.ErrNoRows`），`*[]struct`/`*[]*struct` 才是文档承诺的用法。不要依赖 `Find` + `*struct` 这一退化行为，单行请用 `First`。

## 选型建议

| 想做的事 | 用 | 理由 |
|---|---|---|
| 按唯一键取一行 | `First` + `Where`（必要时 `OrderBy`） | 强制 `LIMIT 1`；未命中用 `sql.ErrNoRows` 判定，比 `Find` 取一行再判长度更明确 |
| 取单列单值 | `Value` | 走原生 `Scan`；NULL 用二级指针或 `sql.NullString` 区分 |
| 判断「是否存在」 | `Exists` | `SELECT 1 ... LIMIT 1`，命中即停，不要用 `Count` 全量计数 |
| 需要总数 | `Count` | 注意 UNION/GROUP BY/HAVING/DISTINCT 会包裹子查询，成本随结果集增长 |
| 单个聚合值 | `Max`/`Min`/`Sum`/`Avg` | 空集语义见上；分组聚合请改用 `Select` + `Find`，标量聚合会返回 `ErrAggregateGrouped` |
| 单列值集合 | `Pluck` | 只查一列；注意它会改写 `columns` |
| 键值对（标量值） | `PluckMap` | 一次扫描两列，键列在前 |
| 按键索引整行 | `KeyBy` | 整行按标签扫描进结构体；键列不在结构体中时会追加为最后一列 |
| 需要总页数、可跳页 | `Paginate` + `ForPage` | 总数与数据一次拿到；深分页 OFFSET 成本高，见 [pagination](../how-to/pagination.md) |
| 大表全量遍历、单条 SQL 即可 | `Cursor` | 流式扫描，内存占用低；但迭代期间持有连接，循环内不要做慢操作 |
| 大表分批处理、可断点续跑 | `CursorBy` | 批间释放连接，按有序唯一列推进；不要与 `Offset` 混用（每批都会带同一 OFFSET） |
| 只要前 N 行、不关心总数 | `Limit(N)` + `Find` 或 `First`/`Value` | 避免 `Paginate` 的 COUNT 成本 |

```go
func example() {
	// 同一个 Builder 状态可以被不同终端方法消费；注意 Pluck 族会改写 columns。
	base := db.Builder().Table("users").Where("status", "=", "active")

	var one User
	if err := base.Clone().OrderBy("id", "ASC").First(ctx, &one); err != nil {
		// errors.Is(err, sql.ErrNoRows) 表示未命中
		panic(err)
	}

	exists, err := base.Clone().Exists(ctx)
	if err != nil {
		panic(err)
	}
	total, err := base.Clone().Count(ctx)
	if err != nil {
		panic(err)
	}

	var page []User
	pageTotal, err := base.Clone().OrderBy("id", "ASC").ForPage(1, 20).Paginate(ctx, &page)
	if err != nil {
		panic(err)
	}
	_, _, _ = exists, total, pageTotal
}
```

## 相关阅读

- 方法级细节与示例：[builder-query](builder-query.md)
- 流式迭代与 keyset 分页：[builder-cursor](builder-cursor.md)
- 扫描与 NULL/类型转换：[scanner](scanner.md)
- 克隆与临时状态机制：[builder-state](../concepts/builder-state.md)
- 路由、锁与回调：[execution-routing](../concepts/execution-routing.md)
- 分页任务写法：[pagination](../how-to/pagination.md)
- 错误清单：[errors](errors.md)
