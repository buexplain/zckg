# UPSERT 与批量写入任务指南

需要「存在则更新、不存在则插入」「冲突就跳过」「把一张表查出来灌进另一张表」时，先选对方法，再核对三方言差异。

## 结论先行

- MySQL 的 `Upsert` 编译为 `INSERT ... ON DUPLICATE KEY UPDATE`：**`uniqueBy` 不参与冲突判定**，任何主键/唯一键冲突都会触发更新，它只影响「默认更新列」的推导。
- PostgreSQL / SQLite 编译为 `INSERT ... ON CONFLICT (...) DO UPDATE`：`uniqueBy` 必填，用来生成冲突目标；为空直接返回 `ErrUpsertUniqueByRequired`。
- `updateColumns` 为空时自动推导为「全部插入列 − uniqueBy」；推导结果为空时退化为「冲突不更新」（PG/SQLite 为 `DO NOTHING`，MySQL 为自赋值 no-op）。
- `InsertOrIgnore` 的「忽略」范围三方言不同：MySQL 还会吞掉截断类警告，SQLite 会跳过其它约束违例，PostgreSQL 基本只忽略唯一/排他冲突。
- `InsertUsing` 的列数校验是**静态尽力而为**：`SELECT *`、含 `*` 的列、`SelectRaw("a, b")` 都会让校验失准或跳过。
- 不要用受影响行数判断成功/失败；冲突跳过与 no-op 都会得到 0。

> 本文的 Go 示例统一包在 `func example()` 中，仅用于语法检查与上下文占位（`db`、`ctx`、`User` 等是外部变量），不保证可独立运行。SQL 以 MySQL 形态为基线，另列 PostgreSQL / SQLite 差异；数据库版本差异（如 PostgreSQL 的 `ON CONFLICT` 需要 9.5+、SQLite 的 UPSERT 需要 3.24+）由运行环境保证。

## Upsert 三方言 SQL

`Upsert(ctx, data, uniqueBy, updateColumns)` 的 `data` 类型与 `Insert` 一致：结构体、结构体指针、结构体切片、结构体指针切片。

```go
func example() {
	type User struct {
		Name  string `db:"name"`
		Age   int    `db:"age"`
		Email string `db:"email"`
	}
	user := User{Name: "alice", Age: 25, Email: "a@t.com"}

	affected, err := db.Builder().Table("users").
		Upsert(ctx, user, []string{"email"}, []string{"name", "age"})
	// MySQL SQL:
	//   INSERT INTO `users` (`name`, `age`, `email`) VALUES (?, ?, ?)
	//   ON DUPLICATE KEY UPDATE `name` = VALUES(`name`), `age` = VALUES(`age`)
	// PostgreSQL SQL:
	//   INSERT INTO "users" ("name", "age", "email") VALUES ($1, $2, $3)
	//   ON CONFLICT ("email") DO UPDATE SET "name" = EXCLUDED."name", "age" = EXCLUDED."age"
	// SQLite SQL:
	//   INSERT INTO "users" ("name", "age", "email") VALUES (?, ?, ?)
	//   ON CONFLICT ("email") DO UPDATE SET "name" = EXCLUDED."name", "age" = EXCLUDED."age"
	_, _ = affected, err
}
```

SQL 的插入列顺序由**实际参与插入的字段**决定（结构体声明顺序、`db` 标签映射、nil 指针 / nil `any` 跳过），与 `uniqueBy`、`updateColumns` 的书写顺序无关。批量插入以首行为模板确定列集合，后续行缺失的列写入 `NULL`：

```go
func example() {
	type User struct {
		Name  string `db:"name"`
		Age   int    `db:"age"`
		Email string `db:"email"`
	}

	affected, err := db.Builder().Table("users").Upsert(ctx, []User{
		{Name: "alice", Age: 25, Email: "a@t.com"},
		{Name: "bob", Age: 30, Email: "b@t.com"},
	}, []string{"email"}, nil)
	// MySQL SQL:
	//   INSERT INTO `users` (`name`, `age`, `email`) VALUES (?, ?, ?), (?, ?, ?)
	//   ON DUPLICATE KEY UPDATE `name` = VALUES(`name`), `age` = VALUES(`age`)
	_, _ = affected, err
}
```

