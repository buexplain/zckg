# 排序、分组、分页、UNION 与行锁参考

本文是 zcdb Builder 在 SELECT 上构造分组、排序、分页、UNION 与行锁的参考：覆盖 `GroupBy`/`GroupByRaw`、`Having` 全家族、`OrderBy`/`OrderByRaw`/`InRandomOrder`、`Limit`/`Offset`/`ForPage`、`Union`/`UnionAll` 与 `LockForUpdate`/`SharedLock` 的语义、编译形态、方言差异和边界。

这些方法只累积状态，SQL 由 Grammar 编译；子句顺序与绑定收集见 [compile-pipeline](../concepts/compile-pipeline.md)。`Primary()`（强制主库读）与 `Clone()`（查询复用）分别在 [builder-core](../reference/builder-core.md) 与 [builder-state](../concepts/builder-state.md) 中定义，本文不重复。

> 本文 SQL 示例统一为 **MySQL 形态**。PostgreSQL 使用双引号包裹与 `$N` 占位符，SQLite 使用双引号与 `?`；共享锁、UNION 括号、随机函数等方言差异见 [dialect-matrix](../reference/dialect-matrix.md)。

示例中的 `db`、`ctx` 等是外部占位变量，示例只保证语法可解析，不保证可独立运行。

## 范围与共同约定

- **子句顺序**：`SELECT → FROM → JOIN → WHERE → GROUP BY → HAVING →（UNION）→ ORDER BY → LIMIT → OFFSET → 锁`。带 UNION 时，主查询的 `ORDER BY`/`LIMIT`/`OFFSET`/锁追加在整条 UNION 之后；各 UNION 分支按自己的子句编译。
- **绑定顺序**：SELECT 子查询 → FROM 子查询 → JOIN → WHERE → **GROUP BY → HAVING** → UNION 各分支。本页涉及绑定的只有 `GroupByRaw`、`Having*`；`OrderBy`/`OrderByRaw`/`Limit`/`Offset`/`ForPage`/锁不产生绑定（`OrderByRaw` 没有绑定参数入口）。
- **仅作用于 SELECT**：这些方法构造的 ORDER BY/LIMIT 若出现在写语句上，PostgreSQL/SQLite 的全部写语句与 MySQL 的多表写语句会返回 `ErrWriteOrderLimitUnsupported`，规则见 [builder-query](../reference/builder-query.md) 与 [safety](../concepts/safety.md)。
- **别名与标识符**：`GroupBy`/`OrderBy`/`Having` 的列参数按方言包裹，可用 `table.column`，也可直接写 SELECT 别名（PG 的 HAVING 例外，见下文）；`GroupByRaw`/`OrderByRaw`/`HavingRaw` 不做包裹。
- **没有隐式校验**：GROUP BY 与 SELECT 投影是否匹配、HAVING 是否伴随 GROUP BY、UNION 各分支列数与类型是否兼容，框架都不校验，由数据库判定。

## GroupBy / GroupByRaw

签名：

- `func (b *Builder) GroupBy(columns ...string) *Builder`
- `func (b *Builder) GroupByRaw(sql string, bindings ...any) *Builder`

**语义**：`GroupBy` 为每个列追加一个 `GROUP BY` 项，多次调用按顺序累积；列名按方言包裹。`GroupByRaw` 追加原始 SQL 片段（表达式、函数、带占位符的条件分组），`?` 在 PG 下转成 `$N`，绑定中的 `Expression` 直接内联且不占位。

**SQL**（MySQL）：

```go
func example() {
	sql, _, _ := db.Builder().Table("orders").Select("user_id").
		SelectRaw("SUM(amount) AS total").GroupBy("user_id").ToSelect()
	// SQL: SELECT `user_id`, SUM(amount) AS total FROM `orders` GROUP BY `user_id`

	// 多列与多次调用等价，按加入顺序拼接
	sql, _, _ = db.Builder().Table("orders").GroupBy("user_id").GroupBy("status").ToSelect()
	// SQL: SELECT * FROM `orders` GROUP BY `user_id`, `status`

	// 表达式分组用 GroupByRaw
	sql, _, _ = db.Builder().Table("orders").
		SelectRaw("DATE(created_at) AS d, COUNT(*) AS cnt").
		GroupByRaw("DATE(created_at)").ToSelect()
	// SQL: SELECT DATE(created_at) AS d, COUNT(*) AS cnt FROM `orders` GROUP BY DATE(created_at)

	// 带绑定的原始分组：绑定顺序 WHERE → GROUP BY
	sql, args, _ := db.Builder().Table("orders").
		Where("status", "=", "paid").
		GroupByRaw("user_id + ?", 0).
		ToSelect()
	// SQL:  SELECT * FROM `orders` WHERE `status` = ? GROUP BY user_id + ?
	// args: [paid 0]
	_, _ = sql, args
}
```

