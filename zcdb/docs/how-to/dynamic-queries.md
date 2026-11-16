# 动态查询条件任务指南

动态查询的难点不是把 SQL 拼出来，而是三件事：条件到底有没有生效、多个条件组合后的优先级对不对、写操作的「安全限定」是否真的收窄了范围。

## 结论先行

- 条件用 `if` 决定是否追加链式调用，**不要拼接 SQL 字符串**；动态值走绑定参数。
- 多个条件要作为一「组」参与 `OR` 时，必须用 `WhereAny` / `WhereNested` 显式加括号。
- 空列表不是「没有过滤」：空 `WhereIn` 编译为恒假 `0 = 1`，空 `WhereNotIn` 编译为恒真 `1 = 1`。
- 写操作先数「实际追加了几个过滤条件」，为 0 就返回；不要用 `Force()` 给动态条件的漏加兜底。
- `WhereExists` 的破坏性操作保护判定是**句法的**，非相关子查询同样会被计为有效限定。
- `WhereRaw` 的 SQL 字符串是代码，绑定参数才是数据；动态标识符用白名单映射到常量列名。
- 需要复用基础条件时用 `Clone()`，不要跨请求共享同一个可变 `Builder`。

> 本文的 Go 示例统一包在 `func example()` 中，仅用于语法检查与上下文占位（`db`、`ctx`、`User` 等是外部变量），不保证可独立运行。SQL 注释统一以 MySQL 形态给出（反引号、`?` 占位符）；PostgreSQL/SQLite 仅标识符改为双引号、PG 占位符为 `$1..$N`。

## 条件式追加：用 if 控制链式调用

`Builder` 的每个条件方法都修改接收者并返回自身，所以「逐个 if 追加」与「写在一条链上」完全等价；前者更适合动态参数。

```go
func example() {
	type Filter struct {
		Name   string
		MinAge int
		Status []any
	}

	build := func(f Filter) *zcdb.Builder {
		// q 是同一个 Builder，后续每个 if 都直接改它
		q := db.Builder().Table("users")
		if f.Name != "" {
			// 通配符拼进「绑定值」是安全的：它仍是参数，不是 SQL 片段
			q.Where("name", "LIKE", "%"+f.Name+"%")
		}
		if f.MinAge > 0 {
			q.Where("age", ">=", f.MinAge)
		}
		if len(f.Status) > 0 {
			q.WhereIn("status", f.Status)
		}
		return q
	}

	sql, args, err := build(Filter{Name: "ali", MinAge: 18, Status: []any{"active", "frozen"}}).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `name` LIKE ? AND `age` >= ? AND `status` IN (?, ?)
	// args: [%ali% 18 active frozen]
	_, _, _ = sql, args, err
}
```

「参数是否提供」不能用零值猜测。`0`、`""`、`false` 都可能是合法过滤值，用 `> 0` / `!= ""` 判断会让这些条件静默消失。用指针或显式的 `has` 标志表达「传了没有」：

```go
func example() {
	type Filter struct {
		Age *int // nil = 不按年龄过滤；非 nil = 按该值过滤（0 也合法）
	}

	f := Filter{Age: new(int)} // 明确要过滤 age = 0
	q := db.Builder().Table("users")
	if f.Age != nil {
		q.Where("age", *f.Age)
	}

	sql, args, err := q.ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `age` = ?
	// args: [0]
	_, _, _ = sql, args, err
}
```

条件方法的参数错误（例如非法运算符）会累积在 `Builder` 内部，直到编译或执行时才返回 `ErrInvalidOperator`。链式调用中间不需要逐步检查，但**终端调用必须检查错误**。

写操作要额外做「生效条件计数」。动态过滤为空时直接返回，而不是执行一条无 WHERE 的写：

