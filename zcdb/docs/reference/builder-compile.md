# 纯编译入口（Builder Compile）

`ToXxx` 系列是 Builder 的纯编译入口：只把 Builder 当前状态编译成 SQL 与绑定参数，不接触数据库。它适合三类场景：

- 打印/记录即将执行的 SQL；
- 把 SQL 交给其它组件执行（自定义连接、分批任务、审计）；
- 单元测试中断言生成的 SQL 与参数。

覆盖 15 个公开入口：`ToSelect`、`ToCount`、`ToExists`、`ToAggregate`、`ToInsert`、`ToInsertOrIgnore`、`ToUpsert`、`ToInsertUsing`、`ToInsertOrIgnoreUsing`、`ToUpdate`、`ToDelete`、`ToDeleteJoin`、`ToTruncate`、`ToIncrement`、`ToDecrement`。

执行型写方法如何调用这些入口、返回什么行数见 [builder-exec](../reference/builder-exec.md)；编译在整体链路中的位置见 [compile-pipeline](../concepts/compile-pipeline.md) 与 [architecture](../concepts/architecture.md)。

> 示例约定：Go 示例统一包在 `func example()` 中，`db` 是外部变量；SQL 以 MySQL 形态为基线，方言差异写在示例注释里。

## 入口与返回约定

```text
ToSelect() (string, []any, error)
ToCount() (string, []any, error)
ToExists() (string, []any, error)
ToAggregate(aggregate string, column string) (string, []any, error)
ToInsert(data any) (string, []any, error)
ToInsertOrIgnore(data any) (string, []any, error)
ToUpsert(data any, uniqueBy []string, updateColumns []string) (string, []any, error)
ToInsertUsing(columns []string, callback func(*Builder)) (string, []any, error)
ToInsertOrIgnoreUsing(columns []string, callback func(*Builder)) (string, []any, error)
ToUpdate(data any) (string, []any, error)
ToDelete() (string, []any, error)
ToDeleteJoin() (string, []any, error)
ToTruncate() (string, error)
ToIncrement(columns []string, amounts []any) (string, []any, error)
ToDecrement(columns []string, amounts []any) (string, []any, error)
```

- 除 `ToTruncate` 返回 `(sql, err)` 外，其余入口统一返回 `(sql string, args []any, err error)` 三元组。
- 纯编译不需要 DAO：`zcdb.NewBuilder(grammar, nil)` 即可编译，默认无注释；用 `db.Builder()` 构造时会把 DAO 默认注释复制进来，编译成功后追加（见下一节）。
- Builder 自身以及挂入 FROM/SELECT/WHERE/JOIN/HAVING/UNION 等位置的任意层子 Builder 在链式构造中已产生的错误（如非法运算符、环引用、子查询错误），都会在外层编译时统一返回；不会静默丢弃子查询错误后继续生成 SQL。
- 失败时统一返回空 SQL、`nil` 参数与原错误；不会返回「只编译了一半」的 SQL。
- 直接调用 Grammar（`db.Grammar()` 返回的实例）与调用 Builder 的 `ToXxx` 不同：前者不追加注释，也不做 Builder 层的数据映射与校验（例如 `Grammar.CompileInsert` 直接接收列与行）。
- 执行型方法先调用这些入口，再 `Exec`；因此同一个 Builder 状态在 `ToInsert` 与 `Insert` 下得到的 SQL、args 与编译期错误一致。

## 注释追加点

全部 15 个公开编译入口在成功生成完整 SQL 后，按当前 Builder 的注释追加一次 ` /* 短业务标识 */`；无注释时原 SQL 逐字节不变，本文所有输出均为无注释基线。

| 入口 | 注释位置 |
|---|---|
| `ToSelect` | 完整 SELECT 的最后：UNION 包装、ORDER BY/LIMIT/OFFSET、锁子句之后 |
| `ToCount` | 计数语句最后；普通分支在 `COUNT(*)` 语句末尾，UNION/GROUP BY/DISTINCT 分支在子查询别名 `AS "t"` 之后 |
| `ToExists` | 普通分支在 `LIMIT 1` 之后；UNION/HAVING 分支在 `AS "t" LIMIT 1` 之后 |
| `ToAggregate` | 聚合语句最后；UNION 分支在子查询别名 `AS "t"` 之后 |
| `ToInsert` / `ToInsertOrIgnore` | 多行 VALUES 之后 |
| `ToUpsert` | `ON DUPLICATE KEY UPDATE ...` 或 `ON CONFLICT ... DO ...` 之后 |
| `ToInsertUsing` / `ToInsertOrIgnoreUsing` | 子查询之后（PostgreSQL 的 `ON CONFLICT DO NOTHING` 之后） |
| `ToUpdate` | MySQL 的 ORDER BY/LIMIT 之后；PostgreSQL/SQLite 的 WHERE 之后 |
| `ToDelete` | MySQL 的 ORDER BY/LIMIT 之后；PostgreSQL/SQLite 的 WHERE 之后 |
| `ToDeleteJoin` | 完整 DELETE 之后；SQLite 在主键 `IN` 包装的 `)` 之后 |
| `ToTruncate` | 完整语句之后；PostgreSQL 在 `RESTART IDENTITY` 之后 |
| `ToIncrement` / `ToDecrement` | 与 `ToUpdate` 相同（MySQL 的 ORDER BY/LIMIT 之后） |

规则与边界：