**边界与错误**：

- `GroupBy()` 不传列时不追加任何项；`GroupByRaw` 不做占位符与绑定数量的编译期校验：绑定多于 `?` 时多余值仍留在 args 中，少于 `?` 时残留占位符（PG 为无人绑定的 `$N`），两种情况都会在执行期参数数量校验处失败。
- 整体绑定顺序固定为 **WHERE → GROUP BY → HAVING**：`GroupByRaw` 的绑定排在 WHERE 之后、HAVING 之前；HAVING 的绑定排在 GROUP BY 之后。
- 列名不做存在性校验；多列分组时各次调用与单次多参等价。

**说明**：需要按表达式分组时用 `GroupByRaw`；列分组用 `GroupBy`，避免手写包裹符。PG 下 `GroupByRaw` 里的 `?` 会转成 `$N` 并计入全局编号。

## Having / OrHaving

签名：

- `func (b *Builder) Having(column string, op any, value ...any) *Builder`
- `func (b *Builder) OrHaving(column string, op any, value ...any) *Builder`

**语义**：追加一个 HAVING 条件，`Having` 用 `AND`、`OrHaving` 用 `OR` 连接。两种调用形态：

- **三参**：`Having(column, op, value)`，`op` 必须是字符串且在运算符白名单内；
- **两参简写**：`Having(column, value)`，运算符缺省为 `=`。

**SQL**（MySQL）：

```go
func example() {
	sql, args, _ := db.Builder().Table("orders").Select("user_id").
		SelectRaw("SUM(amount) AS total").
		GroupBy("user_id").Having("total", ">", 100).ToSelect()
	// SQL:  SELECT `user_id`, SUM(amount) AS total FROM `orders` GROUP BY `user_id` HAVING `total` > ?
	// args: [100]

	// 两参简写：运算符缺省为 =
	sql, args, _ = db.Builder().Table("orders").Select("status").
		SelectRaw("COUNT(*) AS cnt").
		GroupBy("status").Having("cnt", 5).ToSelect()
	// SQL:  SELECT `status`, COUNT(*) AS cnt FROM `orders` GROUP BY `status` HAVING `cnt` = ?
	// args: [5]

	// OrHaving：OR 连接
	sql, args, _ = db.Builder().Table("orders").Select("user_id").
		SelectRaw("SUM(amount) AS total").GroupBy("user_id").
		Having("total", ">", 1000).OrHaving("total", "<", 10).ToSelect()
	// SQL:  SELECT `user_id`, SUM(amount) AS total FROM `orders` GROUP BY `user_id` HAVING `total` > ? OR `total` < ?
	// args: [1000 10]
}
```

**边界与错误**：

- 形态由**实参个数**决定，不是由第二个参数的类型决定：`Having("cnt", ">")` 是两参形态，编译为 `cnt = ?` 并绑定字符串 `">"`，不会报错；三参形态的 `op` 不是字符串时产生 `ErrInvalidOperator`，字符串但不在白名单时同样产生 `ErrInvalidOperator`。
- 值规则：普通值绑定 1 个参数；`nil` 且 `op` 为 `=`/`!=`/`<>` 时编译为 `column IS NULL` / `column IS NOT NULL`（**不占绑定**）；`Expression` 直接内联（**不占绑定**）。
- 值**不支持 `*Builder` 子查询**（与 `Builder.WhereSub` 不同）；需要子查询时用 `HavingRaw`。
- 除 `HavingBetween`/`HavingNotBetween` 外没有其它区间或 LIKE 变体；缺失条件用 `HavingRaw`。
- 框架不校验 HAVING 是否伴随 GROUP BY，也不校验列/别名是否存在。

