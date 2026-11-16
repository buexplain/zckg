# WHERE 条件：方法全览与逐条语义

WHERE 是 Builder 的**追加状态**：每条条件以一个 `WhereClause` 追加到 `wheres`，各自记录 `Boolean`（`AND`/`OR`）。编译时按加入顺序平铺输出，**不会自动加括号**；只有嵌套条件组（`WhereNested` 系列）和子查询自带括号。因此 `Where(a).OrWhere(b).Where(c)` 编译为 `a OR b AND c`，AND 的优先级高于 OR——需要改变结合范围时必须显式分组。

绑定参数按条件在 SQL 中出现的顺序收集：`Where` 的值、`WhereIn` 的每个元素、`Between` 的 min/max、`Raw` 的每个占位符、子查询里各自的条件依次排列；`Expression`、nil 特判、列比较与纯列引用不占绑定。完整顺序见 [compile-pipeline](../concepts/compile-pipeline.md)。

本文覆盖全部 WHERE 家族方法。`Where` 的构造只写 `wheres`；`JOIN ON` 条件属于 [builder-join](../reference/builder-join.md)，`HAVING` 属于 [builder-order-group](../reference/builder-order-group.md)。

示例统一为 MySQL 形态（反引号、`?` 占位符），假定 DAO 第五参数为 `""`（无注释）。`db` 是外部占位变量，示例只保证语法可解析。

## 先读结论

1. **基本比较**用 `Where`/`OrWhere`（两参简写缺省 `=`）；运算符走白名单，`IN`/`BETWEEN` 不在白名单，必须用专用方法。
2. **集合**用 `WhereIn`/`WhereNotIn`：空列表分别编译为恒假 `0 = 1` / 恒真 `1 = 1`；`NOT IN` 的恒真不会收窄写操作范围。
3. **区间**用 `Between` 系列：值区间（两个绑定）、两列区间（无绑定）、值在左（一个绑定）；全部闭区间，绑定顺序先 min 后 max。
4. **NULL** 有两种语义：`WhereNull`/`WhereNotNull` 是 `IS [NOT] NULL`（无绑定）；`WhereNullSafeEquals`/`WhereNullSafeNotEquals` 是空安全比较（绑定 nil 也参与运算）。
5. **子查询**分三种：`EXISTS`、`列 op (SELECT …)`、`列 IN (SELECT …)`；子查询必须自己设置数据源。
6. 构造期错误写入 `Builder.err`（`ErrInvalidOperator`、`ErrInvalidSubQuery`），由编译入口统一返回；链式调用不会中断。

## 方法索引

| 分类 | 方法 | MySQL 编译形态 | 绑定数 |
|---|---|---|---|
| 基本 | `Where` / `OrWhere` | `` `col` op ? ``；nil 时 `IS [NOT] NULL`；Expression 内联 | 0 或 1 |
| 基本 | `WhereColumn` | `` `a` op `b` `` | 0 |
| 基本 | `WhereRaw` / `OrWhereRaw` | 原始片段，代码区 `?` 按序绑定 | 按占位符 |
| 集合 | `WhereIn` / `WhereNotIn` | `` `col` IN (?, ?) `` / `NOT IN`；空列表为 `0 = 1` / `1 = 1` | 每元素 1 |
| 空值 | `WhereNull` / `WhereNotNull` | `` `col` IS [NOT] NULL ``，多列展开为多个 AND | 0 |
| 区间 | `WhereBetween` / `OrWhereBetween` | `` `col` BETWEEN ? AND ? `` | 2 |
| 区间 | `WhereNotBetween` / `OrWhereNotBetween` | `` `col` NOT BETWEEN ? AND ? `` | 2 |
| 区间 | `WhereBetweenColumns` / `OrWhereBetweenColumns` | `` `col` BETWEEN `min` AND `max` `` | 0 |
| 区间 | `WhereNotBetweenColumns` / `OrWhereNotBetweenColumns` | `` `col` NOT BETWEEN `min` AND `max` `` | 0 |
| 区间 | `WhereValueBetween` / `OrWhereValueBetween` | `` ? BETWEEN `min` AND `max` `` | 1 |
| 模糊 | `WhereLike` / `OrWhereLike` | `[BINARY] col LIKE ?`（PG 默认 `ILIKE`；SQLite 区分大小写时 `GLOB`） | 0 或 1 |
| 模糊 | `WhereNotLike` | `col NOT LIKE ?`（PG 为 `NOT ILIKE`） | 0 或 1 |
| 空安全 | `WhereNullSafeEquals` | `` `col` <=> ? ``（PG `IS NOT DISTINCT FROM`；SQLite `IS`） | 1 |
| 空安全 | `WhereNullSafeNotEquals` | `` NOT `col` <=> ? ``（PG `IS DISTINCT FROM`；SQLite `IS NOT`） | 1 |
| 日期 | `WhereDate` | `date(col) = ?`（PG `col::date`；SQLite `strftime`） | 1 |
| 嵌套 | `WhereNested` / `OrWhereNested` | `(组内条件)` | 组内 |
| 嵌套 | `WhereAll` / `OrWhereAll` | `(组内 AND 条件)`（等价 `WhereNested`） | 组内 |
| 嵌套 | `WhereAny` / `OrWhereAny` | `(组内条件，顶层改为 OR)` | 组内 |
| 嵌套 | `WhereNone` / `OrWhereNone` | `NOT (组内条件，顶层改为 OR)` | 组内 |
| 嵌套 | `WhereNot` / `OrWhereNot` | `NOT (组内条件，默认 AND)` | 组内 |
| 子查询 | `WhereExists` / `OrWhereExists` | `[OR] EXISTS (SELECT …)` | 子查询 |
| 子查询 | `WhereNotExists` / `OrWhereNotExists` | `[OR] NOT EXISTS (SELECT …)` | 子查询 |
| 子查询 | `WhereSub` / `OrWhereSub` | `` `col` op (SELECT …) `` | 子查询 |
| 子查询 | `WhereInSub` / `WhereNotInSub` | `` `col` IN (SELECT …) `` / `NOT IN` | 子查询 |

没有 Or 变体的方法：`WhereColumn`、`WhereDate`、`WhereIn`/`WhereNotIn`、`WhereNull`/`WhereNotNull`、`WhereNullSafeEquals`/`WhereNullSafeNotEquals`、`WhereNotLike`、`WhereInSub`/`WhereNotInSub`。需要 OR 时用对应的 `OrWhereRaw` 或包在 `OrWhereNested` 里。

## Where / OrWhere（基本比较）

### Where

```text
func (b *Builder) Where(column string, op any, value ...any) *Builder
```

