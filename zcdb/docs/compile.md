# ToXxx 编译系列与 Expression

Builder 的所有终端执行方法（Find/Insert/Update 等）内部都是"先 ToXxx 编译出 SQL，再交给 DAO 执行"。ToXxx 系列**只生成 SQL 与绑定参数、不执行**，适用于：

- 打印/记录即将执行的 SQL；
- 将 SQL 交给其它组件执行（如分批任务、自定义连接）；
- 单元测试中断言生成的 SQL。

所有 ToXxx 方法返回 `(sql string, args []any, err error)` 三元组（`ToTruncate` 只返回 `(sql, err)`）。Builder 状态在链式构造中已产生的错误（如非法运算符）会在编译时统一返回。

## 查询类编译

### ToSelect

编译完整 SELECT。未设置 Table/TableSub 时返回 `ErrEmptyTable`；SQLite 下使用锁子句报 `ErrSQLiteLockNotSupported`；PostgreSQL 下 UNION + 锁报 `ErrPgUnionLockNotSupported`。

```go
sql, args, _ := db.Builder().Table("users").Where("age", ">", 25).ToSelect()
// SQL:  SELECT * FROM `users` WHERE `age` > ?
// args: [25]
```

### ToCount

```go
sql, args, _ := db.Builder().Table("users").Where("status", "=", "active").ToCount()
// SQL:  SELECT COUNT(*) FROM `users` WHERE `status` = ?
// args: [active]
```

包含 UNION / GROUP BY / DISTINCT 时自动将整个查询包裹为子查询再计数（保留分组数/去重语义），并自动清除分页、排序、锁子句：

```go
sql, _, _ := db.Builder().Table("orders").GroupBy("user_id").ToCount()
// SQL: SELECT COUNT(*) FROM (SELECT 1 FROM `orders` GROUP BY `user_id`) AS `t`

sql, _, _ = db.Builder().Table("users").Select("city").Distinct().ToCount()
// SQL: SELECT COUNT(*) FROM (SELECT DISTINCT `city` FROM `users`) AS `t`
```

### ToExists

编译存在性查询 `SELECT 1 ... LIMIT 1`，清除分页/排序/锁/SELECT 子查询列；UNION 时整体包裹子查询后附加 `LIMIT 1`：

```go
sql, args, _ := db.Builder().Table("users").Where("id", "=", 1).ToExists()
// SQL:  SELECT 1 FROM `users` WHERE `id` = ? LIMIT 1
// args: [1]
```

### ToAggregate

编译 MAX/MIN/SUM/AVG 聚合，仅接受这四个函数名，否则返回 `ErrInvalidAggregate`。UNION 时整体包裹子查询后聚合；分页/排序/锁被自动清除：

```go
sql, args, _ := db.Builder().Table("orders").Where("status", "=", "paid").ToAggregate("MAX", "amount")
// SQL:  SELECT MAX(`amount`) AS `aggregate` FROM `orders` WHERE `status` = ?
// args: [paid]
```

## 插入类编译

### ToInsert

data 必须是结构体或结构体切片（可带指针），否则返回 `ErrInvalidStruct`；字段映射规则见[写操作文档](mutate.md)。

```go
sql, args, _ := db.Builder().Table("users").ToInsert([]User{
	{Name: "alice", Age: 25},
	{Name: "bob", Age: 30},
})
// SQL:  INSERT INTO `users` (`name`, `age`) VALUES (?, ?), (?, ?)
// args: [alice 25 bob 30]
```

### ToInsertOrIgnore

冲突时静默跳过，三方言形态：

```go
sql, args, _ := db.Builder().Table("users").ToInsertOrIgnore(user)
// MySQL:      INSERT IGNORE INTO `users` (`name`, `age`, `email`) VALUES (?, ?, ?)
// PostgreSQL: INSERT INTO "users" ("name", "age", "email") VALUES ($1, $2, $3) ON CONFLICT DO NOTHING
// SQLite:     INSERT OR IGNORE INTO "users" ("name", "age", "email") VALUES (?, ?, ?)
```

### ToUpsert

```go
sql, args, _ := db.Builder().Table("users").
	ToUpsert(user, []string{"email"}, []string{"name", "age"})
// MySQL: INSERT INTO `users` (`name`, `age`, `email`) VALUES (?, ?, ?)
//        ON DUPLICATE KEY UPDATE `name` = VALUES(`name`), `age` = VALUES(`age`)
// PG:    INSERT INTO "users" (...) VALUES (...) ON CONFLICT ("email") DO UPDATE SET "name" = EXCLUDED."name", "age" = EXCLUDED."age"
```