**说明**：HAVING 与 WHERE 的 nil 语义对齐，避免 `= NULL` 恒为 UNKNOWN。引用 SELECT 别名在 MySQL/SQLite 可用，在 PG 不可用，见下文「HAVING 与 SELECT 别名」。

## HavingRaw

签名：`func (b *Builder) HavingRaw(sql string, bindings ...any) *Builder`

**语义**：追加一个 `AND` 连接的原始 HAVING 片段，不做标识符包裹；`?` 按出现顺序绑定，PG 下转成 `$N`（连续 `??` 转义为字面 `?`），绑定中的 `Expression` 直接内联且不占占位符。

**SQL**（MySQL）：

```go
func example() {
	sql, args, _ := db.Builder().Table("orders").Select("user_id").
		SelectRaw("SUM(amount) AS total").GroupBy("user_id").
		HavingRaw("SUM(amount) > ?", 1000).ToSelect()
	// SQL:  SELECT `user_id`, SUM(amount) AS total FROM `orders` GROUP BY `user_id` HAVING SUM(amount) > ?
	// args: [1000]

	// PostgreSQL：同样的调用，? 转为 $1
	pgSQL, args, _ := db.Builder().Table("orders").Select("user_id").
		SelectRaw("SUM(amount) AS total").GroupBy("user_id").
		HavingRaw("SUM(amount) > ?", 1000).ToSelect()
	// PG SQL: SELECT "user_id", SUM(amount) AS total FROM "orders" GROUP BY "user_id" HAVING SUM(amount) > $1
	_, _ = pgSQL, args
}
```

**边界与错误**：

- 不做占位符与绑定数量的编译期校验：绑定多于 `?` 时多余值仍留在 args 中，少于 `?` 时残留占位符（PG 为无人绑定的 `$N`），两种情况都会在执行期参数数量校验处失败。
- 片段原样拼接，防注入责任归调用方；不要拼入外部输入。
- 片段为空白（或只有空白字符）时会原样进入 `HAVING`，生成不可执行的分句；不要传空白片段，也不要依赖它清空 HAVING。

**说明**：这是跨方言最安全的 HAVING 写法：PG 不支持 HAVING 引用 SELECT 别名，用聚合表达式即绕开该限制。`GroupByRaw`（支持绑定）、`HavingRaw`（支持绑定）与 `OrderByRaw`（不支持绑定）都不做标识符包裹与注入防护；值需要绑定时用前两者的绑定参数或 `Expression`。

## HavingBetween / HavingNotBetween

签名：

- `func (b *Builder) HavingBetween(column string, min, max any) *Builder`
- `func (b *Builder) HavingNotBetween(column string, min, max any) *Builder`

**语义**：追加 `HAVING column BETWEEN ? AND ?` 或 `HAVING column NOT BETWEEN ? AND ?`，`min`、`max` 各占一个占位符绑定。

**SQL**：

```go
func example() {
	sql, args, _ := db.Builder().Table("orders").Select("user_id").
		SelectRaw("SUM(amount) AS total").GroupBy("user_id").
		HavingBetween("total", 100, 500).ToSelect()
	// SQL:  SELECT `user_id`, SUM(amount) AS total FROM `orders` GROUP BY `user_id` HAVING `total` BETWEEN ? AND ?
	// args: [100 500]

	sql, args, _ = db.Builder().Table("orders").Select("user_id").
		SelectRaw("SUM(amount) AS total").GroupBy("user_id").
		HavingNotBetween("total", 100, 500).ToSelect()
	// SQL:  SELECT `user_id`, SUM(amount) AS total FROM `orders` GROUP BY `user_id` HAVING `total` NOT BETWEEN ? AND ?
	// args: [100 500]
}
```

**边界与错误**：`min`、`max` 只支持基本类型，且始终走绑定——`Expression` 不会被内联（与 `Having` 的值特例不同）；两端为 nil 时按 nil 绑定，比较结果为 UNKNOWN。没有 `OrHavingBetween` 变体。

**说明**：区间条件不要用 `HavingRaw("total BETWEEN ? AND ?")` 拼字符串，本方法已覆盖；需要函数或列到列区间时用 `HavingRaw`。

## HavingNull / HavingNotNull

签名：

