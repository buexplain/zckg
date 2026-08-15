# 写操作

本文介绍 Builder 的写入终端方法：Insert 系列、Upsert、Update、Increment/Decrement、Delete/DeleteJoin、Truncate，以及无 WHERE 条件的破坏性操作保护机制。

所有写操作固定走主库（或事务连接），返回受影响行数（`Truncate` 除外）。示例统一给出 MySQL 形态 SQL。

## Insert

插入数据并返回受影响行数。data 支持四种类型：结构体、结构体指针、结构体切片、结构体指针切片。

```go
affected, err := db.Builder().Table("users").Insert(ctx, User{Name: "alice", Age: 25})
// SQL:  INSERT INTO `users` (`name`, `age`) VALUES (?, ?)
// args: [alice 25]

// 批量插入（多行 VALUES）
affected, err = db.Builder().Table("users").Insert(ctx, []User{
	{Name: "alice", Age: 25},
	{Name: "bob", Age: 30},
})
// SQL:  INSERT INTO `users` (`name`, `age`) VALUES (?, ?), (?, ?)
// args: [alice 25 bob 30]
```

### 字段映射与值处理规则

- 字段通过 `db` 标签映射列名，无标签时字段名自动转 snake_case，`db:"-"` 的字段跳过；
- **any（interface{}）类型字段**：值为 nil 时该列被跳过（不参与 INSERT）；
- **指针类型字段**（`*string`、`*int` 等）：nil 时该列跳过；非 nil 自动解引用；
- **Expression 类型**：直接内嵌 SQL，不作为绑定参数；
- 批量插入以**首行为模板**确定列集合，后续行对应列若为 nil 则传入 SQL NULL。

```go
type User struct {
	Name  *string `db:"name"`
	Age   *int    `db:"age"`
	Email any     `db:"email"`
}

name, age := "alice", 25
affected, err := db.Builder().Table("users").Insert(ctx, User{Name: &name, Age: &age})
// Email 为 nil → 跳过 email 列
// SQL:  INSERT INTO `users` (`name`, `age`) VALUES (?, ?)
// args: [alice 25]

// Expression：插入时使用数据库函数
affected, err = db.Builder().Table("users").Insert(ctx, struct {
	Name      string       `db:"name"`
	CreatedAt any          `db:"created_at"`
}{"alice", zcdb.NewExpression("NOW()")})
// SQL: INSERT INTO `users` (`name`, `created_at`) VALUES (?, NOW())
```

## InsertGetId

插入并返回自增 ID（`LastInsertId`），**仅支持单个结构体/结构体指针**，不支持切片：

```go
id, err := db.Builder().Table("users").InsertGetId(ctx, User{Name: "alice", Age: 25})
// SQL: INSERT INTO `users` (`name`, `age`) VALUES (?, ?)
```

> 方言限制：**不支持 PostgreSQL**——lib/pq 驱动不支持 `LastInsertId`（PG 获取自增 ID 需 `RETURNING` 子句）。为避免「插入成功但返回错误」的半成功状态，PG 方言下 `InsertGetId` 在执行前直接返回错误，请改用 `Insert` 或带 `RETURNING` 的原始 SQL。

## InsertOrIgnore

插入时忽略唯一键冲突，返回实际插入的行数。三方言 SQL 形态不同：

```go
affected, err := db.Builder().Table("users").InsertOrIgnore(ctx, &user)
// MySQL:      INSERT IGNORE INTO `users` (`name`, `age`) VALUES (?, ?)
// PostgreSQL: INSERT INTO "users" ("name", "age") VALUES ($1, $2) ON CONFLICT DO NOTHING
// SQLite:     INSERT OR IGNORE INTO "users" ("name", "age") VALUES (?, ?)
```

data 的约束与字段处理规则与 Insert 完全一致。

## Upsert

插入或更新（冲突时更新），返回受影响行数：

- `uniqueBy`：唯一索引列名，用于判定冲突；
- `updateColumns`：冲突时要更新的列，为空时更新所有插入列（排除 uniqueBy 列）。

```go
affected, err := db.Builder().Table("users").
	Upsert(ctx, &user, []string{"email"}, []string{"name", "age"})
// MySQL SQL: INSERT INTO `users` (`name`, `age`, `email`) VALUES (?, ?, ?)
//            ON DUPLICATE KEY UPDATE `name` = VALUES(`name`), `age` = VALUES(`age`)
// PostgreSQL: INSERT INTO "users" (...) VALUES (...) ON CONFLICT ("email") DO UPDATE SET "name" = EXCLUDED."name", ...
// SQLite:     同 PostgreSQL 形态
```

> PostgreSQL / SQLite 下 `uniqueBy` 为空时报 `ErrUpsertUniqueByRequired`（无法生成 `ON CONFLICT` 目标）；MySQL 的 `ON DUPLICATE KEY UPDATE` 不需要冲突目标，可省略。

## InsertUsing / InsertOrIgnoreUsing

将 SELECT 子查询的结果插入目标表（`INSERT INTO ... SELECT`），columns 为目标表列名，callback 构造子查询：

```go
affected, err := db.Builder().Table("users_archive").
	InsertUsing(ctx, []string{"name", "age"}, func(sub *zcdb.Builder) {
		sub.Table("users").Select("name", "age").Where("status", "=", "active")
	})
// SQL:  INSERT INTO `users_archive` (`name`, `age`) SELECT `name`, `age` FROM `users` WHERE `status` = ?
// args: [active]
```

`InsertOrIgnoreUsing` 语义相同但冲突时静默跳过（方言差异同 InsertOrIgnore）：

```go
// MySQL SQL: INSERT IGNORE INTO `users_archive` (`name`, `age`) SELECT `name`, `age` FROM `users`
```

