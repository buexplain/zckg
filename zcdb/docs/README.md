# zcdb 数据库访问模块

## 概述

zcdb 是一个基于 `database/sql` 的数据库访问模块，核心是 **Builder 查询构造器**：

- **Builder 累积查询状态，Grammar 编译为 SQL**——同一套链式 API 生成 MySQL / PostgreSQL / SQLite 三种方言的 SQL；
- **主从连接池**：读操作按策略路由到从库，写操作与事务固定走主库，带锁查询强制走主库；
- **结构体自动映射**：按 `db` 标签（或 snake_case 字段名）在 SQL 列与结构体字段之间双向映射，标签名可在创建 DAO 时自定义；
- **破坏性操作保护**：无 WHERE 条件的 Update/Delete 默认拒绝执行，需显式 `Force()`。

zcdb 本身不依赖具体数据库驱动，需由使用方引入 `database/sql` 驱动并传入驱动名。

## 架构

```
                ┌──────────────────────────────┐
                │            DBDao             │  面向用户的唯一入口
                │  Builder() / Schema()        │  Exec/Query/Transaction
                └──────────────┬───────────────┘
                               │
              ┌────────────────┴───────────────┐
              ▼                                ▼
     ┌─────────────────┐             ┌──────────────────┐
     │     Builder     │  编译 SQL   │      Grammar     │
     │ 累积查询状态     │ ──────────► │  MySQLGrammar    │
     │ (链式调用)      │             │  PostgresGrammar │
     └─────────────────┘             │  SQLiteGrammar   │
              │                      └──────────────────┘
              │ 执行
              ▼
     ┌─────────────────┐    读 → PickReadDB（从库，按策略选择）
     │      Pool       │    写 → PickWriteDB（主库）
     │  主从连接池      │    事务 → master.BeginTx
     └─────────────────┘
```

## 方言差异总览

| 项目 | MySQL | PostgreSQL | SQLite |
|---|---|---|---|
| dialect 取值 | `mysql` | `postgresql` / `postgres` / `pgsql` | `sqlite` / `sqlite3` |
| 标识符包裹 | 反引号 `` `col` `` | 双引号 `"col"` | 双引号 `"col"` |
| 占位符 | `?` | `$1`、`$2`… | `?` |
| 共享锁 | `LOCK IN SHARE MODE` | `FOR SHARE` | 不支持（编译报错） |
| UNION + 锁 | 支持 | 不支持（编译报错） | 不支持锁 |
| RIGHT JOIN | 支持 | 支持 | 3.39 以下不支持 |
| CROSS JOIN ON | 直译 | 编译为 `INNER JOIN ... ON`（等价） | 直译 |
| UNION 子查询括号 | 各查询加括号包裹 | 各查询加括号包裹 | 不加括号（SQLite 不允许） |
| Truncate | `TRUNCATE TABLE` | `TRUNCATE TABLE ... RESTART IDENTITY` | `DELETE FROM` + 清 `sqlite_sequence` |
| 随机排序 | `RAND()` | `RANDOM()` | `RANDOM()` |

> 本文档的 SQL 示例统一以 **MySQL 形态**给出；PG/SQLite 仅按上表规则变化（包裹符与占位符），存在语义差异处会单独列出。

## 驱动依赖

zcdb 测试使用的驱动组合（可按需替换为任意 `database/sql` 驱动）：

| 数据库 | 驱动包 | 注册名（DriverName） |
|---|---|---|
| MySQL | `github.com/go-sql-driver/mysql` | `mysql` |
| PostgreSQL | `github.com/lib/pq` | `postgres` |
| SQLite | `modernc.org/sqlite`（纯 Go） | `sqlite` |

## 快速上手