- `func (b *Builder) HavingNull(columns ...string) *Builder`
- `func (b *Builder) HavingNotNull(columns ...string) *Builder`

**语义**：为每个列追加一个 `AND column IS NULL` / `AND column IS NOT NULL` 条件（多列展开为多个 AND 条件），不产生绑定。

**SQL**：

```go
func example() {
	sql, _, _ := db.Builder().Table("orders").Select("remark").
		SelectRaw("COUNT(*) AS cnt").GroupBy("remark").HavingNull("remark").ToSelect()
	// SQL: SELECT `remark`, COUNT(*) AS cnt FROM `orders` GROUP BY `remark` HAVING `remark` IS NULL

	sql, _, _ = db.Builder().Table("orders").GroupBy("status").
		HavingNull("remark", "note").HavingNotNull("deleted_at").ToSelect()
	// SQL: SELECT * FROM `orders` GROUP BY `status` HAVING `remark` IS NULL AND `note` IS NULL AND `deleted_at` IS NOT NULL
}
```

**边界与错误**：不校验列是否存在；不传列时不追加条件；只有 AND 连接，没有 OR 变体。`Having` 的 `= nil` 特例与本方法编译结果相同，但本方法能一次展开多列。

**说明**：多列展开条件的顺序与实参顺序一致；需要 `(a IS NULL OR b IS NULL)` 这类分组时，放进 `HavingNested` 回调。

## HavingNested / OrHavingNested

签名：

- `func (b *Builder) HavingNested(callback func(*Builder)) *Builder`
- `func (b *Builder) OrHavingNested(callback func(*Builder)) *Builder`

**语义**：把回调内的 HAVING 条件编译为一个括号分组，`HavingNested` 用 `AND` 连接、`OrHavingNested` 用 `OR` 连接；回调内条件为空时不追加。

**SQL**：

```go
func example() {
	sql, args, _ := db.Builder().Table("orders").Select("user_id").
		SelectRaw("SUM(amount) AS total, COUNT(*) AS cnt").GroupBy("user_id").
		HavingNested(func(q *zcdb.Builder) {
			q.Having("total", ">", 100).Having("cnt", ">", 3)
		}).ToSelect()
	// SQL:  SELECT `user_id`, SUM(amount) AS total, COUNT(*) AS cnt FROM `orders` GROUP BY `user_id` HAVING (`total` > ? AND `cnt` > ?)
	// args: [100 3]

	sql, args, _ = db.Builder().Table("orders").Select("user_id").
		SelectRaw("SUM(amount) AS total, COUNT(*) AS cnt").GroupBy("user_id").
		Having("total", ">", 1000).
		OrHavingNested(func(q *zcdb.Builder) {
			q.Having("total", ">", 100).Having("cnt", ">", 5)
		}).ToSelect()
	// SQL:  SELECT `user_id`, SUM(amount) AS total, COUNT(*) AS cnt FROM `orders` GROUP BY `user_id` HAVING `total` > ? OR (`total` > ? AND `cnt` > ?)
	// args: [1000 100 5]
}
```

**边界与错误**：

- 回调参数是新的 `*Builder`，但它**只收集 Having 条件**：在回调里调用 `Where`/`GroupBy`/`OrderBy` 不会进入外层 SQL。
- 回调内出现错误（非法运算符等）时错误上抛，整个嵌套组不追加。
- 空回调不追加分组。

**说明**：括号改变布尔结合范围。嵌套组的绑定按它在 HAVING 序列中的位置展开；PG 的别名限制同样适用于组内条件。

## HAVING 与 SELECT 别名：PostgreSQL 限制

**现象**：PostgreSQL 的 HAVING 子句不能引用同层 SELECT 的列别名，实测（PostgreSQL 15.18）：

```text
SELECT user_id, SUM(amount) AS total FROM t GROUP BY user_id HAVING total > 10;
ERROR:  42703: column "total" does not exist
```

MySQL 8.4 与 SQLite 接受 SELECT 别名（SQLite 分支由集成测试真实执行）；PG 允许 `ORDER BY` 使用别名，但不允许 `HAVING` 使用别名。

**编译层行为**：框架对三方言都把 HAVING 的列名按标识符包裹后原样输出（MySQL 为 `` `total` ``、PostgreSQL/SQLite 为 `"total"`），仅包裹符与占位符不同，**不在编译期报错**；PG 的错误来自数据库执行阶段（SQLSTATE 42703），`ToSelect` 本身返回成功的 SQL。