**语义**：追加一个 AND 条件。支持三参 `(列, 运算符, 值)` 与两参简写 `(列, 值)`（缺省 `=`）；三参时 `op` 必须是字符串且落在运算符白名单内。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").Where("age", ">", 25).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `age` > ?
	// args: [25]

	sqlStr, args, _ = db.Builder().Table("users").Where("status", "active").ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `status` = ?
	// args: [active]

	sqlStr, _, _ = db.Builder().Table("users").Where("deleted_at", "=", nil).ToSelect()
	// SQL: SELECT * FROM `users` WHERE `deleted_at` IS NULL

	sqlStr, _, _ = db.Builder().Table("users").
		Where("id", "=", zcdb.NewExpression("parent_id")).ToSelect()
	// SQL: SELECT * FROM `users` WHERE `id` = parent_id
	_, _, _ = sqlStr, args, db
}
```

**边界与错误**：

- 运算符白名单（大小写不敏感匹配）：`=` `!=` `<>` `<` `>` `<=` `>=` `LIKE` `NOT LIKE` `IS` `IS NOT`。不在白名单（含 `IN`、`NOT IN`、`BETWEEN`、`NOT BETWEEN`）或三参形式 `op` 不是字符串时，调用处把 `ErrInvalidOperator` 写入 `Builder.err`，编译入口返回该错误。
- 白名单只校验、不改写：运算符按传入文本输出（`Like` 小写也能通过，SQL 关键字大小写不敏感）。
- **nil 特判只覆盖 `=`、`!=`、`<>`**：`= nil` 编译为 `IS NULL`，`!= nil`/`<> nil` 编译为 `IS NOT NULL`；其它运算符遇到 nil 仍生成占位符并绑定 nil（例如 `LIKE NULL` 恒为未知）。两参简写 `Where("col", nil)` 等价于 `Where("col", "=", nil)`。
- 值传 `zcdb.NewExpression("原始 SQL")` 时直接内联为 SQL 文本，不占绑定；Expression 内容中的 `?` 不会配到绑定参数。
- 超过三参调用（`op` 后给多个值）只取第一个值，多余值被静默忽略；不要依赖这种写法。
- 条件值以 `any` 原样交给驱动，不做类型转换。

**与其他方法的区别**：`Where` 只处理单值比较与白名单运算符；集合用 `WhereIn`、区间用 `WhereBetween`、NULL 判断用 `WhereNull`、模糊匹配用 `WhereLike`、复杂表达式用 `WhereRaw`。列到列比较用 `WhereColumn`；`Where("a", "=", NewExpression("b"))` 虽然也能生成 `a = b`，但语义上是「内联一段表达式」，不会像 `WhereColumn` 那样包裹两侧标识符。

### OrWhere

```text
func (b *Builder) OrWhere(column string, op any, value ...any) *Builder
```

**语义**：与 `Where` 完全相同，只是把该条条件的 `Boolean` 记为 `OR`。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").
		Where("age", ">", 25).OrWhere("vip", "=", 1).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `age` > ? OR `vip` = ?
	// args: [25 1]
	_, _ = sqlStr, args
}
```

**边界与错误**：形式、nil 特判、白名单与错误规则同 `Where`。裸 `OR` 不改变优先级：`Where("a", 1).OrWhere("b", 2).Where("c", 3)` 编译为 `a = ? OR b = ? AND c = ?`，等价于 `a OR (b AND c)`；需要 `(a OR b) AND c` 请用 `WhereNested`/`OrWhereNested` 或 `WhereAny`。

**与其他方法的区别**：需要 OR 连接一个括号组时用 `OrWhereNested`/`OrWhereAny`；需要 OR 连接原始 SQL 时用 `OrWhereRaw`。

## WhereIn / WhereNotIn（集合）

### WhereIn

```text
func (b *Builder) WhereIn(column string, values []any) *Builder
```

**语义**：追加 `column IN (值列表)` 条件。每个非 Expression 元素占一个占位符，按切片顺序绑定；空列表（含 nil 切片）编译为恒假条件 `0 = 1`。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").WhereIn("id", []any{1, 2, 3}).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `id` IN (?, ?, ?)
	// args: [1 2 3]

	sqlStr, _, _ = db.Builder().Table("users").WhereIn("id", []any{}).ToSelect()
	// SQL: SELECT * FROM `users` WHERE 0 = 1
	_, _ = sqlStr, args
}
```

**边界与错误**：

- 只接受 `[]any`；类型化切片需要自行转换。
- 空列表与 nil 切片都编译为 `0 = 1`（恒假），无绑定参数。
- 元素可以是 `Expression`：内联为 SQL 文本且不占占位符（例如 `WhereIn("id", []any{1, zcdb.NewExpression("2 + 3")})`）。
- 列表按引用保存，调用后修改切片内容会影响编译产物；需要稳定快照时先复制。
- 不限制元素个数；占位符数量受驱动/数据库上限约束（如 PostgreSQL 的绑定参数上限）。

**与其他方法的区别**：`WhereIn` 是值列表；`WhereInSub` 是子查询；`WhereBetween` 是连续区间。空 `WhereIn` 恒假，会让整个查询无结果，但不构成写保护问题（见文末）。

### WhereNotIn

```text
func (b *Builder) WhereNotIn(column string, values []any) *Builder
```

**语义**：追加 `column NOT IN (值列表)`；元素与空列表规则同 `WhereIn`，空列表编译为恒真条件 `1 = 1`。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").
		WhereNotIn("status", []any{"banned", "frozen"}).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `status` NOT IN (?, ?)
	// args: [banned frozen]
	_, _ = sqlStr, args
}
```

**边界与错误**：

- 空列表 `1 = 1` 是恒真条件：**不会收窄写操作范围**，不能单独满足 `Update`/`Delete` 的有效 WHERE 保护；与 OR 组合后整体恒真时同样被拒绝（见 [safety](../concepts/safety.md)）。
- 列表含 NULL 时 `NOT IN` 的三值逻辑可能让整行不成立（SQL 语义，不是本库改写）；需要「不存在于子集」语义时优先考虑 `WhereNotExists`。
- 其余边界同 `WhereIn`（`[]any`、Expression 内联、切片按引用）。

**与其他方法的区别**：`WhereNotIn` 与 `WhereIn` 只差 `NOT IN`；与 `WhereNotExists` 的区别在 NULL 语义与执行计划，后者更适合子查询集合。

## WhereNull / WhereNotNull（空值判断）

### WhereNull

```text
func (b *Builder) WhereNull(columns ...string) *Builder
```

**语义**：追加一个或多个 `column IS NULL` 条件；多列按参数顺序展开为多个 AND 条件，每个条件都是独立的 `WhereClause`。

**SQL**：

```go
func example() {
	sqlStr, _, _ := db.Builder().Table("users").WhereNull("deleted_at", "remark").ToSelect()
	// SQL: SELECT * FROM `users` WHERE `deleted_at` IS NULL AND `remark` IS NULL
	_, _ = sqlStr, db
}
```

**边界与错误**：无绑定参数；列名经 `WrapColumn` 包裹，不做存在性校验；`WhereNull()` 无参是空操作（不追加任何条件）。没有 `OrWhereNull` 变体；需要 OR 时用 `OrWhereRaw`。

**与其他方法的区别**：`Where("col", "=", nil)` 对单列生成同样的 `IS NULL`，但 `WhereNull` 支持多列且语义更明确；`WhereNullSafeEquals(col, nil)` 生成方言相关的空安全比较（MySQL `<=> ?` 绑定 nil），与 `IS NULL` 的 SQL 形态不同。

### WhereNotNull

```text
func (b *Builder) WhereNotNull(columns ...string) *Builder
```

**语义**：追加一个或多个 `column IS NOT NULL` 条件，多列展开为多个 AND 条件。

**SQL**：

```go
func example() {
	sqlStr, _, _ := db.Builder().Table("users").WhereNotNull("email").ToSelect()
	// SQL: SELECT * FROM `users` WHERE `email` IS NOT NULL
	_, _ = sqlStr, db
}
```