```go
func example() {
	// 从请求或配置解析出的过滤条件，所有字段都可能为空
	var f struct {
		Name   string
		MinAge int
	}

	q := db.Builder().Table("users")
	applied := 0
	if f.Name != "" {
		q.Where("name", f.Name)
		applied++
	}
	if f.MinAge > 0 {
		q.Where("age", ">=", f.MinAge)
		applied++
	}

	// 没有生效条件就不写；不要用 Force() 把这个问题掩盖过去
	if applied == 0 {
		return
	}
	if _, err := q.Update(ctx, User{Status: "archived"}); err != nil {
		return
	}
}
```

**不要这样做：**

```go
func example() {
	// 反例：把外部输入直接拼进 WHERE 片段
	name := "alice' OR '1'='1"
	_, _, _ = db.Builder().Table("users").WhereRaw("name = '" + name + "'").ToSelect()

	// 反例：动态条件为空时用 Force 让全表更新「跑通」
	_, _ = db.Builder().Table("users").Force().Update(ctx, User{Status: "archived"})
}
```

## 用分组方法固定 OR 的优先级

SQL 里 `AND` 优先于 `OR`。平铺的 `a OR b AND c` 实际是 `a OR (b AND c)`；如果业务要的是 `(a OR b) AND c`，必须显式加括号。分组方法的差异只在括号形态：

| 方法 | 编译形态 | 语义 |
|---|---|---|
| `WhereNested` / `WhereAll` | `(a AND b)` | 组内全部满足 |
| `WhereNot` | `NOT (a AND b)` | 组内条件整体取反 |
| `WhereAny` | `(a OR b)` | 组内任一满足 |
| `WhereNone` | `NOT (a OR b)` | 组内全部不满足 |

`OrWhereNested` / `OrWhereAll` / `OrWhereAny` / `OrWhereNone` 是对应的 `OR` 变体（作为整体与前面的条件用 OR 连接）。`WhereAny` 会把回调内**顶层**条件的连接词统一覆写为 `OR`，回调里再嵌套的分组保持自己的布尔关系。

```go
func example() {
	// (age > 18 AND vip = 1) 作为一个整体，与 status 用 AND 连接
	sql, args, err := db.Builder().Table("users").Where("status", "active").
		WhereNested(func(q *zcdb.Builder) {
			q.Where("age", ">", 18).Where("vip", "=", 1)
		}).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `status` = ? AND (`age` > ? AND `vip` = ?)
	// args: [active 18 1]
	_, _, _ = sql, args, err

	// 同一回调内两个条件变成 OR
	sql, args, err = db.Builder().Table("users").Where("tenant_id", "=", 1).
		WhereAny(func(q *zcdb.Builder) {
			q.Where("age", ">", 60).Where("vip", "=", 1)
		}).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `tenant_id` = ? AND (`age` > ? OR `vip` = ?)
	// args: [1 60 1]
	_, _, _ = sql, args, err

	// 全部不满足 = NOT (a OR b)
	sql, args, err = db.Builder().Table("users").
		WhereNone(func(q *zcdb.Builder) {
			q.Where("status", "banned").Where("age", "<", 18)
		}).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE NOT (`status` = ? OR `age` < ?)
	// args: [banned 18]
	_, _, _ = sql, args, err
}
```

回调内没有添加任何条件时，该组整体被忽略，不会生成空括号；它也不构成写操作保护中的有效限定。

**不要这样做：**

```go
func example() {
	// 反例：以为 OR 只作用于 vip，实际是 status OR (vip AND age)
	sql, _, _ := db.Builder().Table("users").
		Where("status", "active").
		OrWhere("vip", "=", 1).
		Where("age", ">", 18).
		ToSelect()
	// SQL: SELECT * FROM `users` WHERE `status` = ? OR `vip` = ? AND `age` > ?
	_ = sql
}
```

正确写法是把 `status OR vip` 包进 `WhereAny`，再在外层追加 `age` 条件。

## 空 WhereIn / WhereNotIn：先决定「没有条件」还是「恒假/恒真」

空列表不是空操作，而是被编译成常量条件：

| 写法 | 编译结果 | 查询效果 | 写操作保护中的归约 |
|---|---|---|---|
| `WhereIn("id", []any{})`（含 nil 切片） | `0 = 1` | 恒假，查不到任何行 | 恒假，计为有效限定（放行，但影响 0 行） |
| `WhereNotIn("id", []any{})` | `1 = 1` | 恒真，不过滤 | 恒真，**不计为**有效限定 |