**跨方言写法**：PG 下改用聚合表达式而不是别名：

```go
func example() {
	// 用聚合表达式：三方言（含 PG）都可执行
	sql, args, _ := db.Builder().Table("orders").Select("user_id").
		SelectRaw("SUM(amount) AS total").GroupBy("user_id").
		HavingRaw("SUM(amount) > ?", 100).ToSelect()
	// SQL:  SELECT `user_id`, SUM(amount) AS total FROM `orders` GROUP BY `user_id` HAVING SUM(amount) > ?
	// args: [100]

	// Having 的列参数写成含括号的表达式时同样不会被包裹，但可读性不如 HavingRaw
	sql, args, _ = db.Builder().Table("orders").Select("user_id").
		SelectRaw("SUM(amount) AS total").GroupBy("user_id").
		Having("SUM(amount)", ">", 100).ToSelect()
	_, _ = sql, args
}
```

**说明**：需要跨方言共用查询构造时，HAVING 一律使用聚合表达式（`HavingRaw`）；只在 MySQL/SQLite 运行的分支才可依赖别名。

## OrderBy

签名：`func (b *Builder) OrderBy(column string, direction ...string) *Builder`

**语义**：追加一个排序列，多次调用按顺序累积。`direction` 大小写不敏感：只有 `DESC` 为降序，其余任何值（含省略、空串、无法识别的值）均归一为 `ASC`，不报错。

**SQL**：

```go
func example() {
	sql, _, _ := db.Builder().Table("users").OrderBy("age", "DESC").OrderBy("name").ToSelect()
	// SQL: SELECT * FROM `users` ORDER BY `age` DESC, `name` ASC

	// 方向大小写不敏感；省略/未知值都按 ASC
	sql, _, _ = db.Builder().Table("users").
		OrderBy("age", "desc").OrderBy("name", "ASC").OrderBy("id", "unknown").ToSelect()
	// SQL: SELECT * FROM `users` ORDER BY `age` DESC, `name` ASC, `id` ASC

	// 聚合别名同样可排序（PG 允许 ORDER BY 别名）
	sql, _, _ = db.Builder().Table("orders").Select("user_id").
		SelectRaw("SUM(amount) AS total").GroupBy("user_id").
		OrderBy("total", "DESC").ToSelect()
	// SQL: SELECT `user_id`, SUM(amount) AS total FROM `orders` GROUP BY `user_id` ORDER BY `total` DESC
}
```

**边界与错误**：`direction` 是变参但只取第一个值；空列名不报错但生成无意义 SQL；列名按方言包裹（`table.column` 与 `column AS alias` 均按包裹规则处理）。

**说明**：需要函数、`FIELD()`、`CASE` 等表达式排序时用 `OrderByRaw`。带 UNION 时，主查询的 `OrderBy` 作用于整条 UNION 的结果。

## OrderByRaw

签名：`func (b *Builder) OrderByRaw(sql string) *Builder`

**语义**：追加一个原样的 `ORDER BY` 片段，不做标识符包裹、**没有绑定参数入口**（值必须自行内联）。

**SQL**：

```go
func example() {
	sql, _, _ := db.Builder().Table("users").
		OrderByRaw("FIELD(status, 'active', 'frozen')").ToSelect()
	// SQL: SELECT * FROM `users` ORDER BY FIELD(status, 'active', 'frozen')

	// 与 OrderBy 混用时按调用顺序拼接
	sql, _, _ = db.Builder().Table("users").
		OrderBy("id").OrderByRaw("CASE WHEN status = 'active' THEN 0 ELSE 1 END").ToSelect()
	// SQL: SELECT * FROM `users` ORDER BY `id` ASC, CASE WHEN status = 'active' THEN 0 ELSE 1 END
}
```

**边界与错误**：片段原样拼接，内容中的占位符不会得到绑定参数（写了 `?` 会执行期报错）；不做注入防护，**不得拼入外部输入**。多个 `OrderByRaw` 按键入顺序累积。

**说明**：`FIELD()`、`CASE WHEN`、窗口函数排序等都是它的场景；需要值绑定时应改写为 `OrderBy` 支持的列/别名，或把值以字面量安全地内联。

