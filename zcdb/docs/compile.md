# ToXxx 编译系列与 Expression

Builder 的所有终端执行方法（Find/Insert/Update 等）内部都是"先 ToXxx 编译出 SQL，再交给 DAO 执行"。ToXxx 系列**只生成 SQL 与绑定参数、不执行**，适用于：

- 打印/记录即将执行的 SQL；
- 将 SQL 交给其它组件执行（如分批任务、自定义连接）；
- 单元测试中断言生成的 SQL。

所有 ToXxx 方法返回 `(sql string, args []any, err error)` 三元组（`ToTruncate` 只返回 `(sql, err)`）。Builder 本身及挂入 FROM/SELECT/WHERE/JOIN/HAVING/UNION 等位置的任意层子 Builder 在链式构造中已产生的错误（如非法运算符），都会在外层编译时统一返回；不会静默丢弃子查询错误后继续生成 SQL。

## SQL 注释的最终编译边界

全部 15 个公开编译入口（`ToSelect`、`ToInsert`、`ToInsertOrIgnore`、`ToUpsert`、`ToInsertUsing`、`ToInsertOrIgnoreUsing`、`ToUpdate`、`ToDelete`、`ToDeleteJoin`、`ToTruncate`、`ToCount`、`ToExists`、`ToAggregate`、`ToIncrement`、`ToDecrement`）均在成功生成完整 SQL 后，按当前 Builder 的注释追加一次 ` /* 短业务标识 */`。无注释时原 SQL 逐字节不变，本文原有输出均为无注释基线。

- 注释位于完整外层语句的最后：在 UNION 包装、别名、排序/分页、锁子句、冲突处理后缀及 PostgreSQL `RESTART IDENTITY` 之后；`ToCount` 的普通/UNION/GROUP BY/DISTINCT 四种分支、`ToExists` 与 `ToAggregate` 的普通/UNION 分支均如此。SQLite `ToDeleteJoin` 在主键 IN 包装之后追加。
- 结构化子查询（SELECT/FROM/WHERE/JOIN/ON/UNION/INSERT SELECT）忽略子 Builder 注释，仅外层决定注释；独立编译子 Builder 时仍保留其自身注释，不修改子查询状态。
- 不改变占位符、绑定参数顺序、编译后的状态恢复与原有错误；错误出口仍返回空 SQL/参数和原错误，不返回仅含注释的 SQL。重复编译不累积注释。
- 直接调用 Grammar（实例可由 `db.Grammar()` 取得）、DAO 原始 SQL 及 Schema 不自动添加注释。本功能不解析、去重或清理 Raw/Expression 中的手工注释；把已带注释的 SQL 作为 Raw 子查询不属于结构化子查询保证。

DAO 第五参数与 `Comment` 仅用于短业务标识，不得透传外部输入、请求体或秘密；控制字符（`unicode.IsControl`）、`*`、`/` 与 `\` 替换为空格、去首尾空白、按 255 rune 截断，属于有损规范化。覆盖、清空与 Clone 生命周期见[查询构造](query-builder.md)的 Comment 小节。

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

包含 UNION / GROUP BY / HAVING / DISTINCT 时自动将整个查询包裹为子查询再计数（保留分组、HAVING 筛选与去重语义），并自动清除分页、排序、锁子句。HAVING 引用显式 SELECT 别名时保留原投影：

```go
sql, _, _ := db.Builder().Table("orders").GroupBy("user_id").ToCount()
// SQL: SELECT COUNT(*) FROM (SELECT 1 FROM `orders` GROUP BY `user_id`) AS `t`