- 结构化子查询（SELECT/FROM/WHERE/JOIN/ON/UNION/INSERT SELECT）忽略子 Builder 的注释，仅外层决定注释；独立编译子 Builder 时仍保留其自身注释，且不修改子查询状态。
- 不改变占位符、绑定参数顺序、编译后的临时状态恢复与原有错误；错误出口仍返回空 SQL/参数和原错误，不返回仅含注释的 SQL。
- 重复编译不累积注释：每次编译都在新字符串末尾追加一次，`Builder.comment` 本身不变。
- 直接调用 Grammar（实例可由 `db.Grammar()` 取得）、DAO 原始 SQL 及 Schema 查询不自动添加注释。本功能不解析、去重或清理 Raw/Expression 中的手工注释；把已带注释的 SQL 作为 Raw 子查询不属于结构化子查询保证。
- DAO 第五参数与 `Comment` 仅用于短业务标识，不得透传外部输入、请求体或密钥。控制字符（`unicode.IsControl`）、`*`、`/` 与 `\` 替换为空格、去首尾空白、按 255 rune 截断，属于有损规范化。覆盖、清空与 Clone 生命周期见 [builder-state](../concepts/builder-state.md)，安全边界见 [safety](../concepts/safety.md)。

## 查询类编译

### ToSelect

`ToSelect()` 编译完整 SELECT。未设置 `Table`/`TableSub` 时返回 `ErrEmptyTable`；SQLite 下使用锁子句报 `ErrSQLiteLockNotSupported`；PostgreSQL 下 `UNION + 锁` 报 `ErrPgUnionLockNotSupported`（MySQL 允许 UNION 带锁，锁子句置于 UNION 之后）。

```go
func example() {
	sqlStr, args, err := db.Builder().Table("users").Where("age", ">", 25).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `age` > ?
	// args: [25]
	_, _, _ = sqlStr, args, err
}
```

- 校验顺序：累积错误 → 数据源 → 子查询图环 → 锁/UNION 组合 → 编译 → 收集绑定 → 追加注释。
- 未显式指定列时默认 `SELECT *`；列、FROM 子查询、JOIN、WHERE、GROUP BY、HAVING、UNION、锁的编译细节见 [builder-select](../reference/builder-select.md)、[builder-where](../reference/builder-where.md)、[builder-join](../reference/builder-join.md)、[builder-order-group](../reference/builder-order-group.md)。

### ToCount

`ToCount()` 编译 COUNT 查询。普通查询直接替换投影为 `COUNT(*)`；包含 UNION / GROUP BY / HAVING / DISTINCT 时，自动将整个查询包裹为子查询再计数，以保留分组、HAVING 筛选与去重语义，并清除分页、排序、锁子句。

```go
func example() {
	sqlStr, args, err := db.Builder().Table("users").Where("status", "=", "active").ToCount()
	// SQL:  SELECT COUNT(*) FROM `users` WHERE `status` = ?
	// args: [active]
	_, _, _ = sqlStr, args, err

	sqlStr, _, err = db.Builder().Table("orders").GroupBy("user_id").ToCount()
	// SQL: SELECT COUNT(*) FROM (SELECT 1 FROM `orders` GROUP BY `user_id`) AS `t`

	sqlStr, _, err = db.Builder().Table("users").Select("city").Distinct().ToCount()
	// SQL: SELECT COUNT(*) FROM (SELECT DISTINCT `city` FROM `users`) AS `t`
	_, _, _ = sqlStr, args, err
}
```

四种分支：

| 分支 | 产物 |
|---|---|
| 普通 | `SELECT COUNT(*) FROM ...`（清空 SELECT 子查询投影） |
| UNION | 先编译完整 UNION，再 `SELECT COUNT(*) FROM (<union>) AS "t"` |
| GROUP BY / HAVING | 保留分组语义后包裹；有 HAVING 且调用方有显式投影（`columns` 或 `selectSubs`）时保留原投影，否则用常量 `SELECT 1` 避免 `SELECT *` 与 GROUP BY 在严格模式下冲突 |
| DISTINCT | 保留去重语义，`SELECT COUNT(*) FROM (SELECT DISTINCT ...) AS "t"` |

- 数据源校验允许「只有 UNION、没有 Table」的查询（UNION 分支自己提供 FROM）。
- 编译期通过 `saveTransientState` 临时清除 `limit`/`offset`/`orders`/`lockClause`，并按分支替换 `columns`/`selectSubs`；`defer` 恢复全部状态，即使编译中途 panic 也不改变调用方 Builder。
- 错误：`ErrEmptyTable`、`ErrCyclicQuery`、子 Builder 的构造期错误。

### ToExists

`ToExists()` 编译存在性查询并清除分页、排序、锁。普通查询使用 `SELECT 1 ... LIMIT 1`；UNION 为保持各分支列数一致、HAVING 为保留 SELECT 别名，都会保留原投影并整体包裹子查询后附加 `LIMIT 1`。

```go
func example() {
	sqlStr, args, err := db.Builder().Table("users").Where("id", "=", 1).ToExists()
	// SQL:  SELECT 1 FROM `users` WHERE `id` = ? LIMIT 1
	// args: [1]
	_, _, _ = sqlStr, args, err
}
```

- 普通分支：投影替换为 `1`，编译后追加 ` LIMIT 1`。
- `UNION` 或存在 HAVING 时：保留原投影编译，再 `SELECT 1 FROM (<sub>) AS "t" LIMIT 1`。
- 数据源校验同样允许只有 UNION 的查询。
- 编译期临时状态与 `ToCount` 相同（清除后恢复）。

### ToAggregate

`ToAggregate(aggregate, column)` 编译 MAX/MIN/SUM/AVG 聚合，仅接受这四个函数名（大小写敏感），否则返回 `ErrInvalidAggregate`；带 GROUP BY 或 HAVING 时会产生多行分组语义，与标量返回值冲突，返回 `ErrAggregateGrouped`，应改用 `Select` + `Find` 接收各组结果。

```go
func example() {
	sqlStr, args, err := db.Builder().Table("orders").Where("status", "=", "paid").
		ToAggregate("MAX", "amount")
	// SQL:  SELECT MAX(`amount`) AS `aggregate` FROM `orders` WHERE `status` = ?
	// args: [paid]
	_, _, _ = sqlStr, args, err
}
```

- 产物形态：`SELECT AGG(<wrapped column>) AS <wrapped "aggregate"> ...`。
- UNION 时整体包裹子查询后聚合（`SELECT AGG(col) AS "aggregate" FROM (<union>) AS "t"`）；分页/排序/锁被清除并恢复。
- 错误顺序：累积错误 → `ErrEmptyTable` → `ErrCyclicQuery` → `ErrInvalidAggregate` → `ErrAggregateGrouped`。COUNT 不在白名单，计数请用 `ToCount`。

## 插入类编译

### ToInsert

`ToInsert(data)` 编译 INSERT 语句。data 必须是结构体、结构体指针、结构体切片或结构体指针切片，否则返回 `ErrInvalidStruct`；空切片返回 `ErrEmptyData`；没有可用字段或字段全为 nil 返回 `ErrNoFields`。字段映射规则见 [mapping](../concepts/mapping.md) 与 [builder-exec](../reference/builder-exec.md)。

```go
func example() {
	type User struct {
		Name string `db:"name"`
		Age  int    `db:"age"`
	}

	sqlStr, args, err := db.Builder().Table("users").ToInsert([]User{
		{Name: "alice", Age: 25},
		{Name: "bob", Age: 30},
	})
	// SQL:  INSERT INTO `users` (`name`, `age`) VALUES (?, ?), (?, ?)
	// args: [alice 25 bob 30]
	_, _, _ = sqlStr, args, err
}
```

- SQL 形态：MySQL ``INSERT INTO `table` (...) VALUES ...``；PostgreSQL `INSERT INTO "table" (...) VALUES ($1, $2), ($3, $4)`；SQLite 与 MySQL 同为 `?`，标识符用双引号。
- args 是按行展开的扁平值：第 1 行各列 → 第 2 行各列；`Expression` 已内联，不作为绑定参数。
- 错误：`ErrEmptyTable`、`ErrInvalidStruct`、`ErrEmptyData`、`ErrNoFields`。

### ToInsertOrIgnore

`ToInsertOrIgnore(data)` 编译忽略冲突的 INSERT。data 的约束和字段处理规则与 `ToInsert` 完全一致，仅冲突处理不同：

| 方言 | SQL 形态 |
|---|---|
| MySQL | ``INSERT IGNORE INTO `table` (...) VALUES ...`` |
| PostgreSQL | `INSERT INTO "table" (...) VALUES ... ON CONFLICT DO NOTHING` |
| SQLite | `INSERT OR IGNORE INTO "table" (...) VALUES ...` |

```go
func example() {
	type User struct {
		Name  string `db:"name"`
		Age   int    `db:"age"`
		Email string `db:"email"`
	}

	sqlStr, args, err := db.Builder().Table("users").
		ToInsertOrIgnore(User{Name: "alice", Age: 25, Email: "a@t.com"})
	// MySQL:      INSERT IGNORE INTO `users` (`name`, `age`, `email`) VALUES (?, ?, ?)
	// PostgreSQL: INSERT INTO "users" ("name", "age", "email") VALUES ($1, $2, $3) ON CONFLICT DO NOTHING
	// SQLite:     INSERT OR IGNORE INTO "users" ("name", "age", "email") VALUES (?, ?, ?)
	// args: [alice 25 a@t.com]
	_, _, _ = sqlStr, args, err
}
```

三方言的忽略范围差异（MySQL 截断写入、SQLite 跳过其它约束违例、PostgreSQL 严格限于冲突）见 [builder-exec](../reference/builder-exec.md) 的 `InsertOrIgnore` 小节。

### ToUpsert

`ToUpsert(data, uniqueBy, updateColumns)` 编译 UPSERT。data 约束与字段处理规则同 `ToInsert`。

- `uniqueBy`：PostgreSQL/SQLite 的 `ON CONFLICT` 冲突目标；MySQL 的 `ON DUPLICATE KEY UPDATE` 不需要冲突目标，`uniqueBy` 只参与默认更新列的推导（含退化形态的自赋值列选择）。
- `updateColumns` 为空时默认更新所有插入列（排除 `uniqueBy` 列）；排除后仍为空（全部插入列都是 `uniqueBy`）时退化为「冲突时不更新」：PG/SQLite 编译为 `DO NOTHING`，MySQL 编译为单列自赋值 no-op。
- PostgreSQL/SQLite 下 `uniqueBy` 为空返回 `ErrUpsertUniqueByRequired`；MySQL 可省略。

```go
func example() {
	type User struct {
		Name  string `db:"name"`
		Age   int    `db:"age"`
		Email string `db:"email"`
	}

	user := User{Name: "alice", Age: 25, Email: "a@t.com"}

	sqlStr, args, err := db.Builder().Table("users").
		ToUpsert(user, []string{"email"}, []string{"name", "age"})
	// MySQL:      INSERT INTO `users` (`name`, `age`, `email`) VALUES (?, ?, ?)
	//             ON DUPLICATE KEY UPDATE `name` = VALUES(`name`), `age` = VALUES(`age`)
	// PostgreSQL: INSERT INTO "users" ("name", "age", "email") VALUES ($1, $2, $3)
	//             ON CONFLICT ("email") DO UPDATE SET "name" = EXCLUDED."name", "age" = EXCLUDED."age"
	// SQLite:     同 PostgreSQL 形态，占位符为 ?
	_, _, _ = sqlStr, args, err

	onlyEmail := struct {
		Email string `db:"email"`
	}{Email: "a@t.com"}

	sqlStr, _, err = db.Builder().Table("users").ToUpsert(onlyEmail, []string{"email"}, nil)
	// PostgreSQL: INSERT INTO "users" ("email") VALUES ($1) ON CONFLICT ("email") DO NOTHING
	// MySQL:      INSERT INTO `users` (`email`) VALUES (?) ON DUPLICATE KEY UPDATE `email` = VALUES(`email`)
	_, _, _ = sqlStr, args, err
}
```

- args：与 INSERT 一样按行展开；`Expression` 内联，不占绑定。
- 错误：data 错误（`ErrEmptyTable`/`ErrInvalidStruct`/`ErrEmptyData`/`ErrNoFields`）、`ErrUpsertUniqueByRequired`。

### ToInsertUsing

`ToInsertUsing(columns, callback)` 编译 `INSERT INTO ... SELECT`。`columns` 是目标表列名，`callback` 构建 SELECT 子查询。

- 外层 Builder 只提供目标表、注释与累积错误；外层 WHERE/JOIN/ORDER BY/LIMIT 不参与编译，子查询状态全部来自 callback。
- 子查询未设置数据源返回 `ErrEmptyTable`；子查询自身错误、环引用整体返回。
- 列数校验：子查询显式指定列（`Select`/`AddSelect`/`SelectRaw`/`SelectSub`）且所有列值不含 `*` 时，目标列数必须与子查询列数一致，否则返回 `ErrInsertUsingColumnMismatch`。`SELECT *`、`t.*`、`COUNT(*)`、未显式指定列等无法静态判定的场景不做编译期校验，交给数据库运行时校验。单条 `SelectRaw` 只按 1 列计数，`SelectRaw("a, b")` 会被当作 1 列而可能误报；需要精确校验时用 `Select`/`AddSelect` 逐列书写。

```go
func example() {
	sqlStr, args, err := db.Builder().Table("users_archive").
		ToInsertUsing([]string{"name", "age"}, func(sub *zcdb.Builder) {
			sub.Table("users").Select("name", "age").Where("status", "=", "active")
		})
	// MySQL:      INSERT INTO `users_archive` (`name`, `age`) SELECT `name`, `age` FROM `users` WHERE `status` = ?
	// PostgreSQL: INSERT INTO "users_archive" ("name", "age") SELECT "name", "age" FROM "users" WHERE "status" = $1
	// SQLite:     INSERT INTO "users_archive" ("name", "age") SELECT "name", "age" FROM "users" WHERE "status" = ?
	// args: [active]
	_, _, _ = sqlStr, args, err
}
```

- args：子查询的绑定参数，按 SELECT 类的收集顺序（见「绑定参数顺序」）。
- `callback` 为 nil 会在调用处直接 panic（未做 nil 校验）。
- 错误：`ErrEmptyTable`（外层无表名或子查询无数据源）、子查询构造期错误、`ErrCyclicQuery`、`ErrInsertUsingColumnMismatch`。

### ToInsertOrIgnoreUsing

`ToInsertOrIgnoreUsing(columns, callback)` 与 `ToInsertUsing` 的参数、校验规则相同，仅冲突处理不同：

| 方言 | SQL 形态 |
|---|---|
| MySQL | `INSERT IGNORE INTO ... SELECT ...` |
| PostgreSQL | `INSERT INTO ... SELECT ... ON CONFLICT DO NOTHING` |
| SQLite | `INSERT OR IGNORE INTO ... SELECT ...` |

```go
func example() {
	sqlStr, args, err := db.Builder().Table("users_archive").
		ToInsertOrIgnoreUsing([]string{"name", "age"}, func(sub *zcdb.Builder) {
			sub.Table("users").Select("name", "age")
		})
	// MySQL:      INSERT IGNORE INTO `users_archive` (`name`, `age`) SELECT `name`, `age` FROM `users`
	// PostgreSQL: INSERT INTO "users_archive" ("name", "age") SELECT "name", "age" FROM "users" ON CONFLICT DO NOTHING
	// SQLite:     INSERT OR IGNORE INTO "users_archive" ("name", "age") SELECT "name", "age" FROM "users"
	_, _, _ = sqlStr, args, err
}
```

## 更新与删除类编译

### ToUpdate

`ToUpdate(data)` 编译 UPDATE。data 必须是结构体或结构体指针（不支持切片），否则 `ErrInvalidStruct`；没有可用字段或全部被 nil 跳过返回 `ErrNoFields`。字段值处理规则同 INSERT。

校验顺序：累积错误 → 表名 → 子查询图环 → **写语句 ORDER BY/LIMIT 方言校验** → 提取 data → 编译 → 收集绑定 → 追加注释。因此携带不支持的 ORDER BY/LIMIT 时，`ErrWriteOrderLimitUnsupported` 先于 `ErrInvalidStruct`/`ErrNoFields` 返回。

```go
func example() {
	type UserUpdate struct {
		Name *string `db:"name"`
		Age  *int    `db:"age"`
	}

	name, age := "alice_new", 26
	sqlStr, args, err := db.Builder().Table("users").Where("id", "=", 1).
		ToUpdate(UserUpdate{Name: &name, Age: &age})
	// SQL:  UPDATE `users` SET `name` = ?, `age` = ? WHERE `id` = ?
	// args: [alice_new 26 1]
	_, _, _ = sqlStr, args, err
}
```

绑定参数顺序须与 SQL 占位符出现顺序一致，而 MySQL 与 PG/SQLite 的 UPDATE 语法中 SET 和 JOIN 的相对位置相反，框架通过 `Grammar.UpdateSetBeforeJoin()` 自动区分：

| 方言 | SQL 结构 | 绑定顺序 |
|---|---|---|
| MySQL | `UPDATE ... JOIN(ON) ... SET ... WHERE` | JOIN → SET → WHERE |
| PostgreSQL / SQLite | `UPDATE ... SET ... FROM ... WHERE`（JOIN 条件并入 WHERE 前部） | SET → FROM 派生表 → ON → WHERE |

写操作携带 `OrderBy`/`Limit` 时按方言校验：MySQL 仅单表支持，PostgreSQL/SQLite 与 MySQL 多表 UPDATE/DeleteJoin 在编译期返回 `ErrWriteOrderLimitUnsupported`，详见 [builder-exec](../reference/builder-exec.md) 的「ORDER BY / LIMIT 的支持范围」。

### ToDelete

`ToDelete()` 编译 DELETE。普通 DELETE 不编译 JOIN：Builder 带 JOIN 时返回 `ErrDeleteWithJoin`，即使同时有 WHERE 或调用了 `Force()` 也不会生成 SQL；关联删除应用 `ToDeleteJoin`。

校验顺序：累积错误 → 表名 → 子查询图环 → JOIN 检查 → 写语句 ORDER BY/LIMIT 校验 → 编译 → 收集 WHERE 绑定 → 追加注释。因此带 JOIN 且带 LIMIT 时先得到 `ErrDeleteWithJoin`。

```go
func example() {
	sqlStr, args, err := db.Builder().Table("users").Where("id", "=", 1).ToDelete()
	// SQL:  DELETE FROM `users` WHERE `id` = ?
	// args: [1]
	_, _, _ = sqlStr, args, err
}
```

- MySQL 单表 DELETE 支持 `ORDER BY`/`LIMIT`；PostgreSQL/SQLite 没有该语法，携带时返回 `ErrWriteOrderLimitUnsupported`。
- 错误：`ErrEmptyTable`、`ErrCyclicQuery`、`ErrDeleteWithJoin`、`ErrWriteOrderLimitUnsupported`。

### ToDeleteJoin

`ToDeleteJoin()` 编译按关联条件删除主表行的 DELETE，要求至少一个 JOIN（否则 `ErrDeleteJoinNoJoin`）。三方言实现路径不同：

| 方言 | SQL 形态 |
|---|---|
| MySQL | 多表 DELETE 直译：``DELETE `t` FROM `t` INNER JOIN ... ON ... WHERE ...`` |
| PostgreSQL | `DELETE FROM "t" USING ... WHERE <ON 条件> AND <WHERE 条件>` |
| SQLite | `DELETE FROM "t" WHERE "id" IN (SELECT "t"."id" FROM "t" INNER JOIN ... WHERE ...)` |

```go
func example() {
	sqlStr, args, err := db.Builder().Table("users").
		Join("orders", "orders.user_id", "=", "users.id").
		Where("orders.status", "=", "cancelled").
		ToDeleteJoin()
	// MySQL:      DELETE `users` FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id` WHERE `orders`.`status` = ?
	// PostgreSQL: DELETE FROM "users" USING "orders" WHERE "orders"."user_id" = "users"."id" AND "orders"."status" = $1
	// SQLite:     DELETE FROM "users" WHERE "id" IN (SELECT "users"."id" FROM "users" INNER JOIN "orders" ON ... WHERE "orders"."status" = ?)
	// args: [cancelled]
	_, _, _ = sqlStr, args, err
}
```

绑定顺序：

| 方言 | `ToDeleteJoin` 绑定顺序 |
|---|---|
| MySQL / SQLite | 按 JOIN 逐项：该 JOIN 的派生表子查询 → 嵌套 join 组 → ON；最后 WHERE |
| PostgreSQL | 全部派生表子查询（JOIN 顺序）→ 全部 ON（嵌套 join 组条件在前、自身条件在后）→ WHERE |

> SQLite 限制：主键列名硬编码为 `id`；主键非 `id` 的表请改用 `WhereInSub` + `Delete`。

- 校验顺序：累积错误 → 表名 → 子查询图环 → 无 JOIN 检查 → 写语句 ORDER BY/LIMIT 校验 → 编译。
- 错误：`ErrEmptyTable`、`ErrCyclicQuery`、`ErrDeleteJoinNoJoin`、`ErrWriteOrderLimitUnsupported`。

### ToTruncate

`ToTruncate()` 编译清空表语句，返回 `(sql, err)` 两元组。它只检查累积错误与表名，不编译 WHERE/JOIN，也不校验 ORDER BY/LIMIT。

```go
func example() {
	sqlStr, err := db.Builder().Table("users").ToTruncate()
	// MySQL:      TRUNCATE TABLE `users`
	// PostgreSQL: TRUNCATE TABLE "users" RESTART IDENTITY
	// SQLite:     DELETE FROM "users"
	_, _ = sqlStr, err
}
```

- 错误：`ErrEmptyTable` 与累积错误。
- 执行 `Truncate` 时 SQLite 还会在同一事务里做 `sqlite_sequence` 清理；这些辅助语句不经过 `ToTruncate`，也不追加注释，详见 [builder-exec](../reference/builder-exec.md)。

### ToIncrement / ToDecrement

`ToIncrement(columns, amounts)` / `ToDecrement(columns, amounts)` 编译原子自增/自减 UPDATE：`SET col = col + ?`（或 `- ?`）。`columns` 与 `amounts` 必须等长且非空，否则返回 `ErrIncrementColumns`。

```go
func example() {
	sqlStr, args, err := db.Builder().Table("users").Where("id", "=", 1).
		ToIncrement([]string{"wallet", "level"}, []any{100, 1})
	// SQL:  UPDATE `users` SET `wallet` = `wallet` + ?, `level` = `level` + ? WHERE `id` = ?
	// args: [100 1 1]
	_, _, _ = sqlStr, args, err
}
```

- 实现上把每个 `col op ?` 构造成 `Expression`，再复用 UPDATE 编译路径；PostgreSQL 会把其中的 `?` 转成 `$N`，MySQL/SQLite 保持 `?`。
- 校验顺序：累积错误 → 表名 → 子查询图环 → 列/增量校验 → 写语句 ORDER BY/LIMIT 校验 → 编译。`ErrIncrementColumns` 先于 `ErrWriteOrderLimitUnsupported`。
- 带 JOIN 时与 `ToUpdate` 相同，按 `UpdateSetBeforeJoin()` 决定 SET 与 JOIN 的绑定顺序。

## 绑定参数顺序

绑定参数按 **SQL 中占位符出现的逻辑顺序** 收集，而不是按调用链顺序。

### SELECT 类

```text
SELECT_SUB → FROM_SUB → JOIN → WHERE → GROUP BY → HAVING → UNION
```

每个 JOIN 内部又是：

```text
派生表子查询 → 嵌套 join 组 → ON 条件
```

构造多子查询组合的 SQL 时按此顺序排布绑定即可，无需手工管理。

### UPDATE 类（方言不同）

| 方言 | 绑定顺序 |
|---|---|
| MySQL | JOIN（逐 JOIN：派生表 → 嵌套组 → ON）→ SET → WHERE |
| PostgreSQL / SQLite | SET → FROM 派生表（展平顺序）→ ON（嵌套组在前、自身在后）→ WHERE |

`ToIncrement`/`ToDecrement` 复用同一编译路径，顺序相同（amounts 占 SET 位置）。

### DELETE JOIN

| 方言 | 绑定顺序 |
|---|---|
| MySQL / SQLite | 见 `ToDeleteJoin` 的绑定顺序表 |
| PostgreSQL | 全部派生表子查询（JOIN 顺序）→ 全部 ON（嵌套组在前、自身在后）→ WHERE |

### INSERT 族

- `ToInsert`/`ToInsertOrIgnore`/`ToUpsert`：按行展开 VALUES，行内按列顺序；`Expression` 内联，不占绑定位置。
- `ToInsertUsing`/`ToInsertOrIgnoreUsing`：绑定全部来自子查询，顺序与 `ToSelect` 相同（SQL 中 `SELECT` 子句在目标列之后出现）。

### 占位符契约

在调用方遵守 `Expression` 边界（内容不含 `?`）的前提下：

- MySQL/SQLite：SQL 代码区的 `?` 数量等于 `len(args)`；
- PostgreSQL：`$1`…`$N` 连续递增，最大编号等于 `len(args)`。

这个不变量由三方言单元测试逐构造断言（`assertCompilerPlaceholderInvariant`）。若在 PostgreSQL 的 UPDATE SET / JOIN ON 值里塞入含 `?` 的自定义 `Expression`，框架会把 `?` 转成 `$N` 但不增加配对参数，导致后续编号与 args 错位——这正是下一节要求「不要在 Expression 里写 `?`」的原因。

## Expression

`Expression` 表示一个原始 SQL 表达式，不会被参数化、不会做标识符包裹、也不会转义：

```text
type Expression struct { /* 未导出字段 value string */ }
func NewExpression(value string) Expression
func (e Expression) Value() string
```

它是类型安全 API 的「逃逸口」。`Expression` 是值类型，`Value()` 返回原始字符串。

### 适用位置

直接内联（不占绑定位置）的位置：

- `Where`/`OrWhere` 的值比较；
- Builder 级 `WhereIn`/`WhereNotIn` 的元素；
- `WhereLike`/`WhereNotLike`、NULL 安全比较（`WhereNullSafeEquals`/`WhereNullSafeNotEquals`）；
- `Having` 的基本比较；
- `Insert`/`Update`/`Upsert` 的字段值（`any` 或 `Expression` 类型字段）；
- Raw 绑定：`WhereRaw`、`JoinBuilder.Raw`、`HavingRaw`、`GroupByRaw` 的绑定参数；
- `Grammar.CompileRaw` 的 args。

**不支持 Expression**（会被当作普通绑定参数，执行期由驱动拒绝，如 `sql: converting argument $1 type: unsupported type zcdb.Expression`）：

- `WhereBetween`/`WhereNotBetween`/`HavingBetween`/`HavingNotBetween` 的 min/max；
- `JoinBuilder.WhereIn`/`WhereNotIn` 的列表元素。

两列区间请用 `WhereBetweenColumns`/`WhereNotBetweenColumns` 或 `WhereRaw`。

### 占位符边界

**请勿在 `Expression` 内容中使用 `?` 占位符**——它没有配对的绑定参数，按编译路径分两类：

- **原样保留**（`?` 直接进入 SQL 文本，PostgreSQL 下成为非法占位符）：INSERT / INSERT IGNORE / UPSERT 的 VALUES，WHERE / HAVING 的值比较，WHERE IN，LIKE，NULL 安全比较，以及 Raw 绑定（WhereRaw / JoinBuilder.Raw / HavingRaw / GroupByRaw）与 `CompileRaw` 的 Expression 参数；
- **转成 `$N` 并占用占位符编号**（仅 PostgreSQL）：UPDATE SET 值、JOIN ON 值比较——只有框架内部的自增/自减编译依赖该机制并另行配对绑定，用户自带的 Expression 仍无配对参数。

MySQL / SQLite 没有 `$N` 转换，上述两类路径都把 `?` 原样保留。

补充两点：

- PostgreSQL 的 `?`→`$N` 转换路径（Raw 绑定、UPDATE SET 值、JOIN ON 值比较）会把 `??` 转义为字面 `?`（jsonb 键存在操作符），不消耗编号；直接内嵌 Expression 的路径不做这个转义。
- 在 PostgreSQL 的 UPDATE SET / JOIN ON 里使用含 `?` 的 Expression 会让后续 `$N` 与 args 错位，不只是「一个非法占位符」；不要在 Expression 中写 `?`。

> **安全警示**：Expression 内容不做参数化、不做标识符包裹、不做任何转义，直接拼接进 SQL。请勿将用户输入拼入 Expression，防 SQL 注入。用户输入应走普通绑定参数。

```go
func example() {
	// 作为 Where 比较值：列等于另一列
	sqlStr, args, err := db.Builder().Table("users").
		Where("id", "=", zcdb.NewExpression("parent_id")).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `id` = parent_id
	// args: []
	_, _, _ = sqlStr, args, err

	// 作为 Update 字段值：使用数据库函数
	sqlStr, args, err = db.Builder().Table("users").Where("id", "=", 1).
		ToUpdate(struct {
			UpdatedAt any `db:"updated_at"`
		}{UpdatedAt: zcdb.NewExpression("NOW()")})
	// SQL:  UPDATE `users` SET `updated_at` = NOW() WHERE `id` = ?
	// args: [1]
	_, _, _ = sqlStr, args, err
}
```

## Grammar.CompileRaw

需要把一条带占位符的原始 SQL 与参数合成**一条完整 SQL**（不含占位符、不需要参数）时用 `Grammar.CompileRaw`——典型场景是手工执行、日志留档、贴给 DBA 复核。占位符形态随方言而定：MySQL/SQLite 用 `?`，PostgreSQL 用 `$N`。Grammar 实例经 `db.Grammar()` 取得：

```text
CompileRaw(sqlStr string, args []any) (string, error)
```

### 占位符规则

- MySQL/SQLite 认 `?`，产物中的占位符全部被内联为字面量；PostgreSQL 只认 `$N`——`?` 在 PostgreSQL 里是 jsonb 键存在操作符等运算符，故**一律按普通字符处理**、不参与占位符识别。
- MySQL/SQLite：占位符与参数按出现顺序一一对应；数量不一致返回 `ErrRawPlaceholderMismatch`。
- PostgreSQL：按编号取参数内联，编号须落在 `1..参数个数` 内且最大编号等于参数个数（同 `lib/pq` 与引擎的契约，允许重复引用同一编号、允许区间内未被引用的参数）；`$0`、越界、最大编号不足，以及「SQL 不含 `$N` 但传了参数」都返回 `ErrRawPlaceholderMismatch`。
- 数量/编号不一致时产物为空串，调用方不会误用半成品 SQL；错误消息携带两侧实际数量或具体编号。

### 值内联规则

`Expression` 参数原样内嵌；其余值的规则：

| 值类型 | MySQL | PostgreSQL | SQLite |
|---|---|---|---|
| string | 单引号包裹，单引号写成两个；反斜杠、换行、NUL、Ctrl-Z 按默认转义模式处理 | 单引号包裹，单引号写成两个（默认 `standard_conforming_strings=on`，反斜杠是普通字符） | 同 PostgreSQL |
| bool | `1` / `0` | `TRUE` / `FALSE` | `1` / `0` |
| 整数、浮点 | 裸写（最短表示；`NaN`/`Inf` 报错） | 同左 | 同左 |
| time.Time | `'2026-10-16 12:34:56.789'` | `'2026-10-16 12:34:56.789+08:00'` | `'2026-10-16 12:34:56.789'` |
| []byte | `X'00FF'` | `'\x00ff'::bytea` | `X'00FF'` |
| nil / nil 指针 / nil 字节切片 | `NULL` | `NULL` | `NULL` |
| driver.Valuer | 取 `Value()` 后按上表处理（含 `sql.NullString` 等） | 同左 | 同左 |

时间按值自身的挂钟时间格式化，**不做时区换算**；命名标量类型（如 `type Status int`）按底层类型处理；nil 指针（含类型化 nil）写 `NULL`，但**非 nil 指针不解引用**——即使该类型实现了 `driver.Valuer` 也报错，避免隐式解引用。结构体、map、非字节切片、chan、complex 等一律返回 `ErrRawValueNotInlinable`（不做 `fmt` 兜底，避免静默产出非法 SQL），`NaN`/`Inf` 与 Valuer 取值失败同样报错，错误消息携带参数位置（从 1 起）。

### 只识别代码区

占位符识别与数据库引擎的解析一致，只认代码区：

- 字符串字面量（`'...'`、MySQL 的 `"..."`）、引号标识符（`` `...` ``、PostgreSQL/SQLite 的 `"..."`）、行注释（`--`，MySQL 另有 `#`）、块注释（PostgreSQL 支持嵌套）与 PostgreSQL 美元引用（`$$...$$` / `$tag$...$tag$`）里的 `?` / `$N` 都是字面内容、原样保留——`name LIKE '%?%'` 不会被改写。
- MySQL 字符串里的反斜杠按默认转义模式处理（`NO_BACKSLASH_ESCAPES` 未覆盖），PostgreSQL 的 `E'...'` 按转义字符串处理。
- 已知未覆盖的语法（这些位置会被当作代码区处理）：SQLite 的 `[...]` 标识符、MySQL 的 `/*! ... */` 可执行注释、PostgreSQL 的 `U&'...'` / `U&"..."`。
- MySQL/SQLite 的连续 `??` 按**两个占位符**计；裸 `??` 在引擎侧本就是语法错误（只有 PostgreSQL 的 `?` 是运算符），写成 `??` 属于误用形态——PostgreSQL 下 `?` 是普通字符，jsonb 键存在操作符直接写 `?` 即可。编号模式（PostgreSQL）不做 `??` 转义。