## InRandomOrder

签名：`func (b *Builder) InRandomOrder() *Builder`

**语义**：追加方言对应的随机排序表达式：MySQL 为 `RAND()`，PostgreSQL 与 SQLite 为 `RANDOM()`。

**SQL**：

```go
func example() {
	sql, _, _ := db.Builder().Table("users").InRandomOrder().ToSelect()
	// MySQL:     SELECT * FROM `users` ORDER BY RAND()
	// PG/SQLite: SELECT * FROM "users" ORDER BY RANDOM()
	_ = sql
}
```

**边界与错误**：不报错、不校验；多次调用会追加多个随机排序项；与 `OrderBy`/`OrderByRaw` 混用时按键入顺序排列，随机项之后的排序项不再有意义。

**说明**：随机函数由方言提供，框架不控制种子；大表上整表随机排序的成本由数据库承担。

## Limit / Offset

签名：

- `func (b *Builder) Limit(n int) *Builder`
- `func (b *Builder) Offset(n int) *Builder`

**语义**：`Limit` 设置行数上限、`Offset` 设置跳过行数。`n <= 0` 时不输出对应子句。只设置 `Offset(n > 0)` 而 `Limit` 未调用或 `Limit <= 0`（两者都不输出 LIMIT）时，方言行为不同：

- MySQL 自动补 `LIMIT 18446744073709551615`（OFFSET 必须与 LIMIT 同时出现）；
- SQLite 自动补 `LIMIT -1`（表示不限制行数）；
- PostgreSQL 直接输出 `OFFSET n`。

**SQL**：

```go
func example() {
	sql, _, _ := db.Builder().Table("users").Limit(10).Offset(20).ToSelect()
	// SQL: SELECT * FROM `users` LIMIT 10 OFFSET 20

	// 仅 Offset：MySQL/SQLite 补 LIMIT，PG 原生
	sql, _, _ = db.Builder().Table("users").Offset(20).ToSelect()
	// MySQL:  SELECT * FROM `users` LIMIT 18446744073709551615 OFFSET 20
	// SQLite: SELECT * FROM "users" LIMIT -1 OFFSET 20
	// PG:     SELECT * FROM "users" OFFSET 20

	// n <= 0 不输出子句
	sql, _, _ = db.Builder().Table("users").Limit(0).Offset(0).ToSelect()
	// SQL: SELECT * FROM `users`
}
```

**边界与错误**：只接受 `int`，不做溢出/负数校验，负数等同于「不输出」；重复调用以最后一次为准；`Offset` 不进入写语句（写语句的 ORDER BY/LIMIT 限制见 [builder-query](../reference/builder-query.md)）。

**说明**：分页业务建议用 `ForPage` 或 `Paginate`，避免手算 offset；完整分页模式见 [pagination](../how-to/pagination.md)。

## ForPage

签名：`func (b *Builder) ForPage(page, perPage int) *Builder`

**语义**：设置分页，等价于 `Limit(perPage).Offset((page-1)*perPage)`。`page` 从 1 开始，`page < 1` 时修正为 1；`perPage <= 0` 时 limit 与 offset 均 `<= 0`，编译不输出 LIMIT 与 OFFSET（等同不分页）。

**SQL**：

```go
func example() {
	sql, _, _ := db.Builder().Table("users").ForPage(2, 20).ToSelect()
	// SQL: SELECT * FROM `users` LIMIT 20 OFFSET 20

	// page < 1 修正为 1：第 1 页没有 OFFSET
	sql, _, _ = db.Builder().Table("users").ForPage(0, 20).ToSelect()
	// SQL: SELECT * FROM `users` LIMIT 20

	// perPage <= 0：不分页
	sql, _, _ = db.Builder().Table("users").ForPage(3, 0).ToSelect()
	// SQL: SELECT * FROM `users`
}
```

**边界与错误**：不校验 `page`/`perPage` 的上界；会覆盖之前设置的 `Limit`/`Offset`；`perPage` 为负时结果同样是不分页。

**说明**：`ForPage` 不会自动查询总数；需要总数与分页数据时用 `Paginate`，或先 `Count` 再取数据。分页与总数组合的用法见 [pagination](../how-to/pagination.md)。