**边界与错误**：无绑定参数；多列展开；无参空操作；无 Or 变体。

**与其他方法的区别**：对应 `Where("col", "!=", nil)`（或 `<> nil`），但支持多列且不受「运算符必须是白名单字符串」约束。

## BETWEEN 家族（区间）

五种形态、十个方法：

| 形态 | 方法（含 Or 变体） | SQL 形态 | 绑定 |
|---|---|---|---|
| 值区间 | `WhereBetween` / `WhereNotBetween` / `OrWhereBetween` / `OrWhereNotBetween` | `col [NOT] BETWEEN ? AND ?` | 2 |
| 两列区间 | `WhereBetweenColumns` / `WhereNotBetweenColumns` / `OrWhereBetweenColumns` / `OrWhereNotBetweenColumns` | `` col [NOT] BETWEEN `min` AND `max` `` | 0 |
| 值在左 | `WhereValueBetween` / `OrWhereValueBetween` | `` ? BETWEEN `min` AND `max` `` | 1 |

全部为**闭区间**。值区间按「先 min 后 max」绑定；值在左形态只有一个绑定（`value`）。值区间的 min/max 是普通绑定值：**不做 nil 特判，也不识别 Expression**（Expression 会作为值交给驱动，通常在执行期报错）；两列区间的三个参数都是列名，无绑定。

### WhereBetween

```text
func (b *Builder) WhereBetween(column string, min, max any) *Builder
```

**语义**：追加 `column BETWEEN min AND max` 的 AND 条件（闭区间）。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").WhereBetween("age", 18, 30).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `age` BETWEEN ? AND ?
	// args: [18 30]
	_, _ = sqlStr, args
}
```

**边界与错误**：min/max 各占一个绑定；传 nil 会生成 `BETWEEN NULL AND NULL`（结果恒未知），不做 `IS NULL` 转换；`Expression` 不会内联。列名经 `WrapColumn`。

**与其他方法的区别**：`WhereBetweenColumns` 的两端是表中的列（无绑定）；`WhereValueBetween` 是值在左、列在右。非闭区间（`>`/`<`）用 `Where`。

### WhereNotBetween

```text
func (b *Builder) WhereNotBetween(column string, min, max any) *Builder
```

**语义**：追加 `column NOT BETWEEN min AND max` 的 AND 条件。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").WhereNotBetween("age", 18, 30).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `age` NOT BETWEEN ? AND ?
	// args: [18 30]
	_, _ = sqlStr, args
}
```

**边界与错误**：同 `WhereBetween`；注意 `NOT BETWEEN` 对 NULL 也是未知，不会命中 NULL 行。

**与其他方法的区别**：`WhereNotBetweenColumns` 是列到列形态；`WhereNotIn` 是离散集合取反。

### OrWhereBetween

```text
func (b *Builder) OrWhereBetween(column string, min, max any) *Builder
```

**语义**：与 `WhereBetween` 相同，`Boolean` 记为 `OR`。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").
		Where("vip", "=", 1).OrWhereBetween("age", 18, 30).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `vip` = ? OR `age` BETWEEN ? AND ?
	// args: [1 18 30]
	_, _ = sqlStr, args
}
```

**边界与错误**：绑定顺序仍为 min、max，且排在 OR 前一条条件的绑定之后。

**与其他方法的区别**：需要把「(vip OR age 区间)」再与其它条件 AND 时，用 `WhereNested` 包裹。

### OrWhereNotBetween

```text
func (b *Builder) OrWhereNotBetween(column string, min, max any) *Builder
```

**语义**：`OR` 连接的 `NOT BETWEEN` 条件。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").
		Where("vip", "=", 1).OrWhereNotBetween("age", 18, 30).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `vip` = ? OR `age` NOT BETWEEN ? AND ?
	// args: [1 18 30]
	_, _ = sqlStr, args
}
```

**边界与错误**：同 `WhereNotBetween`，仅布尔连接不同。

**与其他方法的区别**：`OrWhereNotBetweenColumns` 是列到列形态。

### WhereBetweenColumns

```text
func (b *Builder) WhereBetweenColumns(column, min, max string) *Builder
```

**语义**：追加两列区间比较 `column BETWEEN min AND max`，三个参数都是列名，经 `WrapColumn` 包裹，**无绑定参数**。实现上借助 `WhereRaw` 生成 SQL。

**SQL**：

```go
func example() {
	sqlStr, _, _ := db.Builder().Table("products").
		WhereBetweenColumns("price", "min_price", "max_price").ToSelect()
	// SQL: SELECT * FROM `products` WHERE `price` BETWEEN `min_price` AND `max_price`
	_, _ = sqlStr, db
}
```

**边界与错误**：列名不做存在性校验；空列名会生成空标识符；无绑定、无 nil/Expression 语义（参数是 `string`）。方言只影响包裹符与绑定形态（本方法没有绑定）。

**与其他方法的区别**：`WhereBetween` 的 min/max 是值；本方法的 min/max 是列，适合「区间存在表里」的行级比较。参数顺序是「被比较列, 下界列, 上界列」。

### WhereNotBetweenColumns

```text
func (b *Builder) WhereNotBetweenColumns(column, min, max string) *Builder
```

**语义**：追加 `column NOT BETWEEN min AND max` 的两列区间取反条件，无绑定。

**SQL**：

```go
func example() {
	sqlStr, _, _ := db.Builder().Table("products").
		WhereNotBetweenColumns("price", "min_price", "max_price").ToSelect()
	// SQL: SELECT * FROM `products` WHERE `price` NOT BETWEEN `min_price` AND `max_price`
	_, _ = sqlStr, db
}
```

**边界与错误**：同 `WhereBetweenColumns`；`NOT BETWEEN` 不会命中 NULL 行。

**与其他方法的区别**：`WhereNotBetween` 是值区间取反；本方法是列区间取反。

### OrWhereBetweenColumns

```text
func (b *Builder) OrWhereBetweenColumns(column, min, max string) *Builder
```

**语义**：`OR` 连接的两列区间条件。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("products").
		Where("vip", "=", 1).OrWhereBetweenColumns("price", "min_price", "max_price").ToSelect()
	// SQL:  SELECT * FROM `products` WHERE `vip` = ? OR `price` BETWEEN `min_price` AND `max_price`
	// args: [1]
	_, _ = sqlStr, args
}
```

**边界与错误**：本方法自身无绑定；`args` 只来自前面的 `vip = ?`。

**与其他方法的区别**：`OrWhereBetween` 是值区间。

### OrWhereNotBetweenColumns

```text
func (b *Builder) OrWhereNotBetweenColumns(column, min, max string) *Builder
```

**语义**：`OR` 连接的两列区间取反条件。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("products").
		Where("vip", "=", 1).OrWhereNotBetweenColumns("price", "min_price", "max_price").ToSelect()
	// SQL:  SELECT * FROM `products` WHERE `vip` = ? OR `price` NOT BETWEEN `min_price` AND `max_price`
	// args: [1]
	_, _ = sqlStr, args
}
```

**边界与错误**：同 `WhereNotBetweenColumns`，仅布尔连接不同。

**与其他方法的区别**：`OrWhereNotBetween` 是值区间取反。

### WhereValueBetween

```text
func (b *Builder) WhereValueBetween(value any, min, max string) *Builder
```

