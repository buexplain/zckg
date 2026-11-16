# 执行型写方法

`Builder` 的写操作分两层：`ToXxx` 只编译出 SQL 与绑定参数，`Insert`、`Update`、`Delete` 等终端方法在编译成功后通过 `DBDao.Exec` 真正执行。本文只讲后者：每个方法接受什么数据、编译成什么 SQL、返回什么、会报什么错，以及哪些行为由方言决定。

纯编译入口的完整参数与产物规则见 [builder-compile](../reference/builder-compile.md)；写请求在分层中的位置见 [architecture](../concepts/architecture.md)；无 WHERE 保护的判定规则见 [safety](../concepts/safety.md)。

> 示例约定：Go 示例统一包在 `func example()` 中，`db`、`ctx` 是外部变量；SQL 以 MySQL 形态为基线，方言差异写在示例注释里。示例不保证可独立运行。

## 方法与返回值

```text
Insert(ctx context.Context, data any) (int64, error)
InsertGetId(ctx context.Context, data any) (int64, error)
InsertOrIgnore(ctx context.Context, data any) (int64, error)
Upsert(ctx context.Context, data any, uniqueBy []string, updateColumns []string) (int64, error)
InsertUsing(ctx context.Context, columns []string, callback func(*Builder)) (int64, error)
InsertOrIgnoreUsing(ctx context.Context, columns []string, callback func(*Builder)) (int64, error)
Update(ctx context.Context, data any) (int64, error)
Increment(ctx context.Context, column string, amount any, extra ...any) (int64, error)
Decrement(ctx context.Context, column string, amount any, extra ...any) (int64, error)
Delete(ctx context.Context) (int64, error)
DeleteJoin(ctx context.Context) (int64, error)
Truncate(ctx context.Context) error
```

- `Insert`、`InsertOrIgnore`、`Upsert`、`InsertUsing`、`InsertOrIgnoreUsing`、`Update`、`Increment`、`Decrement`、`Delete`、`DeleteJoin` 返回 `sql.Result.RowsAffected()` 的原样值；`InsertGetId` 返回 `LastInsertId()`；`Truncate` 只返回 `error`。
- 错误路径统一返回 `0`（`Truncate` 只返回错误），不返回「半成功」的行数。`InsertGetId` 在 PostgreSQL 下刻意在执行前拒绝，就是为了避免「插入成功但返回错误」的半成功状态。
- 每个方法都先调用同名 `ToXxx` 完成编译，再执行。SQL、绑定参数、编译期错误与纯编译入口一致；执行层额外做的是连接选择、`Exec` 与结果读取。唯一例外是 SQLite 的 `Truncate`，它在主语句之外还要清理 `sqlite_sequence`（见下文）。
- 写操作固定走写连接：`DBDao.Exec` 在 `ctx` 携带本连接池事务时使用事务连接，否则固定 `Pool.PickWriteDB()`（主库），不会命中从库。路由与事务细节见 [execution-routing](../concepts/execution-routing.md)。
- 慢 SQL 回调收到的是实际执行的 SQL 与绑定参数，包括 SQLite `Truncate` 的辅助语句。

## 共同语义与错误

### 注释

设置 DAO 默认注释（`NewDBDao` 第五参数）或 `Builder.Comment` 后，写入方法执行各自 `ToXxx` 的带注释产物，**不在执行层二次追加**；绑定参数、返回值与路由不变。注释只拼接在完整语句末尾，规范化规则与生命周期见 [safety](../concepts/safety.md) 和 [builder-state](../concepts/builder-state.md)。

注释不是有效 WHERE/JOIN 条件，不能替代 `Force()` 授权。DAO 原始 `Exec` 仍原文透传，不追加注释。SQLite `Truncate` 只有主 `DELETE FROM` 携带注释，`sqlite_master` 预查询与 `sqlite_sequence` 清理语句不追加（即使 DAO 有默认注释）；慢 SQL 回调会分别收到这些实际执行的语句。

### 错误

编译期错误在 `Exec` 之前返回；驱动/引擎错误原样透传；`RowsAffected()`/`LastInsertId()` 自身的错误也会返回。框架不重试、不吞错，也不做「先查后写」的补偿。

与写操作直接相关的哨兵：