该判定由扫描器 `sqlScan.go` 实现，并由三方言集成测试（`crossDialectTestScanEngineParity` 及 `cross_dialect_{mysql,postgres,sqlite}_integration_test.go` 入口）逐构造比对「扫描器计数 vs 引擎要求参数个数」锁死。

### 其它边界

- 不追加 DAO 默认注释；内联不产生 `$N` 占位符、也不触碰 Grammar 的编译期计数器，可并发调用。
- 产物是单条自足 SQL，可直接交给 `db.Exec(ctx, sqlStr)` 或其它组件。
- 也可以直接吃 Builder 编译出来的产物（PostgreSQL 下是 `$N` + args，`CompileRaw` 会按编号取值内联，无需自己把 `$1` 改写成 `?`）。

```go
func example() {
	// MySQL / SQLite：? 占位符
	sqlStr, err := db.Grammar().CompileRaw(
		"UPDATE `users` SET `name` = ?, `updated_at` = ? WHERE `id` = ?",
		[]any{"张三", zcdb.NewExpression("NOW()"), 7})
	// MySQL： UPDATE `users` SET `name` = '张三', `updated_at` = NOW() WHERE `id` = 7
	_, _ = sqlStr, err

	// PostgreSQL：$N 占位符
	sqlStr, err = db.Grammar().CompileRaw(
		`UPDATE "users" SET "name" = $1, "updated_at" = $2 WHERE "id" = $3`,
		[]any{"张三", zcdb.NewExpression("NOW()"), 7})
	// PostgreSQL： UPDATE "users" SET "name" = '张三', "updated_at" = NOW() WHERE "id" = 7
	_, _ = sqlStr, err
}
```