`Expression` 值会直接内联、不占绑定参数（例如 `CreatedAt: zcdb.NewExpression("NOW()")`），与 `Insert` 的规则一致。

## uniqueBy 的方言差异

这是 `Upsert` 最容易被误用的地方：同一个参数名，在 MySQL 和 PG/SQLite 下语义不同。

| 方言 | `uniqueBy` 的作用 | 为空时 | 冲突判定范围 |
|---|---|---|---|
| MySQL | 只参与「默认更新列」的排除；**不生成冲突目标** | 允许（此时 `ON DUPLICATE KEY UPDATE` 仍生成） | 主键或**任意**唯一索引冲突都会触发更新 |
| PostgreSQL | 生成 `ON CONFLICT (cols)` 的冲突目标 | 返回 `ErrUpsertUniqueByRequired` | 命中 `uniqueBy` 指定的唯一/排他索引 |
| SQLite | 同 PostgreSQL | 返回 `ErrUpsertUniqueByRequired` | 同 PostgreSQL |

后果很直接：表上有 `UNIQUE(email)` 和 `UNIQUE(phone)` 两个索引时，MySQL 下 `uniqueBy: []string{"email"}` 并不能阻止「phone 冲突也走更新分支」。如果业务要求「只按 email 判冲突」，MySQL 方言无法用该 API 表达，需要在 SQL 层保证（例如只保留一个唯一键，或改为在事务中先按 email 查询再决定插入/更新，并处理并发冲突）。

```go
func example() {
	// MySQL 可省略 uniqueBy；此时默认更新列为全部插入列
	affected, err := db.Builder().Table("users").
		Upsert(ctx, User{Name: "alice", Age: 25, Email: "a@t.com"}, nil, nil)
	// MySQL SQL:
	//   INSERT INTO `users` (`name`, `age`, `email`) VALUES (?, ?, ?)
	//   ON DUPLICATE KEY UPDATE `name` = VALUES(`name`), `age` = VALUES(`age`), `email` = VALUES(`email`)
	// 注意：任何唯一键冲突都会把这一行的三列都覆盖成插入值
	_, _ = affected, err

	// PostgreSQL / SQLite 缺少 uniqueBy 时在编译期直接报错
	_, err = db.Builder().Table("users").
		Upsert(ctx, User{Name: "alice", Age: 25, Email: "a@t.com"}, nil, nil)
	if errors.Is(err, zcdb.ErrUpsertUniqueByRequired) {
		return
	}
}
```

PG/SQLite 的 `uniqueBy` 列集合还必须能匹配数据库里真实存在的唯一索引（列集合一致），否则语句能编译、执行时由数据库报「没有对应的唯一约束」。另外，`uniqueBy` 里的列应当确实参与插入；如果某列因 nil 指针 / nil `any` 被跳过，冲突目标里的「提议值」会变成该列的默认值，语义通常不是你想要的。

## updateColumns 推导与退化

规则按顺序生效：

1. `updateColumns` 非空：原样使用，框架**不校验**它是否属于插入列。
2. `updateColumns` 为空：取全部插入列，按插入列顺序排除 `uniqueBy` 中的列（字符串精确匹配）。
3. 排除后仍为空：退化为「冲突时不更新」。

| 方言 | 推导为空时的 SQL 尾部 | 冲突命中时 |
|---|---|---|
| MySQL | `ON DUPLICATE KEY UPDATE \`col\` = VALUES(\`col\`)`（自赋值 no-op；`col` 优先取「既在 uniqueBy 又在插入列」的第一列，否则取插入首列） | 0 行受影响（按默认 affected-rows 语义，无实际变化） |
| PostgreSQL | `ON CONFLICT (...) DO NOTHING` | 0 行受影响 |
| SQLite | `ON CONFLICT (...) DO NOTHING` | 0 行受影响 |

