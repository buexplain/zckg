# JOIN 参考

本文是 zcdb Builder 的 JOIN 构造参考：覆盖单条件简写、`JoinBuilder` 回调、派生表 JOIN 三类入口，`JoinBuilder` 的全部条件方法，以及 ON 条件的编译形态、绑定顺序与 nil/Expression 特例。

Builder 只累积 JOIN 状态，SQL 由 Grammar 编译。职责划分见 [architecture](../concepts/architecture.md)，编译顺序与绑定收集见 [compile-pipeline](../concepts/compile-pipeline.md)，JOIN 状态在克隆与错误累积上的行为见 [builder-state](../concepts/builder-state.md)。

> 本文 SQL 示例统一为 **MySQL 形态**。PostgreSQL 使用双引号包裹与 `$N` 占位符，SQLite 使用双引号与 `?`；`RIGHT JOIN`、`CROSS JOIN ... ON`、派生表别名等方言差异见 [dialect-matrix](../reference/dialect-matrix.md)。

示例中的 `db`、`ctx` 等是外部占位变量，示例只保证语法可解析，不保证可独立运行。

## 范围与共同约定

JOIN 入口分三类，区别在 ON 条件的构造方式：

| 入口 | 方法 | ON 条件 | 绑定参数 |
|---|---|---|---|
| 单条件简写 | `Join` / `LeftJoin` / `RightJoin` | 一个列到列比较 | 无 |
| 单条件简写（无 ON） | `CrossJoin` | 无 | 无 |
| 单条件简写（带 ON） | `CrossJoinOn` | 一个列到列比较 | 无 |
| 回调 | `JoinOn` / `LeftJoinOn` / `RightJoinOn` | `JoinBuilder` 任意条件 | 值、子查询、Raw |
| 派生表 | `JoinSub` / `LeftJoinSub` / `RightJoinSub` | `JoinBuilder` 任意条件 | 子查询 + ON |
| 派生表（无 ON） | `CrossJoinSub` | 无 | 子查询 |

共同约定：

- **编译位置**：JOIN 子句输出在 `FROM` 之后、`WHERE` 之前，多个 JOIN 按调用顺序排列；ON 条件按加入顺序用 `AND`/`OR` 连接。
- **绑定位置**：SELECT 的绑定顺序是 SELECT 子查询 → FROM 子查询 → JOIN → WHERE → GROUP BY → HAVING → UNION。因此 ON 条件里的值绑定**先于 WHERE**，且每个 JOIN 内部按「派生表子查询 → 嵌套 join 组 → 自身 ON 条件」展开，详见下文「ON 条件绑定顺序与 nil/Expression 特例」。
- **运算符白名单**：`op` 大小写不敏感，只接受 `=`、`!=`、`<>`、`<`、`>`、`<=`、`>=`、`LIKE`、`NOT LIKE`、`IS`、`IS NOT`；其它值一律产生 `ErrInvalidOperator`，错误累积到 Builder（或 JoinBuilder），在 `ToSelect` 等编译方法调用时返回。
- **标识符包裹**：表名支持 `table` 与 `table as alias` 两种写法，列参数支持 `table.column`；普通表名/列名按方言包裹，`Raw` 片段不做任何包裹。
- **简写只比较列**：`Join`/`LeftJoin`/`RightJoin`/`CrossJoinOn` 的第二操作数是列名，把值当列名传入不会报错但 SQL 语义错误；值比较必须用 `JoinBuilder.Where`。

## 单条件简写：Join / LeftJoin / RightJoin / CrossJoin

签名：

- `func (b *Builder) Join(table, first, op, second string) *Builder`
- `func (b *Builder) LeftJoin(table, first, op, second string) *Builder`
- `func (b *Builder) RightJoin(table, first, op, second string) *Builder`
- `func (b *Builder) CrossJoin(table string) *Builder`

**语义**：追加一个 JOIN 子句，ON 条件为 `first op second` 的列到列比较，不产生绑定参数。`Join` 为 `INNER JOIN`，`LeftJoin` 保留主表无匹配行，`RightJoin` 保留右表无匹配行，`CrossJoin` 为无条件笛卡尔积。

**SQL**（MySQL）：

```go
func example() {
	sql, _, _ := db.Builder().Table("users").Select("users.name", "orders.amount").
		Join("orders", "users.id", "=", "orders.user_id").ToSelect()
	// SQL: SELECT `users`.`name`, `orders`.`amount` FROM `users` INNER JOIN `orders` ON `users`.`id` = `orders`.`user_id`

	sql, _, _ = db.Builder().Table("users").Select("users.name").
		LeftJoin("orders", "users.id", "=", "orders.user_id").ToSelect()
	// SQL: SELECT `users`.`name` FROM `users` LEFT JOIN `orders` ON `users`.`id` = `orders`.`user_id`

	sql, _, _ = db.Builder().Table("users").Select("users.name").
		RightJoin("orders", "users.id", "=", "orders.user_id").ToSelect()
	// SQL: SELECT `users`.`name` FROM `users` RIGHT JOIN `orders` ON `users`.`id` = `orders`.`user_id`

	sql, _, _ = db.Builder().Table("stores").Select("stores.name", "months.month").
		CrossJoin("months").ToSelect()
	// SQL: SELECT `stores`.`name`, `months`.`month` FROM `stores` CROSS JOIN `months`
}
```

**边界与错误**：

