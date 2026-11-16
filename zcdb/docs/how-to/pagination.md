# 分页：ForPage、Limit/Offset 与 Paginate

分页的难点不在「怎么写出 LIMIT」，而在**排序是否确定、总数是否值得查、偏移是否可接受**。这份指南把三条路线（页码分页、偏移分页、游标分页）的语义、成本与选型边界讲清楚，并给出可直接运行的示例。

## 结论先行

- **浅页 + 需要总数/页码**：`OrderBy + ForPage + Paginate`。
- **只需要当前页、不需要总数**：`OrderBy + ForPage + Find`，省掉一次 `COUNT`。
- **深页、全量导出、批处理**：用 `CursorBy` 或手写 keyset 条件，不要用越来越大的 `OFFSET`。
- **分页必须有确定的 `ORDER BY`**：排序键最好唯一；不唯一时追加主键做 tiebreaker。
- **`Paginate` 不提供默认页大小**：没有设置 `ForPage` 或 `Limit`/`Offset` 时，它会 COUNT 后返回全部行。
- **`Paginate` 是两次独立查询**：`total` 与当前页不保证来自同一快照；需要一致视图时放进同一事务并理解隔离级别。

## ForPage 与 Limit+Offset

三个方法都只改 Builder 状态，不执行 SQL：

| 方法 | 行为 | 边界 |
|---|---|---|
| `ForPage(page, perPage)` | 设置 `limit=perPage`、`offset=(page-1)*perPage` | `page < 1` 修正为 1；`perPage <= 0` 时两者都不输出，等于不分页 |
| `Limit(n)` | 设置 `LIMIT n` | `n <= 0` 不输出 LIMIT |
| `Offset(n)` | 设置 `OFFSET n` | `n <= 0` 不输出；`n > 0` 但没有有效 LIMIT 时由方言补位 |

```go
func example() {
	// 页码语义：第 2 页、每页 20 条
	sqlStr, args, err := db.Builder().Table("users").
		OrderBy("id", "ASC").
		ForPage(2, 20).
		ToSelect()
	// SQL: SELECT * FROM `users` ORDER BY `id` ASC LIMIT 20 OFFSET 20
	// args: 无

	// 与 ForPage(2, 20) 等价
	sqlStr, args, err = db.Builder().Table("users").
		OrderBy("id", "ASC").
		Limit(20).Offset(20).
		ToSelect()
	_, _, _ = sqlStr, args, err
}
```

几个容易踩的边界（SQL 以 MySQL 形态给出）：

| 调用 | 编译结果 | 说明 |
|---|---|---|
| `ForPage(1, 20)` | `... LIMIT 20` | 第 1 页 offset=0，不输出 `OFFSET` |
| `ForPage(0, 20)` | `... LIMIT 20` | `page<1` 修正为 1 |
| `ForPage(1, 0)` | `` SELECT * FROM `users` `` | `perPage<=0` → 不分页，**不是**「取 0 条」 |
| `Limit(20).Offset(40)` | `... LIMIT 20 OFFSET 40` | 常用组合 |
| `Offset(40)`（MySQL） | `... LIMIT 18446744073709551615 OFFSET 40` | MySQL 要求 OFFSET 与 LIMIT 同时出现，Grammar 补最大行数 |
| `Offset(40)`（SQLite） | `... LIMIT -1 OFFSET 40` | SQLite 同样要求 LIMIT 在前 |
| `Offset(40)`（PostgreSQL） | `... OFFSET 40` | PG 允许单独 OFFSET |

`Limit`、`Offset`、`ForPage` 都是**替换**对应字段，后调用者覆盖前值。注意 `ForPage(2, 20).Limit(5)` 只把 limit 改成 5，offset 仍是 20；混用时按字段想清楚，不要误以为 `Limit` 会重置分页。

## Paginate 做了什么：两次查询

`Paginate(ctx, &dest)` 返回 `(totalCount int, err error)`，内部顺序固定：

1. 调用 `Count`：`ToCount` 临时清除 `limit`/`offset`/`orders`/`lockClause`（以及必要的列），编译 `COUNT`，然后用 `saveTransientState` 的恢复函数还原 Builder 状态；
2. `total == 0`：直接返回 `(0, nil)`，**不执行**数据查询，`dest` 保持调用前的内容；
3. `total > 0`：用原 Builder 的 `ORDER BY`/`LIMIT`/`OFFSET` 编译数据 SQL，执行并扫描到 `dest`。

