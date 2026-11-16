# 方言差异矩阵

zcdb 的 Builder 层不写死任何方言细节：标识符包裹、占位符、锁、UNION 括号、写语句能力、UPSERT 与冲突忽略形态都由 `Grammar` 决定（`MySQLGrammar` / `PostgresGrammar` / `SQLiteGrammar`）。这份文档把三方言的行为差异汇到一张表，作为查表入口；每行的机制解释见对应的 reference 文档。

表内 SQL 以 MySQL 形态为基线书写；PostgreSQL / SQLite 单元格只写形态差异。所有示例假定 DAO 第五参数为 `""`（无注释）。

## 方言取值

`NewDBDao(pool, dialect, ...)` 的 `dialect` 是字符串，按固定别名映射：

| dialect 取值 | Grammar |
|---|---|
| `mysql` | `*zcdb.MySQLGrammar` |
| `postgresql`、`postgres`、`pgsql` | `*zcdb.PostgresGrammar` |
| `sqlite`、`sqlite3` | `*zcdb.SQLiteGrammar` |

空串返回 `ErrDialectRequired`，其它值返回 `ErrUnknownDialect`（消息带出实际取值）。方言名大小写敏感，没有 `MySQL` 这样的别名。

## 总表

| 项目 | MySQL | PostgreSQL | SQLite | 关键说明 |
|---|---|---|---|---|
| 标识符包裹 | 反引号 `` `col` ``，内部反引号加倍 | 双引号 `"col"`，内部双引号加倍 | 双引号 `"col"` | 由 `WrapColumn`/`WrapTable` 实现；`*` 原样返回；含 `(` 的列视为原始表达式不包裹；`t.c` 拆成表与列分别包裹；`a AS b` 按别名处理 |
| 占位符 | `?` | `$1`、`$2`… | `?` | PostgreSQL 的编号在**一次编译内**全局递增，嵌套子查询与 UNION 分支共享同一个计数器；计数器位于编译克隆副本，不改变 DAO 持有的 Grammar |
| 排他锁 | `FOR UPDATE` | `FOR UPDATE` | 不支持，编译期返回 `ErrSQLiteLockNotSupported` | 带锁查询在执行时强制走主库（见 [execution-routing](../concepts/execution-routing.md)） |
| 共享锁 | `LOCK IN SHARE MODE` | `FOR SHARE` | 不支持，返回 `ErrSQLiteLockNotSupported` | `SharedLock()` 统一写入 `LOCK IN SHARE MODE`，PostgreSQL 编译时替换为 `FOR SHARE` |
| UNION + 锁 | 支持：锁子句位于整个 UNION 之后 | 不支持，返回 `ErrPgUnionLockNotSupported` | 不支持（任何锁都拒绝） | 该校验在 `ToSelect` 中；绕过它直接调 `Grammar.CompileSelect` 时，PostgreSQL 会生成带锁的 UNION（引擎报错），SQLite 会静默丢弃锁子句 |
| UNION 括号 | 每个分支加括号：`(SELECT ...) UNION (SELECT ...)` | 同 MySQL | **不加**括号：`SELECT ... UNION SELECT ...` | 主查询的 `ORDER BY`/`LIMIT`/`OFFSET`/锁输出在整个 UNION 之后；MySQL/PG 分支括号内的 `ORDER BY`/`LIMIT` 保留在分支内。SQLite 没有分支括号，分支上的子句会落到整条 UNION 尾部（语义泄漏），非末尾分支还会直接语法错误 |
| RIGHT JOIN | `RIGHT JOIN` | `RIGHT JOIN` | `RIGHT JOIN`（SQLite ≥ 3.39） | 框架不做版本检测：低于 3.39 由数据库报错 |
| CROSS JOIN ON | 直译 `CROSS JOIN ... ON ...` | 编译为语义等价的 `INNER JOIN ... ON ...` | 直译 `CROSS JOIN ... ON ...` | PostgreSQL 的 `CROSS JOIN` 不接受 `ON`；无条件 `CROSS JOIN` 三方言都直译 |
| TRUNCATE | `TRUNCATE TABLE t` | `TRUNCATE TABLE t RESTART IDENTITY` | `DELETE FROM t` | `ToTruncate` 只生成主语句；SQLite 的 `Truncate(ctx)` 另在同一事务内清理 `sqlite_sequence`（表存在时） |
| 随机排序 | `RAND()` | `RANDOM()` | `RANDOM()` | `InRandomOrder()` 只是追加一个 `ORDER BY <随机函数>`；它同样受上一行「写语句 ORDER BY/LIMIT」约束 |
| 写语句 ORDER BY/LIMIT | 单表 `UPDATE`/`DELETE`/`Increment`/`Decrement` 支持；多表 `UPDATE`（带 JOIN）与 `DeleteJoin` 拒绝 | 一律拒绝 | 一律拒绝 | 拒绝发生在编译期，返回 `ErrWriteOrderLimitUnsupported`；`Insert` 族与 `Truncate` 不读取这两个状态（设置后静默忽略）；`Offset` 不进入任何写语句。判定与逃逸口见 [safety](../concepts/safety.md) |
| UPSERT 形态 | `INSERT ... ON DUPLICATE KEY UPDATE col = VALUES(col)` | `INSERT ... ON CONFLICT (uniqueBy) DO UPDATE SET col = EXCLUDED.col` | 同 PostgreSQL（`excluded.col` 亦可） | PostgreSQL/SQLite 必须提供 `uniqueBy`，否则 `ErrUpsertUniqueByRequired`；MySQL 可省略。更新列全被 `uniqueBy` 覆盖时退化：MySQL 自赋值 no-op，PostgreSQL/SQLite `DO NOTHING` |
| INSERT IGNORE 语义 | `INSERT IGNORE INTO ...` | `INSERT ... ON CONFLICT DO NOTHING` | `INSERT OR IGNORE INTO ...` | 语法不同，**引擎语义也不同**：MySQL 把重复键与部分数据错误降级为警告（重复键跳过该行，截断/越界等非法值被调整为最接近值后仍插入）；PostgreSQL 只跳过唯一/排他约束冲突；SQLite 跳过违反 UNIQUE/NOT NULL/CHECK 的行（外键冲突仍报错）。zcdb 只生成语法，不归一化受影响行数 |
| DeleteJoin 形态 | `DELETE t FROM t JOIN ... WHERE ...`（多表 DELETE 直译） | `DELETE FROM t USING t2, ... WHERE join 条件 AND where 条件` | `DELETE FROM t WHERE "id" IN (SELECT t."id" FROM t JOIN ... WHERE ...)` | PostgreSQL 把嵌套 join 组、派生表全部展平进 `USING`，ON 条件并入 `WHERE`；SQLite 的子查询方案**固定使用主键列 `id`**，主键列不叫 `id` 的表不能直接用 `DeleteJoin` |
| 外连接括号 join 组 | 保留括号：`LEFT JOIN (t2 INNER JOIN t3 ON ...) ON ...` | 同 MySQL | 同 MySQL | 由 `JoinBuilder.JoinOn` / `CrossJoinOn` 产生的嵌套 join 组；但 PostgreSQL/SQLite 的 `UPDATE ... FROM` 会把 join 目标表展平成逗号列表、ON 条件并入 `WHERE`，括号组不保留（外连接语义随之改变，见下文） |
| 多表 UPDATE 形态 | `UPDATE t JOIN ... SET ... WHERE ...`（JOIN 在 `SET` 之前） | `UPDATE t SET ... FROM ... WHERE ...`（`SET` 在前） | 同 PostgreSQL | 绑定顺序随形态变化，由 `UpdateSetBeforeJoin` 区分：MySQL 是 JOIN→SET→WHERE，PostgreSQL/SQLite 是 SET→FROM→WHERE(ON 条件在前) |
| WhereDate | `date(col) = ?` | `col::date = $1` | `strftime('%Y-%m-%d', col) = ?` | 由 `CompileWhereDate` 生成；SQLite 的日期列必须是可被 `strftime` 解析的文本/时间 |
| LIKE 大小写 | 默认 `LIKE`（是否区分取决于排序规则）；`caseSensitive` 时 `BINARY col LIKE ?` | 默认 `ILIKE`（不区分）；`caseSensitive` 时 `LIKE` | 默认 `LIKE`；`caseSensitive` 时改用 **`GLOB`**（通配符是 `*`/`?`，调用方需自行转换） | `WhereNotLike` 在 PostgreSQL 下为 `NOT ILIKE`，与默认 `WhereLike` 对称 |
| 空安全相等 | `col <=> ?` | `col IS NOT DISTINCT FROM $1` | `col IS ?` | 不等形态：MySQL `NOT col <=> ?`、PostgreSQL `IS DISTINCT FROM`、SQLite `IS NOT ?` |
| InsertGetId | `LastInsertId()` | 不支持，返回 `ErrInsertGetIdUnsupported`（lib/pq 没有 `LastInsertId`） | `LastInsertId()` | 数据/参数错误优先于方言限制返回；PostgreSQL 需 `RETURNING` 时请用原始 SQL |
| 无 LIMIT 的 OFFSET | 自动补 `LIMIT 18446744073709551615` | 直接输出 `OFFSET n` | 自动补 `LIMIT -1` | MySQL/SQLite 的 `OFFSET` 必须与 `LIMIT` 同时出现，否则语法错误 |