sql, _, _ = db.Builder().Table("users").Select("city").Distinct().ToCount()
// SQL: SELECT COUNT(*) FROM (SELECT DISTINCT `city` FROM `users`) AS `t`
```

### ToExists

编译存在性查询并清除分页、排序、锁。普通查询使用 `SELECT 1 ... LIMIT 1`；UNION 为保持各分支列数一致、HAVING 为保留 SELECT 别名，都会保留原投影并整体包裹子查询后附加 `LIMIT 1`：

```go
sql, args, _ := db.Builder().Table("users").Where("id", "=", 1).ToExists()
// SQL:  SELECT 1 FROM `users` WHERE `id` = ? LIMIT 1
// args: [1]
```

### ToAggregate

编译 MAX/MIN/SUM/AVG 聚合，仅接受这四个函数名，否则返回 `ErrInvalidAggregate`。UNION 时整体包裹子查询后聚合；分页/排序/锁被自动清除。带 GROUP BY 或 HAVING 时会产生多行分组语义，与标量返回值冲突，因此返回 `ErrAggregateGrouped`，应改用 `Select` + `Find` 接收各组结果：

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

`updateColumns` 为空时默认更新所有插入列（排除 uniqueBy 列；全部插入列均为 uniqueBy 时退化为「冲突时不更新」：PG/SQLite 编译为 `DO NOTHING`，MySQL 编译为自赋值 no-op）；PostgreSQL/SQLite 下 `uniqueBy` 为空返回 `ErrUpsertUniqueByRequired`。

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
// MySQL:  TRUNCATE TABLE `users`
// PG:     TRUNCATE TABLE "users" RESTART IDENTITY
// SQLite: DELETE FROM "users"
```

`ToDelete` 不编译 JOIN；Builder 带 JOIN 时返回 `ErrDeleteWithJoin`，即使同时有 WHERE 或调用了 `Force()` 也不会生成 SQL。关联删除应使用 `ToDeleteJoin`。

`ToDeleteJoin` 要求至少一个 JOIN（否则 `ErrDeleteJoinNoJoin`）。无派生表 JOIN 时三方言都是 ON → WHERE；带多个 `JoinSub` 时按实际 SQL 占位符顺序收集：

| 方言 | `ToDeleteJoin` 绑定顺序 |
|---|---|
| MySQL / SQLite | 按 JOIN 逐项：该 JOIN 的派生表子查询 → 嵌套 join → ON；最后 WHERE |
| PostgreSQL | 全部派生表子查询（JOIN 顺序）→ 全部 ON（JOIN 顺序）→ WHERE |

### ToIncrement / ToDecrement

接收等长且非空的列名切片与增量切片，编译为 `SET col = col op ?`；任一切片为空或长度不等时返回 `ErrIncrementColumns`：

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
>
> 占位符边界：请勿在 Expression 内容中使用 `?` 占位符——它没有任何配对的绑定参数：INSERT VALUES 与 Raw 子句中 `?` 会原样保留（PostgreSQL 方言下成为非法占位符），PostgreSQL 的 UPDATE SET 路径还会把 `?` 转为 `$N` 并占用占位符编号（仅框架内部自增/自减编译依赖该机制）。

## 原始 SQL 内联编译（CompileRaw）

需要把一条带占位符的原始 SQL 与参数合成**一条完整 SQL**（不含占位符、不需要参数）时用 `Grammar.CompileRaw`——典型场景是手工执行、日志留档、贴给 DBA 复核。占位符形态随方言而定：MySQL/SQLite 用 `?`，PostgreSQL 用 `$N`。Grammar 实例经 `db.Grammar()` 取得：

```go
// MySQL / SQLite：? 占位符
sqlStr, err := db.Grammar().CompileRaw(
	"UPDATE `users` SET `name` = ?, `updated_at` = ? WHERE `id` = ?",
	[]any{"张三", zcdb.NewExpression("NOW()"), 7})
// MySQL： UPDATE `users` SET `name` = '张三', `updated_at` = NOW() WHERE `id` = 7

// PostgreSQL：$N 占位符（`?` 在 PostgreSQL 里是 jsonb 键存在操作符等运算符，不参与占位符识别）
sqlStr, err = db.Grammar().CompileRaw(
	`UPDATE "users" SET "name" = $1, "updated_at" = $2 WHERE "id" = $3`,
	[]any{"张三", zcdb.NewExpression("NOW()"), 7})
// PostgreSQL： UPDATE "users" SET "name" = '张三', "updated_at" = NOW() WHERE "id" = 7
// 产物无需参数即可执行：db.Exec(ctx, sqlStr)
```

也可以直接吃 Builder 编译出来的产物（PostgreSQL 下是 `$N` + args，`CompileRaw` 会按编号取值内联，无需自己把 `$1` 改写成 `?`）：

```go
sqlStr, args, _ := db.Builder().Table("users").Where("id", "=", 7).ToSelect()
complete, err := db.Grammar().CompileRaw(sqlStr, args)
```