```go
func example() {
	var users []User
	total, err := db.Builder().Table("users").
		Where("status", "=", "active").
		OrderBy("id", "ASC").
		ForPage(2, 20).
		Paginate(ctx, &users)
	// COUNT SQL: SELECT COUNT(*) FROM `users` WHERE `status` = ?
	// 数据 SQL:  SELECT * FROM `users` WHERE `status` = ? ORDER BY `id` ASC LIMIT 20 OFFSET 20
	// args: 两条 SQL 都是 [active]
	if err != nil {
		return
	}
	_ = total
}
```

要点：

- `Paginate` **不克隆** Builder；它依赖 `ToCount` 的临时状态保存/恢复，所以调用后原 Builder 的分页与排序仍在。`joins`/`wheres` 等其他字段本来就不会被 `ToCount` 改动。
- `total` 是 `int`，是满足当前 WHERE（以及 GROUP BY/HAVING/UNION/DISTINCT）的总数，不是当前页行数。
- 页越界但 `total > 0` 时，数据 SQL 仍会执行，返回空页；`total` 仍是总数。
- 读写分离下 COUNT 与数据查询各自选择从库（`RandomStrategy` 可能选中不同实例），复制延迟会放大 `total` 与当前页的不一致。
- 配置了 DAO 默认注释或 `Builder.Comment` 时，COUNT 与数据 SQL **各追加一次**注释；慢 SQL 回调会分别观测到两条 SQL。
- 带 `Primary()` 或锁子句时，COUNT 与数据查询都强制主库（事务内则是同一个事务连接）；锁子句只出现在数据 SQL 里（`ToCount` 会清掉锁）。
- `total == 0` 时只有 COUNT 一条 SQL 被观测到。

## ORDER BY 与 LIMIT 的关系

SQL 的逻辑执行顺序是 `WHERE → GROUP BY → HAVING → SELECT → ORDER BY → LIMIT/OFFSET`。`LIMIT/OFFSET` 只是从**已排序**（或未定义顺序）的结果里切一段：

- **没有 `ORDER BY`**：数据库可以按任意顺序返回行，第 1 页与第 2 页可能重叠或漏行。
- **排序键不唯一**：相同键值的行在不同次查询间可能换位，翻页同样会重复/漏行。给排序追加唯一列兜底，例如 `ORDER BY created_at DESC, id DESC`。
- **COUNT 不受 ORDER BY 影响**：`ToCount` 会清空 `orders`、`limit`、`offset` 与锁；给 COUNT 保留排序没有意义，也不应期待它改变计数结果。
- **`CursorBy` 忽略已有 `ORDER BY`**：它强制按游标列升序（或 `desc=true` 时降序）排序，见下文。

```go
func example() {
	type row struct {
		Id   int64  `db:"id"`
		Name string `db:"name"`
	}
	var firstPage, secondPage []row

	// 错误：没有 ORDER BY，两次查询的顺序都没有保证
	_ = db.Builder().Table("orders").ForPage(1, 20).Find(ctx, &firstPage)
	_ = db.Builder().Table("orders").ForPage(2, 20).Find(ctx, &secondPage)

	// 正确：唯一列作为最终 tiebreaker
	_ = db.Builder().Table("orders").
		OrderBy("created_at", "DESC").
		OrderBy("id", "DESC").
		ForPage(1, 20).
		Find(ctx, &firstPage)
}
```

## COUNT 的成本与包裹规则

`COUNT` 不是免费的：它要扫描所有满足条件的行（或索引项）。`Paginate` **每次调用都付一次**这个成本，即使调用方只关心当前页。

`ToCount` 按查询形态选择两种策略：

| 查询形态 | COUNT SQL（MySQL 形态） | 说明 |
|---|---|---|
| 普通 WHERE | `` SELECT COUNT(*) FROM `users` WHERE `status` = ? `` | 投影替换为 `COUNT(*)`，其余条件保留 |
| `GROUP BY`（无 HAVING） | `` SELECT COUNT(*) FROM (SELECT 1 FROM `orders` GROUP BY `user_id`) AS `t` `` | 先保留分组结果，外层数行数；内层用常量避免 `SELECT *` 与严格模式冲突 |
| `HAVING` 且显式选了列 | `` SELECT COUNT(*) FROM (SELECT `user_id`, SUM(amount) AS total FROM `orders` GROUP BY `user_id` HAVING `total` > ?) AS `t` `` | HAVING 可引用 SELECT 别名，因此保留调用方投影 |
| `DISTINCT` | `` SELECT COUNT(*) FROM (SELECT DISTINCT `city` FROM `users`) AS `t` `` | 去重语义必须靠子查询保留；`SELECT COUNT(DISTINCT ...)` 不能替代多列去重 |
| `UNION` / `UNION ALL` | `` SELECT COUNT(*) FROM ((SELECT `name` FROM `admins`) UNION (SELECT `name` FROM `users`)) AS `t` `` | 整个 UNION 包裹后计数；括号形态随方言变化 |