## Union / UnionAll

签名：

- `func (b *Builder) Union(query *Builder) *Builder`
- `func (b *Builder) UnionAll(query *Builder) *Builder`

**语义**：把另一个查询追加为合并分支：`Union` 去重、`UnionAll` 不去重，可链式多次调用。MySQL/PostgreSQL 编译时给每个分支加括号，SQLite 不加括号（SQLite 不允许 UNION 子查询带显式括号）；绑定按「主查询 → 各 UNION 分支」顺序合并。主查询上的 `ORDER BY`/`LIMIT`/`OFFSET` 是整条 UNION 的尾部子句。

**SQL**：

```go
func example() {
	admins := db.Builder().Table("admins").Select("name")

	sql, args, _ := db.Builder().Table("users").Select("name").Union(admins).ToSelect()
	// MySQL:  (SELECT `name` FROM `users`) UNION (SELECT `name` FROM `admins`)
	// PG:     (SELECT "name" FROM "users") UNION (SELECT "name" FROM "admins")
	// SQLite: SELECT "name" FROM "users" UNION SELECT "name" FROM "admins"

	sql, args, _ = db.Builder().Table("users").Select("name").UnionAll(admins).ToSelect()
	// SQL: (SELECT `name` FROM `users`) UNION ALL (SELECT `name` FROM `admins`)

	// 多个分支 + 整体排序/分页：ORDER BY/LIMIT/OFFSET 追加在 UNION 之后
	older := db.Builder().Table("users").Select("name").Where("age", ">", 30)
	sql, args, _ = db.Builder().Table("users").Select("name").Where("status", "=", "active").
		Union(older).UnionAll(admins).
		OrderBy("name", "ASC").Limit(10).Offset(5).
		ToSelect()
	// SQL:  (SELECT `name` FROM `users` WHERE `status` = ?) UNION (SELECT `name` FROM `users` WHERE `age` > ?) UNION ALL (SELECT `name` FROM `admins`) ORDER BY `name` ASC LIMIT 10 OFFSET 5
	// args: [active 30]
}
```

**边界与错误**：

- 不校验分支列数与列类型是否兼容，列数不一致由数据库报错；分支的 SELECT 列顺序决定结果列。
- 自引用/互引用（把 Builder 自己或其派生查询加入 Union）在编译前返回 `ErrCyclicQuery`。
- **SQLite 的分支没有括号隔离**：分支自带的 `ORDER BY`/`LIMIT` 会拼到分支文本里，因而落在整条 UNION 的尾部（MySQL/PostgreSQL 有括号，分支内子句只作用于该分支）。若该分支不是最后一个分支，产物变成 `... ORDER BY ... UNION ...`，SQLite 直接报 `ORDER BY clause should come after UNION not before`（LIMIT 同理）。排序分页应放在主查询，由框架追加到 UNION 之后。
- **PostgreSQL 不支持 UNION + 锁**：`Union(...)` 与 `LockForUpdate()`/`SharedLock()` 组合时 `ToSelect` 返回 `ErrPgUnionLockNotSupported`。
- **SQLite 不支持任何锁**：UNION 与锁组合返回 `ErrSQLiteLockNotSupported`（即使没有 UNION 也一样）。
- **MySQL 支持 UNION + 锁**：锁子句追加在 UNION 尾部（见下节示例）。

**说明**：UNION 分支是独立的 Builder，可以各自携带 JOIN/WHERE/GROUP BY/HAVING；列数、列类型和排序规则需自行保证一致。`Count`/`Exists` 等终端方法对 UNION 的投影包裹规则见 [builder-compile](../reference/builder-compile.md) 与 [builder-exec](../reference/builder-exec.md)。

## 行锁：LockForUpdate / SharedLock

签名：

- `func (b *Builder) LockForUpdate() *Builder`
- `func (b *Builder) SharedLock() *Builder`

**语义**：给 SELECT 追加行锁子句，需在事务中使用。`LockForUpdate` 为排他锁 `FOR UPDATE`；`SharedLock` 为共享锁，MySQL 编译为 `LOCK IN SHARE MODE`，PostgreSQL 自动转成 `FOR SHARE`，SQLite 不支持并在编译时返回 `ErrSQLiteLockNotSupported`。执行时带锁查询**强制走写（主库）连接**，避免读写分离下锁查询打到从库而报错或锁不生效。