```go
func example() {
	// 只插入唯一键列：推导后没有可更新列 → 退化为 DO NOTHING / 自赋值
	type EmailOnly struct {
		Email string `db:"email"`
	}
	only := EmailOnly{Email: "a@t.com"}

	affected, err := db.Builder().Table("users").
		Upsert(ctx, only, []string{"email"}, nil)
	// MySQL SQL:
	//   INSERT INTO `users` (`email`) VALUES (?)
	//   ON DUPLICATE KEY UPDATE `email` = VALUES(`email`)
	// PostgreSQL: INSERT INTO "users" ("email") VALUES ($1) ON CONFLICT ("email") DO NOTHING
	// SQLite:     INSERT INTO "users" ("email") VALUES (?) ON CONFLICT ("email") DO NOTHING
	_, _ = affected, err
}
```

**不要这样做：**把没有参与插入的列放进 `updateColumns`。编译不会报错，但冲突时该列会用「默认值」参与更新：MySQL 的 `VALUES(col)` 在列不在插入列时取默认值，PostgreSQL/SQLite 的 `EXCLUDED.col` 也是提议行里的默认值。

```go
func example() {
	// 反例：updated_at 没有参与 INSERT，冲突时会被默认值覆盖
	_, err := db.Builder().Table("users").
		Upsert(ctx, User{Name: "alice", Age: 25, Email: "a@t.com"},
			[]string{"email"}, []string{"updated_at"})
	// MySQL: ON DUPLICATE KEY UPDATE `updated_at` = VALUES(`updated_at`)
	// PG:    DO UPDATE SET "updated_at" = EXCLUDED."updated_at"
	_ = err
}
```

需要「冲突时更新当前时间」时，把时间作为插入字段传入（或使用 `Expression`），而不是只放进 `updateColumns`。

受影响行数不是成功标志：插入通常是 1，MySQL 的更新分支通常记 2（未启用 `CLIENT_FOUND_ROWS` 时），PG/SQLite 的更新分支记 1；而 `DO NOTHING` / 自赋值 no-op 记 0。判断「到底插入还是更新」应在业务上改用 `RETURNING`（PG）或更新后按唯一键回查。

## InsertOrIgnore 的忽略范围

`InsertOrIgnore` 与 `Insert` 的字段规则完全一致，只改变冲突处理。三方言不仅 SQL 不同，「什么算冲突」也不同：

| 方言 | 编译形态 | 被忽略的情况 | 仍会返回错误的情况 |
|---|---|---|---|
| MySQL | `INSERT IGNORE INTO ...` | 唯一键冲突（跳过该行）；超长值等可降级为警告的错误（实测超长值被截断后写入） | 不能被降级为警告的错误；约束降级行为还受版本与 `sql_mode` 影响 |
| PostgreSQL | `INSERT INTO ... ON CONFLICT DO NOTHING` | 唯一/排他约束冲突（不带目标，任意唯一约束都算） | `NOT NULL`、`CHECK`、外键、语法等错误 |
| SQLite | `INSERT OR IGNORE INTO ...` | `UNIQUE`、`NOT NULL`、`CHECK` 等约束违例（整行跳过） | 外键约束（SQLite 的冲突算法不适用于外键）等其它错误 |

```go
func example() {
	affected, err := db.Builder().Table("users").InsertOrIgnore(ctx, &User{
		Name: "alice", Age: 25, Email: "a@t.com",
	})
	// MySQL:    INSERT IGNORE INTO `users` (`name`, `age`, `email`) VALUES (?, ?, ?)
	// PostgreSQL: INSERT INTO "users" ("name", "age", "email") VALUES ($1, $2, $3) ON CONFLICT DO NOTHING
	// SQLite:   INSERT OR IGNORE INTO "users" ("name", "age", "email") VALUES (?, ?, ?)
	_, _ = affected, err
}
```