| 哨兵 | 触发 |
|---|---|
| `ErrEmptyTable` | 未设置 `Table`（写语句只认表名，`TableSub` 不满足） |
| `ErrInvalidStruct` | data 不是要求的结构体/结构体指针/切片形态，或切片元素为 nil 指针 |
| `ErrEmptyData` | 插入切片为空 |
| `ErrNoFields` | 没有可用导出字段，或全部字段被 nil 跳过 |
| `ErrInsertGetIdUnsupported` | PostgreSQL 下调用 `InsertGetId` |
| `ErrUpsertUniqueByRequired` | PostgreSQL/SQLite 的 `Upsert` 未给 `uniqueBy` |
| `ErrInsertUsingColumnMismatch` | `InsertUsing`/`InsertOrIgnoreUsing` 目标列数与子查询列数可静态判定且不一致 |
| `ErrIncrementColumns` | `Increment`/`Decrement` 的列名与增量不成对、列名非 string，或编译期切片为空/不等长 |
| `ErrDeleteWithJoin` | `Delete` 时 Builder 带 JOIN |
| `ErrDeleteJoinNoJoin` | `DeleteJoin` 时没有任何 JOIN |
| `ErrUpdateWithoutWhere` | `Update`/`Increment`/`Decrement` 无有效限定且未 `Force()` |
| `ErrDeleteWithoutWhere` | `Delete`/`DeleteJoin` 无有效限定且未 `Force()` |
| `ErrWriteOrderLimitUnsupported` | 写语句携带 `ORDER BY`/`LIMIT`，但当前方言或语句形态不支持 |

完整哨兵清单与 `errors.Is` 用法见 [errors](../reference/errors.md)。

## 字段映射与值处理

插入族与 `Update` 共用同一套结构体展开规则（`extractInsertData`/`extractUpdateData`）：

- 列名通过结构体标签映射；默认标签是 `db`，可由 `NewDBDao` 第四参数改成其它标签名。无标签的字段名转 snake_case，标签值为 `-` 的字段跳过。
- **`any`（`interface{}`）类型字段**：值为 nil 时该列跳过；非 nil 时取实际值。
- **指针类型字段**（`*string`、`*int` 等）：nil 时该列跳过；非 nil 时自动解引用为具体值。
- **其它类型字段**：即使值是零值也参与写入（`Id: 0` 也会进列），不会因为零值被跳过。需要「不写」时改用指针或 `any` 并置 nil。
- **`Expression` 字段值**：直接内联进 SQL，不作为绑定参数；`ToInsert`/`ToUpdate` 收集 args 时会跳过它。
- **全部可写字段都被跳过**（全部为 nil 指针/`any`，或结构体没有任何可导出字段）时返回 `ErrNoFields`，不会生成空列集合的 SQL。
- 嵌入结构体中的字段按映射规则展开；嵌入指针为 nil 时该字段不可用，按跳过处理。
- data 形态非法（数字、字符串、非结构体切片、nil 等）返回 `ErrInvalidStruct`；插入切片为空返回 `ErrEmptyData`。

批量插入以**首行**为模板确定列集合；后续行对应列为 nil 时传入 SQL `NULL`，而不是把该列从整条 INSERT 中删掉。切片中任一元素是 nil 指针时返回 `ErrInvalidStruct`。

字段映射的完整规则（嵌入、同名遮蔽、缓存）见 [mapping](../concepts/mapping.md)。

```go
func example() {
	type User struct {
		Name  *string `db:"name"`
		Age   *int    `db:"age"`
		Email any     `db:"email"`
	}

	name, age := "alice", 25
	affected, err := db.Builder().Table("users").Insert(ctx, User{Name: &name, Age: &age})
	// Email 为 nil → 跳过 email 列
	// MySQL:      INSERT INTO `users` (`name`, `age`) VALUES (?, ?)
	// PostgreSQL: INSERT INTO "users" ("name", "age") VALUES ($1, $2)
	// SQLite:     INSERT INTO "users" ("name", "age") VALUES (?, ?)
	// args: [alice 25]
	_, _ = affected, err
}
```

## Insert

`Insert(ctx, data)` 插入并返回受影响行数。data 支持四种形态：

- 单个结构体 `struct{}`；
- 结构体指针 `*struct{}`；
- 结构体切片 `[]struct{}`；
- 结构体指针切片 `[]*struct{}`。

切片生成一条多行 `VALUES` 的 INSERT；列集合由首行决定（规则见上一节）。`Expression` 字段值直接内联，因此同一条 INSERT 里可以混用占位符与数据库函数。