**SQL**：

```go
func example() {
	// 需在事务中使用；First 会补 LIMIT 1，锁子句排在 LIMIT/OFFSET 之后
	err := db.Transaction(ctx, func(ctx context.Context) error {
		var user User
		if err := db.Builder().Table("users").
			Where("id", "=", 1).LockForUpdate().First(ctx, &user); err != nil {
			return err
		}
		return nil
	})
	// SQL:  SELECT * FROM `users` WHERE `id` = ? LIMIT 1 FOR UPDATE
	// args: [1]

	// SharedLock 的三方言形态
	sql, args, _ := db.Builder().Table("users").Where("id", "=", 1).SharedLock().ToSelect()
	// MySQL: SELECT * FROM `users` WHERE `id` = ? LOCK IN SHARE MODE
	// PG:    SELECT * FROM "users" WHERE "id" = $1 FOR SHARE
	// SQLite: 编译返回 ErrSQLiteLockNotSupported
	_, _, _ = sql, args, err
}
```

**方言差异**：

| 场景 | MySQL | PostgreSQL | SQLite |
|---|---|---|---|
| `LockForUpdate` | `FOR UPDATE` | `FOR UPDATE` | `ErrSQLiteLockNotSupported` |
| `SharedLock` | `LOCK IN SHARE MODE` | `FOR SHARE` | `ErrSQLiteLockNotSupported` |
| UNION + 锁 | 支持，锁在 UNION 之后 | `ErrPgUnionLockNotSupported` | `ErrSQLiteLockNotSupported` |
| 无 UNION + 锁 | 支持 | 支持 | `ErrSQLiteLockNotSupported` |
| 锁查询路由 | 写（主库） | 写（主库） | 不可用 |

MySQL 的 UNION + 排他锁形态：

```go
func example() {
	admins := db.Builder().Table("admins").Select("name")
	sql, _, _ := db.Builder().Table("users").Select("name").
		Union(admins).LockForUpdate().ToSelect()
	// MySQL: (SELECT `name` FROM `users`) UNION (SELECT `name` FROM `admins`) FOR UPDATE
	_ = sql
}
```

**边界与错误**：

- `LockForUpdate` 与 `SharedLock` 后调用者覆盖先调用者（内部只有一个锁子句状态），不会叠加。
- 框架不校验是否处于事务中：非事务执行是否报错由数据库决定；在读写分离下带锁查询始终走主库。
- SQLite 的锁错误在 `ToSelect` 阶段返回，SQL 不会生成；PG 的 UNION + 锁错误同样在编译阶段返回。
- 锁子句总是排在 `ORDER BY`/`LIMIT`/`OFFSET` 之后；带 UNION 时排在 UNION 尾部。

**说明**：锁的语义与事务隔离级别由数据库决定，框架只负责生成子句并按锁标记路由到主库。`Primary()` 的路由语义与 `Clone()` 的状态隔离分别见 [builder-core](../reference/builder-core.md) 与 [builder-state](../concepts/builder-state.md)，本文不展开；事务写法见 [transactions](../how-to/transactions.md)。

## 相关文档

- [builder-core](../reference/builder-core.md)：Builder 状态、`Primary`、`Clone` 的入口
- [builder-select](../reference/builder-select.md)：SELECT 投影与 FROM 子查询
- [builder-where](../reference/builder-where.md)：WHERE 条件的 nil/Expression 语义
- [builder-query](../reference/builder-query.md)：查询执行与写语句的 ORDER BY/LIMIT 限制
- [builder-compile](../reference/builder-compile.md)：`ToCount`/`ToAggregate` 对 GROUP BY/HAVING/UNION 的包裹规则
- [builder-exec](../reference/builder-exec.md)：终端方法如何消费这些子句
- [dialect-matrix](../reference/dialect-matrix.md)：三方言编译差异总表
- [errors](../reference/errors.md)：错误变量与匹配方式
- [pagination](../how-to/pagination.md)：分页与总数
- [transactions](../how-to/transactions.md)：事务与行锁
- [builder-state](../concepts/builder-state.md)：`Clone` 与状态隔离
- [compile-pipeline](../concepts/compile-pipeline.md)：编译顺序与绑定收集