因此 `InsertOrIgnore` 不是「数据校验开关」：MySQL 下超长字符串可能被静默截断，SQLite 下 `NOT NULL` 违例可能整行消失，而 PG 下同样的数据却直接报错。批量插入时每一行独立处理，`affected` 可能小于输入行数。

`InsertOrIgnoreUsing` 的忽略范围与 `InsertOrIgnore` 相同，只是 SQL 变成 `INSERT ... SELECT` 形态：

| 方言 | 编译形态 |
|---|---|
| MySQL | `INSERT IGNORE INTO ... SELECT ...` |
| PostgreSQL | `INSERT INTO ... SELECT ... ON CONFLICT DO NOTHING` |
| SQLite | `INSERT OR IGNORE INTO ... SELECT ...` |

## InsertUsing 的列数校验

`InsertUsing(ctx, columns, callback)` 把子查询结果按**位置**插入目标列。编译期会尽力校验列数，但只在「列数可静态判定」时生效：

- 子查询通过 `Select` / `AddSelect` / `SelectRaw` / `SelectSub` 显式指定了列；
- 且所有列值都不含通配符 `*`。

满足条件时，`len(columns)` 与「列数」不一致返回 `ErrInsertUsingColumnMismatch`（错误消息带出两边数量）；不满足时跳过校验，由数据库在运行期报错。

| 子查询投影 | 计数 | 校验结果 |
|---|---|---|
| `Select("name", "age")` | 2 | 参与校验 |
| `Select("name").AddSelect("age")` | 2 | 参与校验 |
| `SelectSub(sub, "cnt")` | 1（每个子查询列算 1） | 参与校验 |
| `SelectRaw("name, age")` | 1（**整段按 1 列**） | 参与校验，但计数错误 |
| `SelectRaw("COUNT(*)")` | 未知（含 `*`） | 跳过校验 |
| `Select("t.*")` | 未知（含 `*`） | 跳过校验 |
| 未调用任何 Select | 未知（默认 `SELECT *`） | 跳过校验 |

```go
func example() {
	// 正确：逐列 Select，数量与顺序都可控
	affected, err := db.Builder().Table("users_archive").
		InsertUsing(ctx, []string{"name", "age"}, func(sub *zcdb.Builder) {
			sub.Table("users").Select("name", "age").Where("status", "=", "active")
		})
	// MySQL SQL:
	//   INSERT INTO `users_archive` (`name`, `age`) SELECT `name`, `age` FROM `users` WHERE `status` = ?
	_, _ = affected, err
}
```

列数不一致会在编译期被拦下：

```go
func example() {
	_, err := db.Builder().Table("users_archive").
		InsertUsing(ctx, []string{"name", "age"}, func(sub *zcdb.Builder) {
			sub.Table("users").Select("name")
		})
	if errors.Is(err, zcdb.ErrInsertUsingColumnMismatch) {
		return
	}
}
```

`SelectRaw` 的多列投影是最典型的「计数陷阱」：它被当作 1 列。

```go
func example() {
	// 反例：目标 2 列，SelectRaw("name, age") 被按 1 列计数 → 误报列数不一致
	_, err := db.Builder().Table("users_archive").
		InsertUsing(ctx, []string{"name", "age"}, func(sub *zcdb.Builder) {
			sub.Table("users").SelectRaw("name, age")
		})
	// err 为 ErrInsertUsingColumnMismatch（insert columns=2, subquery select columns=1）
	_ = err

	// 反例：含 * 时跳过校验；目标列数与实际投影不一致只会在执行期报错
	_, err = db.Builder().Table("users_archive").
		InsertUsing(ctx, []string{"name", "age"}, func(sub *zcdb.Builder) {
			sub.Table("users").SelectRaw("COUNT(*)")
		})
	_ = err
}
```

列数一致也不代表结果正确：`columns` 与子查询投影按**位置**对应，不按列名。目标列顺序与 `Select` 顺序相反时，数据会静默写反：