```go
func example() {
	type User struct {
		Name      string `db:"name"`
		Age       int    `db:"age"`
		CreatedAt any    `db:"created_at"`
	}

	affected, err := db.Builder().Table("users").Insert(ctx, []User{
		{Name: "alice", Age: 25, CreatedAt: zcdb.NewExpression("NOW()")},
		{Name: "bob", Age: 30, CreatedAt: zcdb.NewExpression("NOW()")},
	})
	// MySQL:      INSERT INTO `users` (`name`, `age`, `created_at`) VALUES (?, ?, NOW()), (?, ?, NOW())
	// PostgreSQL: INSERT INTO "users" ("name", "age", "created_at") VALUES ($1, $2, NOW()), ($3, $4, NOW())
	// SQLite:     INSERT INTO "users" ("name", "age", "created_at") VALUES (?, ?, NOW()), (?, ?, NOW())
	// args: [alice 25 bob 30]
	_, _ = affected, err
}
```

- 返回：驱动报告的实际插入行数（MySQL 的 `INSERT` 即插入行数）。
- 错误：`ErrEmptyTable`、`ErrInvalidStruct`、`ErrEmptyData`、`ErrNoFields`，以及驱动错误。参数错误与 `ToInsert` 完全一致。
- 不带 `WHERE`/`JOIN`/`ORDER BY`/`LIMIT` 语义：Builder 上即使设置过这些状态，也不会进入 INSERT。

## InsertGetId

`InsertGetId(ctx, data)` 插入并返回自增 ID。data 必须是**单个结构体或结构体指针**，不支持切片：

- 非空切片直接返回 `ErrInvalidStruct`（多行插入的 `LastInsertId` 语义不明确）；
- 空切片仍交给 `ToInsert`，返回 `ErrEmptyData`；
- 其它 data 错误与 `ToInsert` 一致。

SQL 与同形态的 `Insert` 完全相同；MySQL/SQLite 正常执行后返回 `LastInsertId()`。

```go
func example() {
	type User struct {
		Name string `db:"name"`
		Age  int    `db:"age"`
	}

	id, err := db.Builder().Table("users").InsertGetId(ctx, User{Name: "alice", Age: 25})
	// MySQL:    INSERT INTO `users` (`name`, `age`) VALUES (?, ?)
	// SQLite:   INSERT INTO "users" ("name", "age") VALUES (?, ?)
	// PostgreSQL: 不执行，返回 ErrInsertGetIdUnsupported
	_, _ = id, err
}
```

> **PostgreSQL 限制**：lib/pq 不支持 `LastInsertId`（PG 取自增 ID 需要 `RETURNING` 子句）。为避免「插入成功但返回错误」的半成功状态，PG 方言下 `InsertGetId` 在编译成功之后、执行之前返回 `ErrInsertGetIdUnsupported`（错误消息带出 `lib/pq does not support LastInsertId` 并建议改用 `Insert` 或带 `RETURNING` 的原始 SQL）。数据/编译错误优先于该方言限制返回。

## InsertOrIgnore

`InsertOrIgnore(ctx, data)` 插入时忽略冲突，返回实际插入的行数。data 的类型与字段处理规则与 `Insert` 完全一致。

三方言的**忽略范围不同**，这是引擎语义，不是框架统一出来的：

| 方言 | SQL 形态 | 忽略范围 |
|---|---|---|
| MySQL | `INSERT IGNORE INTO ...` | 唯一键冲突，以及超长值截断等警告级错误会被降级后写入（实测 `VARCHAR(2)` 写入 `abcdef` 得到 `ab`、无错误） |
| PostgreSQL | `INSERT INTO ... ON CONFLICT DO NOTHING` | 严格限于冲突，其它约束错误照常报错 |
| SQLite | `INSERT OR IGNORE INTO ...` | 唯一键冲突，以及 NOT NULL/CHECK 等约束违例会被整行跳过 |

```go
func example() {
	type User struct {
		Name string `db:"name"`
		Age  int    `db:"age"`
	}

	affected, err := db.Builder().Table("users").InsertOrIgnore(ctx, User{Name: "alice", Age: 25})
	// MySQL:      INSERT IGNORE INTO `users` (`name`, `age`) VALUES (?, ?)
	// PostgreSQL: INSERT INTO "users" ("name", "age") VALUES ($1, $2) ON CONFLICT DO NOTHING
	// SQLite:     INSERT OR IGNORE INTO "users" ("name", "age") VALUES (?, ?)
	_, _ = affected, err
}
```

- 返回：实际插入的行数；被忽略的行不计入。
- 错误：与 `Insert` 相同的参数错误，加上驱动/引擎错误。

## Upsert

`Upsert(ctx, data, uniqueBy, updateColumns)` 插入或更新，返回受影响行数。data 的类型与字段处理规则与 `Insert` 完全一致。

