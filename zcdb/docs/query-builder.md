# SELECT 查询构造

`db.Builder()` 返回 `*Builder`，所有构造方法均返回自身以支持链式调用；调用 `ToSelect()` 编译为 SQL，或调用终端方法（Find/First 等，见[查询执行文档](query-exec.md)）直接执行。

所有示例以 MySQL 方言给出（反引号包裹、`?` 占位符）；PostgreSQL/SQLite 仅标识符改为双引号、PG 占位符为 `$1..$N`，语义差异处会单独说明。

## 表与列

### Table / TableSub

`Table` 设置主表；`TableSub` 设置 FROM 子查询（派生表）。二者互斥，后调用者生效。

```go
sql, args, _ := db.Builder().Table("users").Where("age", ">", 18).ToSelect()
// SQL:  SELECT * FROM `users` WHERE `age` > ?
// args: [18]

// FROM 子查询：子查询的绑定参数排在外层 WHERE 之前
sub := db.Builder().Table("orders").Select("user_id").Where("amount", ">", 100)
sql, args, _ = db.Builder().TableSub(sub, "o").Where("o.user_id", ">", 1).ToSelect()
// SQL:  SELECT * FROM (SELECT `user_id` FROM `orders` WHERE `amount` > ?) AS `o` WHERE `o`.`user_id` > ?
// args: [100 1]
```

未设置 Table/TableSub 时编译返回 `ErrEmptyTable`。

### Select / AddSelect / SelectRaw / SelectSub

- `Select`：**替换**语义，覆盖之前的列；未调用时默认 `SELECT *`；
- `AddSelect`：**追加**语义，等价列自动去重；
- `SelectRaw`：追加原始 SQL 表达式列（不包裹标识符），适合聚合/窗口函数；
- `SelectSub`：追加子查询列 `(SELECT ...) AS 别名`，子查询绑定参数排在所有其它绑定之前。

```go
sql, _, _ := db.Builder().Table("users").Select("id", "name").ToSelect()
// SQL: SELECT `id`, `name` FROM `users`

sql, _, _ = db.Builder().Table("users").Select("id").AddSelect("name", "id").ToSelect()
// SQL: SELECT `id`, `name` FROM `users`        // id 已存在，不重复添加

sql, _, _ = db.Builder().Table("orders").Select("user_id").SelectRaw("SUM(amount) AS total").ToSelect()
// SQL: SELECT `user_id`, SUM(amount) AS total FROM `orders`

cnt := db.Builder().Table("orders").SelectRaw("COUNT(*)").
	WhereColumn("orders.user_id", "=", "users.id")
sql, _, _ = db.Builder().Table("users").Select("id").SelectSub(cnt, "order_count").ToSelect()
// SQL: SELECT `id`, (SELECT COUNT(*) FROM `orders` WHERE `orders`.`user_id` = `users`.`id`) AS `order_count` FROM `users`
```

### Distinct

```go
sql, _, _ := db.Builder().Table("users").Select("city").Distinct().ToSelect()
// SQL: SELECT DISTINCT `city` FROM `users`
```

注意：`Distinct` 下 `Count()` 会自动包裹子查询计数，保留去重语义。

## WHERE 条件

### Where / OrWhere（基本比较）

三参形式 `(列, 运算符, 值)` 与两参简写 `(列, 值)`（缺省 `=`）；运算符经白名单校验，非法时编译阶段返回 `ErrInvalidOperator`。

白名单：`=` `!=` `<>` `<` `>` `<=` `>=` `LIKE` `NOT LIKE` `IS` `IS NOT` `IN` `NOT IN` `BETWEEN` `NOT BETWEEN`。

```go
sql, args, _ := db.Builder().Table("users").Where("age", ">", 25).ToSelect()
// SQL:  SELECT * FROM `users` WHERE `age` > ?
// args: [25]

sql, args, _ = db.Builder().Table("users").Where("status", "active").ToSelect() // 两参简写
// SQL:  SELECT * FROM `users` WHERE `status` = ?
// args: [active]

sql, args, _ = db.Builder().Table("users").Where("age", ">", 25).OrWhere("vip", "=", 1).ToSelect()
// SQL:  SELECT * FROM `users` WHERE `age` > ? OR `vip` = ?
// args: [25 1]
```