> **安全警示**：产物把参数值直接写成了 SQL 文本，等于放弃占位符绑定这道注入防线。绝不允许把不可信输入（用户提交、请求体、密钥）当作 args 传入；`Expression` 参数同样原样内嵌，安全责任归调用方。常规执行请继续走 Builder 或带参数的 `Exec`/`Query`。

值内联规则（`Expression` 原样内嵌）：

| 值类型 | MySQL | PostgreSQL | SQLite |
|---|---|---|---|
| string | 单引号包裹，单引号写成两个；反斜杠、换行、NUL、Ctrl-Z 按默认转义模式处理 | 单引号包裹，单引号写成两个（默认 `standard_conforming_strings=on`，反斜杠是普通字符） | 同 PostgreSQL |
| bool | `1` / `0` | `TRUE` / `FALSE` | `1` / `0` |
| 整数、浮点 | 裸写（最短表示；`NaN`/`Inf` 报错） | 同左 | 同左 |
| time.Time | `'2026-10-16 12:34:56.789'` | `'2026-10-16 12:34:56.789+08:00'` | `'2026-10-16 12:34:56.789'` |
| []byte | `X'00FF'` | `'\x00ff'::bytea` | `X'00FF'` |
| nil / nil 指针 / nil 字节切片 | `NULL` | `NULL` | `NULL` |
| driver.Valuer | 取 `Value()` 后按上表处理（含 `sql.NullString` 等） | 同左 | 同左 |

时间按值自身的挂钟时间格式化，**不做时区换算**；命名标量类型（如 `type Status int`）按底层类型处理；nil 指针（含类型化 nil）写 `NULL`，但**非 nil 指针不解引用**——即使该类型实现了 `driver.Valuer` 也报错，避免隐式解引用。结构体、map、非字节切片、chan、complex 等一律返回 `ErrRawValueNotInlinable`（不做 `fmt` 兜底，避免静默产出非法 SQL），`NaN`/`Inf` 与 Valuer 取值失败同样报错，错误消息携带参数位置。

其它边界：

- 占位符与参数按出现顺序一一对应，数量不一致返回 `ErrRawPlaceholderMismatch`（消息含两侧实际数量），失败时产物为空串；
- 占位符形态与方言自身一致：MySQL/SQLite 认 `?`、产物保持 `?`；PostgreSQL 认 `$N`——按编号取参数内联，编号须落在 `1..参数个数` 内且最大编号等于参数个数（同 `lib/pq` 与引擎的契约，允许重复引用同一编号、允许区间内未被引用的参数）。`?` 在 PostgreSQL 里是 jsonb 键存在操作符等运算符，故**一律按普通字符处理**、不参与占位符识别；
- **只识别代码区**：字符串字面量（`'...'`、MySQL 的 `"..."`）、引号标识符（`` `...` ``）、行注释（`--`，MySQL 另有 `#`）、块注释与 PostgreSQL 美元引用（`$$...$$` / `$tag$...$tag$`）里的 `?` / `$N` 都是字面内容、原样保留——`name LIKE '%?%'` 不会被改写。这条判定与引擎一致，由集成测试 [cross_dialect_integration_test.go](../cross_dialect_integration_test.go)（`crossDialectTestScanEngineParity`，三方言入口见 `cross_dialect_{mysql,postgres,sqlite}_integration_test.go`）逐构造比对「扫描器计数 vs 引擎要求参数个数」锁死。MySQL 字符串里的反斜杠按默认转义模式处理（`NO_BACKSLASH_ESCAPES` 未覆盖），PostgreSQL 的 `E'...'` 按转义字符串处理；未覆盖的语法（SQLite 的 `[...]` 标识符、MySQL 的 `/*! */` 可执行注释、PostgreSQL 的 `U&'...'`）见 sqlScan.go；
- MySQL/SQLite 的连续 `??` 按**两个占位符**计；注意裸 `??` 在引擎侧本就是语法错误（只有 PostgreSQL 的 `?` 是运算符），写成 `??` 属于误用形态——PostgreSQL 下 `?` 是普通字符，jsonb 键存在操作符直接写 `?` 即可；
- 不追加 DAO 默认注释；内联不产生 `$N` 占位符、也不触碰 Grammar 的编译期计数器，可并发调用。