- `uniqueBy`：唯一索引列名。PostgreSQL/SQLite 用它生成 `ON CONFLICT` 冲突目标；MySQL 的 `ON DUPLICATE KEY UPDATE` **不用它判定冲突**——主键或任意唯一键冲突都会触发更新，它只参与默认更新列的推导（含退化形态的自赋值列选择）。
- `updateColumns`：冲突时要更新的列。为空（nil 或长度 0）时默认更新所有插入列，并排除 `uniqueBy` 列；显式传入时原样使用，不再过滤 `uniqueBy`。
- 若排除后 `updateColumns` 为空（全部插入列都是 `uniqueBy`），退化为「冲突时不更新」：PostgreSQL/SQLite 编译为 `DO NOTHING`；MySQL 退化为单列自赋值 no-op（该列优先取同时出现在插入列中的 `uniqueBy` 列，否则取首列；冲突时报告 0 行受影响）。

```go
func example() {
	type User struct {
		Name  string `db:"name"`
		Age   int    `db:"age"`
		Email string `db:"email"`
	}

	affected, err := db.Builder().Table("users").Upsert(ctx,
		User{Name: "alice", Age: 25, Email: "a@t.com"},
		[]string{"email"}, []string{"name", "age"})
	// MySQL:      INSERT INTO `users` (`name`, `age`, `email`) VALUES (?, ?, ?)
	//             ON DUPLICATE KEY UPDATE `name` = VALUES(`name`), `age` = VALUES(`age`)
	// PostgreSQL: INSERT INTO "users" ("name", "age", "email") VALUES ($1, $2, $3)
	//             ON CONFLICT ("email") DO UPDATE SET "name" = EXCLUDED."name", "age" = EXCLUDED."age"
	// SQLite:     同 PostgreSQL 形态，占位符为 ?
	_, _ = affected, err
}
```

```go
func example() {
	type OnlyEmail struct {
		Email string `db:"email"`
	}

	// 全部插入列均为 uniqueBy：退化形态
	affected, err := db.Builder().Table("users").Upsert(ctx,
		OnlyEmail{Email: "a@t.com"}, []string{"email"}, nil)
	// PostgreSQL: INSERT INTO "users" ("email") VALUES ($1) ON CONFLICT ("email") DO NOTHING
	// SQLite:     INSERT INTO "users" ("email") VALUES (?) ON CONFLICT ("email") DO NOTHING
	// MySQL:      INSERT INTO `users` (`email`) VALUES (?) ON DUPLICATE KEY UPDATE `email` = VALUES(`email`)
	_, _ = affected, err
}
```

- **PostgreSQL/SQLite 下 `uniqueBy` 为空报 `ErrUpsertUniqueByRequired`**（无法生成 `ON CONFLICT` 目标）；MySQL 的 `ON DUPLICATE KEY UPDATE` 不需要冲突目标，可以省略。
- 返回：MySQL 的 `ON DUPLICATE KEY UPDATE` 在默认未启用 `CLIENT_FOUND_ROWS` 时报告 1（插入）、2（更新）或 0（冲突但值未变）；PG/SQLite 为实际插入或更新的行数（`DO NOTHING` 为 0）。
- 错误：data 错误、`ErrUpsertUniqueByRequired`、驱动错误。

## InsertUsing / InsertOrIgnoreUsing

`InsertUsing(ctx, columns, callback)` 把 SELECT 子查询的结果插入目标表（`INSERT INTO ... SELECT`），返回受影响行数。`columns` 是目标表列名，`callback` 用于构建 SELECT 子查询。

编译语义：

- 外层 Builder 只提供**目标表、注释与累积错误**；外层 WHERE/JOIN/ORDER BY/LIMIT 等状态不参与 `INSERT ... SELECT` 的编译。
- 子查询的数据源、投影、WHERE、JOIN、排序等全部来自 `callback`；子查询自身的累积错误、环引用与空数据源都会整体返回错误。
- `callback` 为 nil 时会在调用处直接 panic（内部直接调用，未做 nil 校验）。

```go
func example() {
	affected, err := db.Builder().Table("users_archive").
		InsertUsing(ctx, []string{"name", "age"}, func(sub *zcdb.Builder) {
			sub.Table("users").Select("name", "age").Where("status", "=", "active")
		})
	// MySQL:      INSERT INTO `users_archive` (`name`, `age`) SELECT `name`, `age` FROM `users` WHERE `status` = ?
	// PostgreSQL: INSERT INTO "users_archive" ("name", "age") SELECT "name", "age" FROM "users" WHERE "status" = $1
	// SQLite:     INSERT INTO "users_archive" ("name", "age") SELECT "name", "age" FROM "users" WHERE "status" = ?
	// args: [active]
	_, _ = affected, err
}
```