**语义**：追加「给定值落在表中区间内」的条件：`? BETWEEN min AND max`，值占一个绑定，min/max 是列名。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").WhereValueBetween(25, "min_age", "max_age").ToSelect()
	// SQL:  SELECT * FROM `users` WHERE ? BETWEEN `min_age` AND `max_age`
	// args: [25]
	_, _ = sqlStr, args
}
```

**边界与错误**：参数顺序是「值, 下界列, 上界列」，与 `WhereBetween(column, min, max)` 的「列, 值, 值」不同，容易混用。实现基于 `WhereRaw`：`value` 为 `Expression` 时会被内联，`nil` 会作为绑定值（`NULL BETWEEN …` 恒未知）。

**与其他方法的区别**：`WhereBetweenColumns` 是「列 BETWEEN 列 AND 列」；本方法是「值 BETWEEN 列 AND 列」。

### OrWhereValueBetween

```text
func (b *Builder) OrWhereValueBetween(value any, min, max string) *Builder
```

**语义**：`OR` 连接的值在左区间条件。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").
		Where("vip", "=", 1).OrWhereValueBetween(25, "min_age", "max_age").ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `vip` = ? OR ? BETWEEN `min_age` AND `max_age`
	// args: [1 25]
	_, _ = sqlStr, args
}
```

**边界与错误**：`value` 的绑定排在前面条件的绑定之后；Expression/nil 规则同 `WhereValueBetween`。

**与其他方法的区别**：`OrWhereBetween` 是值区间（列在左）。

## WhereRaw / OrWhereRaw

### WhereRaw

```text
func (b *Builder) WhereRaw(sql string, bindings ...any) *Builder
```

**语义**：把一段原始 SQL 作为 AND 条件直接嵌入，代码区里的 `?` 按位置绑定 `bindings`。用于函数、运算符、`INTERVAL`、`EXISTS` 等无法用固定方法表达的条件。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").
		WhereRaw("created_at > NOW() - INTERVAL ? DAY", 7).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE created_at > NOW() - INTERVAL ? DAY
	// args: [7]

	sqlStr, args, _ = db.Builder().Table("users").
		WhereRaw(`name LIKE '%?%' AND age > ?`, 18).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE name LIKE '%?%' AND age > ?
	// args: [18]
	_, _ = sqlStr, args
}
```

**边界与错误**：

- 占位符识别与数据库引擎一致：**只认代码区里的 `?`**；字符串字面量、引号标识符与注释里的 `?` 是字面内容，原样保留（`WhereRaw("name LIKE '%?%'")` 不会被改写）。
- PostgreSQL 下编译产物把代码区 `?` 转成 `$N`。若要在代码区写 jsonb 的 `?` 操作符，写成 `??` 转义为一个字面 `?` 且不消耗绑定；直接写单个 `?` 会被当占位符编译成 `$N`，产物执行报语法错误。MySQL/SQLite 无此转义，`??` 就是两个占位符。
- 已知未覆盖语法（按代码区处理）：SQLite 的 `[...]` 标识符、MySQL 的 `/*! */` 可执行注释、PostgreSQL 的 `U&'...'`；在这些位置写 `?` 会与引擎解析不同，绑定可能错位。扫描器的完整边界见 [builder-compile](../reference/builder-compile.md) 的原始 SQL 部分与 `zcdb/sqlScan.go`。
- `bindings` 中的 `Expression` 直接内联为 SQL 文本且**不消耗占位符**；其余值按位置绑定。
- 编译期**不校验**占位符数量与绑定数量是否一致（与 `Grammar.CompileRaw` 的 `ErrRawPlaceholderMismatch` 不同）；数量不一致由驱动/数据库在执行期报错。
- `sql` 为空字符串时该条件被整个跳过（不产生子句，也不产生悬挂的连接词）；**纯空白字符串不算空**，会原样进入 SQL，可能生成 `WHERE    ` 或 `WHERE     AND …` 这类非法语句，不要用它表示「无条件」。
- 原始片段不做标识符包裹、不做语法校验；拼接外部输入等于放弃占位符防线。

**与其他方法的区别**：`WhereColumn` 会包裹两侧标识符，`WhereRaw` 不会；`Where` 的 Expression 值只内联一个值，`WhereRaw` 的 Expression 绑定可以出现在片段任意代码区位置。`Grammar.CompileRaw` 是把参数内联成可执行 SQL（会校验占位符数量），`WhereRaw` 只是构造条件。

### OrWhereRaw

```text
func (b *Builder) OrWhereRaw(sql string, bindings ...any) *Builder
```

**语义**：与 `WhereRaw` 相同，`Boolean` 记为 `OR`。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").
		Where("status", "active").OrWhereRaw("score > ?", 90).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `status` = ? OR score > ?
	// args: [active 90]
	_, _ = sqlStr, args
}
```

**边界与错误**：占位符、Expression、空串/空白串规则同 `WhereRaw`；裸 `OR` 的优先级陷阱同 `OrWhere`，需要分组时包 `OrWhereNested`。

**与其他方法的区别**：同 `WhereRaw` 与 `Where` 的关系。

## WhereColumn（两列比较）

```text
func (b *Builder) WhereColumn(first string, op string, second string) *Builder
```

**语义**：追加两列比较条件，两侧都经 `WrapColumn` 包裹，**无绑定参数**；`op` 走与 `Where` 相同的白名单校验。

**SQL**：

```go
func example() {
	sqlStr, _, _ := db.Builder().Table("users").
		WhereColumn("updated_at", ">", "created_at").ToSelect()
	// SQL: SELECT * FROM `users` WHERE `updated_at` > `created_at`
	_, _ = sqlStr, db
}
```

**边界与错误**：

- `op` 不在白名单时把 `ErrInvalidOperator` 写入 `Builder.err`，编译返回；`LIKE`/`IS`/`IS NOT` 也在白名单内，因此 `WhereColumn("a", "LIKE", "b")` 会生成 `a LIKE b`。
- 两侧都是标识符，不支持 `Expression` 类型；需要内联表达式时用 `Where` 的 Expression 值或 `WhereRaw`。
- 列名不做存在性校验；`表.列` 形式会拆分包裹；无 Or 变体。

**与其他方法的区别**：`Where` 的「值」是绑定参数（除非传 Expression），`WhereColumn` 的「第二操作数」是列引用。`Where("a", "=", zcdb.NewExpression("b"))` 生成 `a = b` 但不会包裹 `b`，语义与安全性都不同。

## 嵌套逻辑组

回调接收一个新的 `*Builder`，组内条件整体加一层括号。所有嵌套方法共有的边界：

- 构建回调用的 Builder 与当前 Builder 共享 `grammar`/`dao`，并复制当前表名；但组编译**只读取它的 `wheres`**——回调里写的 `Table`/`Select`/`Join` 不会进入外层 SQL。
- 回调未添加任何条件时，该组不会被追加（不产生空括号，也不会产生悬挂的 `AND`/`OR`）。
- 回调内的构造错误（如非法运算符）会提升到父 Builder 的 `err`，编译时统一返回。
- 嵌套组作为一条 `WhereClause` 参与外层平铺；组内绑定在组出现的位置按序收集。
- 没有嵌套深度限制，但真正的分组只影响布尔结合，不改变编译出的绑定顺序。