特殊值处理：

- **nil 特判**：`Where("col", "=", nil)` 编译为 `IS NULL`，`!=`/`<>` 编译为 `IS NOT NULL`（避免生成永假的 `= NULL`）；
- **Expression**：值传 `zcdb.NewExpression(...)` 时直接内嵌 SQL，不作为绑定参数。

```go
sql, _, _ := db.Builder().Table("users").Where("deleted_at", "=", nil).ToSelect()
// SQL: SELECT * FROM `users` WHERE `deleted_at` IS NULL

sql, _, _ = db.Builder().Table("users").
	Where("id", "=", zcdb.NewExpression("parent_id")).ToSelect()
// SQL: SELECT * FROM `users` WHERE `id` = parent_id
```

### WhereIn / WhereNotIn

```go
sql, args, _ := db.Builder().Table("users").WhereIn("id", []any{1, 2, 3}).ToSelect()
// SQL:  SELECT * FROM `users` WHERE `id` IN (?, ?, ?)
// args: [1 2 3]

sql, args, _ = db.Builder().Table("users").WhereNotIn("status", []any{"banned", "frozen"}).ToSelect()
// SQL:  SELECT * FROM `users` WHERE `status` NOT IN (?, ?)
// args: [banned frozen]
```

空切片语义：`WhereIn(col, []any{})` 编译为恒假条件 `0 = 1`，`WhereNotIn(col, []any{})` 编译为恒真条件 `1 = 1`。

### WhereNull / WhereNotNull

支持多列，展开为多个 AND 条件，无绑定参数：

```go
sql, _, _ := db.Builder().Table("users").WhereNull("deleted_at", "remark").ToSelect()
// SQL: SELECT * FROM `users` WHERE `deleted_at` IS NULL AND `remark` IS NULL

sql, _, _ = db.Builder().Table("users").WhereNotNull("email").ToSelect()
// SQL: SELECT * FROM `users` WHERE `email` IS NOT NULL
```

### WhereBetween 系列（区间比较）

四种形态，`Between`/`NotBetween` 均为闭区间，绑定顺序为先 min 后 max：

```go
// 值区间：column BETWEEN ? AND ?
sql, args, _ := db.Builder().Table("users").WhereBetween("age", 18, 30).ToSelect()
// SQL:  SELECT * FROM `users` WHERE `age` BETWEEN ? AND ?
// args: [18 30]

sql, args, _ = db.Builder().Table("users").WhereNotBetween("age", 18, 30).ToSelect()
// SQL:  SELECT * FROM `users` WHERE `age` NOT BETWEEN ? AND ?
// args: [18 30]

// OR 变体
sql, args, _ = db.Builder().Table("users").
	Where("vip", "=", 1).OrWhereBetween("age", 18, 30).ToSelect()
// SQL:  SELECT * FROM `users` WHERE `vip` = ? OR `age` BETWEEN ? AND ?
// args: [1 18 30]

// 两列区间：column BETWEEN 列 AND 列（无绑定）
sql, _, _ = db.Builder().Table("products").
	WhereBetweenColumns("price", "min_price", "max_price").ToSelect()
// SQL: SELECT * FROM `products` WHERE `price` BETWEEN `min_price` AND `max_price`

// 值在左：? BETWEEN 列 AND 列（“给定值是否落在表中区间内”）
sql, args, _ = db.Builder().Table("users").WhereValueBetween(25, "min_age", "max_age").ToSelect()
// SQL:  SELECT * FROM `users` WHERE ? BETWEEN `min_age` AND `max_age`
// args: [25]
```

以上每个方法均有对应的 `Or` 前缀变体：`OrWhereNotBetween`、`OrWhereBetweenColumns`、`OrWhereNotBetweenColumns`、`OrWhereValueBetween`。

### WhereRaw / OrWhereRaw

原始 SQL 条件直接嵌入，`?` 占位符按序绑定：

```go
sql, args, _ := db.Builder().Table("users").
	WhereRaw("created_at > NOW() - INTERVAL ? DAY", 7).ToSelect()
// SQL:  SELECT * FROM `users` WHERE created_at > NOW() - INTERVAL ? DAY
// args: [7]
```

### WhereColumn（两列比较）

两侧均为列引用，无绑定参数：