```go
package main

import (
	"context"
	"fmt"

	"github.com/buexplain/zckg/zcdb"
	_ "github.com/go-sql-driver/mysql" // 注册 mysql 驱动
)

type User struct {
	Id   int64  `db:"id"`
	Name string `db:"name"`
	Age  int    `db:"age"`
}

func main() {
	// 1. 创建连接池（主库 + 可选从库，创建时即 Ping 验证）
	pool, err := zcdb.NewPool(zcdb.PoolConfig{
		DriverName: "mysql",
		DSN:        "user:pass@tcp(127.0.0.1:3306)/test?parseTime=true",
		SlaveDSNs:  []string{"user:pass@tcp(127.0.0.1:3307)/test?parseTime=true"},
	})
	if err != nil {
		panic(err)
	}

	// 2. 创建 DAO（dialect 决定 SQL 方言，最后一个参数为列映射标签名，传空串使用默认 db 标签）
	db, err := zcdb.NewDBDao(pool, "mysql", nil, "")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	ctx := context.Background()

	// 3. 查询
	var users []User
	err = db.Builder().Table("users").
		Where("age", ">", 18).
		OrderBy("id", "ASC").
		Limit(10).
		Find(ctx, &users)
	// SQL:  SELECT * FROM `users` WHERE `age` > ? ORDER BY `id` ASC LIMIT 10
	// args: [18]
	if err != nil {
		panic(err)
	}
	fmt.Println(users)

	// 4. 写入
	id, err := db.Builder().Table("users").InsertGetId(ctx, User{Name: "alice", Age: 25})
	// SQL:  INSERT INTO `users` (`id`, `name`, `age`) VALUES (?, ?, ?)
	// 说明：零值列也会参与插入；不想插入的列用指针/any 字段并置 nil
	if err != nil {
		panic(err)
	}
	fmt.Println(id)
}
```

## 文档索引

| 文档 | 内容 |
|---|---|
| [connection.md](connection.md) | 连接池、读写分离、事务、原始 SQL、慢 SQL 回调、自定义列映射标签 |
| [query-builder.md](query-builder.md) | SELECT 构造：表与列、WHERE、JOIN、分组、排序分页、UNION、锁、Clone |
| [query-exec.md](query-exec.md) | 查询执行：Find/First/Value/Count/Exists/聚合/Pluck/Paginate/游标迭代 |
| [mutate.md](mutate.md) | 写操作：Insert/Upsert/Update/Increment/Delete/DeleteJoin/Truncate 与安全防护 |
| [compile.md](compile.md) | ToXxx 纯编译系列与 Expression 原始表达式 |
| [schema.md](schema.md) | Schema 元数据查询（表列表、字段信息） |

## 错误变量速查

所有错误均为可导出的 `errors` 变量，可用 `errors.Is` 精确匹配：

| 变量 | 触发场景 |
|---|---|
| `ErrEmptyTable` | 未设置表名（Table/TableSub）就编译或执行 |
| `ErrInvalidStruct` | Insert/Update 传入了非结构体类型 |
| `ErrNoFields` | 结构体没有可导出字段 |
| `ErrEmptyData` | 插入数据为空 |
| `ErrInvalidOperator` | 运算符不在白名单内 |
| `ErrInvalidSubQuery` | WhereExists 等方法的子查询参数类型非法 |
| `ErrInvalidAggregate` | 聚合函数不是 MAX/MIN/SUM/AVG |
| `ErrPluckDest` / `ErrPluckColumns` | Pluck 目标类型或列数不匹配 |
| `ErrDeleteWithoutWhere` / `ErrUpdateWithoutWhere` | 无 WHERE 的 Delete/Update 被保护机制拒绝 |
| `ErrIncrementColumns` | Increment/Decrement 的多列参数不成对 |
| `ErrUpsertUniqueByRequired` | PG/SQLite 的 Upsert 缺少 uniqueBy |
| `ErrDeleteJoinNoJoin` | DeleteJoin 没有任何 JOIN |
| `ErrPgUnionLockNotSupported` | PostgreSQL 下 UNION + 锁 |
| `ErrSQLiteLockNotSupported` | SQLite 下使用锁子句 |
| `ErrNotPointer` / `ErrNotStruct` | 游标迭代目标不是结构体指针 |
| `ErrCursorFieldNotFound` | 游标列在目标结构体中找不到对应字段 |
| `ErrCursorFieldUnavailable` | 游标列字段不可用（nil 嵌入指针） |
| `ErrCursorColumnNull` | 游标列值为 NULL，无法继续分页 |
| `ErrDialectRequired` / `ErrUnknownDialect` / `ErrPoolRequired` | DAO 创建参数错误 |