`WhereIn` / `WhereNotIn` 按引用保存传入的切片：编译前不要再修改同一个底层数组；需要稳定快照时先复制一份再传入。

```go
func example() {
	sql, _, _ := db.Builder().Table("users").WhereIn("id", []any{}).ToSelect()
	// SQL: SELECT * FROM `users` WHERE 0 = 1

	sql, _, _ = db.Builder().Table("users").WhereNotIn("id", []any{}).ToSelect()
	// SQL: SELECT * FROM `users` WHERE 1 = 1
	_ = sql
}
```

业务上「没有选中任何 ID」通常意味着「不加这个过滤」，而不是「查不到任何行」。这时应显式分支：

```go
func example() {
	var ids []any // 可能为空

	q := db.Builder().Table("users")
	if len(ids) > 0 {
		q.WhereIn("id", ids)
	}

	var users []User
	if err := q.Find(ctx, &users); err != nil {
		return
	}
}
```

写操作同样不能依赖空 `WhereNotIn` 充当 WHERE：它恒真，单独使用时会被保护拒绝；与其它条件用 `OR` 组合成恒真时也会被拒绝。不要把「保护会兜底」当作设计。

列表值里含 `NULL` 时，`NOT IN` 对任何非匹配行都返回 unknown，查询会一行都查不到。外部来源的列表应先剔除 `nil`：

```go
func example() {
	var statuses []any // 可能包含 nil

	q := db.Builder().Table("users")
	notIn := make([]any, 0, len(statuses))
	for _, s := range statuses {
		if s != nil {
			notIn = append(notIn, s)
		}
	}
	if len(notIn) > 0 {
		q.WhereNotIn("status", notIn)
	}
	_ = q
}
```

## WhereExists：保护判定是句法的

查询语义由数据库决定，但**写操作保护**只做句法归约：它检查子查询内部是否有「未知」条件，不分析子查询是否引用外层列。于是 `EXISTS` 内层只要有一个普通条件，非相关子查询也会被计为有效限定。

```go
func example() {
	// 危险：子查询与 users 无关，EXISTS 只取决于 config 是否有 id = 1 的行；
	// 一旦有，这次 Update 影响 users 全表，但保护判定会放行。
	_, err := db.Builder().Table("users").
		WhereExists(func(q *zcdb.Builder) {
			q.Table("config").SelectRaw("1").Where("id", "=", 1)
		}).
		Update(ctx, User{Status: "archived"})
	_ = err
}
```

要真正按行收窄，补外层条件，或写成关联子查询：

```go
func example() {
	// 外层条件自身已经收窄范围
	_, err := db.Builder().Table("users").
		Where("status", "=", "pending").
		WhereExists(func(q *zcdb.Builder) {
			q.Table("config").SelectRaw("1").Where("id", "=", 1)
		}).
		Update(ctx, User{Status: "archived"})
	_ = err

	// 关联子查询：内层通过列引用依赖外层行
	_, err = db.Builder().Table("users").
		WhereExists(func(q *zcdb.Builder) {
			q.Table("orders").SelectRaw("1").
				WhereColumn("orders.user_id", "=", "users.id").
				Where("orders.status", "=", "paid")
		}).
		Update(ctx, User{Status: "archived"})
	_ = err
}
```

两个容易踩的边界：

- 内层没有任何条件（例如只调用 `Table`）时，`EXISTS (SELECT * FROM config)` 编译后通常恒真（表非空），但保护判定把它归为恒真、**不算**有效限定；若没有其它有效限定，写操作仍会被拒绝。
- 回调形式不会自动继承外层表名；必须在回调里自己调用 `Table` / `TableSub`。否则子查询没有数据源，编译产物是「`SELECT * FROM` + 空标识符」的非法 SQL，要到数据库执行时才报错，而不是返回 `ErrEmptyTable`。