| 方法 | 组内默认连接 | 外层连接 | 编译形态 |
|---|---|---|---|
| `WhereNested` / `OrWhereNested` | AND | AND / OR | `(a AND b)` / `OR (a AND b)` |
| `WhereAll` / `OrWhereAll` | AND | AND / OR | 与 `WhereNested` 完全相同 |
| `WhereAny` / `OrWhereAny` | OR（顶层 Boolean 强制改写） | AND / OR | `(a OR b)` / `OR (a OR b)` |
| `WhereNone` / `OrWhereNone` | OR + 整体取反 | AND / OR | `NOT (a OR b)` / `OR NOT (a OR b)` |
| `WhereNot` / `OrWhereNot` | AND（可被 `OrWhere` 改成 OR）+ 整体取反 | AND / OR | `NOT (a AND b)` / `OR NOT (a AND b)` |

### WhereNested

```text
func (b *Builder) WhereNested(callback func(*Builder)) *Builder
```

**语义**：追加一个 AND 连接的括号组，组内条件默认 AND，回调内可继续用任意 WHERE 方法（包括再次嵌套）。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").Where("status", "active").
		WhereNested(func(q *zcdb.Builder) {
			q.Where("age", ">", 18).Where("vip", "=", 1)
		}).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `status` = ? AND (`age` > ? AND `vip` = ?)
	// args: [active 18 1]
	_, _ = sqlStr, args
}
```

**边界与错误**：空回调组被忽略；回调内的非法运算符会让父 Builder 的编译返回 `ErrInvalidOperator`；回调里写的 `Table`/`Select`/`Join` 不进入外层 SQL（组编译只读 `wheres`）；组内 `OrWhere` 会生成 `(a OR b)`。

**与其他方法的区别**：这是最通用的括号分组；`WhereAll` 是它的语义化别名，`WhereAny`/`WhereNone` 会改写组内顶层连接，`WhereNot` 额外加 `NOT`。

### OrWhereNested

```text
func (b *Builder) OrWhereNested(callback func(*Builder)) *Builder
```

**语义**：与 `WhereNested` 相同，整组以 `OR` 接到已有条件之后。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").Where("status", "active").
		OrWhereNested(func(q *zcdb.Builder) {
			q.Where("age", ">", 60).Where("vip", "=", 1)
		}).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `status` = ? OR (`age` > ? AND `vip` = ?)
	// args: [active 60 1]
	_, _ = sqlStr, args
}
```

**边界与错误**：同 `WhereNested`；裸 `OR` 后接括号组时，括号只界定组内结合，不会把前面的条件包进来。

**与其他方法的区别**：`WhereNested` 用 AND 接入，本方法用 OR 接入；组内连接仍默认 AND。

### WhereAll

```text
func (b *Builder) WhereAll(callback func(*Builder)) *Builder
```

**语义**：语义上强调「组内全部满足」，实现直接委托 `WhereNested`，SQL 完全相同。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").WhereAll(func(q *zcdb.Builder) {
		q.Where("age", ">", 18).Where("status", "active")
	}).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE (`age` > ? AND `status` = ?)
	// args: [18 active]
	_, _ = sqlStr, args
}
```

**边界与错误**：同 `WhereNested`；`WhereAll` 不检查组内是否真的用了 AND（回调里仍可写 `OrWhere`）。

**与其他方法的区别**：与 `WhereNested` 等价；与 `WhereAny` 相反（后者组内为 OR）。

### OrWhereAll

```text
func (b *Builder) OrWhereAll(callback func(*Builder)) *Builder
```

**语义**：`OR` 连接的「组内全部满足」括号组，实现直接委托 `OrWhereNested`。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").Where("vip", "=", 1).
		OrWhereAll(func(q *zcdb.Builder) {
			q.Where("age", ">", 60).Where("status", "retired")
		}).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `vip` = ? OR (`age` > ? AND `status` = ?)
	// args: [1 60 retired]
	_, _ = sqlStr, args
}
```

**边界与错误**：同 `OrWhereNested`；别名不改变任何编译行为。

**与其他方法的区别**：与 `OrWhereNested` 等价；与 `OrWhereAny` 相反。

### WhereAny

```text
func (b *Builder) WhereAny(callback func(*Builder)) *Builder
```

**语义**：追加「任一满足」的括号组，编译为 `(a OR b)`。实现上先收集回调内的条件，再把组内**顶层** `Boolean` 统一改写为 `OR`（组内更深层的嵌套组保持各自连接）。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").WhereAny(func(q *zcdb.Builder) {
		q.Where("age", ">", 60).Where("vip", "=", 1)
	}).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE (`age` > ? OR `vip` = ?)
	// args: [60 1]
	_, _ = sqlStr, args
}
```

**边界与错误**：空回调组被忽略；回调内写 `Where`、`OrWhere`、`WhereRaw` 等都会被统一成 OR；组内再调用 `WhereNested` 时，该嵌套组内部的连接不受影响；单条件时编译为 `(a = ?)`。

**与其他方法的区别**：与 `WhereNested` 的差别只在组内顶层连接（OR vs AND）；与 `WhereNone` 的差别是后者整体再取反。

### OrWhereAny

```text
func (b *Builder) OrWhereAny(callback func(*Builder)) *Builder
```

**语义**：`OR` 连接的「任一满足」括号组，组内顶层连接同样被强制改为 OR。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").Where("status", "active").
		OrWhereAny(func(q *zcdb.Builder) {
			q.Where("age", ">", 60).Where("vip", "=", 1)
		}).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `status` = ? OR (`age` > ? OR `vip` = ?)
	// args: [active 60 1]
	_, _ = sqlStr, args
}
```

**边界与错误**：同 `WhereAny`；外层用 OR 接入，组内也全为 OR。

**与其他方法的区别**：`WhereAny` 用 AND 接入，本方法用 OR 接入；组内都是 OR。

### WhereNone

```text
func (b *Builder) WhereNone(callback func(*Builder)) *Builder
```

**语义**：追加「全部不满足」的括号组，编译为 `NOT (a OR b)`（等价于 `NOT a AND NOT b`，由数据库按三值逻辑求值）。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").WhereNone(func(q *zcdb.Builder) {
		q.Where("status", "banned").Where("age", "<", 18)
	}).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE NOT (`status` = ? OR `age` < ?)
	// args: [banned 18]
	_, _ = sqlStr, args
}
```

**边界与错误**：组内顶层 Boolean 被强制改为 OR 后再加 `Not` 标记；空回调组被忽略；单条件时编译为 `NOT (a = ?)`。

**与其他方法的区别**：`WhereNone` = `NOT (a OR b)`；`WhereNot` = `NOT (a AND b)`。二者只在组内连接上不同。

### OrWhereNone

```text
func (b *Builder) OrWhereNone(callback func(*Builder)) *Builder
```

**语义**：`OR` 连接的「全部不满足」括号组，编译为 `OR NOT (a OR b)`。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").Where("vip", "=", 1).
		OrWhereNone(func(q *zcdb.Builder) {
			q.Where("status", "banned").Where("age", "<", 18)
		}).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `vip` = ? OR NOT (`status` = ? OR `age` < ?)
	// args: [1 banned 18]
	_, _ = sqlStr, args
}
```

**边界与错误**：同 `WhereNone`，仅外层布尔连接不同。

**与其他方法的区别**：`WhereNone` 用 AND 接入，本方法用 OR 接入；组内都是 OR + NOT。

### WhereNot