`InsertOrIgnoreUsing` 语义相同，仅冲突处理不同：

```go
func example() {
	affected, err := db.Builder().Table("users_archive").
		InsertOrIgnoreUsing(ctx, []string{"name", "age"}, func(sub *zcdb.Builder) {
			sub.Table("users").Select("name", "age")
		})
	// MySQL:      INSERT IGNORE INTO `users_archive` (`name`, `age`) SELECT `name`, `age` FROM `users`
	// PostgreSQL: INSERT INTO "users_archive" ("name", "age") SELECT "name", "age" FROM "users" ON CONFLICT DO NOTHING
	// SQLite:     INSERT OR IGNORE INTO "users_archive" ("name", "age") SELECT "name", "age" FROM "users"
	_, _ = affected, err
}
```

**列数校验**：子查询通过 `Select`/`AddSelect`/`SelectRaw`/`SelectSub` 指定列，且所有列值都不含通配符 `*` 时，`ToInsertUsing`/`ToInsertOrIgnoreUsing` 会在编译期校验目标列数与子查询列数一致，不一致返回 `ErrInsertUsingColumnMismatch`。

无法静态判定列数的场景不做编译期校验，列数一致性由数据库在运行时校验：`SELECT *`、`t.*`、`COUNT(*)` 等含 `*` 的投影，以及未显式指定列（默认 `SELECT *`）。注意：单条 `SelectRaw` 只按 1 列计数，`SelectRaw("a, b")` 这类多列原始投影会被当作 1 列而可能误报 `ErrInsertUsingColumnMismatch`（反向也可能漏报）；需要精确校验时请用 `Select`/`AddSelect` 逐列书写。`SelectSub` 每个子查询按 1 列计数。

- 返回：实际插入的行数（`InsertOrIgnoreUsing` 中被忽略的行不计入）。
- 错误：`ErrEmptyTable`（外层无表名或子查询无数据源）、子查询的构造期错误、`ErrCyclicQuery`、`ErrInsertUsingColumnMismatch`、驱动错误。

## Update

`Update(ctx, data)` 更新数据，返回受影响行数。data 必须是**单个结构体或结构体指针**，不支持切片；字段映射与 nil 跳过规则同 Insert（nil 字段跳过，因此天然支持部分更新）。

```go
func example() {
	type UserUpdate struct {
		Name *string `db:"name"`
		Age  *int    `db:"age"`
	}

	name, age := "alice_new", 26
	affected, err := db.Builder().Table("users").Where("id", "=", 1).
		Update(ctx, UserUpdate{Name: &name, Age: &age})
	// MySQL:      UPDATE `users` SET `name` = ?, `age` = ? WHERE `id` = ?
	// PostgreSQL: UPDATE "users" SET "name" = $1, "age" = $2 WHERE "id" = $3
	// SQLite:     UPDATE "users" SET "name" = ?, "age" = ? WHERE "id" = ?
	// args: [alice_new 26 1]
	_, _ = affected, err
}
```

`Expression` 可用于基于当前值的更新：

```go
func example() {
	affected, err := db.Builder().Table("users").Where("id", "=", 1).
		Update(ctx, struct {
			Age any `db:"age"`
		}{Age: zcdb.NewExpression("`age` + 1")})
	// SQL:  UPDATE `users` SET `age` = `age` + 1 WHERE `id` = ?
	// args: [1]
	_, _ = affected, err
}
```

带 JOIN 的 UPDATE 也可执行，三方言形态不同：

- MySQL：多表 UPDATE 直译，`UPDATE ... JOIN ... ON ... SET ... WHERE ...`；
- PostgreSQL/SQLite：`UPDATE ... SET ... FROM ... WHERE ...`，JOIN 的 ON 条件并入 WHERE 前部。

SET 与 JOIN 的绑定参数先后顺序由方言决定（MySQL 为 JOIN → SET → WHERE；PG/SQLite 为 SET → FROM/ON → WHERE），框架内部已正确处理；细节见 [builder-compile](../reference/builder-compile.md) 的绑定参数顺序。