## 表外要点

- **校验发生在 Builder 入口，不只在 Grammar**：SQLite 锁、PostgreSQL 的 UNION+锁、写语句 ORDER BY/LIMIT 都由 `ToSelect`/`ToUpdate`/`ToDelete`/`ToDeleteJoin`/`ToIncrement`/`ToDecrement` 前置拒绝。绕过 Builder 直接调 `Grammar.Compile*` 会失去这些保护（SQLite 直接编译时锁被静默丢弃）。
- **`UPDATE ... FROM` 的展平会改变外连接语义**：PostgreSQL/SQLite 把 `LEFT JOIN` 的表放进 `FROM`、把 ON 条件并入 `WHERE`，`WHERE` 对未匹配行（NULL 扩展行）的过滤等价于内连接。需要外连接语义的多表更新请改用原始 SQL，或先用子查询算出主键再 `WhereIn`。
- **删除与更新的方言能力差异**：SQLite 没有多表 DELETE、也没有 `UPDATE ... JOIN`；MySQL 没有 `UPDATE ... FROM`；PostgreSQL 没有多表 DELETE（用 `USING`）。这些差异已封装进 Grammar，但语义并非一一对应——尤其 SQLite 的 `DeleteJoin` 依赖 `id` 列。
- **绑定顺序是方言相关的**：同一组链式调用在 MySQL 与 PostgreSQL/SQLite 下 `args` 顺序可能不同（`UpdateSetBeforeJoin`）。纯编译对比 SQL 时不要只比文本，也要比 `args`，见 [compile-pipeline](../concepts/compile-pipeline.md)。
- **注释不改变方言差异**：DAO 默认注释与 `Builder.Comment` 只在最终 SQL 末尾追加，不影响各语法分支；Schema 查询与 DAO 原始 SQL 不追加注释（见 [safety](../concepts/safety.md)）。
- **按主题继续读**：列引用与 `Select` 族见 [builder-select](builder-select.md)；排序、分页、UNION、锁见 [builder-order-group](builder-order-group.md)；JOIN 与嵌套 join 组见 [builder-join](builder-join.md)；INSERT/UPSERT/UPDATE/DELETE/TRUNCATE 与写语句限制见 [builder-exec](builder-exec.md)；占位符与绑定顺序见 [builder-compile](builder-compile.md) 与 [compile-pipeline](../concepts/compile-pipeline.md)。