```text
func (b *Builder) WhereNot(callback func(*Builder)) *Builder
```

**语义**：追加「整体取反」的括号组，编译为 `NOT (…)`；组内条件默认 AND，但可被回调里的 `OrWhere` 改成 OR（此时语义与 `WhereNone` 相同）。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").WhereNot(func(q *zcdb.Builder) {
		q.Where("status", "banned").Where("age", "<", 18)
	}).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE NOT (`status` = ? AND `age` < ?)
	// args: [banned 18]
	_, _ = sqlStr, args
}
```

**边界与错误**：空回调组被忽略；组内错误上抛；`NOT` 对 NULL 行不改变三值逻辑结果（未知仍为未知，不会命中）。

**与其他方法的区别**：只加 `NOT`、不改写组内连接；`WhereNone` 强制组内为 OR 后再取反。

### OrWhereNot

```text
func (b *Builder) OrWhereNot(callback func(*Builder)) *Builder
```

**语义**：`OR` 连接的「整体取反」括号组，编译为 `OR NOT (…)`。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").Where("vip", "=", 1).
		OrWhereNot(func(q *zcdb.Builder) {
			q.Where("status", "banned")
		}).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `vip` = ? OR NOT (`status` = ?)
	// args: [1 banned]
	_, _ = sqlStr, args
}
```

**边界与错误**：同 `WhereNot`，仅外层布尔连接不同。

**与其他方法的区别**：`WhereNot` 用 AND 接入，本方法用 OR 接入；组内默认仍是 AND。

## WhereDate（日期比较）

```text
func (b *Builder) WhereDate(column string, value any) *Builder
```

**语义**：追加「日期部分相等」的 AND 条件。按方言生成日期提取表达式，`value` 为 `time.Time` 时先格式化为 `YYYY-MM-DD`，其余值按 `WhereRaw` 绑定。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").WhereDate("created_at", "2026-08-08").ToSelect()
	// MySQL:      SELECT * FROM `users` WHERE date(`created_at`) = ?
	// args:       [2026-08-08]
	// PostgreSQL: SELECT * FROM "users" WHERE "created_at"::date = $1
	// SQLite:     SELECT * FROM "users" WHERE strftime('%Y-%m-%d', "created_at") = ?
	_, _ = sqlStr, args
}
```

**边界与错误**：

- 没有 `OrWhereDate`；需要 OR 时用 `OrWhereRaw` 拼同样的日期表达式。
- **不做 nil 特判**：`WhereDate("created_at", nil)` 生成 `date(col) = ?` 并绑定 nil（结果恒未知），不会变成 `IS NULL`。
- 基于 `WhereRaw` 实现，因此 `value` 传 `Expression` 会被内联；但日期函数本身由 Grammar 生成，不受 value 影响。
- `time.Time` 只取日期部分（格式化为 `2006-01-02`）；带时区的时间按该时间的年月日截取，不做时区换算。
- 值类型（`time.Time`/字符串）以驱动支持为准；跨方言建议统一传 `YYYY-MM-DD` 字符串。SQLite 的日期列必须能被 `strftime` 解析（文本或时间值），否则比较结果不符合预期。

**与其他方法的区别**：`WhereBetween` 适合日期范围；`WhereDate` 只比较日期部分，列上的索引可能无法直接命中（表达式比较）。需要自定义日期函数时用 `WhereRaw`。

## WhereLike / WhereNotLike（模糊匹配）

### WhereLike

```text
func (b *Builder) WhereLike(column string, value any, caseSensitive ...bool) *Builder
```

**语义**：追加 `column LIKE value` 的 AND 条件。`caseSensitive` 是可选变参，只有第一个元素生效；为 true 时按方言生成区分大小写的形态。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").WhereLike("name", "%alice%").ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `name` LIKE ?
	// args: [%alice%]

	sqlStr, args, _ = db.Builder().Table("users").WhereLike("name", "a%", true).ToSelect()
	// MySQL:      SELECT * FROM `users` WHERE BINARY `name` LIKE ?
	// PostgreSQL: SELECT * FROM "users" WHERE "name" LIKE $1      // 默认不区分时为 ILIKE
	// SQLite:     SELECT * FROM "users" WHERE "name" GLOB ?       // 通配符为 * / ?，非 % / _
	// args: [a%]
	_, _ = sqlStr, args
}
```

**边界与错误**：默认形态 MySQL `LIKE`（取决于列排序规则）、PostgreSQL `ILIKE`、SQLite `LIKE`（ASCII 不区分）；`caseSensitive=true` 时 MySQL 加 `BINARY`、PostgreSQL 用 `LIKE`、SQLite 用 `GLOB`（通配符是 `*`/`?`，调用方需自行转换模式串）；`value` 为 `Expression` 时内联、不占绑定，`nil` 作为绑定值（`LIKE NULL` 恒未知）；模式串中的 `%`/`_` 不被转义；不产生构造期错误。

**与其他方法的区别**：`WhereNotLike` 没有 `caseSensitive` 参数且 PostgreSQL 下固定 `NOT ILIKE`；精确匹配用 `Where`；需要方言不支持的大小写敏感形态时用 `WhereRaw`。

### OrWhereLike

```text
func (b *Builder) OrWhereLike(column string, value any, caseSensitive ...bool) *Builder
```

**语义**：与 `WhereLike` 相同，`Boolean` 记为 `OR`。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").Where("vip", "=", 1).
		OrWhereLike("name", "%bob%").ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `vip` = ? OR `name` LIKE ?
	// args: [1 %bob%]
	_, _ = sqlStr, args
}
```

**边界与错误**：默认形态、`caseSensitive`、Expression/nil 规则同 `WhereLike`；裸 `OR` 的优先级陷阱同 `OrWhere`。

**与其他方法的区别**：与 `WhereLike` 只差布尔连接；需要整组 OR 时用 `OrWhereNested` 包裹多个 LIKE。

### WhereNotLike

```text
func (b *Builder) WhereNotLike(column string, value any) *Builder
```

**语义**：追加 `column NOT LIKE value` 的 AND 条件；不支持 `caseSensitive` 变参。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").WhereNotLike("name", "%test%").ToSelect()
	// MySQL:      SELECT * FROM `users` WHERE `name` NOT LIKE ?
	// PostgreSQL: SELECT * FROM "users" WHERE "name" NOT ILIKE $1
	// SQLite:     SELECT * FROM "users" WHERE "name" NOT LIKE ?
	// args: [%test%]
	_, _ = sqlStr, args
}
```

**边界与错误**：

- 大小写语义与 `WhereLike` 默认行为对称：MySQL/SQLite 为 `NOT LIKE`（与 `LIKE` 一样取决于排序规则/ASCII），PostgreSQL 为 `NOT ILIKE`（与默认 `ILIKE` 互补）。
- PostgreSQL 下需要区分大小写时用 `WhereRaw` 自行表达（例如 ``WhereRaw(`"name" NOT LIKE ?`, v)``；若把 Expression 传给 `WhereNotLike`，它同样编译为 `NOT ILIKE <expr>`）。
- `value` 为 Expression 时内联、不占绑定；nil 作为绑定值，不做特判。

**与其他方法的区别**：`WhereLike` 有 `caseSensitive` 且 PostgreSQL 默认 `ILIKE`；`WhereNotLike` 无该开关。与 `WhereNotIn` 无关（模糊 vs 集合）。