```go
sql, _, _ := db.Builder().Table("users").WhereColumn("updated_at", ">", "created_at").ToSelect()
// SQL: SELECT * FROM `users` WHERE `updated_at` > `created_at`
```

### 嵌套逻辑组：WhereNested / WhereNot / WhereAll / WhereAny / WhereNone

回调内构造的条件整体加括号。回调未添加任何条件时该组被忽略。

```go
// WhereNested / WhereAll（等价）：AND 分组
sql, args, _ := db.Builder().Table("users").Where("status", "active").
	WhereNested(func(q *zcdb.Builder) {
		q.Where("age", ">", 18).Where("vip", "=", 1)
	}).ToSelect()
// SQL:  SELECT * FROM `users` WHERE `status` = ? AND (`age` > ? AND `vip` = ?)
// args: [active 18 1]

// OrWhereNested：OR 连接的分组（组内仍为 AND）
sql, args, _ = db.Builder().Table("users").Where("status", "active").
	OrWhereNested(func(q *zcdb.Builder) {
		q.Where("age", ">", 60).Where("vip", "=", 1)
	}).ToSelect()
// SQL:  SELECT * FROM `users` WHERE `status` = ? OR (`age` > ? AND `vip` = ?)
// args: [active 60 1]

// WhereNot：整体取反 NOT (...)
sql, args, _ = db.Builder().Table("users").WhereNot(func(q *zcdb.Builder) {
	q.Where("status", "banned").Where("age", "<", 18)
}).ToSelect()
// SQL:  SELECT * FROM `users` WHERE NOT (`status` = ? AND `age` < ?)
// args: [banned 18]

// WhereAny：任一满足 (a OR b)（回调内条件顶层 Boolean 统一覆写为 OR）
sql, args, _ = db.Builder().Table("users").WhereAny(func(q *zcdb.Builder) {
	q.Where("age", ">", 60).Where("vip", "=", 1)
}).ToSelect()
// SQL:  SELECT * FROM `users` WHERE (`age` > ? OR `vip` = ?)
// args: [60 1]

// WhereNone：全部不满足 NOT (a OR b)
sql, args, _ = db.Builder().Table("users").WhereNone(func(q *zcdb.Builder) {
	q.Where("status", "banned").Where("age", "<", 18)
}).ToSelect()
// SQL:  SELECT * FROM `users` WHERE NOT (`status` = ? OR `age` < ?)
// args: [banned 18]
```

`OrWhereNot` / `OrWhereAll` / `OrWhereAny` / `OrWhereNone` 为对应的 OR 变体。

### WhereDate（日期比较，方言分支）

按方言生成日期提取表达式，value 建议传 `"YYYY-MM-DD"` 字符串：

```go
sql, args, _ := db.Builder().Table("users").WhereDate("created_at", "2026-08-08").ToSelect()
// MySQL:      SELECT * FROM `users` WHERE date(`created_at`) = ?            args: [2026-08-08]
// PostgreSQL: SELECT * FROM "users" WHERE "created_at"::date = $1
// SQLite:     SELECT * FROM "users" WHERE strftime('%Y-%m-%d', "created_at") = ?
```

### WhereLike 系列（模糊匹配，方言分支）

`caseSensitive` 变参为 true 时区分大小写，三方言编译形态不同：

```go
sql, args, _ := db.Builder().Table("users").WhereLike("name", "%alice%").ToSelect()
// SQL:  SELECT * FROM `users` WHERE `name` LIKE ?
// args: [%alice%]

sql, args, _ = db.Builder().Table("users").WhereLike("name", "a%", true).ToSelect()
// MySQL:      SELECT * FROM `users` WHERE BINARY `name` LIKE ?
// PostgreSQL: SELECT * FROM "users" WHERE "name" LIKE $1      // 默认不区分时为 ILIKE
// SQLite:     SELECT * FROM "users" WHERE "name" GLOB ?       // 通配符为 * / ?，非 % / _
// args: [a%]

sql, args, _ = db.Builder().Table("users").WhereNotLike("name", "%test%").ToSelect()
// MySQL:      SELECT * FROM `users` WHERE `name` NOT LIKE ?
// PostgreSQL: SELECT * FROM "users" WHERE "name" NOT ILIKE $1   // 与 WhereLike 默认行为对称
// args: [%test%]
```