- `ORDER BY`/`LIMIT`：只对 MySQL 单表 UPDATE 生效；MySQL 多表 UPDATE 与 PG/SQLite 全部拒绝，详见「ORDER BY / LIMIT 的支持范围」。
- 破坏性保护：无有效限定且未 `Force()` 时返回 `ErrUpdateWithoutWhere`；契约见「破坏性操作保护」。
- 返回：驱动报告的受影响行数（MySQL 默认口径为实际变更行，是否按匹配行计数由驱动/DSN 决定）。
- 错误：`ErrEmptyTable`、`ErrInvalidStruct`、`ErrNoFields`、`ErrUpdateWithoutWhere`、`ErrWriteOrderLimitUnsupported`、`ErrCyclicQuery`、驱动错误。

## Increment / Decrement

`Increment(ctx, column, amount, extra...)` 原子自增指定列，`Decrement` 为自减，返回受影响行数。避免「读-改-写」的并发问题。

```go
func example() {
	affected, err := db.Builder().Table("users").Where("id", "=", 1).
		Increment(ctx, "wallet", 100)
	// MySQL:      UPDATE `users` SET `wallet` = `wallet` + ? WHERE `id` = ?
	// PostgreSQL: UPDATE "users" SET "wallet" = "wallet" + $1 WHERE "id" = $2
	// SQLite:     UPDATE "users" SET "wallet" = "wallet" + ? WHERE "id" = ?
	// args: [100 1]
	_, _ = affected, err

	affected, err = db.Builder().Table("users").Where("id", "=", 1).
		Decrement(ctx, "wallet", 50)
	// MySQL:      UPDATE `users` SET `wallet` = `wallet` - ? WHERE `id` = ?
	// PostgreSQL: UPDATE "users" SET "wallet" = "wallet" - $1 WHERE "id" = $2
	// SQLite:     UPDATE "users" SET "wallet" = "wallet" - ? WHERE "id" = ?
	// args: [50 1]
	_, _ = affected, err
}
```

`extra` 变参可交替传入更多「列名, 增量」对，一次更新多列：

```go
func example() {
	affected, err := db.Builder().Table("users").Where("id", "=", 1).
		Increment(ctx, "wallet", 100, "level", 1)
	// MySQL:      UPDATE `users` SET `wallet` = `wallet` + ?, `level` = `level` + ? WHERE `id` = ?
	// PostgreSQL: UPDATE "users" SET "wallet" = "wallet" + $1, "level" = "level" + $2 WHERE "id" = $3
	// SQLite:     UPDATE "users" SET "wallet" = "wallet" + ?, "level" = "level" + ? WHERE "id" = ?
	// args: [100 1 1]
	_, _ = affected, err
}
```

- 参数解析：`extra` 长度为奇数（不成对）或列名位置不是 `string` 时返回 `ErrIncrementColumns`；纯编译 `ToIncrement`/`ToDecrement` 的 `columns`、`amounts` 为空或长度不等时也返回该错误。
- SQL 形态：复用 UPDATE 编译路径，`SET col = col + ?`（或 `- ?`）；带 JOIN 时与 Update 相同，走 `UpdateSetBeforeJoin()` 区分的绑定顺序。
- ORDER BY/LIMIT：与 UPDATE 相同的方言限制。
- 破坏性保护：无有效限定且未 `Force()` 时返回 `ErrUpdateWithoutWhere`（与 Update 同一套保护）。
- 返回/错误：受影响行数；`ErrIncrementColumns`、`ErrEmptyTable`、`ErrUpdateWithoutWhere`、`ErrWriteOrderLimitUnsupported`、`ErrCyclicQuery`、驱动错误。

## Delete / DeleteJoin

`Delete(ctx)` 删除数据，返回受影响行数。普通 `Delete` 不编译 JOIN：只要 Builder 带 JOIN，就返回 `ErrDeleteWithJoin`，即使同时有 WHERE 或调用了 `Force()` 也不放行；请改用 `DeleteJoin` 或 `WhereInSub` + `Delete`。

```go
func example() {
	affected, err := db.Builder().Table("users").Where("id", "=", 1).Delete(ctx)
	// MySQL:      DELETE FROM `users` WHERE `id` = ?
	// PostgreSQL: DELETE FROM "users" WHERE "id" = $1
	// SQLite:     DELETE FROM "users" WHERE "id" = ?
	// args: [1]
	_, _ = affected, err
}
```

`DeleteJoin(ctx)` 按关联条件删除主表行，要求至少一个 JOIN；没有任何 JOIN 时返回 `ErrDeleteJoinNoJoin`。三方言实现路径不同：