- `op` 非法时记录 `ErrInvalidOperator`，该 JOIN **不会追加**，后续链式调用可以继续但最终编译返回错误。
- `RightJoin` 在 SQLite 3.39 以下不支持。框架编译层**不做版本检测**，直接输出 `RIGHT JOIN`，低版本 SQLite 在数据库层面报错。
- `CrossJoin` 没有 ON 条件、没有绑定参数；它不构成写操作保护意义上的「有效限定」，而带 ON 条件的 JOIN 会被 `Update`/`Increment`/`Decrement`/`DeleteJoin` 计入限定条件（见 [safety](../concepts/safety.md)）。

**说明**：多次调用按键入顺序累积。同一张表参与多次 JOIN 时需要自行给别名（`"orders as o"`），否则列引用会歧义。简写只覆盖单条件；多条件、值比较、NULL/IN/EXISTS 或括号分组请用下一节的回调入口。

## CrossJoinOn：带 ON 条件的 CROSS JOIN

签名：`func (b *Builder) CrossJoinOn(table, first, op, second string) *Builder`

**语义**：追加一个带单列比较 ON 条件的 `CROSS JOIN`。MySQL 与 SQLite 直接支持 `CROSS JOIN ... ON`；PostgreSQL 的 `CROSS JOIN` 不接受 ON，编译层自动改写为 `INNER JOIN ... ON`（先笛卡尔积再按 ON 过滤，与内连接语义等价）。

**SQL**：

```go
func example() {
	// MySQL / SQLite：直译 CROSS JOIN ... ON ...
	sql, _, _ := db.Builder().Table("users").
		CrossJoinOn("colors", "colors.id", "=", "users.id").ToSelect()
	// SQL: SELECT * FROM `users` CROSS JOIN `colors` ON `colors`.`id` = `users`.`id`

	// PostgreSQL：CROSS JOIN 不接受 ON，编译为 INNER JOIN ... ON ...
	pgSQL, _, _ := db.Builder().Table("users").
		CrossJoinOn("colors", "colors.id", "=", "users.id").ToSelect()
	// PG SQL: SELECT * FROM "users" INNER JOIN "colors" ON "colors"."id" = "users"."id"

	_, _ = sql, pgSQL
}
```

**边界与错误**：与单条件简写相同，`op` 非法产生 `ErrInvalidOperator` 且不追加 JOIN；只有单列比较，不接受多条件。

**说明**：只有 ON 条件真正参与过滤时，`CROSS JOIN ... ON` 与 `INNER JOIN ... ON` 才等价；PG 的改写不改变结果集。需要多条件时请改用 `JoinOn`（它的 SQL 类型是 `INNER JOIN`）。

## 多条件回调：JoinOn / LeftJoinOn / RightJoinOn

签名：

- `func (b *Builder) JoinOn(table string, callback func(*JoinBuilder)) *Builder`
- `func (b *Builder) LeftJoinOn(table string, callback func(*JoinBuilder)) *Builder`
- `func (b *Builder) RightJoinOn(table string, callback func(*JoinBuilder)) *Builder`

**语义**：回调参数是本次 JOIN 专属的 `*JoinBuilder`，用于构造任意 ON 条件：列比较、值比较（带占位符绑定）、NULL/IN/EXISTS、括号分组与嵌套 join 组。ON 条件按回调内的调用顺序连接。

**SQL**（MySQL）：