`OrWhereLike` 为 OR 变体。

### WhereNullSafe 系列（空安全比较，方言分支）

NULL 参与比较运算（`NULL <=> NULL` 为 true）：

```go
sql, args, _ := db.Builder().Table("users").WhereNullSafeEquals("remark", nil).ToSelect()
// MySQL:      SELECT * FROM `users` WHERE `remark` <=> ?                       args: [<nil>]
// PostgreSQL: SELECT * FROM "users" WHERE "remark" IS NOT DISTINCT FROM $1
// SQLite:     SELECT * FROM "users" WHERE "remark" IS ?

sql, args, _ = db.Builder().Table("users").WhereNullSafeNotEquals("remark", "x").ToSelect()
// MySQL:      SELECT * FROM `users` WHERE NOT `remark` <=> ?                   args: [x]
// PostgreSQL: SELECT * FROM "users" WHERE "remark" IS DISTINCT FROM $1
// SQLite:     SELECT * FROM "users" WHERE "remark" IS NOT ?
```

### 子查询条件：WhereExists / WhereSub / WhereInSub

`WhereExists`/`WhereNotExists` 的入参支持 `func(*Builder)` 回调或已构造的 `*Builder`，其它类型编译时返回 `ErrInvalidSubQuery`：

```go
// EXISTS / NOT EXISTS
sql, _, _ := db.Builder().Table("users").WhereExists(func(q *zcdb.Builder) {
	q.Table("orders").SelectRaw("1").WhereColumn("orders.user_id", "=", "users.id")
}).ToSelect()
// SQL: SELECT * FROM `users` WHERE EXISTS (SELECT 1 FROM `orders` WHERE `orders`.`user_id` = `users`.`id`)

// column op (SELECT ...)
sql, _, _ = db.Builder().Table("users").WhereSub("age", ">", func(q *zcdb.Builder) {
	q.Table("stats").SelectRaw("AVG(age)")
}).ToSelect()
// SQL: SELECT * FROM `users` WHERE `age` > (SELECT AVG(age) FROM `stats`)

// column IN (SELECT ...)
sql, args, _ := db.Builder().Table("users").WhereInSub("dept_id", func(q *zcdb.Builder) {
	q.Table("depts").Select("id").Where("level", ">", 3)
}).ToSelect()
// SQL:  SELECT * FROM `users` WHERE `dept_id` IN (SELECT `id` FROM `depts` WHERE `level` > ?)
// args: [3]
```

`OrWhereExists` / `OrWhereNotExists` / `OrWhereSub` / `WhereNotInSub` 为对应变体。

## JOIN

### 单条件简写：Join / LeftJoin / RightJoin / CrossJoin

条件为列到列比较，无绑定参数。多条件或需要值比较请用 `JoinOn` 系列。

```go
sql, _, _ := db.Builder().Table("users").Select("users.name", "orders.amount").
	Join("orders", "users.id", "=", "orders.user_id").ToSelect()
// SQL: SELECT `users`.`name`, `orders`.`amount` FROM `users` INNER JOIN `orders` ON `users`.`id` = `orders`.`user_id`

sql, _, _ = db.Builder().Table("users").
	LeftJoin("orders", "users.id", "=", "orders.user_id").ToSelect()
// SQL: SELECT * FROM `users` LEFT JOIN `orders` ON `users`.`id` = `orders`.`user_id`

sql, _, _ = db.Builder().Table("stores").CrossJoin("months").ToSelect() // 笛卡尔积
// SQL: SELECT * FROM `stores` CROSS JOIN `months`
```

`RightJoin`：SQLite 3.39 以下不支持 RIGHT JOIN。框架编译层不做版本检测、直接输出 `RIGHT JOIN`，低版本 SQLite 会在数据库层面报错。

`CrossJoinOn`（带 ON 条件的 CROSS JOIN）：PG 的 CROSS JOIN 不接受 ON，编译层自动转为 `INNER JOIN ... ON`（语义等价）：

```go
sql, _, _ := db.Builder().Table("users").
	CrossJoinOn("colors", "colors.id", "=", "users.id").ToSelect()
// MySQL: SELECT * FROM `users` CROSS JOIN `colors` ON `colors`.`id` = `users`.`id`
// PG:    SELECT * FROM "users" INNER JOIN "colors" ON "colors"."id" = "users"."id"
```