```go
func example() {
	// 同一套链式调用，切换 dialect 即切换产物形态
	mysqlDAO, err := zcdb.NewDBDao(pool, "mysql", nil, "", "")
	if err != nil {
		panic(err)
	}
	pgDAO, err := zcdb.NewDBDao(pool, "postgresql", nil, "", "")
	if err != nil {
		panic(err)
	}

	build := func(db *zcdb.DBDao) {
		sqlStr, args, err := db.Builder().Table("users").
			Join("orders", "orders.user_id", "=", "users.id").
			Where("orders.status", "=", "cancelled").
			ToDeleteJoin()
		_, _, _ = sqlStr, args, err
		// MySQL:  DELETE `users` FROM `users` INNER JOIN `orders` ...
		// PG:     DELETE FROM "users" USING "orders" WHERE ... AND ... = $1
	}
	build(mysqlDAO)
	build(pgDAO)
}
```

## 相关阅读

- 编译阶段与绑定顺序：[compile-pipeline](../concepts/compile-pipeline.md)
- 锁与查询形态：[builder-select](builder-select.md)、[builder-join](builder-join.md)
- 写语句与保护判定：[builder-exec](builder-exec.md)、[safety](../concepts/safety.md)
- 编译入口一览：[builder-compile](builder-compile.md)
- 错误与拒绝条件：[errors](errors.md)
- Schema 元数据的方言差异：[schema](schema.md)