包裹时的别名 `t` 也由 Grammar 包裹：MySQL 为 `` `t` ``，PostgreSQL/SQLite 为 `"t"`。包裹意味着数据库可能要**物化派生子查询**；分组/去重/UNION 的结果集越大，COUNT 越贵。

降低成本的常用手段：

- 不需要总数就别用 `Paginate`，用 `ForPage + Find`；
- 只需要判断「有没有」用 `Exists`（`SELECT 1 ... LIMIT 1`），不要 `Count`；
- 过滤条件尽量走索引，避免在 COUNT 里 `SELECT *` 或做函数计算；
- 总数可以接受近似/缓存时，单独 `Count` 并按业务缓存，不要每页重复算。

```go
func example() {
	count, err := db.Builder().Table("users").Where("status", "=", "active").Count(ctx)
	// SQL: SELECT COUNT(*) FROM `users` WHERE `status` = ?
	// args: [active]

	exists, err := db.Builder().Table("users").Where("email", "=", "a@t.com").Exists(ctx)
	// SQL: SELECT 1 FROM `users` WHERE `email` = ? LIMIT 1
	// args: [a@t.com]
	_, _ = count, exists
	_ = err
}
```

## 大偏移的问题

`LIMIT n OFFSET m` 的执行方式是：先按 `ORDER BY` 找到并跳过前 `m` 行，再返回接下来的 `n` 行。成本随 `m` 增长，深页会明显变慢；同时，翻页期间的数据写入会让「第 900 页」的内容前后漂移。

解决思路是 **keyset（键集）分页**：记住上一页最后一行（或最后一批）的排序键，下一页用 `WHERE key > lastKey ORDER BY key LIMIT n` 直接从索引定位，不再扫描被跳过的行。

```go
func example() {
	var next []User
	lastID := int64(1000)
	err := db.Builder().Table("users").
		Where("id", ">", lastID).
		OrderBy("id", "ASC").
		Limit(100).
		Find(ctx, &next)
	// SQL: SELECT * FROM `users` WHERE `id` > ? ORDER BY `id` ASC LIMIT 100
	// args: [1000]
}
```

多列排序需要复合 keyset 条件。例如按 `(created_at DESC, id DESC)` 翻页：

```go
func example() {
	lastCreatedAt := "2026-01-01 00:00:00"
	var lastID int64 = 1000
	var next []Order
	err := db.Builder().Table("orders").
		Where("created_at", "<", lastCreatedAt).
		OrWhereNested(func(q *zcdb.Builder) {
			q.Where("created_at", "=", lastCreatedAt).Where("id", "<", lastID)
		}).
		OrderBy("created_at", "DESC").
		OrderBy("id", "DESC").
		Limit(100).
		Find(ctx, &next)
	// SQL: SELECT * FROM `orders` WHERE `created_at` < ? OR (`created_at` = ? AND `id` < ?)
	//      ORDER BY `created_at` DESC, `id` DESC LIMIT 100
	// args: [2026-01-01 00:00:00 2026-01-01 00:00:00 1000]
}
```

什么时候仍可接受 `OFFSET`：后台列表只翻前几十页、数据量可控、没有严格性能目标。需要随机跳页（用户直接输入第 1000 页）时 keyset 做不到，只能在「页码分页 + 深页变慢」和「只允许顺序翻页」之间取舍。

## 何时用 CursorBy 而不是 Paginate

`CursorBy` 是 zcdb 内置的 keyset 迭代器：首批没有游标条件，后续每批查询 `WHERE cursorColumn > lastValue ORDER BY cursorColumn LIMIT chunkSize+1`，多取一条用于探测是否还有下一页；探测行不产出，下一批从本批最后一个游标值继续。

适用：