```go
func example() {
	// 反例：数量一致（2 = 2），但 age 写进 name、name 写进 age
	_, err := db.Builder().Table("users_archive").
		InsertUsing(ctx, []string{"name", "age"}, func(sub *zcdb.Builder) {
			sub.Table("users").Select("age", "name")
		})
	_ = err
}
```

需要精确校验时用 `Select` / `AddSelect` 逐列书写，并保持两边顺序一致；无法静态判定的投影，列数一致性只能交给数据库在运行期校验。

## 方法对比

| 方法 | 冲突时 | MySQL | PostgreSQL | SQLite | 编译期列数校验 |
|---|---|---|---|---|---|
| `Insert` | 报错 | `INSERT INTO ...` | `INSERT INTO ...` | `INSERT INTO ...` | 无 |
| `Upsert` | 更新指定列（可退化为不更新） | `ON DUPLICATE KEY UPDATE col = VALUES(col)` | `ON CONFLICT (cols) DO UPDATE SET col = EXCLUDED.col` | 同 PG（`?` 占位符） | 无 |
| `InsertOrIgnore` | 跳过该行 | `INSERT IGNORE` | `ON CONFLICT DO NOTHING` | `INSERT OR IGNORE` | 无 |
| `InsertUsing` | 报错 | `INSERT INTO ... SELECT ...` | 同 MySQL | 同 MySQL | 有，静态尽力 |
| `InsertOrIgnoreUsing` | 跳过冲突行 | `INSERT IGNORE ... SELECT` | `... SELECT ... ON CONFLICT DO NOTHING` | `INSERT OR IGNORE ... SELECT` | 有，静态尽力 |

## 反例与上线前检查

| 反例 | 问题 | 正确做法 |
|---|---|---|
| MySQL 下用 `uniqueBy` 期望「只按 email 判冲突」 | MySQL 任意唯一键冲突都会更新 | 接受该语义，或调整唯一索引 / 改用原始 SQL |
| `updateColumns` 传入未参与插入的列 | 冲突时用默认值覆盖 | 只传插入列的子集 |
| 用 `affected == 0` 判断失败 | 冲突跳过与 no-op 都是 0 | PostgreSQL 用 `RETURNING`，其它方言更新后按唯一键回查 |
| PG/SQLite 省略 `uniqueBy` | `ErrUpsertUniqueByRequired` | 显式传冲突目标列 |
| 用 `InsertOrIgnore` 做数据校验 | MySQL 截断、SQLite 跳约束行、PG 报错 | 入库前显式校验 + 统一约束 |
| `InsertUsing` 用 `SelectRaw("a, b")` | 被按 1 列计数，误报/漏报 | 逐列 `Select` / `AddSelect` |
| `InsertUsing` 两边列顺序不一致 | 按位置写反，数量校验发现不了 | 对齐顺序，必要时加断言测试 |

上线前至少核对：

1. `uniqueBy` 是否匹配真实唯一索引（PG/SQLite），以及 MySQL 是否能接受「任意唯一键冲突都更新」。
2. `updateColumns` 是否是插入列的子集；为空时的推导结果是否符合预期（尤其全部插入列都是 uniqueBy 的退化场景）。
3. 是否需要区分「插入/更新/跳过」：需要时不要依赖受影响行数。
4. `InsertUsing` 的目标列与子查询投影是否**数量和顺序都一致**；是否误用了多列 `SelectRaw`。
5. 三方言是否都跑过（SQLite 可用内存库，MySQL/PostgreSQL 用集成测试环境）。

## 延伸阅读

- 插入/更新/删除的完整执行语义与错误哨兵：[builder-exec](../reference/builder-exec.md)
- 写语句 ORDER BY/LIMIT 的方言限制：[safety](../concepts/safety.md)
- 编译期与运行期的边界（哪些错误能提前发现）：[compile-pipeline](../concepts/compile-pipeline.md)
- 三方言 SQL 形态总表：[dialect-matrix](../reference/dialect-matrix.md)