### 多条件回调：JoinOn / LeftJoinOn / RightJoinOn

回调参数为 `*JoinBuilder`，支持列比较、值比较、NULL/IN/EXISTS、括号分组与嵌套 join 组。ON 条件中值比较的绑定参数按 **JOIN → WHERE** 顺序计入总绑定。

```go
sql, args, _ := db.Builder().Table("users").Select("users.name").
	JoinOn("orders", func(j *zcdb.JoinBuilder) {
		j.On("orders.user_id", "=", "users.id").        // 列比较，无绑定
			Where("orders.status", "=", "paid")          // 值比较，占位符绑定
	}).ToSelect()
// SQL:  SELECT `users`.`name` FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id` AND `orders`.`status` = ?
// args: [paid]

// OrOn：多个顶层条件混用 AND/OR 时裸 OR 会改变结合范围，需要分组请用 WhereNested 包裹
sql, _, _ = db.Builder().Table("users").LeftJoinOn("orders", func(j *zcdb.JoinBuilder) {
	j.On("orders.user_id", "=", "users.id").OrOn("orders.ref_user_id", "=", "users.id")
}).ToSelect()
// SQL: SELECT * FROM `users` LEFT JOIN `orders` ON `orders`.`user_id` = `users`.`id` OR `orders`.`ref_user_id` = `users`.`id`
```

`JoinBuilder` 可用的条件方法：

| 方法 | 编译形态 | 绑定 |
|---|---|---|
| `On(a, op, b)` / `OrOn` | `a op b`（列比较） | 无 |
| `Where(col, op, val)` / `OrWhere` | `col op ?`（值比较；val 为 `*Builder` 时编译为子查询比较） | 值 |
| `WhereNull(cols...)` / `WhereNotNull` | `col IS [NOT] NULL`（多列展开） | 无 |
| `WhereIn(col, values)` / `WhereNotIn` | `col IN (?, ?)`；values 为 `*Builder` 时 `IN (SELECT ...)`；空切片分别为 `0 = 1` / `1 = 1` | 值 |
| `WhereExists(sub)` | `EXISTS (SELECT ...)`；sub 支持回调或 `*Builder` | 子查询绑定 |
| `WhereNested(fn)` / `OrWhereNested` / `OnNested` | `(...)` 括号分组 | 组内绑定 |
| `Raw(sql, args...)` | 原始 SQL 片段，`?` 按序绑定 | 值 |

```go
// ON 条件中的 IN / 括号分组 / 原始 SQL
sql, args, _ := db.Builder().Table("users").JoinOn("orders", func(j *zcdb.JoinBuilder) {
	j.On("orders.user_id", "=", "users.id").
		WhereIn("orders.status", []any{"paid", "shipped"}).
		WhereNested(func(q *zcdb.JoinBuilder) {
			q.Where("orders.amount", ">", 100)
		}).
		Raw("YEAR(orders.created_at) = ?", 2026)
}).ToSelect()
// SQL:  SELECT * FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id` AND `orders`.`status` IN (?, ?) AND (`orders`.`amount` > ?) AND YEAR(orders.created_at) = ?
// args: [paid shipped 100 2026]
```

### 嵌套 join 组（括号 join）

在 `JoinBuilder` 内调用 `JoinOn` / `CrossJoinOn` 可继续嵌套 join，编译为带括号的 join 组（MySQL 风格的显式结合优先级控制），支持任意深度：

```go
sql, _, _ := db.Builder().Table("users").JoinOn("orders", func(j *zcdb.JoinBuilder) {
	j.On("orders.user_id", "=", "users.id").JoinOn("order_items", func(q *zcdb.JoinBuilder) {
		q.On("order_items.order_id", "=", "orders.id")
	})
}).ToSelect()
// SQL: SELECT * FROM `users` INNER JOIN (`orders` INNER JOIN `order_items` ON `order_items`.`order_id` = `orders`.`id`) ON `orders`.`user_id` = `users`.`id`
```

### 派生表 JOIN：JoinSub / LeftJoinSub / RightJoinSub / CrossJoinSub

子查询作为 join 的右表，其绑定参数先于 ON 条件计入总绑定：

```go
latest := db.Builder().Table("logs").Select("user_id").
	SelectRaw("MAX(created_at) AS last_at").GroupBy("user_id")