```go
func example() {
	sql, args, _ := db.Builder().Table("users").Select("users.name").
		JoinOn("orders", func(j *zcdb.JoinBuilder) {
			j.On("orders.user_id", "=", "users.id").
				Where("orders.status", "=", "paid")
		}).ToSelect()
	// SQL:  SELECT `users`.`name` FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id` AND `orders`.`status` = ?
	// args: [paid]

	// 多个顶层条件混用 AND/OR 时，裸 OR 会改变结合范围；需要分组时用 WhereNested 括号包裹
	sql, _, _ = db.Builder().Table("users").LeftJoinOn("orders", func(j *zcdb.JoinBuilder) {
		j.On("orders.user_id", "=", "users.id").
			OrOn("orders.ref_user_id", "=", "users.id")
	}).ToSelect()
	// SQL: SELECT * FROM `users` LEFT JOIN `orders` ON `orders`.`user_id` = `users`.`id` OR `orders`.`ref_user_id` = `users`.`id`
}
```

**边界与错误**：

- 回调内出现首个错误（非法运算符、非法 `WhereIn` 值、非法 `WhereExists` 参数）时，**整个 JOIN 不追加**，错误先记录在 `JoinBuilder` 再转交 Builder，最终由编译方法返回。
- 回调内不产生任何条件时，该 JOIN 编译为没有 ON 的 JOIN；能否执行取决于方言（见下文「错误与边界」）。
- `JoinOn` 的 SQL 类型固定为 `INNER JOIN`；`JoinBuilder` 没有嵌套的 `LeftJoinOn` 变体。

**说明**：`JoinBuilder` 只在回调期间有效，回调返回后再调用它的方法不会影响已追加的 JOIN。ON 条件中值比较的绑定参数按 JOIN → WHERE 顺序计入总绑定，PG 下 `$N` 编号全局递增。

## JoinBuilder.On / OrOn

签名：

- `func (j *JoinBuilder) On(first, op, second string) *JoinBuilder`
- `func (j *JoinBuilder) OrOn(first, op, second string) *JoinBuilder`

**语义**：追加一个列到列比较条件，`On` 用 `AND` 连接、`OrOn` 用 `OR` 连接，不产生绑定参数。

**SQL**：

```go
func example() {
	sql, _, _ := db.Builder().Table("users").JoinOn("orders", func(j *zcdb.JoinBuilder) {
		j.On("orders.user_id", "=", "users.id").
			On("orders.tenant_id", "=", "users.tenant_id").
			OrOn("orders.owner_id", "=", "users.id")
	}).ToSelect()
	// SQL: SELECT * FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id` AND `orders`.`tenant_id` = `users`.`tenant_id` OR `orders`.`owner_id` = `users`.`id`
}
```

**边界与错误**：`op` 非法产生 `ErrInvalidOperator` 并终止后续条件追加；列名按方言包裹，表达式（含 `(` 的字符串）不被包裹。

**说明**：每个 JOIN 的**第一条**条件不输出 `AND`/`OR` 前缀——即使第一条是 `OrOn`，编译结果也是 `ON a = b`，`OR` 被省略（避免语法错误）。混用 `AND`/`OR` 时依靠 SQL 的优先级结合（`AND` 先于 `OR`），需要不同结合顺序时用 `WhereNested`/`OnNested` 显式加括号。

## JoinBuilder.Where / OrWhere

签名：

- `func (j *JoinBuilder) Where(column, op string, value any) *JoinBuilder`
- `func (j *JoinBuilder) OrWhere(column, op string, value any) *JoinBuilder`

**语义**：追加一个列到值的比较条件 `column op ?`，值走占位符绑定，`Where` 用 `AND`、`OrWhere` 用 `OR` 连接。`value` 的形态与编译结果：

| value 形态 | 编译结果 | 绑定 |
|---|---|---|
| 普通值（int/string/float/时间等） | `column op ?` | 1 个 |
| `nil` 且 `op` 为 `=` / `!=` / `<>` | `column IS NULL` / `column IS NOT NULL` | 无 |
| `Expression` | `column op <表达式原文>` | 无 |
| `*Builder` | `column op (SELECT ...)` | 子查询绑定 |

`nil` 特例只覆盖 `=`、`!=`、`<>` 三种运算符；白名单里的 `IS`/`IS NOT` 不参与特判，`Where("col", "IS", nil)` 仍编译为 `col IS ?` 并绑定 nil。

**SQL**（MySQL）：

```go
func example() {
	sql, args, _ := db.Builder().Table("users").JoinOn("orders", func(j *zcdb.JoinBuilder) {
		j.On("orders.user_id", "=", "users.id").
			Where("orders.status", "=", "paid").
			OrWhere("orders.vip_channel", "=", 1)
	}).ToSelect()
	// SQL:  SELECT * FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id` AND `orders`.`status` = ? OR `orders`.`vip_channel` = ?
	// args: [paid 1]

	// nil 特例：= 转 IS NULL、!=/<> 转 IS NOT NULL，均不占绑定
	sql, args, _ = db.Builder().Table("users").JoinOn("profiles", func(j *zcdb.JoinBuilder) {
		j.On("profiles.user_id", "=", "users.id").
			Where("profiles.deleted_at", "=", nil).
			Where("profiles.bio", "!=", nil)
	}).ToSelect()
	// SQL:  SELECT * FROM `users` INNER JOIN `profiles` ON `profiles`.`user_id` = `users`.`id` AND `profiles`.`deleted_at` IS NULL AND `profiles`.`bio` IS NOT NULL
	// args: []

	// Expression 直接内联，不占绑定
	sql, args, _ = db.Builder().Table("users").JoinOn("orders", func(j *zcdb.JoinBuilder) {
		j.On("orders.user_id", "=", "users.id").
			Where("orders.amount", ">", zcdb.NewExpression("orders.budget"))
	}).ToSelect()
	// SQL:  SELECT * FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id` AND `orders`.`amount` > orders.budget
	// args: []

	// *Builder 值编译为子查询比较
	avg := db.Builder().Table("stats").SelectRaw("AVG(amount)")
	sql, args, _ = db.Builder().Table("users").JoinOn("orders", func(j *zcdb.JoinBuilder) {
		j.On("orders.user_id", "=", "users.id").
			Where("orders.amount", ">", avg)
	}).ToSelect()
	// SQL:  SELECT * FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id` AND `orders`.`amount` > (SELECT AVG(amount) FROM `stats`)
	// args: []
}
```

**边界与错误**：

- `op` 非法产生 `ErrInvalidOperator`；传入非字符串的运算符（Go 编译期即不允许）不在本方法的错误面内。
- `Expression` 是原样拼接的逃逸口：不要拼入不可信输入，也不要让内容包含 `?` 占位符——ON 值比较在 PG 下会把 `Expression` 里的 `?` 转成 `$N` 却不补绑定，导致占位符数量不匹配。
- 没有 `WhereNotBetween`、`WhereLike`、空安全比较等变体；这些条件用 `Raw` 表达。
- `JoinBuilder.Where` 没有两参简写，运算符必须显式给出。

**说明**：nil 语义与 [builder-where](../reference/builder-where.md) 的 `Builder.Where` 对齐，避免 `col = NULL` 恒为 UNKNOWN 而静默丢行；实现上编译侧与绑定收集侧各有一份 nil/Expression 特判，二者必须保持一致。

## JoinBuilder.WhereNull / WhereNotNull

签名：

- `func (j *JoinBuilder) WhereNull(columns ...string) *JoinBuilder`
- `func (j *JoinBuilder) WhereNotNull(columns ...string) *JoinBuilder`

**语义**：为每个列追加一个 `AND column IS NULL` / `AND column IS NOT NULL` 条件（多列展开为多个 AND 条件），不产生绑定。

**SQL**：

```go
func example() {
	sql, args, _ := db.Builder().Table("users").JoinOn("profiles", func(j *zcdb.JoinBuilder) {
		j.On("profiles.user_id", "=", "users.id").
			WhereNull("profiles.deleted_at", "profiles.archived_at").
			WhereNotNull("profiles.avatar", "profiles.bio")
	}).ToSelect()
	// SQL:  SELECT * FROM `users` INNER JOIN `profiles` ON `profiles`.`user_id` = `users`.`id` AND `profiles`.`deleted_at` IS NULL AND `profiles`.`archived_at` IS NULL AND `profiles`.`avatar` IS NOT NULL AND `profiles`.`bio` IS NOT NULL
	// args: []
}
```

**边界与错误**：不校验列是否存在；不传列时不追加任何条件；只支持 `AND` 连接（没有 `OrWhereNull` 变体）。

**说明**：列名按方言包裹，适合简单列；需要 `IS NULL` 与其它条件组成 OR 分组时，放进 `WhereNested` 回调。

## JoinBuilder.WhereIn / WhereNotIn

签名：

- `func (j *JoinBuilder) WhereIn(column string, values any) *JoinBuilder`
- `func (j *JoinBuilder) WhereNotIn(column string, values any) *JoinBuilder`

**语义**：追加 AND 连接的 `column IN (...)` 或 `column NOT IN (...)`。`values` 只接受两种形态：`[]any` 值列表（逐个占位符绑定）或 `*Builder` 子查询。

**SQL**：

```go
func example() {
	sql, args, _ := db.Builder().Table("users").JoinOn("orders", func(j *zcdb.JoinBuilder) {
		j.On("orders.user_id", "=", "users.id").
			WhereIn("orders.status", []any{"paid", "shipped"}).
			WhereNotIn("orders.channel", []any{"test"})
	}).ToSelect()
	// SQL:  SELECT * FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id` AND `orders`.`status` IN (?, ?) AND `orders`.`channel` NOT IN (?)
	// args: [paid shipped test]

	// 子查询形态
	hot := db.Builder().Table("hot_products").Select("id")
	sql, args, _ = db.Builder().Table("users").JoinOn("orders", func(j *zcdb.JoinBuilder) {
		j.On("orders.user_id", "=", "users.id").
			WhereIn("orders.product_id", hot)
	}).ToSelect()
	// SQL:  SELECT * FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id` AND `orders`.`product_id` IN (SELECT `id` FROM `hot_products`)
	// args: []

	// 空列表：IN 恒假、NOT IN 恒真，均不占绑定
	sql, args, _ = db.Builder().Table("users").JoinOn("orders", func(j *zcdb.JoinBuilder) {
		j.On("orders.user_id", "=", "users.id").
			WhereIn("orders.status", []any{}).
			WhereNotIn("orders.channel", []any{})
	}).ToSelect()
	// SQL:  SELECT * FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id` AND 0 = 1 AND 1 = 1
	// args: []
}
```

**边界与错误**：

- `values` 不是 `[]any` 也不是 `*Builder` 时记录 `ErrInvalidWhereInValues`（`[]string`、`[]int` 等具体类型切片同样报错，需先转成 `[]any`）。
- 空 `[]any`：`IN` 编译为 `0 = 1`（恒假），`NOT IN` 编译为 `1 = 1`（恒真）。
- 值列表元素不做 `Expression` 内联——元素按普通值逐个绑定（与 `Where` 的 Expression 特例不同）。
- 只有 `AND` 连接，没有 `OrWhereIn`/`OrWhereNotIn` 变体。

**说明**：`*Builder` 子查询的绑定参数按条件出现位置插入 ON 绑定序列，PG 下与其它条件共享同一个 `$N` 计数器。

## JoinBuilder.WhereExists

签名：`func (j *JoinBuilder) WhereExists(sub any) *JoinBuilder`

**语义**：追加 `AND EXISTS (SELECT ...)` 条件。`sub` 支持两种形态：`func(*Builder)` 回调（回调参数是注入当前 Grammar/DAO 的新 Builder）或已构造的 `*Builder`。

**SQL**：

```go
func example() {
	sql, args, _ := db.Builder().Table("users").JoinOn("orders", func(j *zcdb.JoinBuilder) {
		j.On("orders.user_id", "=", "users.id").
			WhereExists(func(q *zcdb.Builder) {
				q.Table("payments").SelectRaw("1").
					WhereColumn("payments.order_id", "=", "orders.id")
			})
	}).ToSelect()
	// SQL:  SELECT * FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id` AND EXISTS (SELECT 1 FROM `payments` WHERE `payments`.`order_id` = `orders`.`id`)
	// args: []

	// 已构造的 *Builder 同样可用
	paid := db.Builder().Table("payments").SelectRaw("1").
		WhereColumn("payments.order_id", "=", "orders.id")
	sql, args, _ = db.Builder().Table("users").JoinOn("orders", func(j *zcdb.JoinBuilder) {
		j.On("orders.user_id", "=", "users.id").WhereExists(paid)
	}).ToSelect()
	_, _ = sql, args
}
```

**边界与错误**：`sub` 既不是 `func(*Builder)` 也不是 `*Builder` 时记录 `ErrInvalidSubQuery`；回调内建出的子查询若引用外层 Builder 形成环，编译前返回 `ErrCyclicQuery`。

**说明**：只有 `EXISTS` 的 AND 形态，没有 `NOT EXISTS` / `OR` 变体；`NOT EXISTS` 用 `Raw("NOT EXISTS (SELECT ...)")` 或在子查询内取反。关联条件用子查询 Builder 的 `WhereColumn` 书写，框架不自动改写关联。

## JoinBuilder.WhereNested / OrWhereNested / OnNested

签名：

- `func (j *JoinBuilder) WhereNested(callback func(*JoinBuilder)) *JoinBuilder`
- `func (j *JoinBuilder) OrWhereNested(callback func(*JoinBuilder)) *JoinBuilder`
- `func (j *JoinBuilder) OnNested(callback func(*JoinBuilder)) *JoinBuilder`

**语义**：把回调内的条件编译为一个括号分组，`WhereNested`/`OnNested` 用 `AND` 连接、`OrWhereNested` 用 `OR` 连接。`OnNested` 与 `WhereNested` 语义相同，只是命名对齐 `On` 系列。回调内条件为空时不追加任何条件。

**SQL**：

```go
func example() {
	sql, args, _ := db.Builder().Table("users").JoinOn("orders", func(j *zcdb.JoinBuilder) {
		j.On("orders.user_id", "=", "users.id").
			WhereNested(func(q *zcdb.JoinBuilder) {
				q.Where("orders.status", "=", "paid").
					Where("orders.amount", ">", 100)
			}).
			OrWhereNested(func(q *zcdb.JoinBuilder) {
				q.Where("orders.vip_channel", "=", 1)
			})
	}).ToSelect()
	// SQL:  SELECT * FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id` AND (`orders`.`status` = ? AND `orders`.`amount` > ?) OR (`orders`.`vip_channel` = ?)
	// args: [paid 100 1]

	// 空回调不追加条件
	sql, args, _ = db.Builder().Table("users").JoinOn("orders", func(j *zcdb.JoinBuilder) {
		j.On("orders.user_id", "=", "users.id").
			OnNested(func(q *zcdb.JoinBuilder) {})
	}).ToSelect()
	// SQL:  SELECT * FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id`
	// args: []
}
```

**边界与错误**：

- 回调内出现错误时错误上抛，整个 JOIN 不追加。
- 空回调（既无普通条件也无嵌套 join）不追加条件。
- **回调只收集条件**：在 `WhereNested`/`OrWhereNested`/`OnNested` 回调里调用 `JoinOn`/`CrossJoinOn` 不会编译出 JOIN，嵌套 join 组应直接挂在本次 JOIN 的 `JoinBuilder` 上（见下文「嵌套 join 组」）。

**说明**：括号改变的是布尔结合范围，不改变 JOIN 的嵌套关系；需要改变 JOIN 结合顺序时用「嵌套 join 组」。

## JoinBuilder.Raw

签名：`func (j *JoinBuilder) Raw(sql string, bindings ...any) *JoinBuilder`

**语义**：追加一个 `AND` 连接的原始 SQL 片段。片段不做标识符包裹，`?` 按出现顺序绑定 `bindings`；PG 下 `?` 转成 `$N`（连续 `??` 转义为字面 `?`，供 jsonb 运算符使用），绑定中的 `Expression` 直接内联且**不占**占位符。

**SQL**（MySQL）：

```go
func example() {
	sql, args, _ := db.Builder().Table("users").JoinOn("orders", func(j *zcdb.JoinBuilder) {
		j.On("orders.user_id", "=", "users.id").
			Raw("YEAR(orders.created_at) = ?", 2026)
	}).ToSelect()
	// SQL:  SELECT * FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id` AND YEAR(orders.created_at) = ?
	// args: [2026]

	// 原始片段内的标识符不被包裹；Expression 绑定内联、不占绑定
	sql, args, _ = db.Builder().Table("users").JoinOn("orders", func(j *zcdb.JoinBuilder) {
		j.On("orders.user_id", "=", "users.id").
			Raw("orders.amount > ? AND orders.status = ?",
				zcdb.NewExpression("users.budget"), "paid")
	}).ToSelect()
	// SQL:  SELECT * FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id` AND orders.amount > users.budget AND orders.status = ?
	// args: [paid]
}
```

**边界与错误**：

- 不做占位符与绑定数量的编译期校验：绑定多于 `?` 时多余值仍留在 args 中，绑定少于 `?` 时 MySQL/SQLite 残留 `?`、PG 生成无人绑定的 `$N`，两种情况都会在参数数量校验处失败（需要内联的完整 SQL 请用 [builder-compile](../reference/builder-compile.md) 的 `CompileRaw`）。
- 片段为空白时不产生任何条件（也不会被计为有效限定）。
- 内容原样拼接，防注入责任归调用方；不要拼入外部输入。

**说明**：这是 `JoinBuilder` 上唯一的表达式逃逸口，`LIKE`、`BETWEEN`、`NOT EXISTS`、函数比较等条件都用它表达。

## 嵌套 join 组（JoinBuilder.JoinOn / CrossJoinOn）

签名：

- `func (j *JoinBuilder) JoinOn(table string, callback func(*JoinBuilder)) *JoinBuilder`
- `func (j *JoinBuilder) CrossJoinOn(table, first, op, second string) *JoinBuilder`

**语义**：在当前 JOIN 内继续追加一个内层 JOIN，编译为带括号的 join 组：`INNER JOIN (内层表 INNER JOIN 更内层 ON ...) ON ...`。`JoinOn` 内层为 `INNER JOIN`；`CrossJoinOn` 内层为 `CROSS JOIN ... ON`（PG 下改写为 `INNER JOIN ... ON`）。支持任意深度递归。

**SQL**（MySQL；外层也可是 `LEFT JOIN`）：

```go
func example() {
	sql, args, _ := db.Builder().Table("users").JoinOn("orders", func(j *zcdb.JoinBuilder) {
		j.On("orders.user_id", "=", "users.id").
			JoinOn("order_items", func(q *zcdb.JoinBuilder) {
				q.On("order_items.order_id", "=", "orders.id").
					Where("order_items.qty", ">", 1)
			})
	}).ToSelect()
	// SQL:  SELECT * FROM `users` INNER JOIN (`orders` INNER JOIN `order_items` ON `order_items`.`order_id` = `orders`.`id` AND `order_items`.`qty` > ?) ON `orders`.`user_id` = `users`.`id`
	// args: [1]

	// 外层 LEFT JOIN + 内层 INNER JOIN 组
	sql, _, _ = db.Builder().Table("users").LeftJoinOn("orders", func(j *zcdb.JoinBuilder) {
		j.On("orders.user_id", "=", "users.id").
			JoinOn("order_items", func(q *zcdb.JoinBuilder) {
				q.On("order_items.order_id", "=", "orders.id")
			})
	}).ToSelect()
	// SQL: SELECT * FROM `users` LEFT JOIN (`orders` INNER JOIN `order_items` ON `order_items`.`order_id` = `orders`.`id`) ON `orders`.`user_id` = `users`.`id`

	// 内层 CrossJoinOn：MySQL / SQLite 直译
	sql, _, _ = db.Builder().Table("users").JoinOn("orders", func(j *zcdb.JoinBuilder) {
		j.On("orders.user_id", "=", "users.id").
			CrossJoinOn("coupons", "coupons.id", "=", "orders.coupon_id")
	}).ToSelect()
	// SQL: SELECT * FROM `users` INNER JOIN (`orders` CROSS JOIN `coupons` ON `coupons`.`id` = `orders`.`coupon_id`) ON `orders`.`user_id` = `users`.`id`

	// 同样的 CrossJoinOn 在 PostgreSQL 下内层转为 INNER JOIN
	pgSQL, _, _ := db.Builder().Table("users").JoinOn("orders", func(j *zcdb.JoinBuilder) {
		j.On("orders.user_id", "=", "users.id").
			CrossJoinOn("coupons", "coupons.id", "=", "orders.coupon_id")
	}).ToSelect()
	// PG SQL: SELECT * FROM "users" INNER JOIN ("orders" INNER JOIN "coupons" ON "coupons"."id" = "orders"."coupon_id") ON "orders"."user_id" = "users"."id"
	_, _ = pgSQL
}
```

**三方言可执行性**：括号 join 组在 MySQL、PostgreSQL、SQLite 都是合法语法，三方言集成测试均真实执行了 `users INNER JOIN (profiles INNER JOIN orders ON ...) ON ...`；外层 `LEFT JOIN` 加内层组的形态三方言也都接受（已用原生 SQL 实测）。执行结果受 ON 条件与连接类型影响，框架不重写语义。

**边界与错误**：

- 只有内层 `JoinOn`（INNER）与 `CrossJoinOn` 两种，没有内层 `LeftJoinOn`。内层 `CrossJoinOn` 在 PG 下同样改写为 `INNER JOIN`。
- 内层 JOIN 的条件先于外层 ON 编译，绑定参数也按此次序收集。
- 内层回调为空时生成没有 ON 的内层 JOIN；能否执行取决于方言（见下文「错误与边界」）。
- 子查询图存在环时编译前返回 `ErrCyclicQuery`。

**说明**：嵌套 join 组的用途是**显式控制 JOIN 的结合顺序**（MySQL 风格的括号连接），例如让 `LEFT JOIN (orders INNER JOIN order_items ON ...) ON ...` 先把订单与明细连接成一个整体再与主表左连接。

## 派生表 JOIN：JoinSub / LeftJoinSub / RightJoinSub / CrossJoinSub

签名：

- `func (b *Builder) JoinSub(sub *Builder, alias string, callback func(*JoinBuilder)) *Builder`
- `func (b *Builder) LeftJoinSub(sub *Builder, alias string, callback func(*JoinBuilder)) *Builder`
- `func (b *Builder) RightJoinSub(sub *Builder, alias string, callback func(*JoinBuilder)) *Builder`
- `func (b *Builder) CrossJoinSub(sub *Builder, alias string) *Builder`

**语义**：把子查询 Builder 作为 JOIN 的右表，编译为 `(SELECT ...) AS alias`。`JoinSub`/`LeftJoinSub`/`RightJoinSub` 用回调构造 ON 条件；`CrossJoinSub` 无 ON 条件，结果为笛卡尔积。`callback` 传 nil 等同空回调。

**SQL**（MySQL）：

```go
func example() {
	latest := db.Builder().Table("logs").Select("user_id").
		SelectRaw("MAX(created_at) AS last_at").GroupBy("user_id")

	sql, _, _ := db.Builder().Table("users").Select("users.name", "l.last_at").
		JoinSub(latest, "l", func(j *zcdb.JoinBuilder) {
			j.On("l.user_id", "=", "users.id")
		}).ToSelect()
	// SQL: SELECT `users`.`name`, `l`.`last_at` FROM `users` INNER JOIN (SELECT `user_id`, MAX(created_at) AS last_at FROM `logs` GROUP BY `user_id`) AS `l` ON `l`.`user_id` = `users`.`id`

	sql, _, _ = db.Builder().Table("users").Select("users.name").
		LeftJoinSub(latest, "l", func(j *zcdb.JoinBuilder) {
			j.On("l.user_id", "=", "users.id")
		}).ToSelect()
	// SQL: SELECT `users`.`name` FROM `users` LEFT JOIN (SELECT `user_id`, MAX(created_at) AS last_at FROM `logs` GROUP BY `user_id`) AS `l` ON `l`.`user_id` = `users`.`id`

	sql, _, _ = db.Builder().Table("users").Select("users.name").
		RightJoinSub(latest, "l", func(j *zcdb.JoinBuilder) {
			j.On("l.user_id", "=", "users.id")
		}).ToSelect()
	// SQL: SELECT `users`.`name` FROM `users` RIGHT JOIN (SELECT `user_id`, MAX(created_at) AS last_at FROM `logs` GROUP BY `user_id`) AS `l` ON `l`.`user_id` = `users`.`id`

	// CrossJoinSub：无 ON 的派生表笛卡尔积，常用于「维度组合矩阵」
	months := db.Builder().Table("months").Select("month")
	sql, _, _ = db.Builder().Table("stores").Select("stores.name", "m.month").
		CrossJoinSub(months, "m").ToSelect()
	// SQL: SELECT `stores`.`name`, `m`.`month` FROM `stores` CROSS JOIN (SELECT `month` FROM `months`) AS `m`
}
```

**边界与错误**：

- 子查询的绑定参数**先于** ON 条件计入总绑定；`JoinSub` 的 SQL 中派生表在 ON 之前，绑定顺序与 SQL 文本一致。
- `alias` 与 `sub` 不做空值校验：`sub` 为 nil 或别名为空会生成不可执行的 SQL。
- 子查询自身的错误（如非法运算符）同样上抛；自引用/互引用返回 `ErrCyclicQuery`。
- `RightJoinSub` 的 RIGHT JOIN 限制同 `RightJoin`（SQLite 3.39 以下不支持）。

**说明**：ON 条件里引用派生表列时必须带上别名（`l.user_id`），子查询内部也要用 `SELECT ... AS ...` 暴露需要的列。`CrossJoinSub` 的典型用法是先构造「门店 × 月份」等维度组合，再 `LEFT JOIN` 事实表补零，避免事实表缺失的组合被漏掉。

## ON 条件绑定顺序与 nil/Expression 特例

### 绑定顺序

SELECT 的总绑定顺序是 **SELECT 子查询 → FROM 子查询 → JOIN → WHERE → GROUP BY → HAVING → UNION**。其中 JOIN 部分按下面的规则展开：

1. JOIN 按调用顺序排列；
2. 每个 JOIN 内部：派生表子查询绑定 → 嵌套 join 组绑定（递归，内层先于外层）→ 该 JOIN 自身的 ON 条件绑定；
3. ON 条件按回调内的加入顺序排列，`WhereNested`/`OnNested` 组内的绑定在组出现的位置展开。

```go
func example() {
	hot := db.Builder().Table("hot_products").Select("id").
		Where("weight", ">", 10)
	sql, args, _ := db.Builder().Table("users").Select("users.name").
		JoinSub(hot, "h", func(j *zcdb.JoinBuilder) {
			j.On("h.id", "=", "users.id").
				Where("h.kind", "=", "vip")
		}).
		Where("users.status", "=", "active").
		ToSelect()
	// SQL:  SELECT `users`.`name` FROM `users` INNER JOIN (SELECT `id` FROM `hot_products` WHERE `weight` > ?) AS `h` ON `h`.`id` = `users`.`id` AND `h`.`kind` = ? WHERE `users`.`status` = ?
	// args: [10 vip active]
	_, _ = sql, args
}
```

PG 的 `$N` 编号跟随同一顺序全局递增；MySQL/SQLite 的 `?` 顺序即 args 顺序。同一 JOIN 上混合 IN、嵌套组与 Raw 时，绑定仍按条件的加入顺序排列：

```go
func example() {
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
	_, _ = sql, args
}
```

### 各条件的绑定行为

| 条件 | SQL 形态 | 是否产生绑定 |
|---|---|---|
| `On` / `OrOn` | `a op b` | 无 |
| `Where` / `OrWhere` 普通值 | `column op ?` | 1 个 |
| `Where` / `OrWhere` + `nil` | `IS NULL` / `IS NOT NULL` | 无 |
| `Where` / `OrWhere` + `Expression` | `column op <表达式>` | 无 |
| `Where` / `OrWhere` + `*Builder` | `column op (SELECT ...)` | 子查询绑定 |
| `WhereNull` / `WhereNotNull` | `column IS [NOT] NULL` | 无 |
| `WhereIn` / `WhereNotIn` + 列表 | `IN (?, ?)` / `NOT IN (?)` | 每个元素 1 个 |
| `WhereIn` / `WhereNotIn` + 空列表 | `0 = 1` / `1 = 1` | 无 |
| `WhereIn` / `WhereNotIn` + `*Builder` | `IN (SELECT ...)` | 子查询绑定 |
| `WhereExists` | `EXISTS (SELECT ...)` | 子查询绑定 |
| `WhereNested` / `OrWhereNested` / `OnNested` | `(...)` | 组内条件的绑定 |
| `Raw` | 原始片段 | 按 `?` 位置；`Expression` 绑定内联不占位 |

### nil 特例

- 只有 `Where`/`OrWhere` 的**值**位置有 nil 特判：`= nil` → `IS NULL`，`!= nil` / `<> nil` → `IS NOT NULL`，均不占绑定。
- 其它运算符遇 nil 仍生成占位符并绑定 nil（比较结果按数据库的 NULL 语义为 UNKNOWN）。
- `WhereNull`/`WhereNotNull` 是独立方法，不经过该特判。

### Expression 特例

- `Where`/`OrWhere` 的值、`Raw` 的绑定元素可以是 `zcdb.NewExpression(...)`：直接内联为 SQL 文本，不生成占位符、不占绑定。
- `Expression` 内容应是不含 `?` 的完整片段；内容原样拼接，防注入责任归调用方。
- `WhereIn`/`WhereNotIn` 的**列表元素**不做内联；`HavingBetween` 等其它位置同样不支持内联，见 [builder-order-group](../reference/builder-order-group.md)。

### 子查询与空列表

- `Where` 的 `*Builder` 值、`WhereIn`/`WhereNotIn` 的 `*Builder`、`WhereExists` 的两种入参，都把子查询的完整绑定按条件位置插入。
- 空 `IN` 列表编译为恒假 `0 = 1`，空 `NOT IN` 列表编译为恒真 `1 = 1`，两者都不产生绑定。

## 错误与边界

### 空 ON 条件的可执行性

回调不产生任何条件时（`JoinOn`/`LeftJoinOn`/`JoinSub` 等空回调，或条件全部被忽略），编译结果中不输出 `ON`。三方言对无 ON 的 JOIN 接受程度不同（实测 MySQL 8.4 / PostgreSQL 15 / SQLite 3.53）：

| 编译形态 | MySQL | PostgreSQL | SQLite |
|---|---|---|---|
| `INNER JOIN t`（无 ON） | 可执行（等价笛卡尔积） | 语法错误（要求 ON/USING） | 可执行（等价笛卡尔积） |
| `LEFT JOIN t`（无 ON） | 语法错误 | 语法错误 | 可执行（按无条件连接处理） |
| `(INNER JOIN 组)` 无外层 ON | 可执行 | 语法错误 | 可执行 |

因此 `LeftJoinOn`/`LeftJoinSub` 至少应给一个 ON 条件；`JoinOn` 空回调在 MySQL/SQLite 能跑通，但 PG 会报错，跨方言代码不应依赖该形态。

### 错误来源

| 错误 | 触发点 |
|---|---|
| `ErrInvalidOperator` | 简写方法与 `On`/`OrOn`/`Where`/`OrWhere` 的 `op` 不在白名单 |
| `ErrInvalidWhereInValues` | `WhereIn`/`WhereNotIn` 的 `values` 既不是 `[]any` 也不是 `*Builder` |
| `ErrInvalidSubQuery` | `WhereExists` 的 `sub` 类型非法 |
| `ErrEmptyTable` | 外层查询没有 `Table`/`TableSub` 就编译 |
| `ErrCyclicQuery` | 派生表或 EXISTS 子查询形成自引用/互引用 |

错误在链式调用时记录、在 `ToSelect` 等编译方法返回；具体错误变量与 `errors.Is` 匹配方式见 [errors](../reference/errors.md)。

### 其它边界

- 单条件简写与 `JoinSub` 的 ON 只有列比较；值比较、NULL/IN/EXISTS 一律走 `JoinBuilder`。
- `JoinBuilder` 没有 `WhereNotExists`、`WhereBetween`、`WhereLike`、空安全比较与 OR 版的 `WhereIn`；缺失能力用 `Raw` 表达。
- 带 ON 条件的 JOIN 会计入 `Update`/`Increment`/`Decrement`/`DeleteJoin` 的有效限定判定；无条件 JOIN 不计入（见 [safety](../concepts/safety.md) 与 [builder-query](../reference/builder-query.md)）。

## 相关文档

- [builder-core](../reference/builder-core.md)：Builder 状态、错误累积、`Primary` 与 `Clone` 的入口
- [builder-where](../reference/builder-where.md)：`Builder.Where` 的 nil/Expression 语义（与本页 ON 条件对齐）
- [builder-select](../reference/builder-select.md)：表、列与 FROM 子查询
- [builder-query](../reference/builder-query.md)：`Update`/`DeleteJoin` 如何消费 JOIN 状态
- [builder-compile](../reference/builder-compile.md)：`CompileRaw` 与 `Expression` 的完整规则
- [dialect-matrix](../reference/dialect-matrix.md)：三方言编译差异总表
- [errors](../reference/errors.md)：错误变量与匹配方式
- [dynamic-queries](../how-to/dynamic-queries.md)：动态拼装 JOIN 的用法
- [builder-state](../concepts/builder-state.md)：JOIN 状态的克隆与隔离
- [compile-pipeline](../concepts/compile-pipeline.md)：编译顺序与绑定收集
- [safety](../concepts/safety.md)：写操作保护与有效限定