- 大表全量导出、批处理、回填、迁移；
- 只关心「顺序处理完」，不需要总数与页码；
- 游标列有序且唯一（通常为主键），迭代期间不修改游标列；
- 固定过滤条件（如 `status='active'`）要保留时，`CursorBy` 会把它和游标条件 `AND` 起来。

不适用：

- 需要 `total`、页码或随机跳页 → 用 `Paginate`；
- 想按任意列排序 → `CursorBy` 会忽略已设置的 `ORDER BY`，强制按游标列排序；
- 游标列不唯一（如 `status`、`updated_at`）→ 会重复或漏行；`CursorBy` **只支持单个游标列**，复合排序键请手写上面的 keyset 条件；
- 游标列可能为 NULL → 只有映射到**指针字段**时才能识别并报 `ErrCursorColumnNull`（否则 `col > NULL` 恒假会无限重复同一批）；映射到非指针数值字段时 NULL 会被扫描成零值，无法与真正的 0 区分。因此游标列应选 `NOT NULL` 且唯一的列；可空列请把结构体字段声明为指针并处理该错误。

```go
func example() {
	var user User
	for err := range db.Builder().Table("users").
		Where("status", "=", "active").
		CursorBy(ctx, &user, 100, "id") {
		if err != nil {
			return
		}
		_ = user.Name
	}
	// 首批 SQL: SELECT * FROM `users` WHERE `status` = ? ORDER BY `id` ASC LIMIT 101
	// 后续批次: SELECT * FROM `users` WHERE `status` = ? AND `id` > ? ORDER BY `id` ASC LIMIT 101
	// args: 首批 [active]；后续 [active 上一批最后的 id]
}
```

参数与行为细节：

- `chunkSize == 0`：直接返回，不执行任何查询；`chunkSize < 0`：使用默认值 100。
- 每批实际 `LIMIT chunkSize+1`；数据量恰为整数倍时不会多执行一次返回 0 行的空查询。
- 变参 `desc=true`：条件变为 `cursorColumn < lastValue`，排序变为 `DESC`。
- 游标列支持 `table.column` 限定形式，结构体按标签或字段名匹配。
- 错误：结构体没有游标列字段 → `ErrCursorFieldNotFound`；字段不可用（如 nil 嵌入指针）→ `ErrCursorFieldUnavailable`；游标值为 NULL → `ErrCursorColumnNull`。
- 迭代中 `break` 会关闭当前批 rows；`ctx` 取消/查询错误会作为 `error` yield 给调用方，不会静默结束。

## 选型表

| 场景 | 需要总数 | 需要跳页 | 推荐 | 理由 |
|---|---|---|---|---|
| 后台列表，浅页（前几十页） | 是 | 是 | `ForPage + Paginate` | 实现简单，页码/总数齐全；深页变慢可接受 |
| 列表只需要当前页 | 否 | 是 | `OrderBy + ForPage + Find` | 省掉一次 COUNT，避免为不需要的数字付费 |
| 无限滚动/「加载更多」 | 否 | 否 | `CursorBy` 或 keyset | 不随偏移变慢，适合顺序消费 |
| 全量导出/批处理/数据回填 | 否 | 否 | `CursorBy` | 每批独立查询，不长时间占用连接 |
| 需要按多列排序的深页 | 否 | 否 | 手写复合 keyset | `CursorBy` 只支持单游标列 |
| 只关心是否存在 | 否 | 否 | `Exists` | `SELECT 1 ... LIMIT 1`，命中即返回 |
| 总数很贵但业务需要展示 | 是 | 是 | 单独 `Count` + 缓存总数 | 避免每页重复 COUNT；接受总数非实时 |
| 需要总数与当前页严格一致 | 是 | 是 | 放进同一事务并按隔离级别理解 | `Paginate` 的两次查询默认不是同一快照 |

## 可运行示例

下面程序用内存 SQLite 造 25 行数据，演示浅页 `Paginate`、`CursorBy` 全量遍历和手写 keyset。省略 `package` 子句，实际运行时在文件第一行补 `package main`。