sql, _, _ := db.Builder().Table("users").Select("users.name", "l.last_at").
	JoinSub(latest, "l", func(j *zcdb.JoinBuilder) {
		j.On("l.user_id", "=", "users.id")
	}).ToSelect()
// SQL: SELECT `users`.`name`, `l`.`last_at` FROM `users` INNER JOIN (SELECT `user_id`, MAX(created_at) AS last_at FROM `logs` GROUP BY `user_id`) AS `l` ON `l`.`user_id` = `users`.`id`

// CrossJoinSub：无 ON 条件的派生表笛卡尔积（典型：维度组合矩阵）
months := db.Builder().Table("months").Select("month")
sql, _, _ = db.Builder().Table("stores").CrossJoinSub(months, "m").ToSelect()
// SQL: SELECT * FROM `stores` CROSS JOIN (SELECT `month` FROM `months`) AS `m`
```

## 分组与 HAVING

### GroupBy / GroupByRaw

`GroupBy` 支持多列、多次调用累积；需要表达式分组用 `GroupByRaw`（绑定顺序：WHERE → GROUP BY → HAVING）：

```go
sql, _, _ := db.Builder().Table("orders").Select("user_id").
	SelectRaw("SUM(amount) AS total").GroupBy("user_id").ToSelect()
// SQL: SELECT `user_id`, SUM(amount) AS total FROM `orders` GROUP BY `user_id`

sql, _, _ = db.Builder().Table("orders").GroupBy("user_id", "status").ToSelect()
// SQL: SELECT * FROM `orders` GROUP BY `user_id`, `status`

sql, _, _ = db.Builder().Table("orders").
	SelectRaw("DATE(created_at) AS d, COUNT(*) AS cnt").
	GroupByRaw("DATE(created_at)").ToSelect()
// SQL: SELECT DATE(created_at) AS d, COUNT(*) AS cnt FROM `orders` GROUP BY DATE(created_at)
```

### Having 系列

`Having` 支持三参与两参简写（规则同 Where）；`HavingRaw` 直接嵌入原始 SQL：

```go
sql, args, _ := db.Builder().Table("orders").Select("user_id").
	SelectRaw("SUM(amount) AS total").
	GroupBy("user_id").Having("total", ">", 100).ToSelect()
// SQL:  SELECT `user_id`, SUM(amount) AS total FROM `orders` GROUP BY `user_id` HAVING `total` > ?
// args: [100]

sql, args, _ = db.Builder().Table("orders").Select("user_id").
	SelectRaw("SUM(amount) AS total").
	GroupBy("user_id").HavingRaw("SUM(amount) > ?", 1000).ToSelect()
// SQL:  SELECT `user_id`, SUM(amount) AS total FROM `orders` GROUP BY `user_id` HAVING SUM(amount) > ?
// args: [1000]
```

其它变体：`OrHaving`、`HavingBetween` / `HavingNotBetween`、`HavingNull` / `HavingNotNull`（多列展开）、`HavingNested` / `OrHavingNested`（括号分组）。

> PostgreSQL 的 HAVING 不支持引用 SELECT 别名，PG 下请用 `HavingRaw("SUM(amount) > ?", 100)` 这类聚合表达式写法。

## 排序、分页

### OrderBy / OrderByRaw / InRandomOrder

`OrderBy` 多次调用按顺序累积；方向参数大小写不敏感，仅 `DESC` 为降序，其余（含省略）均为 ASC：

```go
sql, _, _ := db.Builder().Table("users").OrderBy("age", "DESC").OrderBy("name").ToSelect()
// SQL: SELECT * FROM `users` ORDER BY `age` DESC, `name` ASC

sql, _, _ = db.Builder().Table("users").OrderByRaw("FIELD(status, 'active', 'frozen')").ToSelect()
// SQL: SELECT * FROM `users` ORDER BY FIELD(status, 'active', 'frozen')