```go
func example() {
	// 直接消费 Builder 产物：PostgreSQL 的 $N 会自动按编号取值
	sqlStr, args, err := db.Builder().Table("users").Where("id", "=", 7).ToSelect()
	complete, compileErr := db.Grammar().CompileRaw(sqlStr, args)
	// complete 不再含占位符，可直接 db.Exec(ctx, complete)
	_, _, _, _, _ = sqlStr, args, err, complete, compileErr
}
```

> **安全警示**：产物把参数值直接写成了 SQL 文本，等于放弃占位符绑定这道注入防线。绝不允许把不可信输入（用户提交、请求体、密钥）当作 args 传入；`Expression` 参数同样原样内嵌，安全责任归调用方。常规执行请继续走 Builder 或带参数的 `Exec`/`Query`。更多安全边界见 [safety](../concepts/safety.md)。

## 相关阅读

- 执行型写方法如何使用这些编译产物：[builder-exec](../reference/builder-exec.md)
- 编译顺序、临时状态与注释追加的机制：[compile-pipeline](../concepts/compile-pipeline.md)
- Builder 状态与 Clone：[builder-state](../concepts/builder-state.md)
- 查询类终端方法与结果扫描：[builder-query](../reference/builder-query.md)、[terminal-methods](../reference/terminal-methods.md)
- 三方言差异总览：[dialect-matrix](../reference/dialect-matrix.md)
- 错误哨兵清单：[errors](../reference/errors.md)
- Expression 与原始 SQL 的安全边界：[safety](../concepts/safety.md)