```go
// 完整程序。实际运行时请在文件第一行补：package main
import (
	"context"
	"fmt"

	"github.com/buexplain/zckg/zcdb"
	_ "modernc.org/sqlite"
)

type User struct {
	Id   int64  `db:"id"`
	Name string `db:"name"`
}

func main() {
	ctx := context.Background()
	pool, err := zcdb.NewPool(zcdb.PoolConfig{
		DriverName:   "sqlite",
		DSN:          ":memory:",
		MaxOpenConns: 1,
		MaxIdleConns: 1,
	})
	if err != nil {
		panic(err)
	}
	defer pool.Close()
	db, err := zcdb.NewDBDao(pool, "sqlite", nil, "", "")
	if err != nil {
		panic(err)
	}

	if _, err := db.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`); err != nil {
		panic(err)
	}
	all := make([]User, 0, 25)
	for i := 1; i <= 25; i++ {
		all = append(all, User{Id: int64(i), Name: fmt.Sprintf("u%02d", i)})
	}
	if _, err := db.Builder().Table("users").Insert(ctx, all); err != nil {
		panic(err)
	}

	// 1. 浅页：Paginate 内部先 COUNT 再取当前页。
	var page []User
	total, err := db.Builder().Table("users").OrderBy("id", "ASC").ForPage(2, 10).Paginate(ctx, &page)
	if err != nil {
		panic(err)
	}
	fmt.Printf("total=%d len=%d first=%s last=%s\n", total, len(page), page[0].Name, page[len(page)-1].Name)

	// 2. 每批 10 条，用 CursorBy 顺序处理全部 25 条。
	var u User
	count, first, last := 0, "", ""
	for err := range db.Builder().Table("users").Select("id", "name").CursorBy(ctx, &u, 10, "id") {
		if err != nil {
			panic(err)
		}
		if count == 0 {
			first = u.Name
		}
		last = u.Name
		count++
	}
	fmt.Printf("cursorBy count=%d first=%s last=%s\n", count, first, last)

	// 3. 需要自定义排序键时，手写 keyset 条件。
	var next []User
	if err := db.Builder().Table("users").Select("id", "name").
		Where("id", ">", int64(20)).OrderBy("id", "ASC").Limit(3).
		Find(ctx, &next); err != nil {
		panic(err)
	}
	fmt.Printf("keyset after 20: %v\n", next)
}
```

预期输出：

```text
total=25 len=10 first=u11 last=u20
cursorBy count=25 first=u01 last=u25
keyset after 20: [{21 u21} {22 u22} {23 u23}]
```

## 常见错误

- **忘了设置分页范围（`ForPage` 或 `Limit`/`Offset`）**：`Paginate` 没有默认页大小，会 COUNT 后返回全部行。
- **`perPage <= 0`**：不是「取 0 条」，而是完全不输出 LIMIT/OFFSET。
- **没有 `ORDER BY` 或不唯一**：翻页重复/漏行；追加唯一列做 tiebreaker。
- **复用目标切片**：`Find`/`Paginate` 扫描 `*[]struct` 时是追加，不重置切片；复用前 `users = users[:0]`，或每次用新变量。`total == 0` 时连清空都不会发生，切片会保留上一次的内容。
- **`dest` 类型错误发现得晚**：`Paginate` 先 COUNT、再数据查询、最后才扫描校验 `dest`；`total == 0` 时甚至不会报目标类型错误（如 `ErrNotPointer`/`ErrScanDest`）。`dest` 要传 `*[]struct`（或 `*[]*struct`）。
- **`total` 与当前页不一致**：两次查询默认不在同一事务/快照里，之间发生写入就会不一致；需要一致视图时放进同一事务并确认隔离级别。
- **深页硬用 OFFSET**：页码越大越慢；顺序消费改 `CursorBy`，多列排序改复合 keyset。
- **不需要总数却用 `Paginate`**：每页多一次 COUNT；改 `Find`。
- **锁分页放在事务外**：`LockForUpdate`/`SharedLock` 只有事务内才有意义；`Paginate` 的数据 SQL 会带锁子句，COUNT 会去掉锁子句。
- **把 `CursorBy` 当页码分页**：它没有 total，也不能跳页；游标列不唯一会重复/漏行。

## 延伸阅读

- 路由与事务边界：[concepts/execution-routing.md](../concepts/execution-routing.md)
- 事务任务指南：[transactions.md](transactions.md)
- `ForPage`/`Limit`/`Offset`/锁的逐方法说明：[reference/builder-order-group.md](../reference/builder-order-group.md)
- `Paginate`/`Count`/`Exists` 等终端方法：[reference/builder-query.md](../reference/builder-query.md)
- 游标迭代：[reference/builder-cursor.md](../reference/builder-cursor.md)
- COUNT 与包裹的编译细节：[reference/builder-compile.md](../reference/builder-compile.md)
- 从零跑通：[getting-started.md](../getting-started.md)
- 文档总地图：[README.md](../README.md)