sql, _, _ = db.Builder().Table("users").InRandomOrder().ToSelect()
// MySQL:      SELECT * FROM `users` ORDER BY RAND()
// PG/SQLite:  SELECT * FROM "users" ORDER BY RANDOM()
```

`OrderByRaw` 不做标识符包裹、不支持绑定参数，值需自行内联（注意注入风险）。

### Limit / Offset / ForPage

`n <= 0` 时不输出对应子句；`ForPage(page, perPage)` 中 page 从 1 开始（< 1 时修正为 1），`perPage <= 0` 时 limit/offset 均 <= 0，等同不分页：

```go
sql, _, _ := db.Builder().Table("users").Limit(10).Offset(20).ToSelect()
// SQL: SELECT * FROM `users` LIMIT 10 OFFSET 20

sql, _, _ = db.Builder().Table("users").ForPage(2, 20).ToSelect()
// SQL: SELECT * FROM `users` LIMIT 20 OFFSET 20
```

## UNION

`Union` / `UnionAll` 可链式多次调用追加，编译时各查询加括号包裹（**SQLite 方言不加括号**，SQLite 不允许 UNION 子查询显式括号），绑定按主查询 → 各 UNION 查询顺序合并：

```go
admins := db.Builder().Table("admins").Select("name")
sql, _, _ := db.Builder().Table("users").Select("name").Union(admins).ToSelect()
// SQL: (SELECT `name` FROM `users`) UNION (SELECT `name` FROM `admins`)

sql, _, _ = db.Builder().Table("users").Select("name").UnionAll(admins).ToSelect()
// SQL: (SELECT `name` FROM `users`) UNION ALL (SELECT `name` FROM `admins`)
```

方言限制：PostgreSQL 不支持 UNION + 锁组合，编译时返回 `ErrPgUnionLockNotSupported`。

## 行锁

`LockForUpdate`（排他锁）与 `SharedLock`（共享锁）需在事务中使用；执行时带锁查询**强制走主库连接**，避免读写分离下锁不生效。

```go
err := db.Transaction(ctx, func(ctx context.Context) error {
	var user User
	if err := db.Builder().Table("users").
		Where("id", "=", 1).LockForUpdate().First(ctx, &user); err != nil {
		return err
	}
	// ... 业务处理
	return nil
})
// SQL:  SELECT * FROM `users` WHERE `id` = ? LIMIT 1 FOR UPDATE
// args: [1]
```

SharedLock 的方言形态：

```go
sql, args, _ := db.Builder().Table("users").Where("id", "=", 1).SharedLock().ToSelect()
// MySQL: SELECT * FROM `users` WHERE `id` = ? LOCK IN SHARE MODE     args: [1]
// PG:    SELECT * FROM "users" WHERE "id" = $1 FOR SHARE
// SQLite: 编译报错 ErrSQLiteLockNotSupported（SQLite 不支持锁子句）
```

## Primary（强制主库读）

`Primary()` 标记本次读查询强制走写（主库）连接，**不加任何锁、不改变编译出的 SQL**，仅影响连接路由（见[连接文档](connection.md)的读写分离路由表）：

```go
// 写后读：订单写入后立刻回读，从库可能因复制延迟尚无数据
var order Order
err := db.Builder().Table("orders").Where("id", "=", id).Primary().First(ctx, &order)
```

与 `LockForUpdate`/`SharedLock` 相同，`Primary()` 标记经 Clone 保留，First/Value 等内部克隆的终端方法同样生效。

## Clone（查询复用）

`Clone` 深拷贝 Builder 的全部查询状态（列、子查询、JOIN 含嵌套组、WHERE 含嵌套与切片、GROUP/HAVING、ORDER、UNION、锁、force 标记与强制主库标记），副本与原 Builder 完全隔离。适合基于公共条件派生多个查询：

```go
base := db.Builder().Table("users").Where("status", "active")
admins := base.Clone().Where("role", "admin")

sqlBase, _, _ := base.ToSelect()
// SQL: SELECT * FROM `users` WHERE `status` = ?

sqlAdmins, _, _ := admins.ToSelect()
// SQL: SELECT * FROM `users` WHERE `status` = ? AND `role` = ?
```

First/Value/CursorBy 等终端方法内部用 Clone 避免污染调用方的 Builder；Paginate 不克隆，直接复用原 Builder（由 `ToCount` 内部临时清除并 `defer` 恢复分页/排序/列，效果等价）。