「判断某个全局开关是否存在」不是行级条件，应该放在应用层 `Exists` 查询里，而不是塞进 `WHERE` 骗过保护。

## WhereRaw：SQL 片段是代码，绑定才是数据

`WhereRaw(sql, bindings...)` 把 `sql` 原样嵌入产物，不做标识符包裹、不做内容转义；`bindings` 才按占位符绑定。因此边界很清楚：

- 值：必须走 `bindings`（或改用 `Where` / `WhereIn`）。
- `bindings` 里的 `Expression` 会被原样内联、不占占位符；不要把外部输入包装成 `Expression`。
- 列名、表名、函数名、排序表达式：必须是代码里的常量，或从白名单映射出的常量。
- 不要把外部输入拼进 `sql` 字符串，哪怕是「只是个列名」。

```go
func example() {
	// 正确：SQL 片段固定，值走绑定
	sql, args, err := db.Builder().Table("users").
		WhereRaw("created_at > NOW() - INTERVAL ? DAY", 7).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE created_at > NOW() - INTERVAL ? DAY
	// args: [7]
	_, _, _ = sql, args, err
}
```

```go
func example() {
	// 反例：把外部输入拼进 SQL 片段
	name := "alice' OR '1'='1" // 模拟外部输入
	_, _, _ = db.Builder().Table("users").WhereRaw("name = '" + name + "'").ToSelect()
}
```

动态列名用白名单，而不是把用户字符串交给 `WrapColumn`：

```go
func example() {
	sortKey := "email" // 外部输入
	allowed := map[string]string{
		"name":  "name",
		"email": "email",
	}
	column, ok := allowed[sortKey]
	if !ok {
		return
	}
	// direction 也来自外部时无需担心注入：OrderBy 只认 DESC，其余一律归一为 ASC
	_, _, _ = db.Builder().Table("users").OrderBy(column, "DESC").ToSelect()
}
```

PostgreSQL 下 `WhereRaw` 代码区的 `?` 会编译成 `$N`；字符串字面量、引号标识符和注释里的 `?` 原样保留。要在代码区写 jsonb 的 `?` 操作符时写成 `??` 转义为一个字面 `?` 且不消耗绑定；MySQL/SQLite 没有这个转义，`??` 就是两个占位符。占位符识别的完整规则见 [builder-compile](../reference/builder-compile.md)。

另外，**修剪空白后非空的 `WhereRaw` 都会被写操作保护视为有效限定**，包括 `WhereRaw("1 = 1")`。这是刻意保留的逃生口，不是让动态条件「凑数」用的手段；确定要全表操作时用 `Force()` 更便于审查与检索。

## Clone：复用基础 Builder 而不是共享可变状态

`Clone()` 深拷贝全部查询状态（列、WHERE 含嵌套与切片、JOIN、GROUP/HAVING、ORDER、UNION、锁、`Force`/`Primary` 标记与注释），副本与原 Builder 互相隔离；绑定值本身（`any` 里的切片、map、指针）不做深拷贝，编译与执行不会修改它们，但调用方也不要在编译后改动这些底层数据。

```go
func example() {
	base := db.Builder().Table("users").Where("deleted_at", "=", nil)

	admins := base.Clone().Where("role", "=", "admin")
	vips := base.Clone().Where("vip", "=", 1)

	sqlAdmins, _, _ := admins.ToSelect()
	// SQL: SELECT * FROM `users` WHERE `deleted_at` IS NULL AND `role` = ?

	sqlVips, _, _ := vips.ToSelect()
	// SQL: SELECT * FROM `users` WHERE `deleted_at` IS NULL AND `vip` = ?
	_, _, _ = sqlAdmins, sqlVips, base
}
```

两个与动态查询相关的注意点：

- `First` / `Value` 内部按需 `Clone` 并强制 `LIMIT 1`，不会污染原 Builder；`Paginate` 不克隆，但会通过 `ToCount` 的临时状态保存/恢复复用原 Builder。
- `Pluck` / `PluckMap` / `KeyBy` 会**改写接收者的 SELECT 列**（覆盖语义）。如果同一个基础 Builder 既要 `Pluck` 又要 `Find`，先 `Clone()`，否则第二次查询只剩提取用的那几列。