**列数校验**：子查询通过 `Select`/`AddSelect` 显式指定列且不含通配符（`*`）时，
`ToInsertUsing`/`ToInsertOrIgnoreUsing` 会在编译期校验目标列数与子查询列数一致，
不一致返回哨兵错误 `ErrInsertUsingColumnMismatch`；无法静态判定列数的场景
（如 `SELECT *` 或未显式指定列）不做编译期校验，列数一致性由数据库在运行时校验。

## Update

更新数据，data 必须是**单个结构体或结构体指针**（不支持切片）。字段映射与值处理规则同 Insert（nil 跳过、指针解引用、Expression 内嵌）。

```go
affected, err := db.Builder().Table("users").
	Where("id", "=", 1).
	Update(ctx, UserUpdate{Name: &name, Age: &age})
// SQL:  UPDATE `users` SET `name` = ?, `age` = ? WHERE `id` = ?
// args: [alice_new 26 1]
```

利用 Expression 实现基于当前值的更新：

```go
affected, err = db.Builder().Table("users").Where("id", "=", 1).
	Update(ctx, struct {
		Age any `db:"age"`
	}{Age: zcdb.NewExpression("`age` + 1")})
// SQL: UPDATE `users` SET `age` = `age` + 1 WHERE `id` = ?
```

> 带 JOIN 的 UPDATE 也可执行（MySQL 多表 UPDATE 直译；PG/SQLite 转为 FROM/子查询形态），JOIN 绑定与 SET 绑定的先后顺序由方言决定，框架内部已正确处理。

## Increment / Decrement

原子自增/自减指定列，避免"读-改-写"的并发问题：

```go
affected, err := db.Builder().Table("users").Where("id", "=", 1).Increment(ctx, "wallet", 100)
// SQL:  UPDATE `users` SET `wallet` = `wallet` + ? WHERE `id` = ?
// args: [100 1]

affected, err = db.Builder().Table("users").Where("id", "=", 1).Decrement(ctx, "wallet", 50)
// SQL:  UPDATE `users` SET `wallet` = `wallet` - ? WHERE `id` = ?
// args: [50 1]
```

extra 变参可交替传入更多"列名, 增量"对，一次更新多列：

```go
affected, err := db.Builder().Table("users").Where("id", "=", 1).
	Increment(ctx, "wallet", 100, "level", 1)
// SQL:  UPDATE `users` SET `wallet` = `wallet` + ?, `level` = `level` + ? WHERE `id` = ?
// args: [100 1 1]
```

extra 长度为奇数（不成对）或列名不是 string 时返回 `ErrIncrementColumns`。

## Delete / DeleteJoin

```go
affected, err := db.Builder().Table("users").Where("id", "=", 1).Delete(ctx)
// SQL:  DELETE FROM `users` WHERE `id` = ?
// args: [1]
```

`DeleteJoin` 按关联条件删除主表行，三方言实现路径不同：

```go
affected, err := db.Builder().Table("users").
	Join("orders", "orders.user_id", "=", "users.id").
	Where("orders.status", "=", "cancelled").
	DeleteJoin(ctx)
// MySQL:  DELETE `users` FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id` WHERE `orders`.`status` = ?
// PG:     DELETE FROM "users" USING "orders" WHERE "orders"."user_id" = "users"."id" AND "orders"."status" = $1
// SQLite: DELETE FROM "users" WHERE "id" IN (SELECT "users"."id" FROM "users" INNER JOIN "orders" ON ... WHERE "orders"."status" = ?)
// args: [cancelled]
```

没有任何 JOIN 时调用 `DeleteJoin` 返回 `ErrDeleteJoinNoJoin`。

> SQLite 方言限制：`DeleteJoin` 编译为主键 `IN` 子查询，主键列名硬编码为 `id`，主键非 `id` 的表请勿在 SQLite 下使用 DeleteJoin（改用 WhereInSub + Delete 组合）。

## Truncate

清空整表（不受无 WHERE 保护约束，本身就是全表语义）：

```go
err := db.Builder().Table("users").Truncate(ctx)
// MySQL/PG SQL: TRUNCATE TABLE `users`
// SQLite SQL:   DELETE FROM "users"
```

SQLite 特殊处理：`DELETE FROM` 不会重置 AUTOINCREMENT 序列，`Truncate` 会额外清空 `sqlite_sequence` 中该表的记录使自增主键从头开始（表从未使用 AUTOINCREMENT 时忽略该错误）。

## 破坏性操作保护

`Update`、`Increment`、`Decrement`、`Delete`、`DeleteJoin` 在**无有效限定条件**时默认拒绝执行，防止误操作全表：

| 操作 | 无 WHERE/JOIN 时的错误 |
|---|---|
| Update / Increment / Decrement | `ErrUpdateWithoutWhere` |
| Delete / DeleteJoin | `ErrDeleteWithoutWhere` |

"有效限定"的判定规则：

- 存在至少一个 WHERE 条件（空嵌套回调不算，防止绕过保护）；
- 或存在**带 ON 条件**的 JOIN（无条件 JOIN 产生笛卡尔积，不视为限定）。

确需全表更新/删除时显式调用 `Force()`：

```go
// 全表更新（显式确认）
affected, err := db.Builder().Table("users").Force().Update(ctx, UserUpdate{Status: "archived"})
// SQL: UPDATE `users` SET `status` = ?

// 全表删除（显式确认）
affected, err = db.Builder().Table("users").Force().Delete(ctx)
// SQL: DELETE FROM `users`
```

```go
// 错误处理示例
if _, err := db.Builder().Table("users").Delete(ctx); errors.Is(err, zcdb.ErrDeleteWithoutWhere) {
	// 被保护机制拒绝，补充 WHERE 条件或显式 Force()
}
```