```go
func example() {
	affected, err := db.Builder().Table("users").
		Join("orders", "orders.user_id", "=", "users.id").
		Where("orders.status", "=", "cancelled").
		DeleteJoin(ctx)
	// MySQL:      DELETE `users` FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id` WHERE `orders`.`status` = ?
	// PostgreSQL: DELETE FROM "users" USING "orders" WHERE "orders"."user_id" = "users"."id" AND "orders"."status" = $1
	// SQLite:     DELETE FROM "users" WHERE "id" IN (SELECT "users"."id" FROM "users" INNER JOIN "orders" ON ... WHERE "orders"."status" = ?)
	// args: [cancelled]
	_, _ = affected, err
}
```

> **SQLite 方言限制**：`DeleteJoin` 编译为主键 `IN` 子查询，主键列名**硬编码为 `id`**。主键不是 `id` 的表请勿在 SQLite 下使用 `DeleteJoin`，改用 `WhereInSub("实际主键列", callback)` + `Delete` 组合。另外不要给 `DeleteJoin` 传带 `AS` 别名的表名（`Table("users AS u")`）：SQLite 产物会在子查询里生成非法的 `"users" AS "u"."id"`，实测报语法错误。

- 绑定顺序：MySQL/SQLite 按 JOIN 逐项收集（派生表子查询 → 嵌套 join 组 → ON），最后 WHERE；PostgreSQL 先全部派生表子查询（JOIN 顺序），再全部 ON 条件（嵌套组在前、自身条件在后），最后 WHERE。完整规则见 [builder-compile](../reference/builder-compile.md)。
- ORDER BY/LIMIT：MySQL `DeleteJoin` 与 PG/SQLite 全部拒绝，详见下一节。
- 破坏性保护：`Delete` 与 `DeleteJoin` 无有效限定时返回 `ErrDeleteWithoutWhere`；带条件 JOIN 本身可视为有效限定。契约见「破坏性操作保护」。
- 返回/错误：受影响行数；`ErrDeleteWithJoin`、`ErrDeleteJoinNoJoin`、`ErrDeleteWithoutWhere`、`ErrWriteOrderLimitUnsupported`、`ErrEmptyTable`、`ErrCyclicQuery`、驱动错误。

## Truncate

`Truncate(ctx)` 清空整表，只返回 `error`。它不受无 WHERE 保护约束（本身就是全表语义），也不读取 Builder 的 WHERE/JOIN/ORDER BY/LIMIT。

```go
func example() {
	err := db.Builder().Table("users").Truncate(ctx)
	// MySQL:      TRUNCATE TABLE `users`
	// PostgreSQL: TRUNCATE TABLE "users" RESTART IDENTITY
	// SQLite:     DELETE FROM "users"
	_ = err
}
```

SQLite 特殊处理：`DELETE FROM` 不会重置 AUTOINCREMENT 序列，`Truncate` 会额外清空 `sqlite_sequence` 中该表的记录，使自增主键从头开始。

- `DELETE FROM`、`sqlite_master` 存在性预查询与序列清理三步包在**同一事务**中执行；调用方 `ctx` 已携带同一连接池的事务时自动并入外层事务，避免「数据已删但序列未重置」的中间状态。
- 若经 `sqlite_master` 预查询确认 `sqlite_sequence` 表不存在（典型于整库从未使用 AUTOINCREMENT），跳过清理；`sqlite_sequence` 存在时执行 `DELETE FROM sqlite_sequence WHERE name = ?`，该表没有对应序列行时删除 0 行。清理失败如实返回错误，事务回滚。
- 注释：SQLite 仅 `ToTruncate` 生成的 `DELETE FROM` 主语句携带注释；预查询与序列清理语句不自动追加，即使 DAO 有默认注释也如此。MySQL 注释位于完整 `TRUNCATE TABLE` 后，PostgreSQL 位于 `RESTART IDENTITY` 后。
- 错误：`ErrEmptyTable`、SQLite 事务/执行错误。`Truncate` 不主动做子查询图环检测，也不编译 WHERE/JOIN，因此不会因这些状态返回 `ErrWriteOrderLimitUnsupported`；`b.err` 已携带的错误仍会原样返回。

## ORDER BY / LIMIT 的支持范围

写语句携带 `Builder.OrderBy`/`Limit` 时，支持范围很窄；超出即在编译期返回 `ErrWriteOrderLimitUnsupported`：

| 方言 / 形态 | 行为 |
|---|---|
| MySQL 单表 UPDATE / DELETE / Increment / Decrement | 支持 `... ORDER BY ... LIMIT n`：按排序只改/删前 n 行（`ORDER BY` 决定是哪 n 行；省略 `ORDER BY` 时由引擎选择，结果不确定） |
| MySQL 多表 UPDATE（带 JOIN） | 不支持：编译期返回 `ErrWriteOrderLimitUnsupported`（引擎也会以 `Error 1221` 拒绝，框架提前拦截） |
| MySQL DeleteJoin | 不支持：编译期返回 `ErrWriteOrderLimitUnsupported` |
| PostgreSQL / SQLite 的 UPDATE / DELETE / DeleteJoin / Increment / Decrement | 不支持：两者语法中没有该子句，忽略会静默把写范围放大到全部命中行，故编译期拒绝 |

`Insert` 族（Insert/InsertOrIgnore/InsertGetId/Upsert/InsertUsing 系列）与 `Truncate` **不读取外层 Builder 的**这两个状态，设置后静默忽略，不返回 `ErrWriteOrderLimitUnsupported`。`InsertUsing` 的 callback 子查询内部仍可正常使用 ORDER BY/LIMIT，作为 SELECT 的一部分编译。

- 只校验会出现在写语句里的状态：`Limit(n<=0)` 不输出 LIMIT，不计入；`Offset` 不进入任何写语句，也不在校验范围。
- `ORDER BY`/`LIMIT` 不构成有效 WHERE，不能替代破坏性操作保护。

## 破坏性操作保护（使用契约）

`Update`、`Increment`、`Decrement`、`Delete`、`DeleteJoin` 在**无有效限定条件**时默认拒绝执行，防止误操作全表：

- `Update` / `Increment` / `Decrement`：无有效 WHERE/JOIN 且未 `Force()` → `ErrUpdateWithoutWhere`；
- `Delete`：无有效 WHERE 且未 `Force()` → `ErrDeleteWithoutWhere`；Builder 带 JOIN 时无论是否有 WHERE/`Force()` 都返回 `ErrDeleteWithJoin`；
- `DeleteJoin`：没有任何 JOIN → `ErrDeleteJoinNoJoin`；无有效 WHERE/JOIN 且未 `Force()` → `ErrDeleteWithoutWhere`。

使用契约：

- 确需全表更新/删除时显式调用 `Force()`；`Force()` 只跳过「无有效 WHERE」的判定，不改变 SQL、不跳过 `Delete` 的 JOIN 检查。
- `WhereRaw("1 = 1")` 这类非空原始条件按「依赖行数据」计为有效限定，是刻意保留的逃生口；仍建议显式 `Force()`，便于代码审查与检索。
- **有效限定的具体判定**（WHERE 真值归约、空条件、EXISTS 的句法判定、哪些 JOIN 计入等）见 [safety](../concepts/safety.md)，本文不重复真值表。
- 保护判定发生在对应 `ToXxx` 编译之前，前置顺序为：`Increment`/`Decrement` 先解析参数（`ErrIncrementColumns`）；随后统一返回 Builder 图错误（`b.err`/`ErrCyclicQuery`）；再执行 `Delete` 的 JOIN 检查（`ErrDeleteWithJoin`）与 `DeleteJoin` 的无 JOIN 检查（`ErrDeleteJoinNoJoin`）；最后才是无有效限定的保护判定。因此 `ErrUpdateWithoutWhere`/`ErrDeleteWithoutWhere` 可能先于 `ErrEmptyTable`、`ErrNoFields`、`ErrWriteOrderLimitUnsupported` 返回。
- 注释不是条件，不能替代 `Force()`。

```go
func example() {
	// 全表更新（显式确认）
	affected, err := db.Builder().Table("users").Force().Update(ctx, struct {
		Status string `db:"status"`
	}{Status: "archived"})
	// SQL: UPDATE `users` SET `status` = ?
	_, _ = affected, err

	// 全表删除（显式确认）
	affected, err = db.Builder().Table("users").Force().Delete(ctx)
	// SQL: DELETE FROM `users`
	_, _ = affected, err
}
```

## 相关阅读

- 每个方法的纯编译产物、绑定顺序与注释位置：[builder-compile](../reference/builder-compile.md)
- 无 WHERE 保护的完整判定规则：[safety](../concepts/safety.md)
- 结构体字段映射与 nil 语义：[mapping](../concepts/mapping.md)
- 写连接选择、事务传播与读写分离：[execution-routing](../concepts/execution-routing.md)
- 三方言行为差异总览：[dialect-matrix](../reference/dialect-matrix.md)
- 错误哨兵清单：[errors](../reference/errors.md)
- UPSERT 的完整任务写法：[upsert](../how-to/upsert.md)