## WhereNullSafeEquals / WhereNullSafeNotEquals（空安全比较）

### WhereNullSafeEquals

```text
func (b *Builder) WhereNullSafeEquals(column string, value any) *Builder
```

**语义**：追加空安全相等比较：两边的值都为 NULL 时结果为 true，否则按普通相等比较。适合「列值等于某值，或两者均为 NULL」的场景。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").WhereNullSafeEquals("remark", nil).ToSelect()
	// MySQL:      SELECT * FROM `users` WHERE `remark` <=> ?
	// PostgreSQL: SELECT * FROM "users" WHERE "remark" IS NOT DISTINCT FROM $1
	// SQLite:     SELECT * FROM "users" WHERE "remark" IS ?
	// args:       [<nil>]
	_, _ = sqlStr, args
}
```

**边界与错误**：

- **始终绑定 `value`（包括 nil）**：与 `Where(col, "=", nil)` 的 `IS NULL` 特判不同，这里 `<=> ?` 绑定 nil 也参与比较。
- `value` 为 `Expression` 时内联、不占绑定。
- 没有 Or 变体；没有 nil 特判。
- MySQL 用 `<=>`，PostgreSQL 用 `IS NOT DISTINCT FROM`，SQLite 用 `IS`；方言差异只影响文本，不影响绑定数。

**与其他方法的区别**：`Where(col, "=", nil)` 只处理「是否为 NULL」，不会把非 NULL 值也纳入比较；`WhereNullSafeEquals` 同时比较 NULL 与普通值。`WhereNull` 只判断 NULL，不比较值。

### WhereNullSafeNotEquals

```text
func (b *Builder) WhereNullSafeNotEquals(column string, value any) *Builder
```

**语义**：追加空安全不等比较：NULL 与任意值（包括 NULL）比较为不等；NULL 与 NULL 为 false。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").WhereNullSafeNotEquals("remark", "x").ToSelect()
	// MySQL:      SELECT * FROM `users` WHERE NOT `remark` <=> ?
	// PostgreSQL: SELECT * FROM "users" WHERE "remark" IS DISTINCT FROM $1
	// SQLite:     SELECT * FROM "users" WHERE "remark" IS NOT ?
	// args:       [x]
	_, _ = sqlStr, args
}
```

**边界与错误**：

- 始终绑定 `value`（含 nil）；`Expression` 内联。
- MySQL 的 `NOT col <=> ?` 对 `NULL <=> NULL` 取反为 false，对 `NULL <=> 值` 取反为 true；PostgreSQL/SQLite 的 `IS [NOT] DISTINCT FROM` 语义一致。
- 没有 Or 变体、没有 nil 特判。

**与其他方法的区别**：`Where(col, "!=", nil)` 是 `IS NOT NULL`（只排除 NULL），`WhereNullSafeNotEquals` 是「不相等（含 NULL 与值不等）」；`WhereNotNull` 只判断 NULL。

## 子查询条件

三类子查询方法共有的边界：

- 回调收到一个新的 Builder，共享当前 `grammar`/`dao`；回调**必须自己设置数据源**（`Table`/`TableSub`）。
- 外层只校验自己的数据源，**不替子查询检查 `ErrEmptyTable`**；缺表时编译产物形如 `(SELECT * FROM <空标识符> …)`，由数据库执行期拒绝。
- 子查询的绑定参数按它在 WHERE 中出现的位置收集；子查询自身的 `Comment` 不进入外层 SQL。
- 子查询图成环时，外层 `ToSelect` 返回 `ErrCyclicQuery`；子查询累积的构造错误同样上抛。

### WhereExists

```text
func (b *Builder) WhereExists(sub any) *Builder
```

**语义**：追加 `EXISTS (子查询)` 的 AND 条件。`sub` 支持 `func(*Builder)` 回调或已构造好的 `*Builder`；其它类型在调用处写入 `ErrInvalidSubQuery`，编译时返回。

**SQL**：

```go
func example() {
	sqlStr, _, _ := db.Builder().Table("users").WhereExists(func(q *zcdb.Builder) {
		q.Table("orders").SelectRaw("1").WhereColumn("orders.user_id", "=", "users.id")
	}).ToSelect()
	// SQL: SELECT * FROM `users` WHERE EXISTS (SELECT 1 FROM `orders` WHERE `orders`.`user_id` = `users`.`id`)
	_, _ = sqlStr, db
}
```

**边界与错误**：非 `func(*Builder)`/`*Builder` 的 `sub` 得到 `ErrInvalidSubQuery`；`EXISTS` 不关心子查询的 SELECT 列表（习惯写 `SelectRaw("1")`）；回调内不设数据源时编译不报 `ErrEmptyTable`，执行期由数据库报错；无条件 `EXISTS` 在写保护中视为恒真，不构成有效限定。

**与其他方法的区别**：`WhereInSub` 依赖子查询结果集，`WhereSub` 要求标量子查询；`EXISTS` 只判断是否存在行，`NOT EXISTS` 也不受子查询结果含 NULL 的影响。

### OrWhereExists

```text
func (b *Builder) OrWhereExists(sub any) *Builder
```

**语义**：与 `WhereExists` 相同，`Boolean` 记为 `OR`。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").Where("status", "active").
		OrWhereExists(func(q *zcdb.Builder) {
			q.Table("orders").SelectRaw("1").WhereColumn("orders.user_id", "=", "users.id")
		}).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `status` = ? OR EXISTS (SELECT 1 FROM `orders` WHERE `orders`.`user_id` = `users`.`id`)
	// args: [active]
	_, _ = sqlStr, args
}
```

**边界与错误**：`sub` 形式与错误规则同 `WhereExists`；裸 `OR` 的优先级陷阱同 `OrWhere`，需要分组时包 `OrWhereNested`。

**与其他方法的区别**：`WhereExists` 用 AND 接入，本方法用 OR 接入；其余语义相同。

### WhereNotExists

```text
func (b *Builder) WhereNotExists(sub any) *Builder
```

**语义**：追加 `NOT EXISTS (子查询)` 的 AND 条件；`sub` 形式同 `WhereExists`。

**SQL**：

```go
func example() {
	sqlStr, _, _ := db.Builder().Table("users").WhereNotExists(func(q *zcdb.Builder) {
		q.Table("orders").SelectRaw("1").WhereColumn("orders.user_id", "=", "users.id")
	}).ToSelect()
	// SQL: SELECT * FROM `users` WHERE NOT EXISTS (SELECT 1 FROM `orders` WHERE `orders`.`user_id` = `users`.`id`)
	_, _ = sqlStr, db
}
```

**边界与错误**：同 `WhereExists`；`NOT EXISTS` 对 NULL 结果集安全（与 `NOT IN` 不同）。

**与其他方法的区别**：`WhereNotInSub` 在子查询结果含 NULL 时可能整体不成立，`WhereNotExists` 不受影响；这是「不存在」语义的首选。

### OrWhereNotExists

```text
func (b *Builder) OrWhereNotExists(sub any) *Builder
```

**语义**：与 `WhereNotExists` 相同，`Boolean` 记为 `OR`。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").Where("status", "active").
		OrWhereNotExists(func(q *zcdb.Builder) {
			q.Table("orders").SelectRaw("1").WhereColumn("orders.user_id", "=", "users.id")
		}).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `status` = ? OR NOT EXISTS (SELECT 1 FROM `orders` WHERE `orders`.`user_id` = `users`.`id`)
	// args: [active]
	_, _ = sqlStr, args
}
```