`updateColumns` 为空时默认更新所有插入列；PostgreSQL/SQLite 下 `uniqueBy` 为空返回 `ErrUpsertUniqueByRequired`。

### ToInsertUsing / ToInsertOrIgnoreUsing

编译 `INSERT INTO ... SELECT`，callback 内构造子查询；子查询未设置数据源或编译出错时整体返回错误：

```go
sql, args, _ := db.Builder().Table("users_archive").
	ToInsertUsing([]string{"name", "age"}, func(sub *zcdb.Builder) {
		sub.Table("users").Select("name", "age").Where("status", "=", "active")
	})
// SQL:  INSERT INTO `users_archive` (`name`, `age`) SELECT `name`, `age` FROM `users` WHERE `status` = ?
// args: [active]
```

## 更新与删除类编译

### ToUpdate

绑定参数顺序须与 SQL 占位符出现顺序一致，而 MySQL 与 PG/SQLite 的 UPDATE 语法中 SET 和 JOIN 的相对位置相反，框架通过 `grammar.UpdateSetBeforeJoin()` 自动区分：

| 方言 | SQL 结构 | 绑定顺序 |
|---|---|---|
| MySQL | `UPDATE ... JOIN(ON) ... SET ... WHERE` | JOIN → SET → WHERE |
| PostgreSQL / SQLite | `UPDATE ... SET ... FROM ... WHERE`（JOIN 条件并入 WHERE 前部） | SET → JOIN → WHERE |

```go
sql, args, _ := db.Builder().Table("users").Where("id", "=", 1).ToUpdate(UserUpdate{Name: &name, Age: &age})
// SQL:  UPDATE `users` SET `name` = ?, `age` = ? WHERE `id` = ?
// args: [alice_new 26 1]
```

### ToDelete / ToDeleteJoin / ToTruncate

```go
sql, args, _ := db.Builder().Table("users").Where("id", "=", 1).ToDelete()
// SQL:  DELETE FROM `users` WHERE `id` = ?
// args: [1]

sql, err := db.Builder().Table("users").ToTruncate()
// MySQL/PG: TRUNCATE TABLE `users`
// SQLite:   DELETE FROM "users"
```

`ToDeleteJoin` 要求至少一个 JOIN（否则 `ErrDeleteJoinNoJoin`），绑定顺序三方言一致：JOIN 条件 → WHERE 条件。

### ToIncrement / ToDecrement

接收等长的列名切片与增量切片，编译为 `SET col = col op ?`：

```go
sql, args, _ := db.Builder().Table("users").Where("id", "=", 1).
	ToIncrement([]string{"wallet", "level"}, []any{100, 1})
// SQL:  UPDATE `users` SET `wallet` = `wallet` + ?, `level` = `level` + ? WHERE `id` = ?
// args: [100 1 1]
```

## 绑定参数顺序规则

SELECT 类 SQL 的绑定参数按占位符出现顺序收集，固定为：

```
SELECT_SUB → FROM_SUB → JOIN → WHERE → GROUP BY → HAVING → UNION
```

每个 JOIN 内部又是：派生表子查询 → 嵌套 join 组 → ON 条件。构造多子查询组合的 SQL 时按此顺序排布绑定即可，无需手工管理。

`Expression` 类型的值一律直接内嵌进 SQL 文本，**不占用绑定位置**。

## Expression 原始表达式

`NewExpression` 创建原始 SQL 片段，是类型安全 API 的"逃逸口"，可用于所有接受值的位置（Where 比较值、Insert/Update 字段值、Having 等）：

```go
// 作为 Where 比较值：列等于另一列
sql, _, _ := db.Builder().Table("users").
	Where("id", "=", zcdb.NewExpression("parent_id")).ToSelect()
// SQL: SELECT * FROM `users` WHERE `id` = parent_id

// 作为 Insert/Update 字段值：使用数据库函数
sql, _, _ = db.Builder().Table("users").Where("id", "=", 1).
	ToUpdate(struct {
		UpdatedAt any `db:"updated_at"`
	}{UpdatedAt: zcdb.NewExpression("NOW()")})
// SQL: UPDATE `users` SET `updated_at` = NOW() WHERE `id` = ?
```

> Expression 内容**不会被参数化、不做标识符包裹、不做任何转义**，直接拼接进 SQL。请勿将用户输入拼入 Expression，防 SQL 注入。