```go
func example() {
	base := db.Builder().Table("users").Where("status", "=", "active")

	var names []string
	if err := base.Clone().Pluck(ctx, &names, "name"); err != nil {
		return
	}

	var users []User
	if err := base.Clone().Find(ctx, &users); err != nil { // 仍是 SELECT *
		return
	}
}
```

`Builder` 不是为并发修改设计的：不要在多个 goroutine 里同时链式修改同一个实例。需要并行派生时，先串行 `Clone()`，再把各自副本分发出去。

## Force：只用于确定的全表操作

`Force()` 只做一件事：跳过 `Update` / `Increment` / `Decrement` / `Delete` / `DeleteJoin` 的「无有效 WHERE/JOIN」保护判定。它**不改变 SQL**（不会多出任何子句），也不跳过其它错误。

```go
func example() {
	// 适用：确定要归档全表的运维任务/一次性脚本
	affected, err := db.Builder().Table("users").Force().
		Update(ctx, struct {
			Status string `db:"status"`
		}{Status: "archived"})
	// SQL: UPDATE `users` SET `status` = ?
	_, _ = affected, err

	// 适用：确定要清空整表（Truncate 本身就是全表语义，不需要 Force）
	err = db.Builder().Table("users").Force().Delete(ctx)
	// SQL: DELETE FROM `users`
	_ = err
}
```

`Force()` 不会放行的情形：

| 情形 | 仍然返回 |
|---|---|
| `Delete` 的 Builder 带 JOIN | `ErrDeleteWithJoin`（改用 `DeleteJoin`） |
| 未设置表名 | `ErrEmptyTable` |
| 写语句带方言不支持的 `ORDER BY` / `LIMIT` | `ErrWriteOrderLimitUnsupported` |
| 非法运算符、子查询图成环等既有错误 | 对应哨兵错误 |

**不要这样做：** 动态过滤条件漏加、参数解析失败、权限判断没通过时，用 `Force()` 让写操作「先跑通」。这会把一个可见的错误变成一次不可逆的全表写入。`Force()` 的合理用法是「代码审查时一眼能看出的、刻意的全表操作」。

## 反例清单

| 反例 | 后果 | 正确做法 |
|---|---|---|
| `WhereRaw("name = '" + name + "'")` | SQL 注入 | `WhereRaw("name = ?", name)` 或 `Where("name", name)` |
| `WhereRaw(fmt.Sprintf("ORDER BY %s", col))` | 注入/语法错误 | 白名单映射到常量列名，用 `OrderBy` |
| 空 `WhereIn` 当作「不过滤」 | 查询恒假、永远空结果 | `if len(ids) > 0` 再追加 |
| 空 `WhereNotIn` 当作「有 WHERE」 | 恒真，保护拒绝或写范围过大 | 显式判断条件是否存在 |
| 非相关 `WhereExists` 给写操作「凑」限定 | 保护放行，实际全表更新/删除 | 补外层条件或改关联子查询 |
| 动态条件为空时 `Force()` | 不可逆全表写入 | 条件数为 0 时直接返回 |
| 多 goroutine 共享一个 `Builder` | 数据竞争、SQL 串条件 | 先 `Clone()` 再分发 |
| 复用被 `Pluck` 过的 Builder 再 `Find` | 只剩提取列 | 每次终端调用用 `Clone()` |

## 延伸阅读

- WHERE 条件全集与分组方法：[builder-where](../reference/builder-where.md)
- 破坏性操作保护、`Force` 与写语句 ORDER BY/LIMIT 的完整边界：[safety](../concepts/safety.md)
- Builder 状态、`Clone` 与终端方法的克隆策略：[builder-state](../concepts/builder-state.md)
- 写操作执行与错误哨兵：[builder-exec](../reference/builder-exec.md)
- SQL 片段如何进入最终产物：[compile-pipeline](../concepts/compile-pipeline.md)