**边界与错误**：同 `WhereNotExists`，仅布尔连接不同。

**与其他方法的区别**：`WhereNotExists` 用 AND 接入，本方法用 OR 接入。

### WhereSub

```text
func (b *Builder) WhereSub(column string, op string, callback func(*Builder)) *Builder
```

**语义**：追加 `column op (SELECT …)` 的 AND 条件（标量子查询比较）。`op` 走与 `Where` 相同的白名单；只支持回调形式，不接受现成的 `*Builder`。

**SQL**：

```go
func example() {
	sqlStr, _, _ := db.Builder().Table("users").WhereSub("age", ">", func(q *zcdb.Builder) {
		q.Table("stats").SelectRaw("AVG(age)")
	}).ToSelect()
	// SQL: SELECT * FROM `users` WHERE `age` > (SELECT AVG(age) FROM `stats`)
	_, _ = sqlStr, db
}
```

**边界与错误**：`op` 非法时写入 `ErrInvalidOperator`；子查询必须是标量（单列、至多一行），否则执行期由数据库报错；回调内必须设置数据源，外层不检查子查询的 `ErrEmptyTable`；传 nil 回调会在调用时 panic。

**与其他方法的区别**：`WhereInSub` 是集合成员判断；`WhereColumn` 是列对列比较；`Where("age", ">", zcdb.NewExpression("(SELECT …)"))` 也能拼出类似 SQL，但内联表达式不参与子查询的错误/环检测。

### OrWhereSub

```text
func (b *Builder) OrWhereSub(column string, op string, callback func(*Builder)) *Builder
```

**语义**：与 `WhereSub` 相同，`Boolean` 记为 `OR`。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").Where("vip", "=", 1).
		OrWhereSub("age", "<", func(q *zcdb.Builder) {
			q.Table("stats").SelectRaw("MIN(age)")
		}).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `vip` = ? OR `age` < (SELECT MIN(age) FROM `stats`)
	// args: [1]
	_, _ = sqlStr, args
}
```

**边界与错误**：同 `WhereSub`；绑定按位置排在前面条件的绑定之后。

**与其他方法的区别**：`WhereSub` 用 AND 接入，本方法用 OR 接入。

### WhereInSub

```text
func (b *Builder) WhereInSub(column string, callback func(*Builder)) *Builder
```

**语义**：追加 `column IN (SELECT …)` 的 AND 条件；回调内构造子查询并自行设置数据源。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").WhereInSub("dept_id", func(q *zcdb.Builder) {
		q.Table("depts").Select("id").Where("level", ">", 3)
	}).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `dept_id` IN (SELECT `id` FROM `depts` WHERE `level` > ?)
	// args: [3]
	_, _ = sqlStr, args
}
```

**边界与错误**：子查询结果应只有一列；多列由数据库执行期报错；回调内不设数据源时编译不报 `ErrEmptyTable`；子查询绑定按出现位置收集，注释不进入外层 SQL。

**与其他方法的区别**：`WhereIn` 是值列表；`WhereExists` 只判断存在性、不比较列值。

### WhereNotInSub

```text
func (b *Builder) WhereNotInSub(column string, callback func(*Builder)) *Builder
```

**语义**：追加 `column NOT IN (SELECT …)` 的 AND 条件；没有 Or 变体（不存在 `OrWhereNotInSub`）。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").WhereNotInSub("dept_id", func(q *zcdb.Builder) {
		q.Table("depts").Select("id").Where("level", "<=", 1)
	}).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `dept_id` NOT IN (SELECT `id` FROM `depts` WHERE `level` <= ?)
	// args: [1]
	_, _ = sqlStr, args
}
```

**边界与错误**：子查询结果含 NULL 时，`NOT IN` 的三值逻辑会让外层行不成立；需要「不存在」语义时优先 `WhereNotExists`；其余同 `WhereInSub`。

**与其他方法的区别**：`WhereNotIn` 是值列表；`WhereNotExists` 没有 NULL 陷阱且更适合子查询集合。

## 绑定顺序、空条件与写保护

**WHERE 内部绑定顺序**：严格按 `wheres` 追加顺序收集——`Where` 的值、`WhereIn` 的每个元素、`Between` 的 min/max、`Raw` 的每个占位符、嵌套组按组内顺序、子查询按子查询内部顺序。`Expression`、nil 特判、`WhereNull`、`WhereColumn`、两列 `Between` 不占绑定。

**空条件与恒真/恒假**（也是写保护判定依据）：

| 条件 | 编译产物 | 是否收窄范围 |
|---|---|---|
| 空 `WhereRaw("")` | 无子句 | 否（视为缺失） |
| 纯空白 `WhereRaw("   ")` | 原样空白片段（可能非法 SQL） | 否（判定按空白视为缺失） |
| 空 `WhereNested` 回调 | 无子句 | 否（视为缺失） |
| `WhereIn(col, 空)` | `0 = 1` | 恒假，算有效限定 |
| `WhereNotIn(col, 空)` | `1 = 1` | 恒真，不算有效限定 |
| 无条件的 `WhereExists` | `EXISTS (…)` | 恒真，不算有效限定 |
| 非空 `WhereRaw`（含 `"1 = 1"`） | 原样片段 | 视为依赖行数据，算有效限定 |

`Update`/`Increment`/`Decrement`/`Delete`/`DeleteJoin` 在无有效限定时返回 `ErrUpdateWithoutWhere`/`ErrDeleteWithoutWhere`（`DeleteJoin` 另有 `ErrDeleteWithJoin` 等），需要全表操作时显式 `Force()`；判定细节与边界见 [safety](../concepts/safety.md)。

**构造期错误**：

- 非法运算符 → `ErrInvalidOperator`（`Where`/`OrWhere` 三参形式、`WhereColumn`、`WhereSub`/`OrWhereSub`）。
- `WhereExists`/`OrWhereExists`/`WhereNotExists`/`OrWhereNotExists` 的 `sub` 类型非法 → `ErrInvalidSubQuery`。
- 错误写入 `Builder.err` 后链式调用继续，`ToXxx` 统一短路返回；`Comment` 等标记方法不会清除它。

**方言差异**：本文只给 MySQL 形态；PostgreSQL 把 `?` 换成 `$N`、标识符换双引号，`LIKE` 默认 `ILIKE`，NULL 安全比较换 `IS [NOT] DISTINCT FROM`，日期换 `::date`；SQLite 用双引号、`GLOB`、`strftime`、`IS`/`IS NOT`。完整矩阵见 [dialect-matrix](../reference/dialect-matrix.md)。

## 相关阅读

- 条件状态如何累积、Clone 的复制范围：[builder-state](../concepts/builder-state.md)
- 绑定收集顺序与编译入口：[compile-pipeline](../concepts/compile-pipeline.md)
- 写保护判定、`Force` 与注释安全：[safety](../concepts/safety.md)
- 原始 SQL 占位符扫描与 `CompileRaw`：[builder-compile](../reference/builder-compile.md)
- JOIN 条件与 HAVING 条件：[builder-join](../reference/builder-join.md)、[builder-order-group](../reference/builder-order-group.md)
- 错误哨兵清单：[errors](../reference/errors.md)
